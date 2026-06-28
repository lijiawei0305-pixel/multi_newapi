// Package gormrepo 用真实 GORM(MySQL) 实现 wallet.WalletRepo（user_balances + redemption_codes 两表）。
//
// 关键不变量（detailed-design §6.2）：
//   - ChargeBalance 用条件 UPDATE（WHERE balance_usd >= cost）在 DB 层保证「零透支」，
//     0 行受影响即余额不足 -> ErrQuotaInsufficient；新余额在同一事务内回读。
//   - UseRedemption 用 CAS（WHERE status='enabled'）原子翻牌，RowsAffected 决定 ok，杜绝重复兑换。
//   - AddBalance 用 upsert（ON DUPLICATE KEY UPDATE balance_usd=balance_usd+?）原子累加。
//
// 金额列用 decimal(20,8) 精确存储；接口仍以 float64 进出（driver 扫描兼容）。
package gormrepo

import (
	"context"
	"errors"
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

// redemptionRow 是 redemption_codes 表的 GORM 模型。(tenant_id, code) 唯一（租户内码唯一）。
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

// TableName 固定表名。
func (redemptionRow) TableName() string { return "redemption_codes" }

// Repo 是 wallet.WalletRepo 的 GORM 实现，并附带 seed 辅助方法。
type Repo struct {
	db *gorm.DB
}

// 编译期断言：*Repo 满足 wallet.WalletRepo 契约。
var _ wallet.WalletRepo = (*Repo)(nil)

// New 用已建立连接的 *gorm.DB 构造仓储。
func New(db *gorm.DB) *Repo { return &Repo{db: db} }

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
