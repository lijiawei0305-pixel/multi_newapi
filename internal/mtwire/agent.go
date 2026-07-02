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
	"time"

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
	errAgentLevelLocked   = apperr.New("AGENT_LEVEL_LOCKED", "该能力需升级为独立代理后开启", http.StatusForbidden)
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

// migrateAgentProfilesDropType 一次性破坏性迁移：现有代理全部升为独立档（level=1）后删除废弃的 type 列。
// 幂等：以 type 列是否仍存在为一次性信号——列已删即跳过，绝不重复回填（避免每次启动重置 level）。
// 与 migrateUsersTenantID 同套路：information_schema 守卫的 raw MySQL；本地 sqlite 不覆盖，服务器验证。
func migrateAgentProfilesDropType(db *gorm.DB) error {
	var count int64
	if err := db.Raw(
		`SELECT COUNT(*) FROM information_schema.columns
		 WHERE table_schema = DATABASE() AND table_name = 'agent_profiles' AND column_name = 'type'`,
	).Scan(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return nil // type 列已删：一次性迁移已执行，幂等跳过
	}
	// 回填：现有代理全部升为独立档（不拉黑已有站点，spec §3 迁移）。仅当 type 列尚存时执行，故只跑一次。
	if err := db.Exec(`UPDATE agent_profiles SET level = 1`).Error; err != nil {
		return err
	}
	return db.Exec(`ALTER TABLE agent_profiles DROP COLUMN type`).Error
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
	agenthook.ScanUserInput = a.scanUserInputHook // 6e 违禁词：/v1 转发前扫描用户输入
	agenthook.CheckCall = a.checkCallHook         // 7c 风控：/v1 转发前 RPM 限流 + 租户状态
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
	params, found, err := a.AgentRepo.GetAgentType(ctx, tenantID)
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

// userTenantIDStrict 直读 users.tenant_id，区分「用户 tenant_id=0（合法平台用户）」与「查询失败」：
// 失败返回 error，供建单等需要严格归属的场景拒绝，而非静默落为平台工单（tenant_id=0）。
func (a *App) userTenantIDStrict(ctx context.Context, userID int64) (int64, error) {
	var row struct{ TenantID int64 }
	if err := a.DB.WithContext(ctx).Table("users").
		Select("tenant_id").Where("id = ?", userID).Take(&row).Error; err != nil {
		return 0, err
	}
	return row.TenantID, nil
}

// userTenantID 轻量直读 users.tenant_id（不经 new-api model.User）；列缺失/查询失败一律给 0（旁路安全，非严格场景用）。
func (a *App) userTenantID(ctx context.Context, userID int64) int64 {
	tid, _ := a.userTenantIDStrict(ctx, userID)
	return tid
}

// moderationTenantID 解析「内容审核归属租户」：普通用户按自身 tenant_id；
// 站长(代理 owner)自身 tenant_id 多为 0/主租户，但其违规应归到「拥有的代理租户」——
// 这样代理后台「我的违规日志」可见、且套用该租户词库。仅在 tenant_id==0 时回查 owner_user_id（省热路径一次查询）。
func (a *App) moderationTenantID(ctx context.Context, userID int64) int64 {
	tid := a.userTenantID(ctx, userID)
	if tid != 0 {
		return tid
	}
	var owned struct{ ID int64 }
	if err := a.DB.WithContext(ctx).Table("tenants").
		Select("id").Where("owner_user_id = ?", userID).Take(&owned).Error; err == nil && owned.ID > 0 {
		return owned.ID
	}
	return 0
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

// AgentOwnerAuthByUser 是代理自助端点的 owner-based 权威防线：从登录用户「拥有的租户」
// （tenants.owner_user_id == 当前 session 用户，1:1）解析 agentTenantID，与 Host 无关——
// 故 L0 无子域名也能在主站访问自己的控制台，L1 在子域名同样解析到自己的租户。
// 安全不变量：只解析到「当前用户拥有的」那一个租户；绝不接受客户端传 tenant_id；
// 未登录 / 不拥有任何租户 → 403 中止（不放行）。须挂在 new-api UserAuth 之后。
// 替代 agent-self 组原先的 Host-based AgentOwnerAuth（后者留作 HandleAgentContext 的 Host 判定，不删）。
func (a *App) AgentOwnerAuthByUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := int64(c.GetInt("id"))
		if userID <= 0 {
			respondErr(c, errAgentForbidden)
			c.Abort()
			return
		}
		t, err := a.TenantRepo.TenantByOwner(c.Request.Context(), userID)
		if err != nil {
			// 该用户不拥有任何代理租户（含 ErrTenantNotFound）→ 403，绝不放行、绝不回退 Host。
			respondErr(c, errAgentForbidden)
			c.Abort()
			return
		}
		c.Set(ginKeyAgentTenant, t.ID) // handler 只认这个已校验的租户 ID
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

// ensureAgentLevel 纵深校验当前代理租户档位 ≥ min：不足以 AGENT_LEVEL_LOCKED 响应并返回 false。
// 用于路由中间件（RequireAgentLevel）与 handler 入口双保险（spec §5.2.3 纵深）。
func (a *App) ensureAgentLevel(c *gin.Context, min int) bool {
	tenantID := agentTenantID(c)
	if tenantID <= 0 {
		respondErr(c, errAgentForbidden)
		return false
	}
	lvl, err := a.AgentService.AgentLevel(c.Request.Context(), tenantID)
	if err != nil {
		respondErr(c, err)
		return false
	}
	if lvl < min {
		respondErr(c, errAgentLevelLocked)
		return false
	}
	return true
}

// RequireAgentLevel 是「独立能力」路由门禁：须挂在 AgentOwnerAuth 之后（依赖其写入的 agentTenantID）。
func (a *App) RequireAgentLevel(min int) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !a.ensureAgentLevel(c, min) {
			c.Abort()
			return
		}
		c.Next()
	}
}

// callerOwnedTenant 返回当前 session 用户拥有的租户（owner-based：TenantByOwner，与 Host 无关，
// 镜像 AgentOwnerAuthByUser 的解析口径）——L0 无子域名、请求打在主站 Host 上时也能命中自己的租户。
// 未登录 / 不拥有任何租户（含 ErrTenantNotFound）/ 查询失败一律 nil（绝不抛错，调用方保守判非 owner）。
func (a *App) callerOwnedTenant(c *gin.Context) *tenant.Tenant {
	userID := int64(c.GetInt("id"))
	if userID <= 0 {
		return nil // 未登录
	}
	t, err := a.TenantRepo.TenantByOwner(c.Request.Context(), userID)
	if err != nil {
		return nil // 不拥有任何租户 / 查询失败：保守判 false
	}
	return t
}

// isAgentOwner 判定当前 session 用户是否拥有某个代理租户（owner-based、Host 无关，见
// callerOwnedTenant）。供前端门控信号端点（HandleAgentContext）使用。
func (a *App) isAgentOwner(c *gin.Context) bool {
	return a.callerOwnedTenant(c) != nil
}

// HandleAgentContext GET /api/tenant/agent-context —— 代理身份门控信号。**仅 UserAuth**（不挂
// AgentOwnerAuth/AgentOwnerAuthByUser），任何登录用户可调；返回当前用户是否拥有某个代理租户
// ——owner-based 解析（TenantByOwner），与 Host 无关：L0 无子域名、请求打在主站 Host 上也必须能
// 命中，前端代理自助 UI（侧栏 + 10 处路由守卫）才对 L0 可达（Fix 1：此前用 Host 租户判定，L0 因无
// 子域名而永远 false，UI 不可达）。前端据此隐藏代理自助菜单 + 在路由 beforeLoad 拦截直敲 URL，避免
// 普通用户/别站代理触发 AGENT_FORBIDDEN。永远 200：未登录/非 owner/查询失败 → is_agent_owner=false
// （不 abort）。
func (a *App) HandleAgentContext(c *gin.Context) {
	out := agentContextOut{}
	if t := a.callerOwnedTenant(c); t != nil {
		out.IsAgentOwner = true
		if p, found, err := a.AgentRepo.GetAgentType(c.Request.Context(), t.ID); err == nil && found {
			out.Level = p.Level
			out.CanAPI = p.CanAPI
		}
	}
	respondOK(c, out)
}

// ============================================================================
// DTO（snake_case，对齐 doc/api-contract.md §2.7 与前端 Worker）
// ============================================================================

// agentContextOut 是 GET /api/tenant/agent-context 响应：前端据此隐藏菜单 + 路由守卫 gate。
type agentContextOut struct {
	IsAgentOwner bool `json:"is_agent_owner"`
	Level        int  `json:"level"`
	CanAPI       bool `json:"can_api"`
}

type agentOut struct {
	ID              int64   `json:"id"` // = tenant_id
	OwnerUserID     int64   `json:"owner_user_id"`
	OwnerUsername   string  `json:"owner_username"`
	Slug            string  `json:"slug"`
	Name            string  `json:"name"`
	Level           int     `json:"level"`
	CanAPI          bool    `json:"can_api"`
	CostPriceCNY    float64 `json:"cost_price_cny"`
	PackageDiscount float64 `json:"package_discount"`
	CommissionRatio float64 `json:"commission_ratio"`
	Status          string  `json:"status"`
	WithdrawableCNY float64 `json:"withdrawable_cny"`
	FrozenCNY       float64 `json:"frozen_cny"`
	TotalEarnedCNY  float64 `json:"total_earned_cny"`
}

// agentMetricsOut 是 GET /api/admin/agents/:id/metrics 响应：代理升档决策的只读指标
// （总充值 / 累计分润 / 下级用户数），复用 reportrepo 财务聚合 + 一条下级计数薄查询。
type agentMetricsOut struct {
	RechargeTotalCNY    float64 `json:"recharge_total_cny"`
	CommissionEarnedCNY float64 `json:"commission_earned_cny"`
	DownstreamUserCount int64   `json:"downstream_user_count"`
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
	Level           int     `json:"level"`
	CostPriceCNY    float64 `json:"cost_price_cny"`
	PackageDiscount float64 `json:"package_discount"`
	CommissionRatio float64 `json:"commission_ratio"`
	DiscountFloor   float64 `json:"discount_floor"`
}

// agentPatchIn 是 PATCH /api/admin/agents/:id 入参（指针支持局部更新）。
type agentPatchIn struct {
	Name            *string  `json:"name"`
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
// 流程：① 校验参数/折扣（pricing.Guard，失败即返回，不建租户）；② 校验 owner 用户存在且未占用
// （1:1）；③ 建租户（复用 tenant.Create，自动派生域名）；④ 写 owner_user_id + agent_profile + 钱包。
// 注：③④ 跨仓储非单一 DB 事务（已前置强校验把常见失败挡在建租户前，详见报告「风险/未决」）。
func (a *App) HandleAdminCreateAgent(c *gin.Context) {
	var in agentCreateIn
	if err := c.ShouldBindJSON(&in); err != nil {
		respondErr(c, errAgentInputInvalid)
		return
	}
	params := agent.AgentParams{
		CostPrice:       in.CostPriceCNY,
		PackageDiscount: in.PackageDiscount,
		CommissionRatio: in.CommissionRatio,
		Level:           in.Level,
		DiscountFloor:   in.DiscountFloor,
	}
	// ① 前置强校验（纯函数，不写库）：参数 + 折扣保护线。
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
		SkipSubdomain:    params.Level < 1, // L0 普通：不发子域名（spec §5.2.2）
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
	if err := a.AgentService.SetAgentType(ctx, t.ID, params); err != nil {
		respondErr(c, err)
		return
	}
	if err := a.AgentRepo.EnsureWallet(ctx, t.ID, in.OwnerUserID); err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, a.buildAgentOut(ctx, t.ID, in.OwnerUserID, t.Slug, t.Name, string(t.Status), params))
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
			Level:           p.Level,
			CanAPI:          p.CanAPI,
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
	curParams, _, err := a.AgentRepo.GetAgentType(ctx, tenantID)
	if err != nil {
		respondErr(c, err)
		return
	}
	var in agentPatchIn
	if err := c.ShouldBindJSON(&in); err != nil {
		respondErr(c, errAgentInputInvalid)
		return
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
	// 升档 → 独立档：先幂等派生子域名 `<slug>.wedreamhub.com`（resolver 不缓存负结果，无需失效缓存），
	// 成功后才落 level（原子性：EnsureSubdomain 失败绝不能让代理停在「level=1 但无子域名」——那会
	// 解锁独立档自助能力却没有可用站点，见复盘 Fix 3）。EnsureSubdomain 本身幂等，对已是 L1 的代理
	// 重复调用无副作用，故重排序对既有（已是 L1 / 不升档）流程安全。
	if in.Level != nil && *in.Level >= 1 {
		if err := a.TenantService.EnsureSubdomain(ctx, tenantID, t.Slug); err != nil {
			respondErr(c, err)
			return
		}
	}
	// SetAgentType 内含参数 + 折扣保护线校验（非法即上浮，不落库）。
	if err := a.AgentService.SetAgentType(ctx, tenantID, curParams); err != nil {
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
	respondOK(c, a.buildAgentOut(ctx, tenantID, t.OwnerUserID, t.Slug, t.Name, string(t.Status), curParams))
}

// HandleAdminAgentMetrics GET /api/admin/agents/:id/metrics —— 代理升档决策指标（需 AdminAuth；id=tenant_id）。
// 只读复用 reportrepo：总充值=RechargePaid([1,now] 求和)；分润收益=WalletTotals.TotalEarnedCNY（累计）；
// 下级用户数=CountTenantUsers（薄查询）。绝不接受客户端传除 :id 外的任何口径。
func (a *App) HandleAdminAgentMetrics(c *gin.Context) {
	tenantID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || tenantID <= 0 {
		respondErr(c, errAgentInputInvalid)
		return
	}
	ctx := reqCtx(c)
	// 总充值：生命周期窗口 [1, now] 上复用 RechargePaid（单租户 map 至多一条，求和即总额）。
	rechargeMap, err := a.ReportRepo.RechargePaid(ctx, &tenantID, 1, time.Now().Unix())
	if err != nil {
		respondErr(c, err)
		return
	}
	var rechargeTotal float64
	for _, v := range rechargeMap {
		rechargeTotal += v
	}
	// 分润收益：钱包累计已赚（生命周期；缺行返回零值不报错）。
	wallet, err := a.ReportRepo.WalletTotals(ctx, &tenantID)
	if err != nil {
		respondErr(c, err)
		return
	}
	// 下级用户数：薄计数查询（tenant_id=? AND deleted_at IS NULL）。
	userCount, err := a.ReportRepo.CountTenantUsers(ctx, tenantID)
	if err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, agentMetricsOut{
		RechargeTotalCNY:    round2(rechargeTotal),
		CommissionEarnedCNY: round2(wallet.TotalEarnedCNY),
		DownstreamUserCount: userCount,
	})
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
func (a *App) buildAgentOut(ctx context.Context, tenantID, ownerUserID int64, slug, name, status string, p agent.AgentParams) agentOut {
	w, _ := a.AgentService.GetWallet(ctx, tenantID)
	return agentOut{
		ID:              tenantID,
		OwnerUserID:     ownerUserID,
		OwnerUsername:   a.usernamesByIDs(ctx, []int64{ownerUserID})[ownerUserID],
		Slug:            slug,
		Name:            name,
		Level:           p.Level,
		CanAPI:          p.CanAPI,
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
