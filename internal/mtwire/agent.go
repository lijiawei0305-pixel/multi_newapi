package mtwire

// 代理核心闭环（Phase 2）：设代理 → 下级归属 → 分润落账 → 提现审核 —— 在 new-api 基座内的装配与 HTTP 层。
//
// 决策：代理 = User + Tenant 1:1。一个 new-api 用户独占一个租户（tenants.owner_user_id），
// agent_profiles / agent_wallets / agent_earning_logs / agent_withdrawals 一律以 tenant_id 为键。
// 越权防线：代理自助端点的租户一律取自 AgentOwnerAuth 校验过的 ctx，绝不接受客户端传 tenant_id。

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/agent"
	"github.com/QuantumNous/new-api/internal/platform/agenthook"
	"github.com/QuantumNous/new-api/internal/platform/apperr"
	"github.com/QuantumNous/new-api/internal/pricing"
	"github.com/QuantumNous/new-api/internal/promotion"
	"github.com/QuantumNous/new-api/internal/tenant"
	"github.com/QuantumNous/new-api/internal/tokenplan"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// ginKeyAgentTenant 是 AgentOwnerAuth 校验通过后写入的「权威租户 ID」键；代理自助 handler 只认它。
const ginKeyAgentTenant = "mt_agent_tenant"

// 代理端点错误码（沿用模块前缀约定）。
var (
	errAgentForbidden     = apperr.New("AGENT_FORBIDDEN", "无权访问该代理资源", http.StatusForbidden)
	errAgentInputInvalid  = apperr.New("AGENT_INPUT_INVALID", "代理入参非法", http.StatusBadRequest)
	errAgentOwnerTaken    = apperr.New("AGENT_OWNER_TAKEN", "该用户已是其他租户的代理 owner", http.StatusConflict)
	errAgentUserNotFound  = apperr.New("AGENT_USER_NOT_FOUND", "owner 用户不存在", http.StatusBadRequest)
	errAgentTierInvalid   = apperr.New("AGENT_TIER_INVALID", "不允许的用户层级", http.StatusBadRequest)
	errAgentGroupNotModel = apperr.New("AGENT_GROUP_NOT_MODEL", "仅可调整模型分组的倍率", http.StatusBadRequest)
)

// ============================================================================
// 迁移：users.tenant_id（new-api 原生表，幂等 raw ALTER，不改 model.User struct）
// ============================================================================

// migrateUsersTenantID 幂等地给 new-api 原生 users 表加 tenant_id 列 + 索引：
// 先查 information_schema 确认无列才 ALTER（避免重复执行报错），不触碰 new-api 的 model.User
// （加字段会在 upstream rebase 时冲突，见 RETRO「原生表增列」）。下级归属读写均走轻量 Table 查询。
func migrateUsersTenantID(db *gorm.DB) error {
	var count int64
	if err := db.Raw(
		`SELECT COUNT(*) FROM information_schema.columns
		 WHERE table_schema = DATABASE() AND table_name = 'users' AND column_name = 'tenant_id'`,
	).Scan(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil // 已有列：幂等跳过
	}
	return db.Exec(
		`ALTER TABLE users ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 0,
		 ADD INDEX idx_users_tenant (tenant_id)`,
	).Error
}

// migrateUsersPromotionChannelID 幂等地给 new-api 原生 users 表加 promotion_channel_id 列 + 索引
// （= agent_promotion_channels.id；经渠道码注册的用户落此列，0 = 无渠道）。与 tenant_id 同套路：
// 先查 information_schema 确认无列才 ALTER，不触碰 new-api 的 model.User（避免 upstream rebase 冲突）。
func migrateUsersPromotionChannelID(db *gorm.DB) error {
	var count int64
	if err := db.Raw(
		`SELECT COUNT(*) FROM information_schema.columns
		 WHERE table_schema = DATABASE() AND table_name = 'users' AND column_name = 'promotion_channel_id'`,
	).Scan(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil // 已有列：幂等跳过
	}
	return db.Exec(
		`ALTER TABLE users ADD COLUMN promotion_channel_id BIGINT NOT NULL DEFAULT 0,
		 ADD INDEX idx_users_promotion_channel (promotion_channel_id)`,
	).Error
}

// ============================================================================
// 钩子装配：把真实实现注入 agenthook 包级变量（原生 service/controller 旁路调用）
// ============================================================================

// InstallHooks 注入「消耗分润」「注册归属」两个旁路钩子，并装配 2D 倍率钩子（层级×模型分组，§2.15）。
// 由 SetMtRouter 在所有节点调用一次。所有钩子都装：原生 service/controller/计费侧旁路调用，nil 即未装配回退。
func (a *App) InstallHooks() {
	agenthook.ConsumeCommission = a.creditConsumeCommission
	agenthook.AttributeRegistration = a.attributeRegistration
	a.InstallModelGroup2DHook()
}

// attributeRegistration 是 agenthook.AttributeRegistration 实现：把新用户归属到对应代理（租户）。
// 优先级：渠道码 > 注册 Host > 主站根域（均不命中则 tenant_id 保持 0）。
//   - 有渠道码且命中渠道 → UPDATE users SET tenant_id+promotion_channel_id、registered_count+1、落归属记录；
//   - 无码 / 未知码 → 回落按注册 Host 解析租户（仅 UPDATE tenant_id，promotion_channel_id 保持 0）；
//   - 主站根域 / 未知 Host → 不归属。
//
// best-effort：任何失败仅记日志，绝不影响注册主流程。
func (a *App) attributeRegistration(ctx context.Context, host, channelCode string, userID int64) {
	defer func() {
		if r := recover(); r != nil {
			common.SysError("mtwire: attributeRegistration panic recovered")
		}
	}()
	if userID <= 0 {
		return
	}
	// 渠道码优先：命中即归属到渠道所属租户+渠道，不再回落 Host。
	if code := strings.TrimSpace(channelCode); code != "" {
		if a.attributeByChannel(ctx, code, userID) {
			return
		}
		// 未知渠道码：让位给 Host 兜底（不静默丢归属）。
	}
	a.attributeByHost(ctx, host, userID)
}

// attributeByChannel 按渠道码归属：查渠道→UPDATE users(tenant_id,promotion_channel_id)→
// 落归属记录(幂等 by user_id)→registered_count 原子 +1。命中渠道返回 true（调用方据此不再回落 Host）。
// 渠道码未知 / 渠道无效租户返回 false（让位 Host 兜底）。任一写失败仅记日志（best-effort）。
func (a *App) attributeByChannel(ctx context.Context, code string, userID int64) bool {
	if a.PromotionRepo == nil {
		return false
	}
	ch, err := a.PromotionRepo.GetChannelByCode(ctx, code)
	if err != nil || ch == nil || ch.TenantID <= 0 {
		return false // 未知渠道码 / 无效渠道：回落 Host
	}
	// 归属：tenant_id + promotion_channel_id 一次写入（经渠道码注册的权威归属）。
	if err := a.DB.WithContext(ctx).Table("users").
		Where("id = ?", userID).
		Updates(map[string]interface{}{"tenant_id": ch.TenantID, "promotion_channel_id": ch.ID}).Error; err != nil {
		common.SysError("mtwire: attribute user to channel failed: " + err.Error())
		return true // 渠道码已识别：不回落 Host（避免双重归属到不同租户）
	}
	// 归属记录按 user_id 幂等；registered_count 原子 +1。注册天然一次，计数不重复。
	if err := a.PromotionRepo.CreateAttribution(ctx, &promotion.Attribution{
		UserID:      userID,
		TenantID:    ch.TenantID,
		ChannelID:   ch.ID,
		ChannelCode: ch.ChannelCode,
	}); err != nil {
		common.SysError("mtwire: create promotion attribution failed: " + err.Error())
	}
	if err := a.PromotionRepo.IncrRegisteredCount(ctx, ch.ID); err != nil {
		common.SysError("mtwire: incr registered_count failed: " + err.Error())
	}
	return true
}

// attributeByHost 按注册 Host 解析租户并 UPDATE users SET tenant_id（promotion_channel_id 保持 0）。
// 主站根域 / 未知 Host 解析不到则不归属（tenant_id 保持 0）。best-effort。
func (a *App) attributeByHost(ctx context.Context, host string, userID int64) {
	t, err := a.TenantResolver.ResolveByHost(ctx, host)
	if err != nil || t == nil || t.ID <= 0 {
		return // 主站根域 / 未知 Host：归属主站（tenant_id 保持 0）
	}
	if err := a.DB.WithContext(ctx).Table("users").
		Where("id = ?", userID).Update("tenant_id", t.ID).Error; err != nil {
		common.SysError("mtwire: attribute user to tenant failed: " + err.Error())
	}
}

// creditConsumeCommission 是 agenthook.ConsumeCommission 实现：userId→users.tenant_id→agent
// commission_ratio→AddEarning。币种换算 收益¥ = 消耗USD × commission_ratio × USDExchangeRate
// （USD = quotaUnits / QuotaPerUnit）。幂等键 = requestID。best-effort：失败不阻断扣费。
func (a *App) creditConsumeCommission(userID int64, quotaUnits int64, requestID, billingSource string) {
	defer func() {
		if r := recover(); r != nil {
			common.SysError("mtwire: creditConsumeCommission panic recovered")
		}
	}()
	if userID <= 0 || quotaUnits <= 0 || requestID == "" {
		return
	}
	ctx := context.Background()
	tenantID := a.userTenantID(ctx, userID)
	if tenantID <= 0 {
		return // 主站用户 / 未归属：无代理分润
	}
	_, params, found, err := a.AgentRepo.GetAgentType(ctx, tenantID)
	if err != nil || !found || params.CommissionRatio <= 0 {
		return // 该租户未设代理或分润比例为 0
	}
	cny := consumeCommissionCNY(quotaUnits, params.CommissionRatio, operation_setting.USDExchangeRate)
	if cny <= 0 {
		return
	}
	// 钱包桶 → consume_commission；套餐桶 → tokenplan_commission（账目区分；两类均经此单点）。
	source := agent.SourceConsumeCommission
	if billingSource == "subscription" {
		source = agent.SourceTokenplanCommission
	}
	if err := a.AgentEarnings.AddEarning(ctx, agent.EarningEntry{
		TenantID:   tenantID,
		UserID:     userID,
		SourceType: source,
		SourceID:   requestID,
		Amount:     cny,
		Remark:     "consume:" + billingSource,
	}); err != nil {
		common.SysError("mtwire: credit consume commission failed: " + err.Error())
	}
}

// consumeCommissionCNY 计算消耗分润（¥）= 消耗USD × ratio × usdRate，其中 USD = quotaUnits / QuotaPerUnit。
// 任一参数非正返回 0（旁路安全）。提为纯函数便于单测币种换算口径。
func consumeCommissionCNY(quotaUnits int64, ratio, usdRate float64) float64 {
	if quotaUnits <= 0 || ratio <= 0 || usdRate <= 0 {
		return 0
	}
	usd := float64(quotaUnits) / common.QuotaPerUnit
	return usd * ratio * usdRate
}

// userTenantID 轻量直读 users.tenant_id（不经 new-api model.User）；列缺失/查询失败一律给 0。
func (a *App) userTenantID(ctx context.Context, userID int64) int64 {
	var row struct{ TenantID int64 }
	if err := a.DB.WithContext(ctx).Table("users").
		Select("tenant_id").Where("id = ?", userID).Take(&row).Error; err != nil {
		return 0
	}
	return row.TenantID
}

// ============================================================================
// tokenplan 差价收益 → agent 钱包 适配器（替换 wire.go 的 noopEarnings）
// ============================================================================

// tokenplanEarningAdapter 实现 tokenplan.EarningSink，把套餐差价收益转写为 agent.EarningEntry 落账。
// 套餐激活事务内（subscription.go ActivateFromPayment）按 source_order_id 幂等触发，注入即生效。
type tokenplanEarningAdapter struct{ sink agent.EarningSink }

func newTokenplanEarningAdapter(sink agent.EarningSink) *tokenplanEarningAdapter {
	return &tokenplanEarningAdapter{sink: sink}
}

var _ tokenplan.EarningSink = (*tokenplanEarningAdapter)(nil)

func (ad *tokenplanEarningAdapter) AddEarning(ctx context.Context, e tokenplan.EarningEntry) error {
	return ad.sink.AddEarning(ctx, agent.EarningEntry{
		TenantID:   e.TenantID,
		UserID:     e.UserID,
		SourceType: mapTokenplanSource(e.SourceType),
		SourceID:   e.SourceID,
		Amount:     e.Amount,
		Remark:     e.Reference,
	})
}

// mapTokenplanSource 把 tokenplan 收益来源映射到 agent 收益来源（当前同值字符串，显式映射防枚举漂移）。
func mapTokenplanSource(s tokenplan.EarningSource) agent.EarningSource {
	switch s {
	case tokenplan.EarningTokenplanSpread:
		return agent.SourceTokenplanSpread
	default:
		return agent.EarningSource(string(s))
	}
}

// ============================================================================
// agent_owner 鉴权中间件（代理自助端点的权威防线）
// ============================================================================

// AgentOwnerAuth 校验当前 session 用户确为 Host 所指租户的 owner（直读 DB owner_user_id，绕过解析缓存）。
// 通过则把校验过的 tenant_id 写入 ctx（handler 只认它，不接受客户端传 tenant_id）；否则 403 中止。
// 须挂在 TenantMiddleware + new-api UserAuth 之后。
func (a *App) AgentOwnerAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		t := tenantFrom(c)
		if t == nil {
			respondErr(c, tenant.ErrTenantNotFound)
			c.Abort()
			return
		}
		userID := int64(c.GetInt("id"))
		if userID <= 0 {
			respondErr(c, errAgentForbidden)
			c.Abort()
			return
		}
		owner, err := a.TenantRepo.OwnerUserID(c.Request.Context(), t.ID)
		if err != nil {
			respondErr(c, err)
			c.Abort()
			return
		}
		if owner != userID {
			respondErr(c, errAgentForbidden)
			c.Abort()
			return
		}
		c.Set(ginKeyAgentTenant, t.ID)
		c.Next()
	}
}

// agentTenantID 取 AgentOwnerAuth 校验过的租户 ID（0 = 未经校验，handler 应已被中间件挡下）。
func agentTenantID(c *gin.Context) int64 {
	if v, ok := c.Get(ginKeyAgentTenant); ok {
		if id, ok := v.(int64); ok {
			return id
		}
	}
	return 0
}

// isAgentOwner 复用 AgentOwnerAuth 的**权威**判定（Host 解析出的租户 owner_user_id == 当前 session
// 用户 id；直读 DB 绕过解析缓存），但以布尔返回而非中止请求。无租户（主站/未知 Host）/ 未登录 /
// 非 owner / 直读失败一律 false（绝不抛错）。供前端门控信号端点（HandleAgentContext）使用。
func (a *App) isAgentOwner(c *gin.Context) bool {
	t := tenantFrom(c)
	if t == nil {
		return false // 主站 / 未知 Host：无租户即非代理 owner
	}
	userID := int64(c.GetInt("id"))
	if userID <= 0 {
		return false // 未登录
	}
	owner, err := a.TenantRepo.OwnerUserID(c.Request.Context(), t.ID)
	if err != nil {
		return false // 租户不存在 / 查询失败：保守判 false
	}
	return owner == userID
}

// HandleAgentContext GET /api/tenant/agent-context —— 代理身份门控信号。**仅 UserAuth**（不挂
// AgentOwnerAuth），任何登录用户可调；返回当前用户是否为「当前 Host 所指租户」的代理 owner。
// 前端据此隐藏代理自助菜单 + 在路由 beforeLoad 拦截直敲 URL，避免普通用户/别站代理触发
// AGENT_FORBIDDEN。永远 200：无租户/未登录/非 owner → is_agent_owner=false（不 abort）。
func (a *App) HandleAgentContext(c *gin.Context) {
	respondOK(c, gin.H{"is_agent_owner": a.isAgentOwner(c)})
}

// ============================================================================
// DTO（snake_case，对齐 doc/api-contract.md §2.7 与前端 Worker）
// ============================================================================

type agentOut struct {
	ID              int64   `json:"id"` // = tenant_id
	OwnerUserID     int64   `json:"owner_user_id"`
	OwnerUsername   string  `json:"owner_username"`
	Slug            string  `json:"slug"`
	Name            string  `json:"name"`
	Type            string  `json:"type"`
	Level           int     `json:"level"`
	CostPriceCNY    float64 `json:"cost_price_cny"`
	PackageDiscount float64 `json:"package_discount"`
	CommissionRatio float64 `json:"commission_ratio"`
	Status          string  `json:"status"`
	WithdrawableCNY float64 `json:"withdrawable_cny"`
	FrozenCNY       float64 `json:"frozen_cny"`
	TotalEarnedCNY  float64 `json:"total_earned_cny"`
}

type withdrawalOut struct {
	ID         int64   `json:"id"`
	TenantID   int64   `json:"tenant_id"`
	AgentName  string  `json:"agent_name"`
	AmountCNY  float64 `json:"amount_cny"`
	Status     string  `json:"status"`
	CreatedAt  string  `json:"created_at"`
	ReviewedAt string  `json:"reviewed_at"`
}

type earningOut struct {
	SourceType string  `json:"source_type"`
	AmountCNY  float64 `json:"amount_cny"`
	Reference  string  `json:"reference"`
	CreatedAt  string  `json:"created_at"`
}

// agentCreateIn 是 POST /api/admin/agents 入参。
type agentCreateIn struct {
	Slug            string  `json:"slug"`
	Name            string  `json:"name"`
	OwnerUserID     int64   `json:"owner_user_id"`
	Type            string  `json:"type"`
	Level           int     `json:"level"`
	CostPriceCNY    float64 `json:"cost_price_cny"`
	PackageDiscount float64 `json:"package_discount"`
	CommissionRatio float64 `json:"commission_ratio"`
	DiscountFloor   float64 `json:"discount_floor"`
}

// agentPatchIn 是 PATCH /api/admin/agents/:id 入参（指针支持局部更新）。
type agentPatchIn struct {
	Name            *string  `json:"name"`
	Type            *string  `json:"type"`
	Level           *int     `json:"level"`
	CostPriceCNY    *float64 `json:"cost_price_cny"`
	PackageDiscount *float64 `json:"package_discount"`
	CommissionRatio *float64 `json:"commission_ratio"`
	DiscountFloor   *float64 `json:"discount_floor"`
	Status          *string  `json:"status"`
}

// ============================================================================
// 主站管理：设代理 / 列表 / 改代理（AdminAuth；非租户维度）
// ============================================================================

// HandleAdminCreateAgent POST /api/admin/agents —— 设代理。需 AdminAuth。
//
// 流程：① 校验类型/参数/折扣（pricing.Guard，失败即返回，不建租户）；② 校验 owner 用户存在且未占用
// （1:1）；③ 建租户（复用 tenant.Create，自动派生域名）；④ 写 owner_user_id + agent_profile + 钱包。
// 注：③④ 跨仓储非单一 DB 事务（已前置强校验把常见失败挡在建租户前，详见报告「风险/未决」）。
func (a *App) HandleAdminCreateAgent(c *gin.Context) {
	var in agentCreateIn
	if err := c.ShouldBindJSON(&in); err != nil {
		respondErr(c, errAgentInputInvalid)
		return
	}
	at := agent.AgentType(in.Type)
	params := agent.AgentParams{
		CostPrice:       in.CostPriceCNY,
		PackageDiscount: in.PackageDiscount,
		CommissionRatio: in.CommissionRatio,
		Level:           in.Level,
		DiscountFloor:   in.DiscountFloor,
	}
	// ① 前置强校验（纯函数，不写库）：类型 + 参数 + 折扣保护线。
	if !at.Valid() {
		respondErr(c, agent.ErrAgentTypeInvalid)
		return
	}
	if err := params.Validate(); err != nil {
		respondErr(c, err)
		return
	}
	if err := pricing.NewGuard().ValidateGroupRatio(params.PackageDiscount, params.DiscountFloor); err != nil {
		respondErr(c, err)
		return
	}
	ctx := reqCtx(c)
	// ② owner 用户存在性 + 1:1 占用校验。
	if in.OwnerUserID <= 0 || !a.userExists(ctx, in.OwnerUserID) {
		respondErr(c, errAgentUserNotFound)
		return
	}
	if a.ownerTaken(ctx, in.OwnerUserID, 0) {
		respondErr(c, errAgentOwnerTaken)
		return
	}
	// ③ 建租户（复用 tenant.Create：slug 校验 + 派生 <slug>.wedreamhub.com 域名）。
	t, err := a.TenantService.Create(ctx, tenant.CreateTenantInput{
		Slug:             in.Slug,
		Name:             in.Name,
		TokenplanEnabled: true,
	})
	if err != nil {
		respondErr(c, err) // SLUG_INVALID / SLUG_RESERVED / SLUG_DUPLICATE
		return
	}
	// ④ owner 归属 + agent_profile + 钱包。
	if err := a.TenantRepo.SetOwnerUserID(ctx, t.ID, in.OwnerUserID); err != nil {
		respondErr(c, err)
		return
	}
	// owner 自身落「主站基准」(tenant_id=0)：代理 owner 自用按进货价/平台基准，**不落任何代理店**
	// （他设的模型分组加价只对其名下用户生效；见 doc/detailed-design.md §2.15，用户确认 Option B）。
	// 否则 owner 若仍带注册时的 tenant_id（甚至别人的店），自用会错按那家的覆盖计费。
	if err := a.DB.WithContext(ctx).Table("users").Where("id = ?", in.OwnerUserID).Update("tenant_id", 0).Error; err != nil {
		respondErr(c, err)
		return
	}
	if err := a.AgentService.SetAgentType(ctx, t.ID, at, params); err != nil {
		respondErr(c, err)
		return
	}
	if err := a.AgentRepo.EnsureWallet(ctx, t.ID, in.OwnerUserID); err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, a.buildAgentOut(ctx, t.ID, in.OwnerUserID, t.Slug, t.Name, string(t.Status), at, params))
}

// HandleAdminListAgents GET /api/admin/agents —— 代理列表（含 owner 用户名 + 钱包）。需 AdminAuth。
func (a *App) HandleAdminListAgents(c *gin.Context) {
	ctx := reqCtx(c)
	profiles, err := a.AgentRepo.ListProfiles(ctx)
	if err != nil {
		respondErr(c, err)
		return
	}
	ids := make([]int64, 0, len(profiles))
	for _, p := range profiles {
		ids = append(ids, p.UserID)
	}
	names := a.usernamesByIDs(ctx, ids)
	out := make([]agentOut, 0, len(profiles))
	for _, p := range profiles {
		t, terr := a.TenantService.Get(ctx, p.TenantID)
		slug, name, status := "", "", ""
		if terr == nil && t != nil {
			slug, name, status = t.Slug, t.Name, string(t.Status)
		}
		w, _ := a.AgentService.GetWallet(ctx, p.TenantID)
		out = append(out, agentOut{
			ID:              p.TenantID,
			OwnerUserID:     p.UserID,
			OwnerUsername:   names[p.UserID],
			Slug:            slug,
			Name:            name,
			Type:            string(p.Type),
			Level:           p.Level,
			CostPriceCNY:    p.CostPriceCNY,
			PackageDiscount: p.PackageDiscount,
			CommissionRatio: p.CommissionRatio,
			Status:          status,
			WithdrawableCNY: walletField(w, func(x *agent.AgentWallet) float64 { return x.WithdrawableBalance }),
			FrozenCNY:       walletField(w, func(x *agent.AgentWallet) float64 { return x.FrozenWithdrawAmount }),
			TotalEarnedCNY:  walletField(w, func(x *agent.AgentWallet) float64 { return x.TotalEarned }),
		})
	}
	respondOK(c, out)
}

// HandleAdminUpdateAgent PATCH /api/admin/agents/:id —— 改代理（id = tenant_id）。需 AdminAuth。
// 支持局部更新类型/等级/成本/折扣/分润（重过保护线校验）与租户名/状态。
func (a *App) HandleAdminUpdateAgent(c *gin.Context) {
	tenantID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		respondErr(c, errAgentInputInvalid)
		return
	}
	ctx := reqCtx(c)
	t, err := a.TenantService.Get(ctx, tenantID)
	if err != nil {
		respondErr(c, err) // TENANT_NOT_FOUND
		return
	}
	curType, curParams, _, err := a.AgentRepo.GetAgentType(ctx, tenantID)
	if err != nil {
		respondErr(c, err)
		return
	}
	var in agentPatchIn
	if err := c.ShouldBindJSON(&in); err != nil {
		respondErr(c, errAgentInputInvalid)
		return
	}
	if in.Type != nil {
		curType = agent.AgentType(*in.Type)
	}
	if in.Level != nil {
		curParams.Level = *in.Level
	}
	if in.CostPriceCNY != nil {
		curParams.CostPrice = *in.CostPriceCNY
	}
	if in.PackageDiscount != nil {
		curParams.PackageDiscount = *in.PackageDiscount
	}
	if in.CommissionRatio != nil {
		curParams.CommissionRatio = *in.CommissionRatio
	}
	if in.DiscountFloor != nil {
		curParams.DiscountFloor = *in.DiscountFloor
	}
	// SetAgentType 内含类型 + 参数 + 折扣保护线校验（非法即上浮，不落库）。
	if err := a.AgentService.SetAgentType(ctx, tenantID, curType, curParams); err != nil {
		respondErr(c, err)
		return
	}
	// 可选：更新租户名 / 状态。
	if in.Name != nil && *in.Name != "" {
		if err := a.TenantRepo.UpdateName(ctx, tenantID, *in.Name); err != nil {
			respondErr(c, err)
			return
		}
		t.Name = *in.Name
	}
	if in.Status != nil {
		if err := a.TenantService.SetStatus(ctx, tenantID, tenant.TenantStatus(*in.Status)); err != nil {
			respondErr(c, err)
			return
		}
		t.Status = tenant.TenantStatus(*in.Status)
	}
	respondOK(c, a.buildAgentOut(ctx, tenantID, t.OwnerUserID, t.Slug, t.Name, string(t.Status), curType, curParams))
}

// ============================================================================
// 代理自助：提现申请 / 列表 / 收益（AgentOwnerAuth 校验 owner == 当前用户）
// ============================================================================

// HandleAgentRequestWithdrawal POST /api/tenant/withdrawals —— 申请提现（冻结可提现余额）。
// 租户取自 AgentOwnerAuth 校验过的 ctx，绝不接受客户端 tenant_id。
func (a *App) HandleAgentRequestWithdrawal(c *gin.Context) {
	tenantID := agentTenantID(c)
	if tenantID <= 0 {
		respondErr(c, errAgentForbidden)
		return
	}
	var body struct {
		AmountCNY float64 `json:"amount_cny"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		respondErr(c, errAgentInputInvalid)
		return
	}
	wd, err := a.Withdrawals.Request(reqCtx(c), agent.WithdrawInput{
		TenantID: tenantID,
		UserID:   int64(c.GetInt("id")),
		Amount:   body.AmountCNY,
	})
	if err != nil {
		respondErr(c, err) // WITHDRAW_INSUFFICIENT
		return
	}
	respondOK(c, a.toWithdrawalOut(reqCtx(c), *wd))
}

// HandleAgentListWithdrawals GET /api/tenant/withdrawals —— 本代理的提现单（按时间倒序）。
func (a *App) HandleAgentListWithdrawals(c *gin.Context) {
	tenantID := agentTenantID(c)
	if tenantID <= 0 {
		respondErr(c, errAgentForbidden)
		return
	}
	ctx := reqCtx(c)
	rows, err := a.AgentRepo.ListWithdrawalsByTenant(ctx, tenantID)
	if err != nil {
		respondErr(c, err)
		return
	}
	out := make([]withdrawalOut, 0, len(rows))
	for _, w := range rows {
		out = append(out, a.toWithdrawalOut(ctx, w))
	}
	respondOK(c, out)
}

// HandleAgentListEarnings GET /api/tenant/earnings —— 本代理的收益台账（按时间倒序）。
func (a *App) HandleAgentListEarnings(c *gin.Context) {
	tenantID := agentTenantID(c)
	if tenantID <= 0 {
		respondErr(c, errAgentForbidden)
		return
	}
	ctx := reqCtx(c)
	rows, err := a.AgentRepo.ListEarningsByTenant(ctx, tenantID)
	if err != nil {
		respondErr(c, err)
		return
	}
	items := make([]earningOut, 0, len(rows))
	for _, e := range rows {
		items = append(items, earningOut{
			SourceType: string(e.SourceType),
			AmountCNY:  e.Amount,
			Reference:  e.Remark,
			CreatedAt:  isoUTC(e.CreatedAt),
		})
	}
	// 对象形态：顶部汇总卡（可提现/冻结/累计）+ 明细 items（前端 agent-earnings 页消费）。
	w, _ := a.AgentService.GetWallet(ctx, tenantID)
	respondOK(c, gin.H{
		"withdrawable_cny": walletField(w, func(x *agent.AgentWallet) float64 { return x.WithdrawableBalance }),
		"frozen_cny":       walletField(w, func(x *agent.AgentWallet) float64 { return x.FrozenWithdrawAmount }),
		"total_earned_cny": walletField(w, func(x *agent.AgentWallet) float64 { return x.TotalEarned }),
		"items":            items,
	})
}

// ============================================================================
// 主站管理：提现审核（AdminAuth）
// ============================================================================

// HandleAdminListWithdrawals GET /api/admin/withdrawals —— 全部提现单（可选 ?status= 过滤）。需 AdminAuth。
func (a *App) HandleAdminListWithdrawals(c *gin.Context) {
	ctx := reqCtx(c)
	rows, err := a.AgentRepo.ListWithdrawals(ctx, c.Query("status"))
	if err != nil {
		respondErr(c, err)
		return
	}
	out := make([]withdrawalOut, 0, len(rows))
	for _, w := range rows {
		out = append(out, a.toWithdrawalOut(ctx, w))
	}
	respondOK(c, out)
}

// HandleAdminApproveWithdrawal POST /api/admin/withdrawals/:id/approve —— 通过（扣冻结，线下打款）。需 AdminAuth。
func (a *App) HandleAdminApproveWithdrawal(c *gin.Context) {
	a.reviewWithdrawal(c, true)
}

// HandleAdminRejectWithdrawal POST /api/admin/withdrawals/:id/reject —— 拒绝（解冻退回）。需 AdminAuth。
func (a *App) HandleAdminRejectWithdrawal(c *gin.Context) {
	a.reviewWithdrawal(c, false)
}

func (a *App) reviewWithdrawal(c *gin.Context, approve bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		respondErr(c, errAgentInputInvalid)
		return
	}
	var body struct {
		Remark string `json:"remark"`
	}
	_ = c.ShouldBindJSON(&body) // remark 可选
	if err := a.Withdrawals.Review(reqCtx(c), id, approve, body.Remark); err != nil {
		respondErr(c, err) // WITHDRAW_NOT_PENDING / WITHDRAW_NOT_FOUND
		return
	}
	respondOK(c, gin.H{"id": id})
}

// ============================================================================
// 辅助
// ============================================================================

// buildAgentOut 组装单个代理对象（含 owner 用户名 + 钱包）。
func (a *App) buildAgentOut(ctx context.Context, tenantID, ownerUserID int64, slug, name, status string, at agent.AgentType, p agent.AgentParams) agentOut {
	w, _ := a.AgentService.GetWallet(ctx, tenantID)
	return agentOut{
		ID:              tenantID,
		OwnerUserID:     ownerUserID,
		OwnerUsername:   a.usernamesByIDs(ctx, []int64{ownerUserID})[ownerUserID],
		Slug:            slug,
		Name:            name,
		Type:            string(at),
		Level:           p.Level,
		CostPriceCNY:    p.CostPrice,
		PackageDiscount: p.PackageDiscount,
		CommissionRatio: p.CommissionRatio,
		Status:          status,
		WithdrawableCNY: walletField(w, func(x *agent.AgentWallet) float64 { return x.WithdrawableBalance }),
		FrozenCNY:       walletField(w, func(x *agent.AgentWallet) float64 { return x.FrozenWithdrawAmount }),
		TotalEarnedCNY:  walletField(w, func(x *agent.AgentWallet) float64 { return x.TotalEarned }),
	}
}

// toWithdrawalOut 映射提现单（agent_name 取租户名）。
func (a *App) toWithdrawalOut(ctx context.Context, w agent.Withdrawal) withdrawalOut {
	name := ""
	if t, err := a.TenantService.Get(ctx, w.TenantID); err == nil && t != nil {
		name = t.Name
	}
	return withdrawalOut{
		ID:         w.ID,
		TenantID:   w.TenantID,
		AgentName:  name,
		AmountCNY:  w.Amount,
		Status:     string(w.Status),
		CreatedAt:  isoUTC(w.CreatedAt),
		ReviewedAt: isoUTC(w.ReviewedAt),
	}
}

// userExists 轻量校验 new-api 用户存在（不经 model.User）。
func (a *App) userExists(ctx context.Context, userID int64) bool {
	var n int64
	if err := a.DB.WithContext(ctx).Table("users").Where("id = ?", userID).Count(&n).Error; err != nil {
		return false
	}
	return n > 0
}

// ownerTaken 报告某 owner 用户是否已是其他租户的 owner（1:1 占用校验；excludeTenantID 排除自身）。
func (a *App) ownerTaken(ctx context.Context, ownerUserID, excludeTenantID int64) bool {
	var n int64
	q := a.DB.WithContext(ctx).Table("tenants").Where("owner_user_id = ?", ownerUserID)
	if excludeTenantID > 0 {
		q = q.Where("id <> ?", excludeTenantID)
	}
	if err := q.Count(&n).Error; err != nil {
		return false
	}
	return n > 0
}

// walletField 安全取钱包字段（w 可能为 nil）。
func walletField(w *agent.AgentWallet, f func(*agent.AgentWallet) float64) float64 {
	if w == nil {
		return 0
	}
	return f(w)
}
