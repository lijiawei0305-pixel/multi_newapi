// Package gormrepo 用真实 GORM(MySQL) 实现 agent.AgentRepo（代理资料 / 钱包 / 收益台账 / 提现单 四表）。
//
// 键统一为 tenant_id（决策：代理=User+Tenant 1:1）。关键不变量（detailed-design §6.2）：
//   - AppendEarning：收益日志 idem_key 唯一索引 + ON CONFLICT DO NOTHING 强幂等；仅首次入账才动钱包，
//     钱包用 upsert 原子累加（withdrawable += amount、total_earned += amount）。
//   - CreateWithdrawal：条件 UPDATE（WHERE withdrawable >= amount）在 DB 层保证「不透支」，
//     0 行受影响即余额不足 -> ErrWithdrawInsufficient；同事务再建 pending 提现单（金额守恒：可提现→冻结）。
//   - ResolveWithdrawal：CAS（WHERE status='pending'）原子翻牌，杜绝并发重复审核；approved=扣冻结（打款），
//     rejected=解冻退回（金额守恒：冻结→可提现）。全程不依赖 SELECT ... FOR UPDATE，方言可移植（含 sqlite 单测）。
//
// 金额列用 decimal(20,8) 精确存储（¥ 收益/提现；为佣金换算 USD×ratio×rate 预留小数位）；接口仍以 float64 进出。
package gormrepo

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/QuantumNous/new-api/internal/agent"
)

// ---- 表 1：agent_profiles —— 代理资料（tenant_id 主键，1:1 独占） ----

type profileRow struct {
	TenantID        int64     `gorm:"column:tenant_id;primaryKey"`
	UserID          int64     `gorm:"column:user_id;not null;default:0;index:idx_agent_profiles_user"`
	Type            string    `gorm:"column:type;type:varchar(16);not null;default:normal"`
	Level           int       `gorm:"column:level;not null;default:0"`
	CanAPI          bool      `gorm:"column:can_api;not null;default:false"`
	CostPriceCNY    float64   `gorm:"column:cost_price_cny;type:decimal(20,8);not null;default:0"`
	PackageDiscount float64   `gorm:"column:package_discount;type:decimal(20,8);not null;default:0"`
	CommissionRatio float64   `gorm:"column:commission_ratio;type:decimal(20,8);not null;default:0"`
	DiscountFloor   float64   `gorm:"column:discount_floor;type:decimal(20,8);not null;default:0"`
	CreatedAt       time.Time `gorm:"column:created_at"`
	UpdatedAt       time.Time `gorm:"column:updated_at"`
}

func (profileRow) TableName() string { return "agent_profiles" }

// ---- 表 2：agent_wallets —— 代理钱包（tenant_id 主键） ----

type walletRow struct {
	TenantID             int64     `gorm:"column:tenant_id;primaryKey"`
	UserID               int64     `gorm:"column:user_id;not null;default:0"`
	APIBalance           float64   `gorm:"column:api_balance;type:decimal(20,8);not null;default:0"`
	WithdrawableBalance  float64   `gorm:"column:withdrawable_balance;type:decimal(20,8);not null;default:0"`
	FrozenWithdrawAmount float64   `gorm:"column:frozen_withdraw_amount;type:decimal(20,8);not null;default:0"`
	TotalEarned          float64   `gorm:"column:total_earned;type:decimal(20,8);not null;default:0"`
	UpdatedAt            time.Time `gorm:"column:updated_at"`
}

func (walletRow) TableName() string { return "agent_wallets" }

// ---- 表 3：agent_earning_logs —— 收益台账（idem_key 唯一，强幂等） ----

type earningRow struct {
	ID         int64     `gorm:"column:id;primaryKey;autoIncrement"`
	TenantID   int64     `gorm:"column:tenant_id;not null;index:idx_agent_earnings_tenant"`
	UserID     int64     `gorm:"column:user_id;not null;default:0"`
	SourceType string    `gorm:"column:source_type;type:varchar(32);not null"`
	SourceID   string    `gorm:"column:source_id;type:varchar(128);not null"`
	IdemKey    string    `gorm:"column:idem_key;type:varchar(200);not null;uniqueIndex:idx_agent_earnings_idem"`
	Amount     float64   `gorm:"column:amount;type:decimal(20,8);not null"`
	Remark     string    `gorm:"column:remark;type:varchar(255);not null;default:''"`
	CreatedAt  time.Time `gorm:"column:created_at"`
}

func (earningRow) TableName() string { return "agent_earning_logs" }

// ---- 表 4：agent_withdrawals —— 提现单（状态机 pending→approved/rejected） ----

type withdrawalRow struct {
	ID         int64      `gorm:"column:id;primaryKey;autoIncrement"`
	TenantID   int64      `gorm:"column:tenant_id;not null;index:idx_agent_withdrawals_tenant"`
	UserID     int64      `gorm:"column:user_id;not null;default:0"`
	Amount     float64    `gorm:"column:amount;type:decimal(20,8);not null"`
	Status     string     `gorm:"column:status;type:varchar(16);not null;default:pending;index:idx_agent_withdrawals_status"`
	Remark     string     `gorm:"column:remark;type:varchar(255);not null;default:''"`
	CreatedAt  time.Time  `gorm:"column:created_at"`
	UpdatedAt  time.Time  `gorm:"column:updated_at"`
	ReviewedAt *time.Time `gorm:"column:reviewed_at"`
}

func (withdrawalRow) TableName() string { return "agent_withdrawals" }

// Repo 是 agent.AgentRepo 的 GORM 实现（替换 MemRepo）。
type Repo struct {
	db  *gorm.DB
	now func() time.Time
}

// 编译期断言：*Repo 满足 agent.AgentRepo 契约。
var _ agent.AgentRepo = (*Repo)(nil)

// New 用已建立连接的 *gorm.DB 构造仓储。
func New(db *gorm.DB) *Repo { return &Repo{db: db, now: time.Now} }

// AutoMigrate 建/补 4 张代理表结构（含唯一/普通索引）。由 mtwire.Migrate 在 master 节点调用。
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&profileRow{}, &walletRow{}, &earningRow{}, &withdrawalRow{})
}

// ---- AgentRepo：代理资料 ----

// SetAgentType 按 tenant_id 主键 upsert 代理资料（设代理 / 改代理复用）。
func (r *Repo) SetAgentType(ctx context.Context, tenantID int64, t agent.AgentType, p agent.AgentParams) error {
	now := r.now()
	row := profileRow{
		TenantID:        tenantID,
		Type:            string(t),
		Level:           p.Level,
		CanAPI:          p.CanAPI,
		CostPriceCNY:    p.CostPrice,
		PackageDiscount: p.PackageDiscount,
		CommissionRatio: p.CommissionRatio,
		DiscountFloor:   p.DiscountFloor,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "tenant_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"type", "level", "can_api", "cost_price_cny", "package_discount",
			"commission_ratio", "discount_floor", "updated_at",
		}),
	}).Create(&row).Error
}

// GetAgentType 读取代理资料；found=false 表示该租户尚未设代理。
func (r *Repo) GetAgentType(ctx context.Context, tenantID int64) (agent.AgentType, agent.AgentParams, bool, error) {
	var row profileRow
	err := r.db.WithContext(ctx).Take(&row, "tenant_id = ?", tenantID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", agent.AgentParams{}, false, nil
		}
		return "", agent.AgentParams{}, false, err
	}
	return agent.AgentType(row.Type), agent.AgentParams{
		CostPrice:       row.CostPriceCNY,
		PackageDiscount: row.PackageDiscount,
		CommissionRatio: row.CommissionRatio,
		Level:           row.Level,
		CanAPI:          row.CanAPI,
		DiscountFloor:   row.DiscountFloor,
	}, true, nil
}

// ---- AgentRepo：钱包（只读；写由 AppendEarning / 提现状态机驱动） ----

// GetWallet 返回租户钱包；行不存在返回该租户的零值钱包（不报错），对齐 MemRepo 行为。
func (r *Repo) GetWallet(ctx context.Context, tenantID int64) (*agent.AgentWallet, error) {
	var row walletRow
	err := r.db.WithContext(ctx).Take(&row, "tenant_id = ?", tenantID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &agent.AgentWallet{TenantID: tenantID}, nil
		}
		return nil, err
	}
	return toWallet(&row), nil
}

// ---- AgentRepo：收益入账（强幂等 + 原子累加） ----

// AppendEarning 幂等入账：先以 idem_key 唯一约束 INSERT（冲突即已入账，applied=false 且不动钱包），
// 首次入账才在同事务 upsert 钱包（withdrawable / total_earned 原子累加）。
func (r *Repo) AppendEarning(ctx context.Context, e agent.EarningEntry) (bool, error) {
	now := r.now()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	applied := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		log := earningRow{
			TenantID:   e.TenantID,
			UserID:     e.UserID,
			SourceType: string(e.SourceType),
			SourceID:   e.SourceID,
			IdemKey:    e.IdempotencyKey(),
			Amount:     e.Amount,
			Remark:     e.Remark,
			CreatedAt:  e.CreatedAt,
		}
		// ON CONFLICT DO NOTHING：捕获 idem_key 唯一冲突；RowsAffected==0 即重复入账。
		res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&log)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil // 幂等：同来源不重复入账，applied 保持 false
		}
		// 首次入账：钱包原子累加（行不存在则创建）。
		w := walletRow{
			TenantID:            e.TenantID,
			UserID:              e.UserID,
			WithdrawableBalance: e.Amount,
			TotalEarned:         e.Amount,
			UpdatedAt:           now,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "tenant_id"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"withdrawable_balance": gorm.Expr("withdrawable_balance + ?", e.Amount),
				"total_earned":         gorm.Expr("total_earned + ?", e.Amount),
				"user_id":              e.UserID,
				"updated_at":           now,
			}),
		}).Create(&w).Error; err != nil {
			return err
		}
		applied = true
		return nil
	})
	return applied, err
}

// ---- AgentRepo：提现状态机（原子冻结 / 审核翻牌 + 资金守恒） ----

// CreateWithdrawal 原子冻结可提现余额并建 pending 提现单。
// 金额非正 -> ErrWithdrawInsufficient；条件 UPDATE 0 行（余额不足 / 无钱包）-> ErrWithdrawInsufficient。
func (r *Repo) CreateWithdrawal(ctx context.Context, wd *agent.Withdrawal) error {
	if wd.Amount <= 0 {
		return agent.ErrWithdrawInsufficient
	}
	now := r.now()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 原子冻结：可提现 -= amount、冻结 += amount，仅当可提现充足。
		res := tx.Model(&walletRow{}).
			Where("tenant_id = ? AND withdrawable_balance >= ?", wd.TenantID, wd.Amount).
			Updates(map[string]interface{}{
				"withdrawable_balance":   gorm.Expr("withdrawable_balance - ?", wd.Amount),
				"frozen_withdraw_amount": gorm.Expr("frozen_withdraw_amount + ?", wd.Amount),
				"updated_at":             now,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return agent.ErrWithdrawInsufficient
		}
		row := withdrawalRow{
			TenantID:  wd.TenantID,
			UserID:    wd.UserID,
			Amount:    wd.Amount,
			Status:    string(agent.WithdrawPending),
			Remark:    wd.Remark,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		wd.ID = row.ID
		wd.Status = agent.WithdrawPending
		wd.CreatedAt = now
		wd.UpdatedAt = now
		return nil
	})
}

// GetWithdrawal 按 id 读取提现单；不存在返回 ErrWithdrawNotFound。
func (r *Repo) GetWithdrawal(ctx context.Context, id int64) (*agent.Withdrawal, error) {
	var row withdrawalRow
	err := r.db.WithContext(ctx).Take(&row, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, agent.ErrWithdrawNotFound
		}
		return nil, err
	}
	return toWithdrawal(&row), nil
}

// ResolveWithdrawal 原子迁移提现单状态并移动资金：approved=扣冻结（线下打款），rejected=解冻退回。
// 不存在 -> ErrWithdrawNotFound；非 pending（或并发已被审核）-> ErrWithdrawNotPending。
func (r *Repo) ResolveWithdrawal(ctx context.Context, id int64, target agent.WithdrawStatus, remark string) error {
	now := r.now()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row withdrawalRow
		if err := tx.Take(&row, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return agent.ErrWithdrawNotFound
			}
			return err
		}
		if !agent.WithdrawStatus(row.Status).CanTransitionTo(target) {
			return agent.ErrWithdrawNotPending
		}
		// CAS：pending→target，抢到者负责移动资金；并发败者 RowsAffected==0。
		res := tx.Model(&withdrawalRow{}).
			Where("id = ? AND status = ?", id, string(agent.WithdrawPending)).
			Updates(map[string]interface{}{
				"status":      string(target),
				"remark":      remark,
				"reviewed_at": now,
				"updated_at":  now,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return agent.ErrWithdrawNotPending
		}
		// 资金移动：approved 仅扣冻结（资金离开系统）；rejected 解冻退回可提现。
		updates := map[string]interface{}{
			"frozen_withdraw_amount": gorm.Expr("frozen_withdraw_amount - ?", row.Amount),
			"updated_at":             now,
		}
		if target == agent.WithdrawRejected {
			updates["withdrawable_balance"] = gorm.Expr("withdrawable_balance + ?", row.Amount)
		}
		return tx.Model(&walletRow{}).Where("tenant_id = ?", row.TenantID).Updates(updates).Error
	})
}

// ---- 非接口辅助方法（供 mtwire handler / seed 装配；不属 AgentRepo 契约） ----

// EnsureWallet 幂等地为某租户建一个零值钱包（账户已存在则不动）。供 seed / 设代理。
func (r *Repo) EnsureWallet(ctx context.Context, tenantID, userID int64) error {
	row := walletRow{TenantID: tenantID, UserID: userID, UpdatedAt: r.now()}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
}

// AgentRow 是「设代理列表」的一行原始资料（tenant_id + owner + 类型/参数）。
type AgentRow struct {
	TenantID        int64
	UserID          int64
	Type            agent.AgentType
	Level           int
	CanAPI          bool
	CostPriceCNY    float64
	PackageDiscount float64
	CommissionRatio float64
}

// ListProfiles 列出全部代理资料（每行 = 一个代理租户），供管理端 GET /api/admin/agents 装配。
func (r *Repo) ListProfiles(ctx context.Context) ([]AgentRow, error) {
	var rows []profileRow
	if err := r.db.WithContext(ctx).Order("tenant_id asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]AgentRow, 0, len(rows))
	for _, p := range rows {
		out = append(out, AgentRow{
			TenantID:        p.TenantID,
			UserID:          p.UserID,
			Type:            agent.AgentType(p.Type),
			Level:           p.Level,
			CanAPI:          p.CanAPI,
			CostPriceCNY:    p.CostPriceCNY,
			PackageDiscount: p.PackageDiscount,
			CommissionRatio: p.CommissionRatio,
		})
	}
	return out, nil
}

// ListWithdrawalsByTenant 列出某租户的提现单（按时间倒序）。供代理自助 GET /api/tenant/withdrawals。
func (r *Repo) ListWithdrawalsByTenant(ctx context.Context, tenantID int64) ([]agent.Withdrawal, error) {
	var rows []withdrawalRow
	if err := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID).
		Order("created_at desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	return toWithdrawals(rows), nil
}

// ListWithdrawals 列出全部提现单（status 非空则按状态过滤），按时间倒序。供管理端 GET /api/admin/withdrawals。
func (r *Repo) ListWithdrawals(ctx context.Context, status string) ([]agent.Withdrawal, error) {
	q := r.db.WithContext(ctx).Model(&withdrawalRow{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var rows []withdrawalRow
	if err := q.Order("created_at desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	return toWithdrawals(rows), nil
}

// ListEarningsByTenant 列出某租户的收益台账（按时间倒序）。供代理自助 GET /api/tenant/earnings。
func (r *Repo) ListEarningsByTenant(ctx context.Context, tenantID int64) ([]agent.EarningEntry, error) {
	var rows []earningRow
	if err := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID).
		Order("created_at desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]agent.EarningEntry, 0, len(rows))
	for _, e := range rows {
		out = append(out, agent.EarningEntry{
			TenantID:   e.TenantID,
			UserID:     e.UserID,
			SourceType: agent.EarningSource(e.SourceType),
			SourceID:   e.SourceID,
			Amount:     e.Amount,
			Remark:     e.Remark,
			CreatedAt:  e.CreatedAt,
		})
	}
	return out, nil
}

// ---- 映射辅助 ----

func toWallet(row *walletRow) *agent.AgentWallet {
	return &agent.AgentWallet{
		TenantID:             row.TenantID,
		UserID:               row.UserID,
		APIBalance:           row.APIBalance,
		WithdrawableBalance:  row.WithdrawableBalance,
		FrozenWithdrawAmount: row.FrozenWithdrawAmount,
		TotalEarned:          row.TotalEarned,
		UpdatedAt:            row.UpdatedAt,
	}
}

func toWithdrawal(row *withdrawalRow) *agent.Withdrawal {
	w := &agent.Withdrawal{
		ID:        row.ID,
		TenantID:  row.TenantID,
		UserID:    row.UserID,
		Amount:    row.Amount,
		Status:    agent.WithdrawStatus(row.Status),
		Remark:    row.Remark,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
	if row.ReviewedAt != nil {
		w.ReviewedAt = *row.ReviewedAt
	}
	return w
}

func toWithdrawals(rows []withdrawalRow) []agent.Withdrawal {
	out := make([]agent.Withdrawal, 0, len(rows))
	for i := range rows {
		out = append(out, *toWithdrawal(&rows[i]))
	}
	return out
}
