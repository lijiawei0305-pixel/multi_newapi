// Package gormrepo 用真实 GORM(MySQL) 实现 tokenplan.PlanRepo 与 tokenplan.SubscriptionRepo。
//
// 五张表（proposal §6.17–§6.20 + 待支付订单）：
//   - token_plans              主站套餐定义（code 唯一，自然键 + seed 幂等基准）
//   - tenant_token_plans       代理上架/定价（UNIQUE(tenant_id, plan_id)）
//   - user_subscriptions       订阅实例（source_order_id 唯一 → 激活幂等）
//   - subscription_usage_logs  套餐内计量日志（INDEX(subscription_id)）
//   - pending_subscription_orders 待支付购买意图（order_id PK，支持异步回调激活）
//
// 关键不变量（detailed-design §6.2）：
//   - Meter 用条件 UPDATE（WHERE active AND expire_at>now AND used+cost<=month_limit）
//     在 DB 层保证「零击穿月限额」，0 行受影响再诊断置 exhausted/expired；计量日志同事务写。
//   - ActivateFromOrder 用 INSERT IGNORE（OnConflict(source_order_id) DoNothing）保证同订单只建一个实例。
//
// 金额列：¥ 价用 decimal(20,2)；USD 额度/计量用 decimal(20,8)（与 wallet 一致）；倍率 decimal(10,4)。
package gormrepo

import (
	"context"
	"errors"
	"math"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/QuantumNous/new-api/internal/tokenplan"
)

// planRow 是 token_plans 表的 GORM 模型。code 唯一索引（自然键，seed 幂等基准）。
type planRow struct {
	ID              int64     `gorm:"column:id;primaryKey;autoIncrement"`
	Code            string    `gorm:"column:code;type:varchar(32);not null;uniqueIndex:idx_token_plans_code"`
	Name            string    `gorm:"column:name;type:varchar(64);not null"`
	BasePrice       float64   `gorm:"column:base_price;type:decimal(20,2);not null;default:0"`
	AnchorPrice     float64   `gorm:"column:anchor_price;type:decimal(20,2);not null;default:0"`
	DiscountLabel   string    `gorm:"column:discount_label;type:varchar(32)"`
	Multiplier      float64   `gorm:"column:multiplier;type:decimal(10,4);not null;default:1"`
	MonthLimitUSD   float64   `gorm:"column:month_limit_usd;type:decimal(20,8);not null;default:0"`
	ValidDays       int       `gorm:"column:valid_days;not null;default:30"`
	UpstreamCostEst float64   `gorm:"column:upstream_cost_est;type:decimal(20,2);not null;default:0"`
	AgentCostPrice  float64   `gorm:"column:agent_cost_price;type:decimal(20,2);not null;default:0"`
	MinPrice        float64   `gorm:"column:min_price;type:decimal(20,2);not null;default:0"`
	IsRecommended   bool      `gorm:"column:is_recommended;not null;default:false"`
	Badge           string    `gorm:"column:badge;type:varchar(64)"`
	Sort            int       `gorm:"column:sort;not null;default:0"`
	Status          string    `gorm:"column:status;type:varchar(16);not null;default:enabled"`
	CreatedAt       time.Time `gorm:"column:created_at"`
	UpdatedAt       time.Time `gorm:"column:updated_at"`
}

// TableName 固定表名。
func (planRow) TableName() string { return "token_plans" }

// listingRow 是 tenant_token_plans 表的 GORM 模型。UNIQUE(tenant_id, plan_id)。
type listingRow struct {
	ID          int64     `gorm:"column:id;primaryKey;autoIncrement"`
	TenantID    int64     `gorm:"column:tenant_id;not null;uniqueIndex:idx_tenant_plans_tenant_plan,priority:1"`
	PlanID      int64     `gorm:"column:plan_id;not null;uniqueIndex:idx_tenant_plans_tenant_plan,priority:2"`
	Enabled     bool      `gorm:"column:enabled;not null;default:false"`
	RetailPrice float64   `gorm:"column:retail_price;type:decimal(20,2);not null;default:0"`
	CreatedAt   time.Time `gorm:"column:created_at"`
	UpdatedAt   time.Time `gorm:"column:updated_at"`
}

// TableName 固定表名。
func (listingRow) TableName() string { return "tenant_token_plans" }

// subRow 是 user_subscriptions 表的 GORM 模型。source_order_id 唯一 → 激活幂等。
type subRow struct {
	ID             int64     `gorm:"column:id;primaryKey;autoIncrement"`
	TenantID       int64     `gorm:"column:tenant_id;not null;index:idx_user_subs_tenant_user_status,priority:1"`
	UserID         int64     `gorm:"column:user_id;not null;index:idx_user_subs_tenant_user_status,priority:2"`
	PlanID         int64     `gorm:"column:plan_id;not null"`
	PurchasedPrice float64   `gorm:"column:purchased_price;type:decimal(20,2);not null;default:0"`
	MonthLimitUSD  float64   `gorm:"column:month_limit_usd;type:decimal(20,8);not null;default:0"`
	UsedUSD        float64   `gorm:"column:used_usd;type:decimal(20,8);not null;default:0"`
	Status         string    `gorm:"column:status;type:varchar(16);not null;default:active;index:idx_user_subs_tenant_user_status,priority:3"`
	StartAt        time.Time `gorm:"column:start_at"`
	ExpireAt       time.Time `gorm:"column:expire_at"`
	SourceOrderID  string    `gorm:"column:source_order_id;type:varchar(128);not null;uniqueIndex:idx_user_subs_order"`
	CreatedAt      time.Time `gorm:"column:created_at"`
	UpdatedAt      time.Time `gorm:"column:updated_at"`
}

// TableName 固定表名。
func (subRow) TableName() string { return "user_subscriptions" }

// usageRow 是 subscription_usage_logs 表的 GORM 模型。INDEX(subscription_id)。
type usageRow struct {
	ID              int64     `gorm:"column:id;primaryKey;autoIncrement"`
	SubscriptionID  int64     `gorm:"column:subscription_id;not null;index:idx_sub_usage_sub"`
	TenantID        int64     `gorm:"column:tenant_id;not null"`
	UserID          int64     `gorm:"column:user_id;not null"`
	Model           string    `gorm:"column:model;type:varchar(128)"`
	UpstreamCostUSD float64   `gorm:"column:upstream_cost_usd;type:decimal(20,8);not null;default:0"`
	RequestID       string    `gorm:"column:request_id;type:varchar(64)"`
	CreatedAt       time.Time `gorm:"column:created_at"`
}

// TableName 固定表名。
func (usageRow) TableName() string { return "subscription_usage_logs" }

// pendingRow 是 pending_subscription_orders 表的 GORM 模型：暂存下单意图（按 order_id），
// 供 ActivateFromPayment(orderID) 凭订单号还原订阅快照（支持异步支付回调）。
type pendingRow struct {
	OrderID        string    `gorm:"column:order_id;primaryKey;type:varchar(128)"`
	TenantID       int64     `gorm:"column:tenant_id;not null"`
	UserID         int64     `gorm:"column:user_id;not null"`
	PlanID         int64     `gorm:"column:plan_id;not null"`
	RetailPrice    float64   `gorm:"column:retail_price;type:decimal(20,2);not null;default:0"`
	AgentCostPrice float64   `gorm:"column:agent_cost_price;type:decimal(20,2);not null;default:0"`
	MonthLimitUSD  float64   `gorm:"column:month_limit_usd;type:decimal(20,8);not null;default:0"`
	ValidDays      int       `gorm:"column:valid_days;not null;default:30"`
	CreatedAt      time.Time `gorm:"column:created_at"`
}

// TableName 固定表名。
func (pendingRow) TableName() string { return "pending_subscription_orders" }

// Repo 是 tokenplan.PlanRepo + tokenplan.SubscriptionRepo 的 GORM 实现，并附带 seed 辅助方法。
type Repo struct {
	db *gorm.DB
}

// 编译期断言：*Repo 同时满足两套契约。
var (
	_ tokenplan.PlanRepo         = (*Repo)(nil)
	_ tokenplan.SubscriptionRepo = (*Repo)(nil)
)

// New 用已建立连接的 *gorm.DB 构造仓储。
func New(db *gorm.DB) *Repo { return &Repo{db: db} }

// AutoMigrate 建/补 tokenplan 五张表结构（含唯一/普通索引）。由 cmd/server 在启动时调用。
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&planRow{}, &listingRow{}, &subRow{}, &usageRow{}, &pendingRow{})
}

// ---- PlanRepo ----

// CreatePlan 入库套餐并回填 ID/时间戳；code 冲突翻译为 ErrPlanInputInvalid（PLAN_INPUT_INVALID）。
func (r *Repo) CreatePlan(ctx context.Context, p *tokenplan.Plan) error {
	row := toPlanRow(p)
	now := time.Now()
	row.CreatedAt = now
	row.UpdatedAt = now
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return tokenplan.ErrPlanInputInvalid // 同 code 已存在：视为非法入参
		}
		return err
	}
	p.ID = row.ID
	p.CreatedAt = row.CreatedAt
	p.UpdatedAt = row.UpdatedAt
	return nil
}

// UpdatePlan 全量更新；不存在返回 ErrPlanNotFound，code 撞他行返回 ErrPlanInputInvalid。
func (r *Repo) UpdatePlan(ctx context.Context, id int64, in tokenplan.PlanInput) error {
	var cur planRow
	if err := r.db.WithContext(ctx).Take(&cur, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tokenplan.ErrPlanNotFound
		}
		return err
	}
	res := r.db.WithContext(ctx).Model(&planRow{}).Where("id = ?", id).Updates(map[string]any{
		"code":              in.Code,
		"name":              in.Name,
		"base_price":        in.BasePrice,
		"anchor_price":      in.AnchorPrice,
		"discount_label":    in.DiscountLabel,
		"multiplier":        in.Multiplier,
		"month_limit_usd":   in.MonthLimitUSD,
		"valid_days":        in.ValidDays,
		"upstream_cost_est": in.UpstreamCostEst,
		"agent_cost_price":  in.AgentCostPrice,
		"min_price":         in.MinPrice,
		"is_recommended":    in.IsRecommended,
		"badge":             in.Badge,
		"sort":              in.Sort,
		"status":            string(in.Status),
		"updated_at":        time.Now(),
	})
	if res.Error != nil {
		if errors.Is(res.Error, gorm.ErrDuplicatedKey) {
			return tokenplan.ErrPlanInputInvalid
		}
		return res.Error
	}
	return nil
}

// GetPlan 按 id 读取；不存在返回 ErrPlanNotFound。
func (r *Repo) GetPlan(ctx context.Context, id int64) (*tokenplan.Plan, error) {
	var row planRow
	if err := r.db.WithContext(ctx).Take(&row, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, tokenplan.ErrPlanNotFound
		}
		return nil, err
	}
	return toPlan(&row), nil
}

// ListPlans 返回全部套餐（按 sort、id 升序，对齐 MemRepo 确定性排序）。
func (r *Repo) ListPlans(ctx context.Context) ([]tokenplan.Plan, error) {
	var rows []planRow
	if err := r.db.WithContext(ctx).Order("sort ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]tokenplan.Plan, 0, len(rows))
	for i := range rows {
		out = append(out, *toPlan(&rows[i]))
	}
	return out, nil
}

// GetListing 读取某租户对某套餐的上架记录；不存在返回 (nil, nil)（未上架非错误）。
func (r *Repo) GetListing(ctx context.Context, tenantID, planID int64) (*tokenplan.TenantPlan, error) {
	var row listingRow
	err := r.db.WithContext(ctx).Take(&row, "tenant_id = ? AND plan_id = ?", tenantID, planID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toTenantPlan(&row), nil
}

// UpsertListing 按 (tenant_id, plan_id) 唯一键 upsert 上架记录，回填 ID/时间戳。
func (r *Repo) UpsertListing(ctx context.Context, tp *tokenplan.TenantPlan) error {
	now := time.Now()
	row := listingRow{
		TenantID:    tp.TenantID,
		PlanID:      tp.PlanID,
		Enabled:     tp.Enabled,
		RetailPrice: tp.RetailPrice,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "tenant_id"}, {Name: "plan_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"enabled":      tp.Enabled,
			"retail_price": tp.RetailPrice,
			"updated_at":   now,
		}),
	}).Create(&row).Error; err != nil {
		return err
	}
	// 回读规范行，回填 ID/时间戳（upsert 命中更新时 row.ID 可能为 0）。
	var saved listingRow
	if err := r.db.WithContext(ctx).Take(&saved, "tenant_id = ? AND plan_id = ?", tp.TenantID, tp.PlanID).Error; err != nil {
		return err
	}
	tp.ID = saved.ID
	tp.CreatedAt = saved.CreatedAt
	tp.UpdatedAt = saved.UpdatedAt
	return nil
}

// ListListings 返回某租户全部上架记录（按 plan_id 升序）。
func (r *Repo) ListListings(ctx context.Context, tenantID int64) ([]tokenplan.TenantPlan, error) {
	var rows []listingRow
	if err := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Order("plan_id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]tokenplan.TenantPlan, 0, len(rows))
	for i := range rows {
		out = append(out, *toTenantPlan(&rows[i]))
	}
	return out, nil
}

// ---- SubscriptionRepo ----

// SavePendingPurchase 暂存下单意图（按 order_id；幂等 DoNothing，不覆盖既有）。
func (r *Repo) SavePendingPurchase(ctx context.Context, p *tokenplan.PendingPurchase) error {
	row := pendingRow{
		OrderID:        p.OrderID,
		TenantID:       p.TenantID,
		UserID:         p.UserID,
		PlanID:         p.PlanID,
		RetailPrice:    p.RetailPrice,
		AgentCostPrice: p.AgentCostPrice,
		MonthLimitUSD:  p.MonthLimitUSD,
		ValidDays:      p.ValidDays,
		CreatedAt:      time.Now(),
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
}

// GetPendingPurchase 凭订单号取回购买意图；不存在返回 ErrSubscriptionNotFound。
func (r *Repo) GetPendingPurchase(ctx context.Context, orderID string) (*tokenplan.PendingPurchase, error) {
	var row pendingRow
	if err := r.db.WithContext(ctx).Take(&row, "order_id = ?", orderID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, tokenplan.ErrSubscriptionNotFound
		}
		return nil, err
	}
	return &tokenplan.PendingPurchase{
		OrderID:        row.OrderID,
		TenantID:       row.TenantID,
		UserID:         row.UserID,
		PlanID:         row.PlanID,
		RetailPrice:    row.RetailPrice,
		AgentCostPrice: row.AgentCostPrice,
		MonthLimitUSD:  row.MonthLimitUSD,
		ValidDays:      row.ValidDays,
	}, nil
}

// ActivateFromOrder 幂等创建：INSERT IGNORE(source_order_id)。RowsAffected==1 → 新建并回填；
// ==0 → 同订单已存在，回读既有实例填回 sub 返回 created=false。
func (r *Repo) ActivateFromOrder(ctx context.Context, sub *tokenplan.Subscription) (bool, error) {
	row := toSubRow(sub)
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "source_order_id"}},
		DoNothing: true,
	}).Create(&row)
	if res.Error != nil {
		return false, res.Error
	}
	if res.RowsAffected == 1 {
		sub.ID = row.ID
		return true, nil
	}
	// 已存在：回读既有实例回填（幂等结果）。
	var existing subRow
	if err := r.db.WithContext(ctx).Take(&existing, "source_order_id = ?", sub.SourceOrderID).Error; err != nil {
		return false, err
	}
	*sub = *toSubscription(&existing)
	return false, nil
}

// GetActiveByUser 返回用户当前 active 订阅（先持久化惰性过期，再取最近一条未过期 active）；无则 (nil, nil)。
func (r *Repo) GetActiveByUser(ctx context.Context, userID int64, now time.Time) (*tokenplan.Subscription, error) {
	// 惰性过期：把所有 active 且已到期的实例翻为 expired（持久化，对齐 MemRepo 读时翻牌）。
	if err := r.db.WithContext(ctx).Model(&subRow{}).
		Where("user_id = ? AND status = ? AND expire_at <= ?", userID, statusActive, now).
		Updates(map[string]any{"status": statusExpired, "updated_at": now}).Error; err != nil {
		return nil, err
	}
	var row subRow
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND status = ? AND expire_at > ?", userID, statusActive, now).
		Order("id DESC").Limit(1).Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toSubscription(&row), nil
}

// GetByID 按 id 读取订阅；不存在返回 ErrSubscriptionNotFound。
func (r *Repo) GetByID(ctx context.Context, id int64) (*tokenplan.Subscription, error) {
	var row subRow
	if err := r.db.WithContext(ctx).Take(&row, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, tokenplan.ErrSubscriptionNotFound
		}
		return nil, err
	}
	return toSubscription(&row), nil
}

// Meter 原子条件累加（detailed-design §6.2）：
//
//	条件 UPDATE（active 且未过期 且 used+cost<=month_limit）命中 → used+=cost，同事务写计量日志，返回 nil；
//	0 行 → 诊断并持久化终态：到期置 expired→SUBSCRIPTION_EXPIRED；超额置 exhausted→SUBSCRIPTION_EXHAUSTED；
//	      已是终态按既有状态上浮；行不存在→SUBSCRIPTION_NOT_FOUND。
//
// 终态翻牌须落库，故业务错误经 bizErr 旁路携带、事务返回 nil 提交（仅基础设施错误回滚）。
func (r *Repo) Meter(ctx context.Context, subID int64, costUSD float64, now time.Time) (float64, error) {
	if !validAmount(costUSD) || costUSD < 0 {
		return 0, tokenplan.ErrAmountInvalid
	}
	var newUsed float64
	var bizErr error
	txErr := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&subRow{}).
			Where("id = ? AND status = ? AND expire_at > ? AND used_usd + ? <= month_limit_usd",
				subID, statusActive, now, costUSD).
			Updates(map[string]any{
				"used_usd":   gorm.Expr("used_usd + ?", costUSD),
				"updated_at": now,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 1 {
			var row subRow
			if e := tx.Take(&row, "id = ?", subID).Error; e != nil {
				return e
			}
			newUsed = row.UsedUSD
			return tx.Create(&usageRow{
				SubscriptionID:  subID,
				TenantID:        row.TenantID,
				UserID:          row.UserID,
				UpstreamCostUSD: costUSD,
				CreatedAt:       now,
			}).Error
		}
		// 0 行：诊断现状并持久化终态。
		var row subRow
		if e := tx.Take(&row, "id = ?", subID).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				bizErr = tokenplan.ErrSubscriptionNotFound
				return nil
			}
			return e
		}
		newUsed = row.UsedUSD
		// 惰性过期优先于额度判定。
		if row.Status == statusActive && !now.Before(row.ExpireAt) {
			if e := tx.Model(&subRow{}).Where("id = ? AND status = ?", subID, statusActive).
				Updates(map[string]any{"status": statusExpired, "updated_at": now}).Error; e != nil {
				return e
			}
			bizErr = tokenplan.ErrSubscriptionExpired
			return nil
		}
		switch row.Status {
		case statusExpired, statusRefunded:
			bizErr = tokenplan.ErrSubscriptionExpired
		case statusExhausted:
			bizErr = tokenplan.ErrSubscriptionExhausted
		case statusActive:
			// 未过期但条件未命中 → 超额 → 置 exhausted（整笔拒绝）。
			if e := tx.Model(&subRow{}).Where("id = ? AND status = ?", subID, statusActive).
				Updates(map[string]any{"status": statusExhausted, "updated_at": now}).Error; e != nil {
				return e
			}
			bizErr = tokenplan.ErrSubscriptionExhausted
		default:
			bizErr = tokenplan.ErrSubscriptionNotFound
		}
		return nil
	})
	if txErr != nil {
		return 0, txErr
	}
	return newUsed, bizErr
}

// ---- seed / 装配辅助（非接口方法，main 直接调用）----

// EnsurePlan 幂等地按 code 落一个套餐（已存在则不覆盖），回读返回其 ID。供 seed。
func (r *Repo) EnsurePlan(ctx context.Context, p *tokenplan.Plan) (int64, error) {
	row := toPlanRow(p)
	now := time.Now()
	row.CreatedAt = now
	row.UpdatedAt = now
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return 0, err
	}
	var saved planRow
	if err := r.db.WithContext(ctx).Take(&saved, "code = ?", p.Code).Error; err != nil {
		return 0, err
	}
	return saved.ID, nil
}

// EnsureListing 幂等地为租户上架某套餐（已存在则不覆盖人工改动）。供 seed。
func (r *Repo) EnsureListing(ctx context.Context, tenantID, planID int64, enabled bool, retail float64) error {
	now := time.Now()
	row := listingRow{
		TenantID:    tenantID,
		PlanID:      planID,
		Enabled:     enabled,
		RetailPrice: retail,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
}

// ListSubscriptionsByUser 返回某租户下用户的全部订阅（按 id 降序；先持久化惰性过期）。
// 非 SubscriptionRepo 接口方法——供「我的套餐」列表 handler（含历史 exhausted/expired）。
func (r *Repo) ListSubscriptionsByUser(ctx context.Context, tenantID, userID int64, now time.Time) ([]tokenplan.Subscription, error) {
	if err := r.db.WithContext(ctx).Model(&subRow{}).
		Where("tenant_id = ? AND user_id = ? AND status = ? AND expire_at <= ?", tenantID, userID, statusActive, now).
		Updates(map[string]any{"status": statusExpired, "updated_at": now}).Error; err != nil {
		return nil, err
	}
	var rows []subRow
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND user_id = ?", tenantID, userID).
		Order("id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]tokenplan.Subscription, 0, len(rows))
	for i := range rows {
		out = append(out, *toSubscription(&rows[i]))
	}
	return out, nil
}

// ---- 状态字面值（与 tokenplan.SubStatus / PlanStatus 字符串一致）----
var (
	statusActive    = string(tokenplan.SubActive)
	statusExhausted = string(tokenplan.SubExhausted)
	statusExpired   = string(tokenplan.SubExpired)
	statusRefunded  = string(tokenplan.SubRefunded)
)

// ---- domain <-> row 映射 ----

func toPlanRow(p *tokenplan.Plan) planRow {
	return planRow{
		ID:              p.ID,
		Code:            p.Code,
		Name:            p.Name,
		BasePrice:       p.BasePrice,
		AnchorPrice:     p.AnchorPrice,
		DiscountLabel:   p.DiscountLabel,
		Multiplier:      p.Multiplier,
		MonthLimitUSD:   p.MonthLimitUSD,
		ValidDays:       p.ValidDays,
		UpstreamCostEst: p.UpstreamCostEst,
		AgentCostPrice:  p.AgentCostPrice,
		MinPrice:        p.MinPrice,
		IsRecommended:   p.IsRecommended,
		Badge:           p.Badge,
		Sort:            p.Sort,
		Status:          string(p.Status),
	}
}

func toPlan(row *planRow) *tokenplan.Plan {
	return &tokenplan.Plan{
		ID:              row.ID,
		Code:            row.Code,
		Name:            row.Name,
		BasePrice:       row.BasePrice,
		AnchorPrice:     row.AnchorPrice,
		DiscountLabel:   row.DiscountLabel,
		Multiplier:      row.Multiplier,
		MonthLimitUSD:   row.MonthLimitUSD,
		ValidDays:       row.ValidDays,
		UpstreamCostEst: row.UpstreamCostEst,
		AgentCostPrice:  row.AgentCostPrice,
		MinPrice:        row.MinPrice,
		IsRecommended:   row.IsRecommended,
		Badge:           row.Badge,
		Sort:            row.Sort,
		Status:          tokenplan.PlanStatus(row.Status),
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
}

func toTenantPlan(row *listingRow) *tokenplan.TenantPlan {
	return &tokenplan.TenantPlan{
		ID:          row.ID,
		TenantID:    row.TenantID,
		PlanID:      row.PlanID,
		Enabled:     row.Enabled,
		RetailPrice: row.RetailPrice,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}

func toSubRow(s *tokenplan.Subscription) subRow {
	return subRow{
		ID:             s.ID,
		TenantID:       s.TenantID,
		UserID:         s.UserID,
		PlanID:         s.PlanID,
		PurchasedPrice: s.PurchasedPrice,
		MonthLimitUSD:  s.MonthLimitUSD,
		UsedUSD:        s.UsedUSD,
		Status:         string(s.Status),
		StartAt:        s.StartAt,
		ExpireAt:       s.ExpireAt,
		SourceOrderID:  s.SourceOrderID,
		CreatedAt:      s.CreatedAt,
		UpdatedAt:      s.UpdatedAt,
	}
}

func toSubscription(row *subRow) *tokenplan.Subscription {
	return &tokenplan.Subscription{
		ID:             row.ID,
		TenantID:       row.TenantID,
		UserID:         row.UserID,
		PlanID:         row.PlanID,
		PurchasedPrice: row.PurchasedPrice,
		MonthLimitUSD:  row.MonthLimitUSD,
		UsedUSD:        row.UsedUSD,
		Status:         tokenplan.SubStatus(row.Status),
		StartAt:        row.StartAt,
		ExpireAt:       row.ExpireAt,
		SourceOrderID:  row.SourceOrderID,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}

// validAmount 拒绝 NaN / ±Inf（金额入口防御，复刻 tokenplan 内部同名纯函数）。
func validAmount(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
