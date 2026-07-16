// Package gormrepo 用真实 GORM(MySQL) 实现 wallet.WalletRepo（user_balances + agent_redemption_codes 两表）。
//
// 关键不变量（detailed-design §6.2）：
//   - ChargeBalance 用条件 UPDATE（WHERE balance_usd >= cost）在 DB 层保证「零透支」，
//     0 行受影响即余额不足 -> ErrQuotaInsufficient；新余额在同一事务内回读。
//   - UseRedemption / RedeemCode 用 CAS（WHERE status='enabled'）原子翻牌，RowsAffected 决定 ok，杜绝重复兑换。
//   - AddBalance 用 upsert（ON DUPLICATE KEY UPDATE balance_usd=balance_usd+?）原子累加。
//
// 兑换码改造为「原生 quota 口径」（P1-UI-04）：代理建码经 CreateCodesWithDeduction 在同一事务内
// 从代理 owner 原生 users.quota **条件扣减**（不足拒 ErrInsufficientQuota）+ 批量建码；用户兑换经
// RedeemCode 单赢家 CAS 返回面额，由 mtwire 调原生 IncreaseUserQuota 入账（本仓储不碰 new-api model）。
//
// 表名 agent_redemption_codes（租户维度）：刻意区别于 new-api 原生 redemptions 表，避免撞名。
//
// 金额列用 decimal(20,8) 精确存储；接口仍以 float64 进出（driver 扫描兼容）。
package gormrepo

import (
	"context"
	"errors"
	"math"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/QuantumNous/new-api/internal/wallet"
)

// balanceRow 是 user_balances 表的 GORM 模型，复合主键 (tenant_id, user_id) 即多租户隔离根。
type balanceRow struct {
	TenantID   int64     `gorm:"column:tenant_id;primaryKey"`
	UserID     int64     `gorm:"column:user_id;primaryKey"`
	BalanceUSD float64   `gorm:"column:balance_usd;type:decimal(20,8);not null;default:0"`
	CreatedAt  time.Time `gorm:"column:created_at"`
	UpdatedAt  time.Time `gorm:"column:updated_at"`
}

// TableName 固定表名。
func (balanceRow) TableName() string { return "user_balances" }

// redemptionRow 是 agent_redemption_codes 表的 GORM 模型。(tenant_id, code) 唯一（租户内码唯一）。
// ExpireAt/UsedAt 用 *time.Time：nil=NULL，规避 MySQL 零值日期('0000-00-00')写入报错。
type redemptionRow struct {
	ID           int64      `gorm:"column:id;primaryKey;autoIncrement"`
	TenantID     int64      `gorm:"column:tenant_id;not null;uniqueIndex:idx_redemption_tenant_code,priority:1"`
	Code         string     `gorm:"column:code;type:varchar(64);not null;uniqueIndex:idx_redemption_tenant_code,priority:2"`
	AmountUSD    float64    `gorm:"column:amount_usd;type:decimal(20,8);not null"`
	Status       string     `gorm:"column:status;type:varchar(16);not null;default:enabled"`
	ExpireAt     *time.Time `gorm:"column:expire_at"`
	UsedByUserID int64      `gorm:"column:used_by_user_id;not null;default:0"`
	UsedAt       *time.Time `gorm:"column:used_at"`
	CreatedAt    time.Time  `gorm:"column:created_at"`
}

// TableName 固定表名（租户维度兑换码，区别于 new-api 原生 redemptions 表）。
func (redemptionRow) TableName() string { return "agent_redemption_codes" }

// Repo 是 wallet.WalletRepo 的 GORM 实现，并附带 seed 辅助方法。
type Repo struct {
	db  *gorm.DB
	now func() time.Time
}

// 编译期断言：*Repo 满足 wallet.WalletRepo 契约。
var _ wallet.WalletRepo = (*Repo)(nil)

// New 用已建立连接的 *gorm.DB 构造仓储。
func New(db *gorm.DB) *Repo { return &Repo{db: db, now: time.Now} }

// AutoMigrate 建/补 user_balances 与 redemption_codes 表结构。
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&balanceRow{}, &redemptionRow{})
}

// Balance 返回用户当前余额（USD）；账户不存在视为 0。
func (r *Repo) Balance(ctx context.Context, tenantID, userID int64) (float64, error) {
	var row balanceRow
	err := r.db.WithContext(ctx).Take(&row, "tenant_id = ? AND user_id = ?", tenantID, userID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return row.BalanceUSD, nil
}

// AddBalance 入账：余额 += deltaUSD（账户不存在则创建）。upsert 原子累加。
func (r *Repo) AddBalance(ctx context.Context, tenantID, userID int64, deltaUSD float64) error {
	row := balanceRow{TenantID: tenantID, UserID: userID, BalanceUSD: deltaUSD}
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "tenant_id"}, {Name: "user_id"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"balance_usd": gorm.Expr("balance_usd + ?", deltaUSD),
			}),
		}).
		Create(&row).Error
}

// ChargeBalance 条件原子扣减：仅当 balance-cost>=0 才扣减，并在同一事务回读新余额。
// 0 行受影响（不足/账户不存在）-> 返回当前余额 + ErrQuotaInsufficient，余额不变。
func (r *Repo) ChargeBalance(ctx context.Context, tenantID, userID int64, costUSD float64) (float64, error) {
	var remaining float64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&balanceRow{}).
			Where("tenant_id = ? AND user_id = ? AND balance_usd >= ?", tenantID, userID, costUSD).
			UpdateColumn("balance_usd", gorm.Expr("balance_usd - ?", costUSD))
		if res.Error != nil {
			return res.Error
		}
		// 回读余额（扣减成功取扣后值；不足取当前值，账户不存在取 0）。
		var row balanceRow
		if e := tx.Take(&row, "tenant_id = ? AND user_id = ?", tenantID, userID).Error; e != nil {
			if !errors.Is(e, gorm.ErrRecordNotFound) {
				return e
			}
		}
		remaining = row.BalanceUSD
		if res.RowsAffected == 0 {
			return wallet.ErrQuotaInsufficient
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, wallet.ErrQuotaInsufficient) {
			return remaining, err
		}
		return 0, err
	}
	return remaining, nil
}

// GetRedemption 按租户+码查兑换码；不存在返回 ErrRedeemCodeInvalid。
func (r *Repo) GetRedemption(ctx context.Context, tenantID int64, code string) (*wallet.RedemptionCode, error) {
	var row redemptionRow
	err := r.db.WithContext(ctx).Take(&row, "tenant_id = ? AND code = ?", tenantID, code).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, wallet.ErrRedeemCodeInvalid
		}
		return nil, err
	}
	return toRedemption(&row), nil
}

// UseRedemption 原子 CAS：仅当 status 仍为 enabled 才翻为 used 并记录使用者/时间。
// RowsAffected==1 -> ok=true；==0 -> 已被并发用掉/禁用，ok=false。
func (r *Repo) UseRedemption(ctx context.Context, id, userID int64, now time.Time) (bool, error) {
	res := r.db.WithContext(ctx).Model(&redemptionRow{}).
		Where("id = ? AND status = ?", id, string(wallet.RedemptionEnabled)).
		Updates(map[string]interface{}{
			"status":          string(wallet.RedemptionUsed),
			"used_by_user_id": userID,
			"used_at":         now,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// EnsureBalance 幂等地置初始余额（账户已存在则不动）。供 seed。
func (r *Repo) EnsureBalance(ctx context.Context, tenantID, userID int64, usd float64) error {
	row := balanceRow{TenantID: tenantID, UserID: userID, BalanceUSD: usd}
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&row).Error
}

// EnsureRedemption 幂等地建一张兑换码（按 tenant_id+code 去重）。供 seed。
func (r *Repo) EnsureRedemption(ctx context.Context, c *wallet.RedemptionCode) error {
	row := redemptionRow{
		TenantID:  c.TenantID,
		Code:      c.Code,
		AmountUSD: c.AmountUSD,
		Status:    string(statusOrDefault(c.Status)),
	}
	if !c.ExpireAt.IsZero() {
		t := c.ExpireAt
		row.ExpireAt = &t
	}
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&row).Error
}

// ---- 原生 quota 口径兑换码（P1-UI-04 代理自助分销） ----

// CreateCodesWithDeduction 在单一事务内：① 从代理 owner 原生 users.quota **条件扣减**
// totalQuotaUnits（WHERE id=? AND quota>=?，0 行受影响即不足 -> ErrInsufficientQuota，不透支）；
// ② 批量插入 codes（回填 ID/TenantID/CreatedAt）。扣减与建码原子化：任一失败整体回滚。
//
// 仅以 raw Table("users") 触原生表（不 import new-api model，避免 upstream 耦合）；额度单位换算
// （amount_usd × QuotaPerUnit）与缓存失效由 mtwire 负责。
func (r *Repo) CreateCodesWithDeduction(ctx context.Context, tenantID, ownerUserID int64, totalQuotaUnits int64, codes []*wallet.RedemptionCode) error {
	if totalQuotaUnits <= 0 || len(codes) == 0 {
		return wallet.ErrAmountInvalid
	}
	now := r.now()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Table("users").
			Where("id = ? AND quota >= ?", ownerUserID, totalQuotaUnits).
			UpdateColumn("quota", gorm.Expr("quota - ?", totalQuotaUnits))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return wallet.ErrInsufficientQuota // 余额不足 / owner 不存在：拒绝且不动库
		}
		rows := make([]redemptionRow, len(codes))
		for i, c := range codes {
			rows[i] = redemptionRow{
				TenantID:  tenantID,
				Code:      c.Code,
				AmountUSD: c.AmountUSD,
				Status:    string(statusOrDefault(c.Status)),
				CreatedAt: now,
			}
			if !c.ExpireAt.IsZero() {
				t := c.ExpireAt
				rows[i].ExpireAt = &t
			}
		}
		if err := tx.Create(&rows).Error; err != nil {
			return err
		}
		for i := range rows {
			codes[i].ID = rows[i].ID
			codes[i].TenantID = tenantID
			codes[i].Status = wallet.RedemptionStatus(rows[i].Status)
			codes[i].CreatedAt = rows[i].CreatedAt
		}
		return nil
	})
}

// redeemCAS 在给定 db 句柄（可为 r.db 或事务 tx）上执行用户兑换的核心：查码（按 tenant+code）→
// 状态机校验 → 单赢家 CAS（WHERE status='enabled' AND tenant_id=?）翻 used 并记录使用者，返回面额。
// 抽为共享内核，供 RedeemCode（仅 CAS）与 RedeemCodeAndCredit（CAS+入账同事务）复用，逻辑不分叉。
//
//	不存在/禁用/过期            -> wallet.ErrRedeemCodeInvalid
//	已用（含并发竞态败者）       -> wallet.ErrRedeemCodeUsed
//
// CAS 是单赢家闸门：并发兑换同一码仅一人 RowsAffected==1。跨租户：tenant 不匹配则查码即 invalid（越权防线）。
func redeemCAS(db *gorm.DB, tenantID int64, code string, userID int64, now time.Time) (float64, error) {
	var row redemptionRow
	err := db.Take(&row, "tenant_id = ? AND code = ?", tenantID, code).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, wallet.ErrRedeemCodeInvalid
		}
		return 0, err
	}
	if e := toRedemption(&row).RedeemableError(now); e != nil {
		return 0, e // used / invalid（含过期、禁用）—— 快速失败，省一次 CAS
	}
	res := db.Model(&redemptionRow{}).
		Where("id = ? AND tenant_id = ? AND status = ?", row.ID, tenantID, string(wallet.RedemptionEnabled)).
		Updates(map[string]interface{}{
			"status":          string(wallet.RedemptionUsed),
			"used_by_user_id": userID,
			"used_at":         now,
		})
	if res.Error != nil {
		return 0, res.Error
	}
	if res.RowsAffected == 0 {
		return 0, wallet.ErrRedeemCodeUsed // 并发竞态败者
	}
	return row.AmountUSD, nil
}

// RedeemCode 仅执行单赢家 CAS 翻 used 并返回面额（不碰 quota）——保留给纯状态机路径与并发测试。
// 生产兑换入账请用 RedeemCodeAndCredit（CAS + 原生 quota 入账在同一事务内原子完成）。
func (r *Repo) RedeemCode(ctx context.Context, tenantID int64, code string, userID int64, now time.Time) (float64, error) {
	return redeemCAS(r.db.WithContext(ctx), tenantID, code, userID, now)
}

// RedeemCodeAndCredit 在**单一事务**内原子完成用户兑换：① 单赢家 CAS 翻 used（redeemCAS）；
// ② 同事务把面额折算成原生 quota 记入 users.quota（WHERE id=? 加 quota+credit）。任一失败整体回滚——
// 从根上消除「CAS 已翻 used 但入账另起事务失败 → 码永久作废、额度不到账、无对账兜底」的跨事务窗口，
// 对齐充值 OnPaid / CreateCodesWithDeduction 的同事务标准。
//
// quotaPerUnit 由 mtwire 传入（$1 = quotaPerUnit），使本仓储保持与 new-api model 解耦；换算沿用
// int64(amount_usd × quotaPerUnit)，入账数额与旧「两步」路径逐位一致。额度缓存失效由调用方在
// 事务提交成功后调 InvalidateUserCache 负责（DB 为真相源，缓存永不与 DB 发散）。
//
// 返回：面额 amount_usd 与实际入账 quota 单位数 creditUnits（供调用方回执/日志）。
func (r *Repo) RedeemCodeAndCredit(ctx context.Context, tenantID int64, code string, userID int64, now time.Time, quotaPerUnit float64) (amountUSD float64, creditUnits int64, err error) {
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		amt, e := redeemCAS(tx, tenantID, code, userID, now)
		if e != nil {
			return e // REDEEM_CODE_INVALID / REDEEM_CODE_USED —— 回滚（CAS 本就无副作用）
		}
		// 入账前的溢出兜底（前瞻性纵深防御，利用链最后一环）：拒绝会让 int64(amt×quotaPerUnit)
		// 溢出/回绕的天价面额。当前 amount_usd 列为 decimal(20,8)（整数位≤12 → 面额≤~1e12，
		// perCode≤~5e17 < int64 max），加上建码侧 maxRedemptionAmountUSD 上界，此路本不可达；保留它
		// 是为「crediting 绝不搬运无法忠实表示的数额」这一不变量兜底——防日后放宽列宽/上界或出现
		// 未受约束的旁路建码。amt≤0 交由下方 credit>0 短路（不入账、不报错）。
		if amt > float64(math.MaxInt64)/quotaPerUnit {
			return wallet.ErrRedeemCodeInvalid
		}
		credit := int64(amt * quotaPerUnit)
		if credit > 0 {
			// 与 CAS 同事务落 users.quota，二者原子：要么都成、要么都回滚。入账失败 → 整个事务
			// 回滚、CAS 一并撤销，码保持 enabled 可再兑，无「作废且不到账」的悬空态。
			if e := tx.Table("users").Where("id = ?", userID).
				UpdateColumn("quota", gorm.Expr("quota + ?", credit)).Error; e != nil {
				return e
			}
		}
		amountUSD, creditUnits = amt, credit
		return nil
	})
	return amountUSD, creditUnits, err
}

// redemptionListCap 是自助兑换码列表「最新 N 条」的安全上限，防止无界 Find 随建码累积而 OOM/长阻塞（按 id 倒序取最新 N 条）。
const redemptionListCap = 1000

// ListCodesByTenant 列出某租户的兑换码（按 id 倒序，上限最新 redemptionListCap 条）。
// 强制 WHERE tenant_id=? —— 代理自助列表的越权防线（scopeByTenant）。
func (r *Repo) ListCodesByTenant(ctx context.Context, tenantID int64) ([]wallet.RedemptionCode, error) {
	var rows []redemptionRow
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).Order("id desc").Limit(redemptionListCap).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]wallet.RedemptionCode, 0, len(rows))
	for i := range rows {
		out = append(out, *toRedemption(&rows[i]))
	}
	return out, nil
}

// statusOrDefault 空状态回退 enabled。
func statusOrDefault(s wallet.RedemptionStatus) wallet.RedemptionStatus {
	if s == "" {
		return wallet.RedemptionEnabled
	}
	return s
}

// toRedemption 把 DB 行映射为 domain 模型（*time.Time -> 零值 time.Time）。
func toRedemption(r *redemptionRow) *wallet.RedemptionCode {
	rc := &wallet.RedemptionCode{
		ID:           r.ID,
		TenantID:     r.TenantID,
		Code:         r.Code,
		AmountUSD:    r.AmountUSD,
		Status:       wallet.RedemptionStatus(r.Status),
		UsedByUserID: r.UsedByUserID,
		CreatedAt:    r.CreatedAt,
	}
	if r.ExpireAt != nil {
		rc.ExpireAt = *r.ExpireAt
	}
	if r.UsedAt != nil {
		rc.UsedAt = *r.UsedAt
	}
	return rc
}
