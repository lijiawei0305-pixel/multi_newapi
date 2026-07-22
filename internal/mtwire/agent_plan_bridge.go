package mtwire

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/QuantumNous/new-api/internal/agent"
	"github.com/QuantumNous/new-api/internal/agentplan"
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

// 订单激活状态机。pending→activating 的条件 UPDATE 在任何开通副作用前取得持久认领；activated 为
// 完成态。expired 是本地未付判断，但后续可信已付事实可以将其重新认领为 activating。
const (
	agtOrderPending    = "pending"
	agtOrderActivating = "activating"
	agtOrderActivated  = "activated"
	// agtOrderExpired 本地扫描终态：pending 单超时仍未付 / 网关查无此单时停止主动查单；若平台后续
	// 给出可信已付回调，仍可恢复为 activating（正向付款事实优先）。
	agtOrderExpired = "expired"
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

// agentMembershipGrantRow 是会员有效期的逐订单应用台账。order_no 主键保证同一已付订单无论因回调重推、
// 进程崩溃恢复还是多实例重入，都只会把 ValidDays 加到会员到期时间一次。
type agentMembershipGrantRow struct {
	OrderNo   string    `gorm:"column:order_no;primaryKey;type:varchar(64)"`
	TenantID  int64     `gorm:"column:tenant_id;not null;index"`
	ValidDays int       `gorm:"column:valid_days;not null"`
	AppliedAt time.Time `gorm:"column:applied_at"`
}

func (agentMembershipGrantRow) TableName() string { return "mt_agent_membership_grants" }

// migrateAgentPlanBridge 建 AGT 订单、会员与逐订单续期 grant 三张表（由 App.Migrate 调用）。
func migrateAgentPlanBridge(db *gorm.DB) error {
	return db.AutoMigrate(&agentPlanOrderRow{}, &agentMembershipRow{}, &agentMembershipGrantRow{})
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
//	① 取订单；② 反篡改金额校验；③ CAS pending|expired→activating 持久认领；④ 开通/升级代理
//	（create-or-upgrade）+ 按 source order 恰一次累加会员有效期；⑤ CAS activating→activated。
//
// activating 超过 reconcileMinAge 可被新调用方原子退回 pending 后重新认领，恢复认领后崩溃的 saga；
// 会员 grant 台账保证恢复不会重复加有效期。
func (a *App) ActivatePaidAgentPlanOrder(ctx context.Context, orderNo string, paidAmountCNY float64) error {
	if !IsAgentPlanOrderNo(orderNo) {
		return payment.ErrOrderInvalid
	}
	// 先轻量读一次拿 owner,用于按 owner 串行化激活(下方 lockAgentActivation)。
	var ord agentPlanOrderRow
	if err := a.DB.WithContext(ctx).Take(&ord, "order_no = ?", orderNo).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return payment.ErrOrderInvalid
		}
		return err
	}

	// 按 owner 串行化:支付平台常并发重推回调,若两个回调都读到 pending 都进 provision,新代理
	// 会各自建同一 owner 派生的同一 slug 租户(唯一键兜底不产生孤儿,但会撞键报错 + 无谓重试)。
	// 串行后同一 owner 至多一个激活在跑,其余在临界区内重读状态。DB 的 pending→activating CAS 才是
	// 跨实例权威认领；进程锁只减少本实例重复工作。陈旧 activating 由对账恢复，会员 grant 台账兜底
	// 副作用重入不重复续期。锁细节见 activation_lock.go；对账兜底见 agent_plan_reconcile.go。
	unlock := lockAgentActivation(ord.OwnerUserID)
	defer unlock()

	for {
		// 临界区内重读订单：拿锁前可能已被并发赢家推进（业务快照不变，仅状态/更新时间变化）。
		if err := a.DB.WithContext(ctx).Take(&ord, "order_no = ?", orderNo).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return payment.ErrOrderInvalid
			}
			return err
		}
		if ord.Status == agtOrderActivated {
			return nil
		}
		// 反篡改：回传实付须与库内订单一致（<=0 表示已由主动查单确认，跳过比对）。
		if !amountMatchesCNY(paidAmountCNY, ord.AmountCNY) {
			return payment.ErrAmountMismatch
		}

		switch ord.Status {
		case agtOrderActivating:
			// 新鲜认领由另一调用方持有，返回错误让支付平台继续重推。过期认领说明进程可能在副作用
			// 中崩溃：仅一个调用方能把它 CAS 回 pending，随后循环重新执行 pending→activating 认领。
			staleCutoff := time.Now().Add(-reconcileMinAge)
			if !ord.UpdatedAt.Before(staleCutoff) {
				return errors.New("agent plan activation is already in progress")
			}
			res := a.DB.WithContext(ctx).Model(&agentPlanOrderRow{}).
				Where("order_no = ? AND status = ? AND updated_at < ?", orderNo, agtOrderActivating, staleCutoff).
				Updates(map[string]any{"status": agtOrderPending, "updated_at": time.Now()})
			if res.Error != nil {
				return res.Error
			}
			continue
		case agtOrderPending, agtOrderExpired:
			// 可信已付事实可覆盖本地 expired 判断。任何开通副作用前先持久认领；RowsAffected 必须为 1。
			res := a.DB.WithContext(ctx).Model(&agentPlanOrderRow{}).
				Where("order_no = ? AND status = ?", orderNo, ord.Status).
				Updates(map[string]any{"status": agtOrderActivating, "updated_at": time.Now()})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				continue
			}
			ord.Status = agtOrderActivating
		default:
			return payment.ErrOrderInvalid
		}

		// 只有成功取得 pending|expired→activating 认领的调用方才能进入副作用链。
		agentTenantID, err := a.provisionAgentFromOrder(ctx, &ord)
		if err != nil {
			return err // 留 activating；5 分钟后由回调/对账安全恢复
		}

		res := a.DB.WithContext(ctx).Model(&agentPlanOrderRow{}).
			Where("order_no = ? AND status = ?", orderNo, agtOrderActivating).
			Updates(map[string]any{
				"status":          agtOrderActivated,
				"agent_tenant_id": agentTenantID,
				"updated_at":      time.Now(),
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 1 {
			return nil
		}
		// 绝不忽略最终 CAS 失败：只有确认别的恢复者已完成 activated 才能幂等成功。
		var current agentPlanOrderRow
		if err := a.DB.WithContext(ctx).Take(&current, "order_no = ?", orderNo).Error; err != nil {
			return err
		}
		if current.Status == agtOrderActivated {
			return nil
		}
		return errors.New("agent plan activation lost its durable claim")
	}
}

// provisionAgentFromOrder 把订单买家开通/升级为对应档位代理，并写会员台账（含到期）。返回代理租户 id。
//
// 复刻 HandleAdminCreateAgent 的建代理链路（建租户 + owner 归属 + owner tenant_id=0 + SetAgentType +
// EnsureWallet），但对「已是代理」的买家改为**升级**既有租户（只覆盖 Level/CanAPI/DiscountRatio，保留其余
// 管理员配置）。全部按 tenant/owner 幂等，可被回调重试安全重入。
func (a *App) provisionAgentFromOrder(ctx context.Context, ord *agentPlanOrderRow) (int64, error) {
	now := time.Now()

	// 已是代理？→ 升级既有租户。
	tenantID, tstatus, existing, err := a.agentTenantByOwner(ctx, ord.OwnerUserID)
	if err != nil {
		return 0, err
	}
	// 复活自己的旧站：owner 曾被软删的代理租户仍占着 tenants.slug 唯一索引（软删只翻 status=deleted +
	// 删域名记录，租户行/归属/下级/钱包均保留）。若落入下方「新建」分支，normalizeAgentSlug 派生的 slug
	// 必撞该软删占位 → 永久 ErrSlugDuplicate（付款黑洞的「无需用户输入」触发点）。故先翻回 active，
	// 复用「已是代理→升级」分支（自带 GrantLevel≥1 时 EnsureSubdomain 重建被删域名 + SetAgentType +
	// upsertMembership，全幂等）。deleted 是终态、状态机禁 deleted→active；复活是回调激活的确定性重入而
	// 非用户态迁移，故走 repo 级 SetTenantStatus 直写、刻意绕过状态机。
	if !existing {
		delID, hasDeleted, derr := a.agentDeletedTenantByOwner(ctx, ord.OwnerUserID)
		if derr != nil {
			return 0, derr
		}
		if hasDeleted {
			// 翻活前把 owner 自用归位主站——deleted 占位既可能是真旧站（曾完整开通），也可能是购买预留行
			// （precheckAndReserveAgentSlug 插入，从未开通）。tenant_id=0 与初建不变量一致（复刻新建分支），
			// 对真旧站幂等无害。钱包由下方升级分支统一幂等补建（对预留行/半开通行是真建、真旧站 no-op）。
			// 先归位再翻活：中途失败订单仍 pending，对账重驱动安全重入。
			if err := a.DB.WithContext(ctx).Table("users").
				Where("id = ?", ord.OwnerUserID).Update("tenant_id", 0).Error; err != nil {
				return 0, err
			}
			if serr := a.TenantRepo.SetTenantStatus(ctx, delID, tenant.StatusActive); serr != nil {
				return 0, serr
			}
			tenantID, tstatus, existing = delID, string(tenant.StatusActive), true
		}
	}
	if existing {
		// suspended 兜底：升级分支从不翻 status，若为停用态放行会造成「收钱+台账已激活+agent 鉴权仍拒
		// （TenantByOwner 排除 suspended）」三态不一致（C5 软款版付款黑洞）。付款前 precheck 已同构拒单，
		// 此处再兜底对账重驱动的 pending 单，绝不静默升级停用代理。解封须管理员显式执行。
		if tstatus == string(tenant.StatusSuspended) {
			return 0, agentplan.ErrAgentSuspended
		}
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
		// 幂等补建钱包：真升级恒 no-op；对「预留行 revive 翻活」「认领归属后再崩」的半开通行是
		// 唯一补建点（发现#6）——缺它则重试产出无钱包代理，分润无处入账。
		if err := a.AgentRepo.EnsureWallet(ctx, tenantID, ord.OwnerUserID); err != nil {
			return 0, err
		}
		if err := a.upsertMembership(ctx, tenantID, ord, now); err != nil {
			return 0, err
		}
		return tenantID, nil
	}

	// 新代理：建租户 + 归属 + 档位 + 钱包（复刻 admin 建代理）。
	// A positive anchor whose tenant row no longer exists means the unpaid
	// expiry path legitimately released this order's reservation. A trusted
	// late-paid callback still has to deliver value; if another buyer acquired
	// the original slug in that interval, recovery may use an order-derived slug
	// instead of either stealing the new buyer's slug or leaving paid money stuck.
	releasedReservation := false
	if ord.AgentTenantID > 0 {
		var anchorCount int64
		if err := a.DB.WithContext(ctx).Table("tenants").
			Where("id = ? AND owner_user_id = ?", ord.AgentTenantID, ord.OwnerUserID).
			Count(&anchorCount).Error; err != nil {
			return 0, err
		}
		releasedReservation = anchorCount == 0
	}
	slug := normalizeAgentSlug(ord.Slug, ord.OwnerUserID)
	name := ord.Name
	if name == "" {
		name = "代理站 " + slug
	}
	t, err := a.createOrClaimAgentTenant(ctx, ord, slug, name)
	if err != nil && releasedReservation && errors.Is(err, tenant.ErrSlugDuplicate) {
		for attempt := 0; attempt < 16; attempt++ {
			recoverySlug := paidAgentRecoverySlug(ord, attempt)
			t, err = a.createOrClaimAgentTenant(ctx, ord, recoverySlug, name)
			if err == nil {
				break
			}
			if !errors.Is(err, tenant.ErrSlugDuplicate) {
				return 0, err
			}
		}
	}
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
	if err := a.upsertMembership(ctx, t.ID, ord, now); err != nil {
		return 0, err
	}
	return t.ID, nil
}

// createOrClaimAgentTenant creates one agent tenant candidate and recovers the
// precise crash gap where Create succeeded before owner assignment. It never
// claims an active tenant already owned by another user.
func (a *App) createOrClaimAgentTenant(ctx context.Context, ord *agentPlanOrderRow, slug string, name string) (*tenant.Tenant, error) {
	t, err := a.TenantService.Create(ctx, tenant.CreateTenantInput{
		Slug:             slug,
		Name:             name,
		TokenplanEnabled: true,
		SkipSubdomain:    ord.GrantLevel < 1,
	})
	if err == nil || !errors.Is(err, tenant.ErrSlugDuplicate) {
		return t, err
	}
	var row struct{ ID int64 }
	lookupErr := a.DB.WithContext(ctx).Table("tenants").Select("id").
		Where("slug = ? AND owner_user_id = 0 AND status = ?", slug, string(tenant.StatusActive)).
		Take(&row).Error
	if lookupErr != nil {
		if errors.Is(lookupErr, gorm.ErrRecordNotFound) {
			return nil, err
		}
		return nil, lookupErr
	}
	t = &tenant.Tenant{ID: row.ID, Slug: slug}
	if ord.GrantLevel >= 1 {
		if ensureErr := a.TenantService.EnsureSubdomain(ctx, t.ID, slug); ensureErr != nil {
			return nil, ensureErr
		}
	}
	return t, nil
}

func paidAgentRecoverySlug(ord *agentPlanOrderRow, attempt int) string {
	digest := sha256.Sum256([]byte(ord.OrderNo))
	base := fmt.Sprintf("agent%d-%x", ord.OwnerUserID, digest[:6])
	if attempt == 0 {
		return base
	}
	return fmt.Sprintf("%s-%d", base, attempt+1)
}

// upsertMembership 按 source order 恰一次延长会员：先在同一事务插入 order_no 唯一 grant；重复订单
// RowsAffected=0 直接幂等返回。首次应用从 max(now, current_expire_at) 起累加 ValidDays，续费不会覆盖
// 尚未使用的剩余天数；grant 与会员更新任一步失败会一起回滚。
func (a *App) upsertMembership(ctx context.Context, tenantID int64, ord *agentPlanOrderRow, now time.Time) error {
	validDays := ord.ValidDays
	if validDays < 1 {
		validDays = 1
	}
	return a.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		grant := agentMembershipGrantRow{
			OrderNo: ord.OrderNo, TenantID: tenantID, ValidDays: validDays, AppliedAt: now,
		}
		created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&grant)
		if created.Error != nil {
			return created.Error
		}
		if created.RowsAffected == 0 {
			return nil
		}

		var current agentMembershipRow
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&current, "tenant_id = ?", tenantID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tx.Create(&agentMembershipRow{
				TenantID: tenantID, OwnerUserID: ord.OwnerUserID, PlanCode: ord.PlanCode,
				GrantLevel: ord.GrantLevel, GrantCanAPI: ord.GrantCanAPI, SourceOrderNo: ord.OrderNo,
				Status: agtMembershipActive, StartAt: now, ExpireAt: now.AddDate(0, 0, validDays),
				CreatedAt: now, UpdatedAt: now,
			}).Error
		}
		if err != nil {
			return err
		}

		base := now
		startAt := current.StartAt
		if current.ExpireAt.After(now) {
			base = current.ExpireAt
		} else {
			startAt = now
		}
		return tx.Model(&agentMembershipRow{}).Where("tenant_id = ?", tenantID).Updates(map[string]any{
			"owner_user_id":   ord.OwnerUserID,
			"plan_code":       ord.PlanCode,
			"grant_level":     ord.GrantLevel,
			"grant_can_api":   ord.GrantCanAPI,
			"source_order_no": ord.OrderNo,
			"status":          agtMembershipActive,
			"start_at":        startAt,
			"expire_at":       base.AddDate(0, 0, validDays),
			"updated_at":      now,
		}).Error
	})
}

// agentTenantByOwner 找买家名下未删除的代理租户（1:1）；无则 found=false。同时回其 status（active/suspended）——
// 调用方据此区分「active 升级」与「suspended 拒单」（升级分支从不翻 status，若放行 suspended 会收钱却不可交付）。
func (a *App) agentTenantByOwner(ctx context.Context, userID int64) (int64, string, bool, error) {
	var row struct {
		ID     int64
		Status string
	}
	err := a.DB.WithContext(ctx).Table("tenants").Select("id, status").
		Where("owner_user_id = ? AND status <> ?", userID, string(tenant.StatusDeleted)).
		Order("id ASC").Limit(1).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, "", false, nil
	}
	if err != nil {
		return 0, "", false, err
	}
	return row.ID, row.Status, true, nil
}

// agentDeletedTenantByOwner 查 owner 名下**软删**（status=deleted）的代理租户——与 agentTenantByOwner
// 对称但只看 deleted。供付款前预检与激活复活分支识别「该 slug 占位其实是买家自己的旧站」。无则 (0,false,nil)。
func (a *App) agentDeletedTenantByOwner(ctx context.Context, userID int64) (int64, bool, error) {
	var row struct{ ID int64 }
	err := a.DB.WithContext(ctx).Table("tenants").Select("id").
		Where("owner_user_id = ? AND status = ?", userID, string(tenant.StatusDeleted)).
		Order("id ASC").Limit(1).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return row.ID, true, nil
}

// precheckAndReserveAgentSlug 在**下单收钱前**判定这笔购买激活时会不会因 slug 确定性失败，并对全新代理
// **预留 slug**——fail-closed 且由唯一索引仲裁（audit 2026-07-17 发现#5）。非法/保留/占用一律返回
// tenant.ErrSlug*（apperr，respondErr 直出前端 400/409），绝不落 AGT 订单、绝不向平台下单收钱。
//
// 预留式而非纯查重：旧版 Count 是无预留的 check-then-act，窗口=买家整个付款会话（数分钟）——两买家可
// 先后预检同一 slug、双双下单扣款、激活单赢家（败者钱离账一无所获）。现在预检成功即插入一行
// **软删态（status=deleted）tenants 占位行**（owner=买家、归一化 slug），idx_tenants_slug 在钱动之前仲裁：
// 并发同 slug 的第二人 INSERT 撞索引 → 付款前 409。选 deleted 而非新状态（如 pending_payment）是刻意的：
// 占位行与既有软删租户**形状完全一致**——不解析 Host（无域名行 + getActiveTenant 排 deleted）、不参与
// owner 反查鉴权（TenantByOwner 排 deleted）、不被 agentTenantByOwner 视作在营代理，全站按状态过滤的
// 查询天然已处理，零新增泄漏面；且激活可原样复用 revive 分支（agentDeletedTenantByOwner 只认 deleted）。
// 预留生命周期：激活 → revive 翻 active（provisionAgentFromOrder 补完整开通）；未付款 → 对账过期
// expireStuckAgentPlanOrder 释放（releaseAgentSlugReservation，订单 agent_tenant_id 为锚）。
//
//	已是代理(active)          → 升级路径，忽略 slug                    → (0, nil)
//	代理被停用(suspended)      → 付款前拒单（避免收钱后不可交付）        → (0, ErrAgentSuspended)
//	owner 有软删租户           → 复活路径，复用旧 slug（买家自己的）；若该占位正被本人 pending AGT 单
//	                            引用（＝未付预留），回传同一锚点令新单也引用，弃单重试不致提前释放 → (锚点|0, nil)
//	全新代理                  → normalize + 格式/保留词 + 查重（tenants.slug ∪ tenant_domains.domain）
//	                            + 插入预留行（唯一索引仲裁）             → (预留行 id, nil)
func (a *App) precheckAndReserveAgentSlug(ctx context.Context, ownerUserID int64, rawSlug, rawName string) (int64, error) {
	if _, tstatus, existing, err := a.agentTenantByOwner(ctx, ownerUserID); err != nil {
		return 0, err
	} else if existing {
		// suspended：付款前拒单（fail-closed）——被管理员停用的代理不得再付费升级/续费，否则收钱后
		// agent 鉴权仍拒（TenantByOwner 排除 suspended），钱离账却登不进后台。与 provision 升级分支同构。
		if tstatus == string(tenant.StatusSuspended) {
			return 0, agentplan.ErrAgentSuspended
		}
		return 0, nil // active 升级：slug 忽略（维持「已是代理则忽略」契约）
	}
	if delID, hasDeleted, err := a.agentDeletedTenantByOwner(ctx, ownerUserID); err != nil {
		return 0, err
	} else if hasDeleted {
		// 复活：复用买家自己的旧 slug，占位非「他人占用」。若占位是本人未付预留（仍被 pending 单引用），
		// 回传锚点：新单也带上它，释放条件（无任何 pending 单引用，见 releaseAgentSlugReservation）就会
		// 把兄弟单计入——防「先单过期→释放→他人抢注→后单已付撞键」。真旧站无 pending 引用 → 0。
		var n int64
		if err := a.DB.WithContext(ctx).Model(&agentPlanOrderRow{}).
			Where("owner_user_id = ? AND agent_tenant_id = ? AND status IN ?",
				ownerUserID, delID, []string{agtOrderPending, agtOrderActivating}).Count(&n).Error; err != nil {
			return 0, err
		}
		if n > 0 {
			return delID, nil
		}
		return 0, nil
	}
	// 全新代理：归一化后必须通过与 tenant.Create 同源的格式/保留词校验 + 双口径查重
	// （tenants.slug 唯一索引状态盲、软删行仍占位，Count 用 raw Table 不套 gorm 软删 scope，与索引一致；
	// tenant_domains.domain 对齐激活侧 CreateDomain——AddSubdomain 可把任意 label 指给任何租户，slug 空闲
	// 但派生域名被占时激活必撞键，须付款前拒绝。查重先行只为可读错误快路径，权威仲裁是下方 INSERT）。
	slug := normalizeAgentSlug(rawSlug, ownerUserID)
	if err := tenant.NewSlugValidator().Validate(slug); err != nil {
		return 0, err // ErrSlugInvalid / ErrSlugReserved
	}
	var n int64
	if err := a.DB.WithContext(ctx).Table("tenants").
		Where("slug = ?", slug).Count(&n).Error; err != nil {
		return 0, err
	}
	if n == 0 {
		if err := a.DB.WithContext(ctx).Table("tenant_domains").
			Where("domain = ?", tenant.DomainForSlug(slug)).Count(&n).Error; err != nil {
			return 0, err
		}
	}
	if n > 0 {
		return 0, tenant.ErrSlugDuplicate
	}
	// 预留：唯一索引在钱动之前仲裁。并发同 slug 在此撞 idx_tenants_slug → ErrSlugDuplicate（付款前 409）。
	name := rawName
	if name == "" {
		name = "代理站 " + slug // 与 provisionAgentFromOrder 新建分支同一兜底
	}
	resv := &tenant.Tenant{
		Slug:             slug,
		Name:             name,
		Status:           tenant.StatusDeleted,
		TokenplanEnabled: true,
		OwnerUserID:      ownerUserID,
	}
	if err := a.TenantRepo.CreateTenant(ctx, resv); err != nil {
		return 0, err
	}
	return resv.ID, nil
}

// releaseAgentSlugReservation 释放一笔 slug 预留（购买时插入的软删态 tenants 占位行）。单条原子 SQL
// 带双守卫，任何交错下都安全：① `status='deleted'`——预留已被激活（revive 翻 active）则 0 行不删，
// 已释放再删无害（幂等）；② `NOT EXISTS pending 单引用`——同 owner 弃单重试的兄弟单（复活路径回传
// 同锚点）仍 pending 时不释放，其付款后要靠该占位复活，最后一笔引用单过期才真正删除。
// 绝不触碰 tenant_domains（预留从不建域名行）。id<=0（无预留）直接 no-op。
// 只允许以「订单 agent_tenant_id 锚点」调用——绝不按 owner 反查删除，防误删真实软删旧站。
func (a *App) releaseAgentSlugReservation(ctx context.Context, tenantID int64) error {
	return releaseAgentSlugReservationTx(a.DB.WithContext(ctx), tenantID)
}

func releaseAgentSlugReservationTx(tx *gorm.DB, tenantID int64) error {
	if tenantID <= 0 {
		return nil
	}
	return tx.Exec(
		`DELETE FROM tenants WHERE id = ? AND status = ? AND NOT EXISTS (
			SELECT 1 FROM mt_agent_plan_orders WHERE agent_tenant_id = ? AND status IN (?, ?))`,
		tenantID, string(tenant.StatusDeleted), tenantID, agtOrderPending, agtOrderActivating).Error
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
