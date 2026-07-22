package mtwire

// resolveBuyerTenant 回归测试：主站 Host（无租户命中）此前对三个买家端点
// （HandleListTokenPlans/HandlePurchase/HandleListSubscriptions）一律报 TENANT_NOT_FOUND
// （"租户不存在"），即便产品侧已确认「主站自身也直销 tokenplan 套餐」。修复：resolveBuyerTenant
// 在 Host 未解析出租户但命中 tenant.IsMainSiteHost 时回退到 seed 好的平台租户（seed.go）。
//
// 主站 Host 用 www.wedreamhub.com / 裸根域名（apex）—— 这两个是当前 tenant.IsMainSiteHost
// （internal/tenant/resolver.go）三态判别下真正落「主站」分支的 Host（见其 TestIsMainSiteHost）；
// api.wedreamhub.com 按 doc/domains-ssl.md §6.4 走独立 3000 vhost、不经本仓库这套多租户解析逻辑
// （现网拓扑，见 RETRO.md），因此不用作本文件的"主站"测试夹具。
// 未知 Host 用 *.wedreamhub.com 下的未注册子域（如 foo.wedreamhub.com）——这是当前设计下唯一真正
// 落「既非主站、也未解析出租户」分支的场景（IsMainSiteHost 对无关外部域名/本地直连另有兜底为
// true 的开发者友好分支，不在此处覆盖，见 resolver_test.go）。

import (
	"context"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	agentrepo "github.com/QuantumNous/new-api/internal/agent/gormrepo"
	"github.com/QuantumNous/new-api/internal/payment"
	"github.com/QuantumNous/new-api/internal/platform/apperr"
	"github.com/QuantumNous/new-api/internal/pricing"
	"github.com/QuantumNous/new-api/internal/tenant"
	tenantrepo "github.com/QuantumNous/new-api/internal/tenant/gormrepo"
	"github.com/QuantumNous/new-api/internal/tokenplan"
	tprepo "github.com/QuantumNous/new-api/internal/tokenplan/gormrepo"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type buyerTestPayCreator struct{}

func (buyerTestPayCreator) CreatePay(context.Context, payment.Provider, string, string, float64, string) (string, error) {
	return "https://pay.example.test/checkout", nil
}

// newBuyerTestApp 装配买家套餐端点（HandleListTokenPlans/HandlePurchase/HandleListSubscriptions）
// 所需的最小 App：sqlite(:memory:) + tenant/tokenplan 两套 gorm 表 + 订阅桥接表（Purchase 会落
// mt_subscription_orders，见 subscription_bridge.go）。不装配 providerMgr，显式注入本地支付创建桩，
// 避免测试依赖生产的 fail-closed 装配错误分支，也不发真实 HTTP。
func newBuyerTestApp(t *testing.T) *App {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := tenantrepo.AutoMigrate(db); err != nil {
		t.Fatalf("tenant migrate: %v", err)
	}
	if err := tprepo.AutoMigrate(db); err != nil {
		t.Fatalf("tokenplan migrate: %v", err)
	}
	if err := migrateSubscriptionBridge(db); err != nil {
		t.Fatalf("bridge migrate: %v", err)
	}

	tr := tenantrepo.New(db)
	tp := tprepo.New(db)
	guard := pricing.NewGuard()
	return &App{
		DB:            db,
		TenantRepo:    tr,
		TenantService: tenant.NewService(tr, tenant.NewSlugValidator()),
		TokenPlanRepo: tp,
		Catalog:       tokenplan.NewCatalog(tp),
		Retail:        tokenplan.NewRetailService(tp, guard),
		Subscriptions: tokenplan.NewSubscriptionService(
			tp, tp, newSubPayment(newSubOrderStore(db), tp), allowAllRisk{}, noopEarnings{}, nil),
		subscriptionPayCreator: buyerTestPayCreator{},
	}
}

// newBuyerCtx 构造买家端点的 gin 上下文：Host 头 + 可选「TenantMiddleware 已解析」的租户
// （nil 模拟 Host 未命中任何 tenant_domains，正是本次改动要处理的场景）+ UserAuth 写入的 id。
func newBuyerCtx(host string, resolved *tenant.Tenant, userID int, method, body string, params gin.Params) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(method, "/", strings.NewReader(body))
	req.Host = host
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	c.Params = params
	c.Set("id", userID)
	if resolved != nil {
		c.Set(ginKeyTenant, resolved)
	}
	return c, rec
}

// seedPlatformPlan 建平台租户（幂等）+ 上架一个可购买套餐，返回 (平台租户, 套餐ID)。
func seedPlatformPlan(t *testing.T, app *App) (*tenant.Tenant, int64) {
	t.Helper()
	ctx := context.Background()
	pt, _, err := app.ensurePlatformTenant(ctx)
	if err != nil {
		t.Fatalf("ensurePlatformTenant: %v", err)
	}
	planID, err := app.TokenPlanRepo.EnsurePlan(ctx, &tokenplan.Plan{
		Code: "pro", Name: "Pro", BasePrice: 30, AnchorPrice: 60, Multiplier: 1,
		MonthLimitUSD: 20, ValidDays: 30, AgentCostPrice: 5, MinPrice: 8, Status: tokenplan.PlanEnabled,
	})
	if err != nil {
		t.Fatalf("EnsurePlan: %v", err)
	}
	if err := app.TokenPlanRepo.EnsureListing(ctx, pt.ID, planID, true, 30); err != nil {
		t.Fatalf("EnsureListing: %v", err)
	}
	return pt, planID
}

// apiResp / decodeResp 复用 distribution_test.go 已定义的响应解码小工具（同包 _test.go 互见）。

// ============================================================================
// resolveBuyerTenant：核心新增逻辑的穷举分支测试
// ============================================================================

// TestResolveBuyerTenant_AlreadyResolvedPassthrough 校验 TenantMiddleware 已解析出租户时
// （代理子域名场景）原样返回，完全不触碰 Host / 主站兜底逻辑——既有行为零回归。
func TestResolveBuyerTenant_AlreadyResolvedPassthrough(t *testing.T) {
	app := newBuyerTestApp(t)
	real := &tenant.Tenant{ID: 99, Slug: "acme"}
	c, _ := newBuyerCtx("acme.wedreamhub.com", real, 1, "GET", "", nil)

	got, err := app.resolveBuyerTenant(c)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if got != real {
		t.Fatalf("got %+v, want the same already-resolved tenant %+v", got, real)
	}
}

// TestResolveBuyerTenant_MainSiteFallsBackToPlatformTenant 是本次改动要修的核心场景：
// 主站 Host（www / 裸根域名 / 本地直连）+ 未解析出租户 → 应回退平台租户，而不是 TENANT_NOT_FOUND。
// 同时校验回退结果写回了 gin ctx，使同一请求内后续 tenantFrom(c) 读到一致结果。
func TestResolveBuyerTenant_MainSiteFallsBackToPlatformTenant(t *testing.T) {
	app := newBuyerTestApp(t)
	pt, _, err := app.ensurePlatformTenant(context.Background())
	if err != nil {
		t.Fatalf("ensurePlatformTenant: %v", err)
	}

	for _, host := range []string{
		"www.wedreamhub.com",
		"wedreamhub.com",
		"WWW.WedreamHub.com:443",
		"localhost:3100", // 本地直连兜底（IsMainSiteHost 对非平台域名的开发者友好分支）
	} {
		c, _ := newBuyerCtx(host, nil, 1, "GET", "", nil)
		got, err := app.resolveBuyerTenant(c)
		if err != nil {
			t.Fatalf("host %q: err = %v, want nil", host, err)
		}
		if got == nil || got.ID != pt.ID {
			t.Fatalf("host %q: tenant = %+v, want platform tenant id=%d", host, got, pt.ID)
		}
		if tf := tenantFrom(c); tf == nil || tf.ID != pt.ID {
			t.Fatalf("host %q: tenantFrom(c) after fallback = %+v, want platform tenant written back", host, tf)
		}
	}
}

// TestResolveBuyerTenant_MainSiteButPlatformNotSeeded 校验极端边缘情形（seed 尚未跑过/失败）
// 主站 Host 仍安全地退回既有 TENANT_NOT_FOUND，不 panic、不误报别的租户。
func TestResolveBuyerTenant_MainSiteButPlatformNotSeeded(t *testing.T) {
	app := newBuyerTestApp(t) // 未调用 ensurePlatformTenant
	c, _ := newBuyerCtx("www.wedreamhub.com", nil, 1, "GET", "", nil)

	_, err := app.resolveBuyerTenant(c)
	if !apperr.Is(err, "TENANT_NOT_FOUND") {
		t.Fatalf("err = %v, want TENANT_NOT_FOUND", err)
	}
}

// TestResolveBuyerTenant_UnregisteredSubdomainStillTenantNotFound 校验「*.wedreamhub.com 下未注册
// 子域（既非主站、也未解析出租户，如误配的 api./随意猜测的子域）」维持既有 TENANT_NOT_FOUND
// （"站点未开通"）——不能把它当成主站放行，也不能因为平台租户已 seed 就误配到它。
func TestResolveBuyerTenant_UnregisteredSubdomainStillTenantNotFound(t *testing.T) {
	app := newBuyerTestApp(t)
	if _, _, err := app.ensurePlatformTenant(context.Background()); err != nil {
		t.Fatalf("ensurePlatformTenant: %v", err) // 即便平台租户已 seed 也不该影响这个场景
	}
	for _, host := range []string{"foo.wedreamhub.com", "api.wedreamhub.com"} {
		c, _ := newBuyerCtx(host, nil, 1, "GET", "", nil)
		_, err := app.resolveBuyerTenant(c)
		if !apperr.Is(err, "TENANT_NOT_FOUND") {
			t.Fatalf("host %q: err = %v, want TENANT_NOT_FOUND", host, err)
		}
	}
}

// ============================================================================
// HandleListTokenPlans
// ============================================================================

func TestHandleListTokenPlans_MainSiteFallback(t *testing.T) {
	app := newBuyerTestApp(t)
	seedPlatformPlan(t, app)

	c, rec := newBuyerCtx("www.wedreamhub.com", nil, 1, "GET", "", nil)
	app.HandleListTokenPlans(c)

	r := decodeResp(t, rec)
	if !r.Success {
		t.Fatalf("main-site buyer listing should succeed, got %+v", r)
	}
	var plans []buyerPlanOut
	if err := common.Unmarshal(r.Data, &plans); err != nil {
		t.Fatalf("decode plans: %v", err)
	}
	if len(plans) != 1 || plans[0].Code != "pro" {
		t.Fatalf("plans = %+v, want 1 plan (pro)", plans)
	}
}

func TestHandleListTokenPlans_UnregisteredSubdomainStillTenantNotFound(t *testing.T) {
	app := newBuyerTestApp(t)
	c, rec := newBuyerCtx("foo.wedreamhub.com", nil, 1, "GET", "", nil)
	app.HandleListTokenPlans(c)

	r := decodeResp(t, rec)
	if r.Success || r.Code != "TENANT_NOT_FOUND" {
		t.Fatalf("unregistered subdomain must still be TENANT_NOT_FOUND, got %+v", r)
	}
}

// TestHandleListTokenPlans_RealTenantHostUnaffected 回归：真实代理子域名场景（TenantMiddleware
// 已解析）行为不变——即使 Host 字符串本身凑巧不是主站保留域名，也应直接用已解析租户，不再兜底判断。
func TestHandleListTokenPlans_RealTenantHostUnaffected(t *testing.T) {
	app := newBuyerTestApp(t)
	ctx := context.Background()
	acme, err := app.TenantService.Create(ctx, tenant.CreateTenantInput{Slug: "acme", Name: "Acme", TokenplanEnabled: true})
	if err != nil {
		t.Fatalf("create acme: %v", err)
	}
	planID, err := app.TokenPlanRepo.EnsurePlan(ctx, &tokenplan.Plan{
		Code: "mini", Name: "Mini", BasePrice: 10, AnchorPrice: 20, Multiplier: 1,
		MonthLimitUSD: 5, ValidDays: 30, AgentCostPrice: 3, MinPrice: 4, Status: tokenplan.PlanEnabled,
	})
	if err != nil {
		t.Fatalf("EnsurePlan: %v", err)
	}
	if err := app.TokenPlanRepo.EnsureListing(ctx, acme.ID, planID, true, 12); err != nil {
		t.Fatalf("EnsureListing: %v", err)
	}

	c, rec := newBuyerCtx("acme.wedreamhub.com", acme, 1, "GET", "", nil)
	app.HandleListTokenPlans(c)

	r := decodeResp(t, rec)
	if !r.Success {
		t.Fatalf("resolved-tenant listing should succeed, got %+v", r)
	}
	var plans []buyerPlanOut
	if err := common.Unmarshal(r.Data, &plans); err != nil {
		t.Fatalf("decode plans: %v", err)
	}
	if len(plans) != 1 || plans[0].Code != "mini" || plans[0].RetailPriceCNY != 12 {
		t.Fatalf("plans = %+v, want acme's own listing (mini @ 12)", plans)
	}
}

// ============================================================================
// HandlePurchase
// ============================================================================

func TestHandlePurchase_MainSiteFallback(t *testing.T) {
	app := newBuyerTestApp(t)
	pt, planID := seedPlatformPlan(t, app)
	// HandlePurchase 下单前校验渠道 configured（对齐 HandleWalletRecharge，见 payment_providers.go
	// ensureProviderUsable）；stubProviderConfigured 定义于 payment_providers_test.go，同包共享。
	// 这里只关心租户解析分支，故放行默认渠道（wxpay）。
	stubProviderConfigured(t, func(p payment.Provider) bool { return p == payment.ProviderWxpay })

	c, rec := newBuyerCtx("www.wedreamhub.com", nil, 7, "POST", "{}",
		gin.Params{{Key: "id", Value: strconv.FormatInt(planID, 10)}})
	app.HandlePurchase(c)

	r := decodeResp(t, rec)
	if !r.Success {
		t.Fatalf("main-site purchase should succeed, got %+v", r)
	}
	var out struct {
		OrderNo   string  `json:"order_no"`
		AmountCNY float64 `json:"amount_cny"`
		PlanID    int64   `json:"plan_id"`
	}
	if err := common.Unmarshal(r.Data, &out); err != nil {
		t.Fatalf("decode purchase result: %v", err)
	}
	if out.PlanID != planID || out.OrderNo == "" {
		t.Fatalf("purchase result = %+v, want plan_id=%d and non-empty order_no", out, planID)
	}

	// 落库的待支付快照必须归属平台租户（差价/激活链路据此找到正确的 tenant_id）。
	snap, err := app.TokenPlanRepo.GetPendingPurchase(context.Background(), out.OrderNo)
	if err != nil {
		t.Fatalf("GetPendingPurchase: %v", err)
	}
	if snap.TenantID != pt.ID {
		t.Fatalf("pending purchase tenant_id = %d, want platform tenant %d", snap.TenantID, pt.ID)
	}
}

func TestHandlePurchaseOptionalBodyAcceptsOnlyEOFAsEmpty(t *testing.T) {
	app := newBuyerTestApp(t)
	_, planID := seedPlatformPlan(t, app)
	var configuredProvider payment.Provider
	stubProviderConfigured(t, func(p payment.Provider) bool {
		configuredProvider = p
		return p == payment.ProviderWxpay
	})
	require.True(t, providerConfigured(app, payment.ProviderWxpay))
	param := gin.Params{{Key: "id", Value: strconv.FormatInt(planID, 10)}}

	// 真正空 body 保持既有默认微信语义。
	c, rec := newBuyerCtx("www.wedreamhub.com", nil, 7, "POST", "", param)
	app.HandlePurchase(c)
	resp := decodeResp(t, rec)
	assert.Equal(t, payment.ProviderWxpay, configuredProvider)
	require.True(t, resp.Success, "empty body should use default wxpay: %+v", resp)
	var out struct {
		OrderNo string `json:"order_no"`
	}
	require.NoError(t, common.Unmarshal(resp.Data, &out))
	var order subscriptionOrderRow
	require.NoError(t, app.DB.Take(&order, "order_no = ?", out.OrderNo).Error)
	assert.Equal(t, string(payment.ProviderWxpay), order.Provider)
	assert.Equal(t, subOrderPending, order.Status, "payment credentials must be confirmed before ordinary pending")

	for name, body := range map[string]string{
		"truncated":         `{"provider":`,
		"wrong field type":  `{"provider":123}`,
		"null body":         `null`,
		"wrong top-level":   `[]`,
		"trailing garbage":  `{} trailing`,
		"second JSON value": `{} {}`,
		"oversized body":    strings.Repeat(" ", int(purchaseRequestBodyLimit)+1),
	} {
		t.Run(name, func(t *testing.T) {
			var beforeOrders int64
			require.NoError(t, app.DB.Model(&subscriptionOrderRow{}).Count(&beforeOrders).Error)
			c, rec := newBuyerCtx("www.wedreamhub.com", nil, 7, "POST", body, param)
			app.HandlePurchase(c)
			got := decodeResp(t, rec)
			assert.False(t, got.Success)
			assert.Equal(t, payment.CodeOrderInvalid, got.Code)
			var afterOrders int64
			require.NoError(t, app.DB.Model(&subscriptionOrderRow{}).Count(&afterOrders).Error)
			assert.Equal(t, beforeOrders, afterOrders, "malformed body must not create an order")
		})
	}
}

func TestHandlePurchaseMissingPaymentCreatorFailsClosed(t *testing.T) {
	app := newBuyerTestApp(t)
	app.subscriptionPayCreator = nil
	app.providerMgr = nil
	_, planID := seedPlatformPlan(t, app)
	stubProviderConfigured(t, func(p payment.Provider) bool { return p == payment.ProviderWxpay })

	c, rec := newBuyerCtx("www.wedreamhub.com", nil, 7, "POST", `{}`,
		gin.Params{{Key: "id", Value: strconv.FormatInt(planID, 10)}})
	app.HandlePurchase(c)
	resp := decodeResp(t, rec)
	assert.False(t, resp.Success)
	assert.Equal(t, "PROVIDER_MGR_UNSET", resp.Code)
	assert.NotContains(t, string(resp.Data), "pay.example.test")

	var order subscriptionOrderRow
	require.NoError(t, app.DB.First(&order).Error)
	assert.Equal(t, subOrderPayFailed, order.Status)
	assert.Equal(t, string(payment.ProviderWxpay), order.Provider)
}

func TestHandlePurchaseAgentDiscountReadErrorCreatesNothing(t *testing.T) {
	app := newBuyerTestApp(t)
	_, planID := seedPlatformPlan(t, app)
	stubProviderConfigured(t, func(p payment.Provider) bool { return p == payment.ProviderWxpay })
	// 不迁移 agent_profiles，强制真实 AgentRepo.GetAgentType 返回数据库错误。
	app.AgentRepo = agentrepo.New(app.DB)

	c, rec := newBuyerCtx("www.wedreamhub.com", nil, 7, "POST", `{}`,
		gin.Params{{Key: "id", Value: strconv.FormatInt(planID, 10)}})
	app.HandlePurchase(c)
	resp := decodeResp(t, rec)
	assert.False(t, resp.Success)
	assert.Equal(t, "INTERNAL", resp.Code)
	var orders, snapshots int64
	require.NoError(t, app.DB.Model(&subscriptionOrderRow{}).Count(&orders).Error)
	require.NoError(t, app.DB.Table("pending_subscription_orders").Count(&snapshots).Error)
	assert.Zero(t, orders)
	assert.Zero(t, snapshots)
}

func TestHandlePurchase_UnregisteredSubdomainStillTenantNotFound(t *testing.T) {
	app := newBuyerTestApp(t)
	c, rec := newBuyerCtx("foo.wedreamhub.com", nil, 7, "POST", "{}",
		gin.Params{{Key: "id", Value: "1"}})
	app.HandlePurchase(c)

	r := decodeResp(t, rec)
	if r.Success || r.Code != "TENANT_NOT_FOUND" {
		t.Fatalf("unregistered subdomain must still be TENANT_NOT_FOUND, got %+v", r)
	}
}

// deviceFingerprint —— Trial 设备维度必须由服务端从「不可被监管方自选」的信号（ClientIP）派生
// （绝不读请求体、绝不含 UA 等客户端可自选字段）。契约：① 有 IP → 32 字符 hex；② 同 IP（不论
// 端口/UA）→ 指纹一致；③ 不同 IP → 指纹不同；④ IP 为空（无论 UA 有无）→ ""（跳过维度，不用空
// 串哈希把所有无信号请求锁成同一设备）。锁死 audit 2026-07-18 F1 的修复：唯一性键只认不可自选
// 信号——UA 可每请求轮换、放进键即等于让被监管方自选分桶，绕过成本与自报随机 device_id 相同。
func TestDeviceFingerprint(t *testing.T) {
	fp := func(remoteAddr, ua string) string {
		req := httptest.NewRequest("POST", "/api/tenant/token-plans/1/purchase", nil)
		req.RemoteAddr = remoteAddr
		if ua != "" {
			req.Header.Set("User-Agent", ua)
		}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = req
		return deviceFingerprint(c)
	}

	a := fp("203.0.113.7:5555", "codex-cli/1.0")
	if len(a) != 32 {
		t.Fatalf("fingerprint = %q, want 32-char hex", a)
	}
	// 同一 IP、不同端口 → 指纹稳定一致（端口非区分维度）。
	if b := fp("203.0.113.7:9999", "codex-cli/1.0"); b != a {
		t.Fatalf("same IP must be stable regardless of port: got %q vs %q", b, a)
	}
	// 同一 IP、不同 UA → 指纹必须**相同**：UA 是客户端可自选字段，绝不进唯一性键，否则攻击者
	// 每请求换个 UA 即得一把全新互不碰撞的终身键、去重形同虚设（audit 2026-07-18 F1 修复的缺陷本身）。
	if b := fp("203.0.113.7:5555", "cursor/2.0"); b != a {
		t.Fatalf("same IP with different UA must yield the SAME fingerprint (UA must not affect the key): got %q vs %q", b, a)
	}
	// 不同 IP → 不同指纹（ClientIP 是唯一区分维度）。
	if b := fp("198.51.100.1:5555", "codex-cli/1.0"); b == a {
		t.Fatalf("different IP must yield different fingerprint, both = %q", a)
	}
	// IP 为空（无论 UA 是否存在）→ "" → 引擎跳过设备维度，不建常量键。UA 不是信号，不能把无 IP
	// 的请求哈希成设备键。
	if empty := fp("", "codex-cli/1.0"); empty != "" {
		t.Fatalf("empty IP with a UA present must still yield empty fingerprint (UA is not a signal), got %q", empty)
	}
	if empty := fp("", ""); empty != "" {
		t.Fatalf("no signal must yield empty fingerprint, got %q", empty)
	}
}

// ============================================================================
// HandleListSubscriptions
// ============================================================================

func TestHandleListSubscriptions_MainSiteFallback(t *testing.T) {
	app := newBuyerTestApp(t)
	seedPlatformPlan(t, app)

	c, rec := newBuyerCtx("www.wedreamhub.com", nil, 7, "GET", "", nil)
	app.HandleListSubscriptions(c)

	r := decodeResp(t, rec)
	if !r.Success {
		t.Fatalf("main-site subscriptions listing should succeed, got %+v", r)
	}
	var subs []buyerSubOut
	if err := common.Unmarshal(r.Data, &subs); err != nil {
		t.Fatalf("decode subs: %v", err)
	}
	if len(subs) != 0 {
		t.Fatalf("subs = %+v, want empty (no purchase made yet)", subs)
	}
}

func TestHandleListSubscriptions_UnregisteredSubdomainStillTenantNotFound(t *testing.T) {
	app := newBuyerTestApp(t)
	c, rec := newBuyerCtx("foo.wedreamhub.com", nil, 7, "GET", "", nil)
	app.HandleListSubscriptions(c)

	r := decodeResp(t, rec)
	if r.Success || r.Code != "TENANT_NOT_FOUND" {
		t.Fatalf("unregistered subdomain must still be TENANT_NOT_FOUND, got %+v", r)
	}
}

// TestHandleListTokenPlans_NativePlanIDMapped 一键续费(P3-RNW 降级版)回归:买家套餐卡须携带
// native_plan_id(mt_native_subscription_plans 映射)——满额/到期横幅只知道原生订阅的 plan_id,
// 靠它反查"我们的"套餐 id 拼 /plans?renew=<id> 深链;无映射(尚未售出过)时为 0。
func TestHandleListTokenPlans_NativePlanIDMapped(t *testing.T) {
	app := newBuyerTestApp(t)
	ctx := context.Background()
	acme, err := app.TenantService.Create(ctx, tenant.CreateTenantInput{Slug: "acme2", Name: "Acme2", TokenplanEnabled: true})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	planID, err := app.TokenPlanRepo.EnsurePlan(ctx, &tokenplan.Plan{
		Code: "solo", Name: "Solo", BasePrice: 10, AnchorPrice: 20, Multiplier: 1,
		MonthLimitUSD: 5, ValidDays: 30, AgentCostPrice: 3, MinPrice: 4, Status: tokenplan.PlanEnabled,
	})
	if err != nil {
		t.Fatalf("EnsurePlan: %v", err)
	}
	if err := app.TokenPlanRepo.EnsureListing(ctx, acme.ID, planID, true, 12); err != nil {
		t.Fatalf("EnsureListing: %v", err)
	}
	// 造映射:该 tokenplan 已对应原生 plan 777。
	if err := app.DB.Create(&model.TokenPlanNativeSubscriptionPlan{TokenPlanID: planID, NativePlanID: 777}).Error; err != nil {
		t.Fatalf("seed native map: %v", err)
	}

	c, rec := newBuyerCtx("acme2.wedreamhub.com", acme, 1, "GET", "", nil)
	app.HandleListTokenPlans(c)

	r := decodeResp(t, rec)
	if !r.Success {
		t.Fatalf("listing should succeed, got %+v", r)
	}
	var plans []buyerPlanOut
	if err := common.Unmarshal(r.Data, &plans); err != nil {
		t.Fatalf("decode plans: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("plans = %d, want 1", len(plans))
	}
	if plans[0].NativePlanID != 777 {
		t.Fatalf("native_plan_id = %d, want 777(横幅原生 plan_id→我们套餐 id 的反查依据)", plans[0].NativePlanID)
	}
}
