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
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/agent"
	agentrepo "github.com/QuantumNous/new-api/internal/agent/gormrepo"
	"github.com/QuantumNous/new-api/internal/payment"
	paymentrepo "github.com/QuantumNous/new-api/internal/payment/gormrepo"
	"github.com/QuantumNous/new-api/internal/pricing"
	"github.com/QuantumNous/new-api/internal/promotion"
	promotionrepo "github.com/QuantumNous/new-api/internal/promotion/gormrepo"
	"github.com/QuantumNous/new-api/internal/tenant"
	tenantrepo "github.com/QuantumNous/new-api/internal/tenant/gormrepo"
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

	// --- tokenplan 模块 ---
	TokenPlanRepo *tprepo.Repo // 持有具体类型：seed 与「我的套餐」列表用到非接口方法
	Catalog       tokenplan.PlanCatalog
	Retail        tokenplan.PlanRetailService
	Subscriptions tokenplan.SubscriptionService

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

	// --- payment/recharge 模块（目标③）---
	// RechargeGateway 下单（落库 RCG 订单 + 调 auth-service）与内网入账（强幂等状态机）。
	RechargeGateway *payment.Gateway
	rechargeCfg     rechargeConfig

	// activateNativeSub 在激活事务内建原生 UserSubscription（由 subscription_bridge.go 使用，
	// 默认 defaultActivateNativeSub，可注入桩便于单测）。
	// 注：此字段 + New 中默认注入为「保编译/运行」的最小装配，若 Track 1 另行装配请 Master 去重。
	activateNativeSub func(ctx context.Context, tx *gorm.DB, snap *tokenplan.PendingPurchase) (int64, int64, error)
}

// New 用 new-api 的共享 db 装配全部增量服务（不再自开连接）。
func New(db *gorm.DB) *App {
	// tenant：GORM 仓储 + 内存解析缓存（Redis 适配顺延）。
	tr := tenantrepo.New(db)
	resolver := tenant.NewResolver(tr, tenant.NewMemCache())
	tsvc := tenant.NewService(tr, tenant.NewSlugValidator())

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

	// tokenplan：GORM 仓储（同时满足 PlanRepo + SubscriptionRepo）+ 纯函数成本守卫。
	tp := tprepo.New(db)
	catalog := tokenplan.NewCatalog(tp)
	retail := tokenplan.NewRetailService(tp, guard)
	// Purchase 经 subPayment 落一条真实 pending 订单（前缀 SUB），可被支付回调用
	// App.ActivatePaidTokenplanOrder 激活（见 subscription_bridge.go）。risk 仍占位；agent 套餐差价
	// 收益经 tokenplanEarningAdapter 真实落到 agent 钱包（ActivateFromPayment 激活事务内、按 source_order_id 幂等）。
	subs := tokenplan.NewSubscriptionService(tp, tp, newSubPayment(newSubOrderStore(db)), allowAllRisk{}, newTokenplanEarningAdapter(agentEarnings), nil)

	// payment/recharge：GORM 订单仓储（payment_orders，仅存 RCG 充值订单）+ auth-service 客户端（PaySDK）。
	// 入账 Sink 只挂 recharge→原生 quota；SUB 套餐订单不入 payment_orders，由内网入账端点按前缀
	// 分发到 App.ActivatePaidTokenplanOrder（Track 1 桥接，读 mt_subscription_orders）。
	rechargeCfg := loadRechargeConfig()
	orderRepo := paymentrepo.New(db)
	authClient := newAuthServiceClient(rechargeCfg.authServiceURL)
	rechargeSinks := map[payment.OrderType]payment.OrderSink{
		payment.OrderTypeRecharge: rechargeQuotaSink{},
	}
	rechargeGateway := payment.NewGateway(
		orderRepo, authClient, rechargeSinks,
		// 充值端点只产 RCG 订单；SUB 订单由 Track 1 购买流程产出（入账侧按库内 type 分发，与前缀无关）。
		payment.WithOrderNoFunc(func() string { return payment.NewOrderNo(payment.OrderNoPrefixRecharge) }),
		payment.WithNotifyBaseURL(rechargeCfg.notifyBaseURL),
	)

	app := &App{
		DB:              db,
		TenantRepo:      tr,
		TenantResolver:  resolver,
		TenantService:   tsvc,
		TokenPlanRepo:   tp,
		Catalog:         catalog,
		Retail:          retail,
		Subscriptions:   subs,
		AgentRepo:       ar,
		AgentService:    agentSvc,
		Withdrawals:     withdrawals,
		AgentEarnings:   agentEarnings,
		PromotionRepo:   promoRepo,
		Promotion:       promoSvc,
		RedemptionRepo:  redemptionRepo,
		RechargeGateway: rechargeGateway,
		rechargeCfg:     rechargeCfg,
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
	// new-api 原生表 users 增列 tenant_id：用幂等 raw ALTER（不改 new-api model.User struct，避免 upstream rebase 冲突）。
	if err := migrateUsersTenantID(a.DB); err != nil {
		return err
	}
	// 目标③桥接表：mt_subscription_orders（SUB 套餐订单状态机）+ mt_native_subscription_plans
	// （tokenplan→原生 SubscriptionPlan 映射）。均为 mt_ 前缀，不与原生订阅表冲突。
	return migrateSubscriptionBridge(a.DB)
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
