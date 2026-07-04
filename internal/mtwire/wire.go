// Package mtwire 是「多租户增量」在 new-api 基座内的装配层（Phase 2）。
//
// 设计要点：
//   - 复用 new-api 的共享 *gorm.DB（model.DB），**不自开 gorm.Open**；
//   - 在 new-api InitDB 之后对我们的增量表做 AutoMigrate（Migrate）；
//   - 把 internal/tenant 与 internal/tokenplan 两个模块组装成可被 gin 复用的 App；
//   - 鉴权复用 new-api（UserAuth/AdminAuth，见 router/mt-router.go），租户来自 Host 中间件。
//
// 仅新增、不改 new-api 既有业务。真实 payment/risk/agent 集成顺延（见本文件下方占位适配与报告风险）。
package mtwire

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/agent"
	agentrepo "github.com/QuantumNous/new-api/internal/agent/gormrepo"
	"github.com/QuantumNous/new-api/internal/agentplan"
	agentplanrepo "github.com/QuantumNous/new-api/internal/agentplan/gormrepo"
	"github.com/QuantumNous/new-api/internal/modelgroup"
	"github.com/QuantumNous/new-api/internal/moderation"
	moderationrepo "github.com/QuantumNous/new-api/internal/moderation/gormrepo"
	"github.com/QuantumNous/new-api/internal/payment"
	paymentrepo "github.com/QuantumNous/new-api/internal/payment/gormrepo"
	"github.com/QuantumNous/new-api/internal/pricing"
	"github.com/QuantumNous/new-api/internal/promotion"
	promotionrepo "github.com/QuantumNous/new-api/internal/promotion/gormrepo"
	"github.com/QuantumNous/new-api/internal/report/reportrepo"
	"github.com/QuantumNous/new-api/internal/risk"
	"github.com/QuantumNous/new-api/internal/siteconfig"
	siteconfigrepo "github.com/QuantumNous/new-api/internal/siteconfig/gormrepo"
	"github.com/QuantumNous/new-api/internal/tenant"
	tenantrepo "github.com/QuantumNous/new-api/internal/tenant/gormrepo"
	"github.com/QuantumNous/new-api/internal/ticket"
	ticketrepo "github.com/QuantumNous/new-api/internal/ticket/gormrepo"
	"github.com/QuantumNous/new-api/internal/tokenplan"
	tprepo "github.com/QuantumNous/new-api/internal/tokenplan/gormrepo"
	walletrepo "github.com/QuantumNous/new-api/internal/wallet/gormrepo"
)

// App 持有装配好的多租户增量服务（全部构建于 new-api 的共享 *gorm.DB 之上）。
type App struct {
	DB *gorm.DB

	// --- tenant 模块 ---
	TenantRepo     *tenantrepo.Repo
	TenantResolver tenant.TenantResolver
	TenantService  tenant.TenantService
	// CustomDomains 自定义域名绑定（OEM，§6.2/§6.3）：代理自助 Bind/Verify/Unbind + 内网发证回写。
	CustomDomains tenant.CustomDomainService
	// tenantCache 是 resolver 共享的 Host->Tenant 缓存引用，供写后失效（绑定转 active / 解绑）。
	tenantCache tenant.Cache
	// internalSecret 内网回写端点（/api/internal/domain/*）共享密钥（MT_INTERNAL_SECRET）；空=拒绝全部内网调用。
	internalSecret string
	// siteIP 返回给前端的 A 记录目标 IP（自定义域名配置指引，MT_SITE_IP）。
	siteIP string

	// --- siteconfig 模块（OEM 装修：站名/Logo/品牌隐藏，§5/§9）---
	SiteConfig     siteconfig.SiteConfigService // 读写租户装修配置（受控字段校验在包内）
	Assets         siteconfig.AssetService      // Logo 等素材上传（类型/大小校验 + data: URL）
	siteConfigRepo siteconfig.SiteConfigRepo    // found-aware 直读（/api/tenant/current 区分"已配置"与默认回退）

	// --- tokenplan 模块 ---
	TokenPlanRepo *tprepo.Repo // 持有具体类型：seed 与「我的套餐」列表用到非接口方法
	Catalog       tokenplan.PlanCatalog
	Retail        tokenplan.PlanRetailService
	Subscriptions tokenplan.SubscriptionService

	// --- agentplan 模块（购买代理套餐：一次性+有效期，落地页展示+控制台购买）---
	// AgentPlanRepo 持具体类型（seed 用 EnsurePlan）；AgentCatalog 管理员 CRUD。
	AgentPlanRepo *agentplanrepo.Repo
	AgentCatalog  agentplan.PlanCatalog

	// --- agent 模块（代理核心闭环）---
	// AgentRepo 持有具体类型：列表/seed 用到非接口方法（ListProfiles/ListWithdrawals/EnsureWallet 等）。
	AgentRepo     *agentrepo.Repo
	AgentService  agent.AgentService
	Withdrawals   agent.WithdrawalService
	AgentEarnings agent.EarningSink // 真实收益入账口（写 agent_earning_logs，幂等），注入 tokenplan + consume hook

	// --- 代理自助分销（P1-UI-04）---
	// PromotionRepo 持有具体类型（列表用非接口方法 ListChannelsByTenant）；Promotion 复用其领域服务（建渠道码）。
	PromotionRepo *promotionrepo.Repo
	Promotion     promotion.PromotionService
	// RedemptionRepo 兑换码仓储（原生 quota 口径）：建码预扣 + 单赢家兑换 + 按租户列表。
	RedemptionRepo *walletrepo.Repo

	// --- 2D 倍率（层级 × 模型分组，Phase 1，§2.15）---
	// ModelGroupRepo 持有 model_groups 登记 + 进程内缓存（IsModelGroup 供 /v1 计费热路径免查库）。
	// 单实例：计费旁路钩子（resolveModelGroup2D）与后台增删（HandleAdmin*ModelGroup*）共享同一缓存。
	ModelGroupRepo *modelgroup.Repo

	// --- moderation 模块（违禁词屏蔽，6e · §2.14）---
	// ModerationRepo 持具体类型（BannedWordRepo+ViolationSink，handlers 用）；Moderator 供 /v1 转发前扫描。
	ModerationRepo *moderationrepo.Repo
	Moderator      moderation.Moderator

	// --- ticket 模块（支持工单，用户/代理/管理员三端 + 隔离）---
	// TicketRepo 持具体类型（TicketRepo 实现）；TicketService 提供状态流转/校验/归属复校。
	TicketRepo    *ticketrepo.Repo
	TicketService ticket.TicketService

	// --- risk 模块（多档风控，7c · §2.13）---
	// RiskEngine 调用前风控（本轮 RPM 限流 + 租户状态），由 checkCallHook 经 agenthook.CheckCall 在 /v1 调用。
	// nil = Redis 未启用 → 风控旁路（CheckCall 直接放行，限流需共享计数）。
	RiskEngine risk.RiskEngine

	// --- report 模块（财务报表聚合，§财务报表契约）---
	// ReportRepo 持具体 *Repo：财务报表 handlers（report.go）经 raw Table()/Joins() 跨表 SUM/GROUP BY，
	// 四透镜（收益/充值/消耗/提现）只读聚合，不 import 兄弟模块 model 结构。
	ReportRepo *reportrepo.Repo

	// --- payment/recharge 模块（目标③，支付重构后）---
	// RechargeGateway 下单（落库 RCG 订单 + 经 inProcessPaySDK 进程内向平台下单）与入账（强幂等状态机）。
	RechargeGateway *payment.Gateway
	rechargeCfg     rechargeConfig
	// providerMgr 进程内支付适配（微信/支付宝，凭据指纹缓存 realpay.SDK）：RCG 充值下单（经
	// inProcessPaySDK）、SUB 套餐购买下单（HandlePurchase → subscriptionPayURL）、回调验签
	// （/api/pay/*/notify）、卡单对账主动查单共用同一缓存 SDK。
	providerMgr *providerManager

	// activateNativeSub 在激活事务内建原生 UserSubscription（由 subscription_bridge.go 使用，
	// 默认 defaultActivateNativeSub，可注入桩便于单测）。
	// 注：此字段 + New 中默认注入为「保编译/运行」的最小装配，若 Track 1 另行装配请 Master 去重。
	activateNativeSub func(ctx context.Context, tx *gorm.DB, snap *tokenplan.PendingPurchase) (int64, int64, error)
}

// New 用 new-api 的共享 db 装配全部增量服务（不再自开连接）。
func New(db *gorm.DB) *App {
	// tenant：GORM 仓储 + 内存解析缓存（Redis 适配顺延）。tcache 引用留给装配层做写后失效。
	tr := tenantrepo.New(db)
	tcache := tenant.NewMemCache()
	resolver := tenant.NewResolver(tr, tcache)
	tsvc := tenant.NewService(tr, tenant.NewSlugValidator())
	// 自定义域名服务：tr 同时实现 CustomDomainRepo；DNS 用标准库 net.LookupTXT（带超时）。
	customDomains := tenant.NewCustomDomainService(tr, nil)

	// siteconfig：GORM 仓储（tenant_site_configs + tenant_assets）+ 装修服务 + 素材上传（data: URL Blob，免对象存储）。
	scRepo := siteconfigrepo.New(db)
	siteConfigSvc := siteconfig.NewService(scRepo)
	assetSvc := siteconfig.NewAssetService(scRepo, newDataURLBlob())

	// agent：GORM 仓储（4 表）+ 真实成本守卫；服务 / 提现状态机 / 收益入账口。
	ar := agentrepo.New(db)
	guard := pricing.NewGuard() // 复用真实成本保护守卫（纯函数，零依赖）；设代理折扣经其校验
	agentSvc := agent.NewService(ar, guard)
	withdrawals := agent.NewWithdrawalService(ar)
	agentEarnings := agent.NewEarningSink(ar) // 真实入账：写 agent_earning_logs（幂等）+ 累加钱包

	// 代理自助分销（P1-UI-04）：推广渠道仓储 + 领域服务（建码 <prefix>_<rand>）；兑换码仓储（原生 quota 口径）。
	promoRepo := promotionrepo.New(db)
	promoSvc := promotion.NewService(promoRepo)
	redemptionRepo := walletrepo.New(db)

	// 2D 倍率：模型分组登记仓储 + 缓存。此处 best-effort 预热缓存（首次启动表未迁移则失败，
	// 由 Migrate() 后再重载；非 master 节点表已存在即可装载）。
	mgRepo := modelgroup.New(db)
	_ = mgRepo.ReloadCache(context.Background())

	// moderation（6e）：违禁词仓储（2 表）+ 服务（AC 匹配 + 租户合并）。
	modRepo := moderationrepo.New(db)
	moderator := moderation.NewService(modRepo, moderation.NewMatcher())

	// ticket（支持工单）：工单仓储（2 表）+ 服务（状态流转/校验/三端隔离）。
	ticketRepo := ticketrepo.New(db)
	ticketSvc := ticket.NewService(ticketRepo)

	// risk（7c）：调用前风控引擎。RPM 固定窗口限流需共享计数 → 仅 Redis 启用时装配；
	// 否则置 nil（checkCallHook 旁路放行）。RPM 默认阈值来自 env RISK_DEFAULT_RPM（0=不限）。
	var riskEngine risk.RiskEngine
	if common.RedisEnabled && common.RDB != nil {
		riskEngine = risk.NewEngine(
			risk.NewRedisKVCache(common.RDB),
			risk.WithStatusChecker(tenantStatusChecker{db: db}),
			risk.WithConfig(risk.Config{DefaultRPM: common.GetEnvOrDefault("RISK_DEFAULT_RPM", 0)}),
		)
	}

	// tokenplan：GORM 仓储（同时满足 PlanRepo + SubscriptionRepo）+ 纯函数成本守卫。
	tp := tprepo.New(db)
	catalog := tokenplan.NewCatalog(tp)
	retail := tokenplan.NewRetailService(tp, guard)
	// Purchase 经 subPayment 落一条真实 pending 订单（前缀 SUB），可被支付回调用
	// App.ActivatePaidTokenplanOrder 激活（见 subscription_bridge.go）。risk 仍占位；agent 套餐差价
	// 收益经 tokenplanEarningAdapter 真实落到 agent 钱包（ActivateFromPayment 激活事务内、按 source_order_id 幂等）。
	subs := tokenplan.NewSubscriptionService(tp, tp, newSubPayment(newSubOrderStore(db)), allowAllRisk{}, newTokenplanEarningAdapter(agentEarnings), nil)

	// agentplan：GORM 仓储（agent_plans）+ 管理员 CRUD 目录。购买/激活在 P3（AGT 订单 → SetAgentType）。
	agentPlanRepo := agentplanrepo.New(db)
	agentCatalog := agentplan.NewCatalog(agentPlanRepo)

	// payment/recharge：GORM 订单仓储（payment_orders，仅存 RCG 充值订单）+ 进程内支付适配（PaySDK）。
	// 入账 Sink 只挂 recharge→原生 quota；SUB 套餐订单不入 payment_orders，由回调按前缀
	// 分发到 App.ActivatePaidTokenplanOrder（Track 1 桥接，读 mt_subscription_orders）。
	rechargeCfg := loadRechargeConfig()
	orderRepo := paymentrepo.New(db)
	providerMgr := newProviderManager()
	rechargeSinks := map[payment.OrderType]payment.OrderSink{
		payment.OrderTypeRecharge: rechargeQuotaSink{db: db},
	}
	rechargeGateway := payment.NewGateway(
		orderRepo, &inProcessPaySDK{mgr: providerMgr}, rechargeSinks,
		// 充值端点只产 RCG 订单；SUB 订单由 Track 1 购买流程产出（入账侧按库内 type 分发，与前缀无关）。
		payment.WithOrderNoFunc(func() string { return payment.NewOrderNo(payment.OrderNoPrefixRecharge) }),
		payment.WithNotifyBaseURL(rechargeCfg.notifyBaseURL),
		// 把入账状态推进等静默异常接到主站日志（替代原 `_, _ =` 吞错）。
		payment.WithErrorLogf(func(format string, args ...any) { common.SysLog(fmt.Sprintf(format, args...)) }),
	)

	// report（财务报表）：聚合仓储（raw Table()/Joins() 跨表只读聚合），构于同一主库。
	reportRepo := reportrepo.New(db)

	app := &App{
		DB:              db,
		TenantRepo:      tr,
		TenantResolver:  resolver,
		TenantService:   tsvc,
		CustomDomains:   customDomains,
		tenantCache:     tcache,
		internalSecret:  common.GetEnvOrDefaultString("MT_INTERNAL_SECRET", ""),
		siteIP:          common.GetEnvOrDefaultString("MT_SITE_IP", ""),
		SiteConfig:      siteConfigSvc,
		Assets:          assetSvc,
		siteConfigRepo:  scRepo,
		TokenPlanRepo:   tp,
		Catalog:         catalog,
		Retail:          retail,
		Subscriptions:   subs,
		AgentPlanRepo:   agentPlanRepo,
		AgentCatalog:    agentCatalog,
		AgentRepo:       ar,
		AgentService:    agentSvc,
		Withdrawals:     withdrawals,
		AgentEarnings:   agentEarnings,
		PromotionRepo:   promoRepo,
		Promotion:       promoSvc,
		RedemptionRepo:  redemptionRepo,
		ModelGroupRepo:  mgRepo,
		ModerationRepo:  modRepo,
		Moderator:       moderator,
		TicketRepo:      ticketRepo,
		TicketService:   ticketSvc,
		RiskEngine:      riskEngine,
		ReportRepo:      reportRepo,
		RechargeGateway: rechargeGateway,
		rechargeCfg:     rechargeCfg,
		providerMgr:     providerMgr, // 复用同一进程内适配器供 tokenplan 购买（SUB）下单 + 回调验签 + 对账查单
	}
	if app.activateNativeSub == nil {
		app.activateNativeSub = app.defaultActivateNativeSub // 目标③桥接默认实现（subscription_bridge.go）
	}
	return app
}

// Migrate 在 new-api InitDB 之后 AutoMigrate 我们的增量表（共享库）：
//
//	tenants / tenant_domains（tenant 模块）
//	token_plans / tenant_token_plans / user_subscriptions /
//	subscription_usage_logs / pending_subscription_orders（tokenplan 模块）
func (a *App) Migrate() error {
	if err := tenantrepo.AutoMigrate(a.DB); err != nil {
		return err
	}
	if err := agentplanrepo.AutoMigrate(a.DB); err != nil { // agent_plans（购买代理套餐定义）
		return err
	}
	if err := tprepo.AutoMigrate(a.DB); err != nil {
		return err
	}
	if err := agentrepo.AutoMigrate(a.DB); err != nil { // agent_profiles/agent_wallets/agent_earning_logs/agent_withdrawals
		return err
	}
	if err := promotionrepo.AutoMigrate(a.DB); err != nil { // agent_promotion_channels/agent_promotion_attributions（P1-UI-04）
		return err
	}
	if err := walletrepo.AutoMigrate(a.DB); err != nil { // user_balances/agent_redemption_codes（兑换码原生 quota 口径）
		return err
	}
	if err := paymentrepo.AutoMigrate(a.DB); err != nil { // payment_orders（Track 2 充值订单）
		return err
	}
	if err := modelgroup.AutoMigrate(a.DB); err != nil { // model_groups（2D 倍率 · 模型分组登记，§2.15）
		return err
	}
	if err := moderationrepo.AutoMigrate(a.DB); err != nil { // moderation_banned_words/moderation_content_violations（6e）
		return err
	}
	if err := ticketrepo.AutoMigrate(a.DB); err != nil { // support_tickets/support_ticket_messages（支持工单）
		return err
	}
	if err := siteconfigrepo.AutoMigrate(a.DB); err != nil { // tenant_site_configs/tenant_assets（OEM 装修，§5/§9）
		return err
	}
	// 迁移后重载模型分组缓存（master 节点建表 / 补 seed 后，IsModelGroup 即时生效）。
	if a.ModelGroupRepo != nil {
		_ = a.ModelGroupRepo.ReloadCache(context.Background())
	}
	// new-api 原生表 users 增列 tenant_id：用幂等 raw ALTER（不改 new-api model.User struct，避免 upstream rebase 冲突）。
	if err := migrateUsersTenantID(a.DB); err != nil {
		return err
	}
	// users 增列 promotion_channel_id（经渠道码注册的归属落此列）：同套幂等 raw ALTER。
	if err := migrateUsersPromotionChannelID(a.DB); err != nil {
		return err
	}
	// 财务报表区间覆盖索引（agent_earning_logs(tenant_id,created_at) + logs(user_id,created_at)）：
	// 幂等 information_schema 守卫的 raw CREATE INDEX，不改 model.Log/model.User struct（同 migrateUsersTenantID 套路）。
	if err := reportrepo.AutoMigrate(a.DB); err != nil {
		return err
	}
	// 充值入账幂等台账：mt_recharge_credit_ledger（order_no 唯一，防额度双扣，审计 C1）。
	if err := migrateRechargeLedger(a.DB); err != nil {
		return err
	}
	// 目标③桥接表：mt_subscription_orders（SUB 套餐订单状态机）+ mt_native_subscription_plans
	// （tokenplan→原生 SubscriptionPlan 映射）。均为 mt_ 前缀，不与原生订阅表冲突。
	if err := migrateSubscriptionBridge(a.DB); err != nil {
		return err
	}
	// 购买代理套餐（P3）：mt_agent_plan_orders（AGT 订单状态机）+ mt_agent_memberships（会员台账/到期）。
	if err := migrateAgentPlanBridge(a.DB); err != nil {
		return err
	}
	// 对账记录 + 心跳（reconcile-history）：历史列表 reconcile_runs + 单行心跳 reconcile_heartbeat。
	if err := migrateReconcileRuns(a.DB); err != nil {
		return err
	}
	if err := migrateReconcileHeartbeat(a.DB); err != nil {
		return err
	}
	// 钱包消耗台账 mt_wallet_consume_log（财务报表 v3「钱包消耗」精确口径；(user_id,request_id) 幂等，
	// (tenant_id,created_at) 覆盖区间扫描，见 wallet_consume_log.go）。
	if err := migrateWalletConsumeLog(a.DB); err != nil {
		return err
	}
	// 代理分层：现有代理回填 level=1 + 删废弃 type 列（一次性、information_schema 守卫，见 agent.go）。
	return migrateAgentProfilesDropType(a.DB)
}

// ----------------------------------------------------------------------------
// Phase 2 占位适配（最小可跑；真实集成顺延，见报告「风险/未决」）。
//   - allowAllRisk 一律放行（Trial 用户/实名/设备限购等真实规则顺延 internal/risk）。
//
// 套餐差价分润占位 noopEarnings 已被 tokenplanEarningAdapter 取代（真实落到 internal/agent 钱包，见 agent.go）；
// 支付占位 stubPayment 已被 subPayment 取代（落真实 SUB 订单，见 subscription_bridge.go）。
// ----------------------------------------------------------------------------

// allowAllRisk 是占位风控引擎。
type allowAllRisk struct{}

func (allowAllRisk) CheckPurchaseLimit(_ context.Context, _ tokenplan.PurchaseLimitCheck) error {
	return nil
}

// randToken 返回一个十六进制随机串（订单号占位）；熵不足时回退时间戳。
func randToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b)
}
