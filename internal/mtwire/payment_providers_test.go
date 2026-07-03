package mtwire

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/payment"
	"github.com/QuantumNous/new-api/internal/tenant"
	tenantrepo "github.com/QuantumNous/new-api/internal/tenant/gormrepo"
)

// newTenantReqCtx 构造带 Host 租户（可为 nil）+ 可选 user id + JSON body + 路由参数的 gin 上下文。
func newTenantReqCtx(method, body string, tn *tenant.Tenant, userID int, params gin.Params) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(method, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	if tn != nil {
		c.Set(ginKeyTenant, tn)
	}
	if userID > 0 {
		c.Set("id", userID)
	}
	if params != nil {
		c.Params = params
	}
	return c, rec
}

// stubProviderConfigured 替换 providerConfigured seam（按 provider 返回固定 configured）；测试结束自动还原。
func stubProviderConfigured(t *testing.T, fn func(provider payment.Provider) bool) {
	t.Helper()
	orig := providerConfigured
	t.Cleanup(func() { providerConfigured = orig })
	providerConfigured = func(_ *App, provider payment.Provider) bool { return fn(provider) }
}

// TestEnsureProviderUsable configured → 放行；未 configured → errProviderDisabled。
func TestEnsureProviderUsable(t *testing.T) {
	app := &App{}
	ctx := context.Background()

	// 仅 wxpay configured。
	stubProviderConfigured(t, func(p payment.Provider) bool { return p == payment.ProviderWxpay })

	if err := app.ensureProviderUsable(ctx, payment.ProviderWxpay); err != nil {
		t.Fatalf("configured wxpay must pass: %v", err)
	}
	if err := app.ensureProviderUsable(ctx, payment.ProviderAlipay); err == nil {
		t.Fatal("not-configured alipay must be rejected")
	}
}

// TestHandleTenantRechargeMethods 缺租户报错 / 仅 Configured 渠道入选 / 两渠道都不可用给空列表。
func TestHandleTenantRechargeMethods(t *testing.T) {
	app := &App{} // 零值 App：TenantRepo==nil，resolveBuyerTenant 据此短路（不解析 IsMainSiteHost）。

	// 缺 Host 租户 → TENANT_NOT_FOUND（resolveBuyerTenant 的 a.TenantRepo==nil 保守短路分支；
	// 主站回退场景见下方 TestHandleTenantRechargeMethods_MainSiteFallback，用真实 TenantRepo 装配）。
	c, rec := newTenantReqCtx(http.MethodGet, "", nil, 0, nil)
	app.HandleTenantRechargeMethods(c)
	if r := decodeResp(t, rec); r.Success || r.Code != "TENANT_NOT_FOUND" {
		t.Fatalf("no tenant: success=%v code=%q, want TENANT_NOT_FOUND", r.Success, r.Code)
	}

	// 仅 wxpay configured → [wxpay]。
	stubProviderConfigured(t, func(p payment.Provider) bool { return p == payment.ProviderWxpay })
	c, rec = newTenantReqCtx(http.MethodGet, "", &tenant.Tenant{ID: 1}, 0, nil)
	app.HandleTenantRechargeMethods(c)
	if got := decodeMethods(t, rec); !equalStrSlice(got, []string{"wxpay"}) {
		t.Fatalf("methods=%v want [wxpay]", got)
	}

	// 两渠道都 configured → [wxpay, alipay]（顺序固定）。
	stubProviderConfigured(t, func(p payment.Provider) bool { return true })
	c, rec = newTenantReqCtx(http.MethodGet, "", &tenant.Tenant{ID: 1}, 0, nil)
	app.HandleTenantRechargeMethods(c)
	if got := decodeMethods(t, rec); !equalStrSlice(got, []string{"wxpay", "alipay"}) {
		t.Fatalf("methods=%v want [wxpay alipay]", got)
	}

	// 两渠道都不可用 → []（前端据此隐藏整卡）。
	stubProviderConfigured(t, func(p payment.Provider) bool { return false })
	c, rec = newTenantReqCtx(http.MethodGet, "", &tenant.Tenant{ID: 1}, 0, nil)
	app.HandleTenantRechargeMethods(c)
	if got := decodeMethods(t, rec); len(got) != 0 {
		t.Fatalf("methods=%v want [] (none configured)", got)
	}
}

// TestHandleWalletRecharge_RejectsUnusableProvider 下单时不可用渠道被拒（PROVIDER_DISABLED，
// 不触达 CreateOrder：RechargeGateway 为 nil 也不 panic）。
func TestHandleWalletRecharge_RejectsUnusableProvider(t *testing.T) {
	app := &App{} // RechargeGateway=nil：被拒前不应触达
	stubProviderConfigured(t, func(p payment.Provider) bool { return false })

	for _, prov := range []string{"wxpay", "alipay"} {
		c, rec := newTenantReqCtx(http.MethodPost, `{"amount_usd":10,"provider":"`+prov+`"}`, &tenant.Tenant{ID: 1}, 100, nil)
		app.HandleWalletRecharge(c)
		if r := decodeResp(t, rec); r.Success || r.Code != "PROVIDER_DISABLED" {
			t.Fatalf("unusable %s recharge: success=%v code=%q, want PROVIDER_DISABLED", prov, r.Success, r.Code)
		}
	}
}

// ============================================================================
// 主站 Host 回退平台租户（resolveBuyerTenant）：HandleWalletRecharge / HandleTenantRechargeMethods
//
// 修复的正是「Recharge (same root cause)」——HandleWalletRecharge 原先与 3 个买家套餐端点同款
// tenantFrom(c)==nil→TENANT_NOT_FOUND。HandleTenantRechargeMethods 虽未在报告里点名，但前端
// useRechargeMethods 把它当作充值卡是否渲染的单一开关（获取失败即回退空集隐藏整卡，见
// web/default/src/features/wallet/api.ts getTenantRechargeMethods 注释）——若只修
// HandleWalletRecharge、不修它，主站充值卡仍会因为这一个端点 404 而整卡不渲染，"充值在主站可用"
// 依旧不成立，故一并納入同一 resolveBuyerTenant 修复（同一根因、同一 main-site direct-sales 口径）。
// ============================================================================

// newMainSiteRechargeTestApp 装配一个真实 TenantRepo/TenantService（main-site 回退所需，见
// resolveBuyerTenant）+ 内存 RechargeGateway（payment.NewMemRepo/NewStubPaySDK，风格同
// recharge_status_test.go newRechargeStatusApp，不依赖真实 DB 落充值订单）的 App。
func newMainSiteRechargeTestApp(t *testing.T) *App {
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
	tr := tenantrepo.New(db)
	gw := payment.NewGateway(payment.NewMemRepo(), payment.NewStubPaySDK("main-site-recharge-test-secret"), map[payment.OrderType]payment.OrderSink{})
	return &App{
		DB:              db,
		TenantRepo:      tr,
		TenantService:   tenant.NewService(tr, tenant.NewSlugValidator()),
		RechargeGateway: gw,
	}
}

// TestHandleWalletRecharge_MainSiteFallback 是本次改动要修的核心场景：主站 Host（无租户命中）+
// 已 seed 平台租户 + 可用渠道 → 下单成功（不再 TENANT_NOT_FOUND），订单落到平台租户名下。
func TestHandleWalletRecharge_MainSiteFallback(t *testing.T) {
	app := newMainSiteRechargeTestApp(t)
	pt, _, err := app.ensurePlatformTenant(context.Background())
	if err != nil {
		t.Fatalf("ensurePlatformTenant: %v", err)
	}
	stubProviderConfigured(t, func(p payment.Provider) bool { return p == payment.ProviderWxpay })

	c, rec := newBuyerCtx("www.wedreamhub.com", nil, 100, http.MethodPost, `{"amount_usd":10,"provider":"wxpay"}`, nil)
	app.HandleWalletRecharge(c)

	r := decodeResp(t, rec)
	if !r.Success {
		t.Fatalf("main-site recharge should succeed, got %+v", r)
	}
	var out struct {
		OrderNo string `json:"order_no"`
	}
	if err := json.Unmarshal(r.Data, &out); err != nil {
		t.Fatalf("decode recharge result: %v", err)
	}
	if out.OrderNo == "" {
		t.Fatalf("recharge result = %+v, want non-empty order_no", out)
	}
	ord, err := app.RechargeGateway.GetByOrderNo(context.Background(), out.OrderNo)
	if err != nil {
		t.Fatalf("GetByOrderNo: %v", err)
	}
	if ord.TenantID != pt.ID {
		t.Fatalf("recharge order tenant_id = %d, want platform tenant %d", ord.TenantID, pt.ID)
	}
}

// TestHandleWalletRecharge_UnregisteredSubdomainStillTenantNotFound 校验「*.wedreamhub.com 下未注册
// 子域」维持既有 TENANT_NOT_FOUND，不因平台租户已 seed 就被误判为主站。
func TestHandleWalletRecharge_UnregisteredSubdomainStillTenantNotFound(t *testing.T) {
	app := newMainSiteRechargeTestApp(t)
	if _, _, err := app.ensurePlatformTenant(context.Background()); err != nil {
		t.Fatalf("ensurePlatformTenant: %v", err)
	}
	stubProviderConfigured(t, func(p payment.Provider) bool { return true })

	c, rec := newBuyerCtx("foo.wedreamhub.com", nil, 100, http.MethodPost, `{"amount_usd":10,"provider":"wxpay"}`, nil)
	app.HandleWalletRecharge(c)

	r := decodeResp(t, rec)
	if r.Success || r.Code != "TENANT_NOT_FOUND" {
		t.Fatalf("unregistered subdomain recharge must still be TENANT_NOT_FOUND, got %+v", r)
	}
}

// TestHandleWalletRecharge_RealTenantHostUnaffected 回归：真实代理子域名（TenantMiddleware 已解析）
// 场景零改动——直接用已解析租户，不经 IsMainSiteHost 判断（即便 Host 字符串凑巧不是保留域名）。
func TestHandleWalletRecharge_RealTenantHostUnaffected(t *testing.T) {
	app := newMainSiteRechargeTestApp(t)
	acme, err := app.TenantService.Create(context.Background(), tenant.CreateTenantInput{Slug: "acme", Name: "Acme", TokenplanEnabled: true})
	if err != nil {
		t.Fatalf("create acme: %v", err)
	}
	stubProviderConfigured(t, func(p payment.Provider) bool { return true })

	c, rec := newBuyerCtx("acme.wedreamhub.com", acme, 100, http.MethodPost, `{"amount_usd":10,"provider":"wxpay"}`, nil)
	app.HandleWalletRecharge(c)

	r := decodeResp(t, rec)
	if !r.Success {
		t.Fatalf("resolved-tenant recharge should succeed, got %+v", r)
	}
	var out struct {
		OrderNo string `json:"order_no"`
	}
	if err := json.Unmarshal(r.Data, &out); err != nil {
		t.Fatalf("decode recharge result: %v", err)
	}
	ord, err := app.RechargeGateway.GetByOrderNo(context.Background(), out.OrderNo)
	if err != nil {
		t.Fatalf("GetByOrderNo: %v", err)
	}
	if ord.TenantID != acme.ID {
		t.Fatalf("recharge order tenant_id = %d, want acme's own tenant %d (unaffected by main-site fallback)", ord.TenantID, acme.ID)
	}
}

// TestHandleTenantRechargeMethods_MainSiteFallback 主站 Host + 已 seed 平台租户 + 可用渠道 →
// 返回渠道列表（不再 TENANT_NOT_FOUND）——前端据此在主站正常渲染充值卡。
func TestHandleTenantRechargeMethods_MainSiteFallback(t *testing.T) {
	app := newMainSiteRechargeTestApp(t)
	if _, _, err := app.ensurePlatformTenant(context.Background()); err != nil {
		t.Fatalf("ensurePlatformTenant: %v", err)
	}
	stubProviderConfigured(t, func(p payment.Provider) bool { return true })

	c, rec := newBuyerCtx("www.wedreamhub.com", nil, 100, http.MethodGet, "", nil)
	app.HandleTenantRechargeMethods(c)

	if got := decodeMethods(t, rec); !equalStrSlice(got, []string{"wxpay", "alipay"}) {
		t.Fatalf("main-site methods=%v want [wxpay alipay]", got)
	}
}

// TestHandleTenantRechargeMethods_UnregisteredSubdomainStillTenantNotFound 校验未注册子域仍
// TENANT_NOT_FOUND（同 HandleWalletRecharge 的对应用例）。
func TestHandleTenantRechargeMethods_UnregisteredSubdomainStillTenantNotFound(t *testing.T) {
	app := newMainSiteRechargeTestApp(t)
	if _, _, err := app.ensurePlatformTenant(context.Background()); err != nil {
		t.Fatalf("ensurePlatformTenant: %v", err)
	}
	stubProviderConfigured(t, func(p payment.Provider) bool { return true })

	c, rec := newBuyerCtx("foo.wedreamhub.com", nil, 100, http.MethodGet, "", nil)
	app.HandleTenantRechargeMethods(c)

	r := decodeResp(t, rec)
	if r.Success || r.Code != "TENANT_NOT_FOUND" {
		t.Fatalf("unregistered subdomain methods must still be TENANT_NOT_FOUND, got %+v", r)
	}
}

// ---- 测试辅助 ----

func decodeMethods(t *testing.T, rec *httptest.ResponseRecorder) []string {
	t.Helper()
	var r struct {
		Success bool `json:"success"`
		Data    struct {
			Methods []string `json:"methods"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatalf("decode methods %q: %v", rec.Body.String(), err)
	}
	return r.Data.Methods
}

func equalStrSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
