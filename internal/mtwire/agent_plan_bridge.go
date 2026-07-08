package mtwire

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/QuantumNous/new-api/internal/agent"
	"github.com/QuantumNous/new-api/internal/payment"
	"github.com/QuantumNous/new-api/internal/tenant"
)

// ============================================================================
// P3 —— 购买代理套餐（一次性 + 有效期）
//
// 用户在控制台购买一档代理套餐 → 下 AGT 订单 → 走 realpay 出支付凭据 → 平台异步回调
// （payment_inprocess.go 按前缀分发：AGT→ActivatePaidAgentPlanOrder）→ 激活：把买家「开通/升级」为
// 对应档位代理（create-or-upgrade，复刻 HandleAdminCreateAgent 的建租户/归属/档位/钱包），并记一条
// mt_agent_memberships（含到期时间），供 P4 定时任务到期降级。
//
// 与 tokenplan(SUB) 的区别：AGT 激活不建原生订阅额度桶，而是写 agent 档位 + 到期台账。
// 订单/表均 mt_ 前缀、AGT 订单号前缀，与 SUB/RCG 分开存、分开分发。
// ============================================================================

// AgentPlanOrderPrefix 是代理套餐订单号前缀。支付回调据此分发到 ActivatePaidAgentPlanOrder。
const AgentPlanOrderPrefix = "AGT"

// 订单激活状态机（最简两态）。
const (
	agtOrderPending   = "pending"
	agtOrderActivated = "activated"
)

// 会员状态。
const (
	agtMembershipActive  = "active"
	agtMembershipExpired = "expired"
)

// IsAgentPlanOrderNo 报告订单号是否为代理套餐订单（AGT 前缀）。
func IsAgentPlanOrderNo(orderNo string) bool {
	return strings.HasPrefix(orderNo, AgentPlanOrderPrefix)
}

// ---- 表 1：mt_agent_plan_orders —— 代理套餐待支付订单 + 激活状态机 + 授予快照 ----
//
// 快照 grant_*/valid_days/slug/name → 激活自包含（不依赖套餐定义激活时是否被改）。
type agentPlanOrderRow struct {
	OrderNo            string    `gorm:"column:order_no;primaryKey;type:varchar(64)"`
	OwnerUserID        int64     `gorm:"column:owner_user_id;not null;index"` // 买家（成为代理 owner）
	PlanID             int64     `gorm:"column:plan_id;not null"`
	AmountCNY          float64   `gorm:"column:amount_cny;type:decimal(20,2);not null;default:0"`
	Provider           string    `gorm:"column:provider;type:varchar(16);not null;default:''"`
	Status             string    `gorm:"column:status;type:varchar(16);not null;default:pending;index"`
	GrantLevel         int       `gorm:"column:grant_level;not null;default:0"`
	GrantCanAPI        bool      `gorm:"column:grant_can_api;not null;default:false"`
	GrantDiscountRatio float64   `gorm:"column:grant_discount_ratio;type:decimal(10,4);not null;default:0"`
	ValidDays          int       `gorm:"column:valid_days;not null;default:365"`
	Slug               string    `gorm:"column:slug;type:varchar(64)"`              // 新建代理租户用（子域名/标识）
	Name               string    `gorm:"column:name;type:varchar(64)"`              // 站点名
	PlanCode           string    `gorm:"column:plan_code;type:varchar(32)"`         // 会员台账用
	AgentTenantID      int64     `gorm:"column:agent_tenant_id;not null;default:0"` // 激活回填：开通/升级的代理租户
	CreatedAt          time.Time `gorm:"column:created_at"`
	UpdatedAt          time.Time `gorm:"column:updated_at"`
}

// TableName 固定表名。
func (agentPlanOrderRow) TableName() string { return "mt_agent_plan_orders" }

// ---- 表 2：mt_agent_memberships —— 代理会员台账（到期降级依据；每代理租户一条 active） ----
type agentMembershipRow struct {
	TenantID      int64     `gorm:"column:tenant_id;primaryKey"` // 代理租户（一租户一条）
	OwnerUserID   int64     `gorm:"column:owner_user_id;not null;index"`
	PlanCode      string    `gorm:"column:plan_code;type:varchar(32)"`
	GrantLevel    int       `gorm:"column:grant_level;not null;default:0"`
	GrantCanAPI   bool      `gorm:"column:grant_can_api;not null;default:false"`
	SourceOrderNo string    `gorm:"column:source_order_no;type:varchar(64)"`
	Status        string    `gorm:"column:status;type:varchar(16);not null;default:active;index:idx_agent_memb_status_expire,priority:1"`
	StartAt       time.Time `gorm:"column:start_at"`
	ExpireAt      time.Time `gorm:"column:expire_at;index:idx_agent_memb_status_expire,priority:2"`
	CreatedAt     time.Time `gorm:"column:created_at"`
	UpdatedAt     time.Time `gorm:"column:updated_at"`
}

// TableName 固定表名。
func (agentMembershipRow) TableName() string { return "mt_agent_memberships" }

// migrateAgentPlanBridge 建 AGT 两张表（由 App.Migrate 调用）。
func migrateAgentPlanBridge(db *gorm.DB) error {
	return db.AutoMigrate(&agentPlanOrderRow{}, &agentMembershipRow{})
}

// ---- 订单存储 ----

type agentPlanOrderStore struct{ db *gorm.DB }

func newAgentPlanOrderStore(db *gorm.DB) *agentPlanOrderStore { return &agentPlanOrderStore{db: db} }

func (s *agentPlanOrderStore) create(ctx context.Context, row *agentPlanOrderRow) error {
	return s.db.WithContext(ctx).Create(row).Error
}

// ---- 激活（支付成功后）----

// notifyActivateAgentPlan 激活一笔已支付的 AGT 代理套餐订单（幂等）；单测可替换为计数桩。
var notifyActivateAgentPlan = func(a *App, ctx context.Context, orderNo string, paidCNY float64) error {
	return a.ActivatePaidAgentPlanOrder(ctx, orderNo, paidCNY)
}

// ActivatePaidAgentPlanOrder 支付成功后激活一笔代理套餐订单（供回调按 AGT 前缀分发）：
//
//	① 取订单（mt_agent_plan_orders，pending）；② 反篡改金额校验；③ 开通/升级代理（create-or-upgrade）
//	+ 写会员台账（含到期）；④ CAS pending→activated（幂等：已 activated 直接返回 nil）。
//
// 幂等：③ provision 全部按 tenant/owner upsert 幂等；④ CAS 保证会员台账不重复起算有效期。
func (a *App) ActivatePaidAgentPlanOrder(ctx context.Context, orderNo string, paidAmountCNY float64) error {
	if !IsAgentPlanOrderNo(orderNo) {
		return payment.ErrOrderInvalid
	}
	var ord agentPlanOrderRow
	if err := a.DB.WithContext(ctx).Take(&ord, "order_no = ?", orderNo).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return payment.ErrOrderInvalid
		}
		return err
	}
	if ord.Status == agtOrderActivated {
		return nil // 幂等：已激活
	}
	if ord.Status != agtOrderPending {
		return payment.ErrOrderInvalid
	}
	// 反篡改：回传实付须与库内订单一致（<=0 表示对账兜底查单未提供 → 跳过比对）。
	if !amountMatchesCNY(paidAmountCNY, ord.AmountCNY) {
		return payment.ErrAmountMismatch
	}

	// ③ 开通/升级代理 + 写会员台账（幂等）。
	agentTenantID, err := a.provisionAgentFromOrder(ctx, &ord)
	if err != nil {
		return err
	}

	// ④ CAS pending→activated，回填代理租户。
	res := a.DB.WithContext(ctx).Model(&agentPlanOrderRow{}).
		Where("order_no = ? AND status = ?", orderNo, agtOrderPending).
		Updates(map[string]any{
			"status":          agtOrderActivated,
			"agent_tenant_id": agentTenantID,
			"updated_at":      time.Now(),
		})
	if res.Error != nil {
		return res.Error
	}
	return nil
}

// provisionAgentFromOrder 把订单买家开通/升级为对应档位代理，并写会员台账（含到期）。返回代理租户 id。
//
// 复刻 HandleAdminCreateAgent 的建代理链路（建租户 + owner 归属 + owner tenant_id=0 + SetAgentType +
// EnsureWallet），但对「已是代理」的买家改为**升级**既有租户（只覆盖 Level/CanAPI/DiscountRatio，保留其余
// 管理员配置）。全部按 tenant/owner 幂等，可被回调重试安全重入。
func (a *App) provisionAgentFromOrder(ctx context.Context, ord *agentPlanOrderRow) (int64, error) {
	now := time.Now()
	expireAt := now.AddDate(0, 0, maxInt(ord.ValidDays, 1))

	// 已是代理？→ 升级既有租户。
	tenantID, existing, err := a.agentTenantByOwner(ctx, ord.OwnerUserID)
	if err != nil {
		return 0, err
	}
	if existing {
		params, found, gerr := a.AgentRepo.GetAgentType(ctx, tenantID)
		if gerr != nil {
			return 0, gerr
		}
		if !found {
			params = agent.AgentParams{UserID: ord.OwnerUserID, PackageDiscount: 1.0}
		}
		prevLevel := params.Level
		// 升独立档基建（镜像 HandleAdminUpdateAgent 促升序列，原子性教训见 agent.go Fix 3 注释）：
		// GrantLevel≥1 先幂等派生子域名——用租户**既有 slug**（ord.Slug 对升级维持「已是代理则忽略」
		// 契约），失败则整体失败、level 绝不落库（不得留下「付费升独立却没有站」，2026-07-08 用户报
		// gap）。仅「真促升」（此前 level<1）才作废推广渠道；**年度续费（已是 L1 再购）不得再作废**
		// ——否则每年续费都打断代理正在用的邀请链接（购买路径与 admin PATCH 语义的关键差异）。
		if ord.GrantLevel >= 1 {
			tn, terr := a.TenantService.Get(ctx, tenantID)
			if terr != nil {
				return 0, terr
			}
			if err := a.TenantService.EnsureSubdomain(ctx, tenantID, tn.Slug); err != nil {
				return 0, err
			}
			if prevLevel < 1 {
				if err := a.Promotion.VoidChannelsByTenant(ctx, tenantID); err != nil {
					return 0, err
				}
			}
		}
		params.UserID = ord.OwnerUserID
		params.Level = ord.GrantLevel
		params.CanAPI = ord.GrantCanAPI
		if ord.GrantDiscountRatio > 0 {
			params.DiscountRatio = ord.GrantDiscountRatio
		}
		if err := a.AgentService.SetAgentType(ctx, tenantID, params); err != nil {
			return 0, err
		}
		if err := a.upsertMembership(ctx, tenantID, ord, now, expireAt); err != nil {
			return 0, err
		}
		return tenantID, nil
	}

	// 新代理：建租户 + 归属 + 档位 + 钱包（复刻 admin 建代理）。
	slug := normalizeAgentSlug(ord.Slug, ord.OwnerUserID)
	name := ord.Name
	if name == "" {
		name = "代理站 " + slug
	}
	t, err := a.TenantService.Create(ctx, tenant.CreateTenantInput{
		Slug:             slug,
		Name:             name,
		TokenplanEnabled: true,
		SkipSubdomain:    ord.GrantLevel < 1, // L0 普通代理不发子域名
	})
	if err != nil {
		return 0, err
	}
	if err := a.TenantRepo.SetOwnerUserID(ctx, t.ID, ord.OwnerUserID); err != nil {
		return 0, err
	}
	// owner 自用落主站基准（tenant_id=0），与 admin 建代理一致，避免自用错按某店覆盖计费。
	if err := a.DB.WithContext(ctx).Table("users").
		Where("id = ?", ord.OwnerUserID).Update("tenant_id", 0).Error; err != nil {
		return 0, err
	}
	params := agent.AgentParams{
		UserID:          ord.OwnerUserID,
		PackageDiscount: 1.0, // 无套餐折扣（管理员可后配）
		Level:           ord.GrantLevel,
		CanAPI:          ord.GrantCanAPI,
		DiscountRatio:   ord.GrantDiscountRatio,
	}
	if err := a.AgentService.SetAgentType(ctx, t.ID, params); err != nil {
		return 0, err
	}
	if err := a.AgentRepo.EnsureWallet(ctx, t.ID, ord.OwnerUserID); err != nil {
		return 0, err
	}
	if err := a.upsertMembership(ctx, t.ID, ord, now, expireAt); err != nil {
		return 0, err
	}
	return t.ID, nil
}

// upsertMembership 幂等写/更新代理会员台账（一租户一条 active，含到期时间；供 P4 到期降级）。
func (a *App) upsertMembership(ctx context.Context, tenantID int64, ord *agentPlanOrderRow, start, expire time.Time) error {
	row := agentMembershipRow{
		TenantID:      tenantID,
		OwnerUserID:   ord.OwnerUserID,
		PlanCode:      ord.PlanCode,
		GrantLevel:    ord.GrantLevel,
		GrantCanAPI:   ord.GrantCanAPI,
		SourceOrderNo: ord.OrderNo,
		Status:        agtMembershipActive,
		StartAt:       start,
		ExpireAt:      expire,
		CreatedAt:     start,
		UpdatedAt:     start,
	}
	return a.DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "tenant_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"plan_code":       ord.PlanCode,
			"grant_level":     ord.GrantLevel,
			"grant_can_api":   ord.GrantCanAPI,
			"source_order_no": ord.OrderNo,
			"status":          agtMembershipActive,
			"start_at":        start,
			"expire_at":       expire,
			"updated_at":      start,
		}),
	}).Create(&row).Error
}

// agentTenantByOwner 找买家名下未删除的代理租户（1:1）；无则 found=false。
func (a *App) agentTenantByOwner(ctx context.Context, userID int64) (int64, bool, error) {
	var row struct{ ID int64 }
	err := a.DB.WithContext(ctx).Table("tenants").Select("id").
		Where("owner_user_id = ? AND status <> ?", userID, string(tenant.StatusDeleted)).
		Order("id ASC").Limit(1).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return row.ID, true, nil
}

// normalizeAgentSlug 规整/兜底代理子域名 slug：非空取原值（tenant.Create 会再校验格式/查重/保留词），
// 空则派生 `agent<userID>`（自助购买未填时的确定性兜底）。
func normalizeAgentSlug(slug string, userID int64) string {
	s := strings.TrimSpace(strings.ToLower(slug))
	if s == "" {
		return "agent" + strconv.FormatInt(userID, 10)
	}
	return s
}

// maxInt 返回两者较大值。
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
