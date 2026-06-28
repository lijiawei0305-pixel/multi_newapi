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

	"github.com/QuantumNous/new-api/internal/pricing"
	"github.com/QuantumNous/new-api/internal/tenant"
	tenantrepo "github.com/QuantumNous/new-api/internal/tenant/gormrepo"
	"github.com/QuantumNous/new-api/internal/tokenplan"
	tprepo "github.com/QuantumNous/new-api/internal/tokenplan/gormrepo"
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
}

// New 用 new-api 的共享 db 装配全部增量服务（不再自开连接）。
func New(db *gorm.DB) *App {
	// tenant：GORM 仓储 + 内存解析缓存（Redis 适配顺延）。
	tr := tenantrepo.New(db)
	resolver := tenant.NewResolver(tr, tenant.NewMemCache())
	tsvc := tenant.NewService(tr, tenant.NewSlugValidator())

	// tokenplan：GORM 仓储（同时满足 PlanRepo + SubscriptionRepo）+ 纯函数成本守卫。
	tp := tprepo.New(db)
	guard := pricing.NewGuard() // 复用真实成本保护守卫（纯函数，零依赖）
	catalog := tokenplan.NewCatalog(tp)
	retail := tokenplan.NewRetailService(tp, guard)
	// Purchase 依赖 payment/risk/earning：本阶段用占位适配让流程可跑通（见下方说明）。
	subs := tokenplan.NewSubscriptionService(tp, tp, stubPayment{}, allowAllRisk{}, noopEarnings{}, nil)

	return &App{
		DB:             db,
		TenantRepo:     tr,
		TenantResolver: resolver,
		TenantService:  tsvc,
		TokenPlanRepo:  tp,
		Catalog:        catalog,
		Retail:         retail,
		Subscriptions:  subs,
	}
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
	return tprepo.AutoMigrate(a.DB)
}

// ----------------------------------------------------------------------------
// Phase 2 占位适配（最小可跑；真实集成顺延，见报告「风险/未决」）。
// 这些类型仅满足 tokenplan 的消费者依赖接口，使 Purchase 流程在 new-api 内可端到端跑通：
//   - stubPayment   生成订单号 + 占位支付链接，不接真实支付/回调（激活留待后续）；
//   - allowAllRisk  一律放行（Trial 用户/实名/设备限购等真实规则顺延 internal/risk）；
//   - noopEarnings  不入账（套餐差价分润真实落到 internal/agent 钱包顺延）。
// ----------------------------------------------------------------------------

// stubPayment 是占位支付网关。
type stubPayment struct{}

func (stubPayment) CreateOrder(_ context.Context, _ tokenplan.OrderInput) (*tokenplan.PayOrder, error) {
	id := "tp-" + randToken(12)
	return &tokenplan.PayOrder{OrderID: id, PayURL: "/console/tokenplan/pay?order=" + id}, nil
}

// allowAllRisk 是占位风控引擎。
type allowAllRisk struct{}

func (allowAllRisk) CheckPurchaseLimit(_ context.Context, _ tokenplan.PurchaseLimitCheck) error {
	return nil
}

// noopEarnings 是占位代理收益接收器。
type noopEarnings struct{}

func (noopEarnings) AddEarning(_ context.Context, _ tokenplan.EarningEntry) error { return nil }

// randToken 返回一个十六进制随机串（订单号占位）；熵不足时回退时间戳。
func randToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b)
}
