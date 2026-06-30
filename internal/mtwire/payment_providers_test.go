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
)

// newPaymentProvidersApp 建一个仅含 mt_payment_provider_settings 的最小 App（sqlite :memory:）。
func newPaymentProvidersApp(t *testing.T) *App {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := migratePaymentProviders(db); err != nil {
		t.Fatalf("migrate payment providers: %v", err)
	}
	return &App{DB: db}
}

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

// stubProviderStatus 替换 providerStatusQuery seam，返回固定 configured；自动在测试结束还原。
func stubProviderStatus(t *testing.T, wx, ali bool, err error) {
	t.Helper()
	orig := providerStatusQuery
	t.Cleanup(func() { providerStatusQuery = orig })
	providerStatusQuery = func(_ *App, _ context.Context) (bool, bool, error) { return wx, ali, err }
}

// TestPaymentProviderStore_DefaultTrue 启用开关 get/set + 缺省（无记录）= true。
func TestPaymentProviderStore_DefaultTrue(t *testing.T) {
	app := newPaymentProvidersApp(t)
	store := newPaymentProviderStore(app.DB)
	ctx := context.Background()

	// 无记录 → exists=false、缺省启用。
	if _, exists := store.getEnabled(ctx, "wxpay"); exists {
		t.Fatal("no record must report exists=false")
	}
	if !store.providerEnabled(ctx, "wxpay") {
		t.Fatal("missing record must default to enabled=true")
	}

	// set false → 读到 false。
	if err := store.setEnabled(ctx, "wxpay", false); err != nil {
		t.Fatalf("setEnabled false: %v", err)
	}
	enabled, exists := store.getEnabled(ctx, "wxpay")
	if !exists || enabled {
		t.Fatalf("after set false: enabled=%v exists=%v, want false/true", enabled, exists)
	}
	if store.providerEnabled(ctx, "wxpay") {
		t.Fatal("disabled provider must report false")
	}

	// upsert true → 读到 true（同主键更新，不新增行）。
	if err := store.setEnabled(ctx, "wxpay", true); err != nil {
		t.Fatalf("setEnabled true: %v", err)
	}
	if !store.providerEnabled(ctx, "wxpay") {
		t.Fatal("re-enabled provider must report true")
	}
	var n int64
	app.DB.Model(&paymentProviderSetting{}).Where("provider = ?", "wxpay").Count(&n)
	if n != 1 {
		t.Fatalf("upsert must keep exactly 1 row, got %d", n)
	}
}

// TestEnsureProviderUsable 覆盖下单校验四分支：configured+enabled 放行 / enabled=false 拒 /
// 未 configured 拒 / auth-service 出错放行。
func TestEnsureProviderUsable(t *testing.T) {
	app := newPaymentProvidersApp(t)
	store := newPaymentProviderStore(app.DB)
	ctx := context.Background()

	// configured + enabled(缺省) → 放行。
	stubProviderStatus(t, true, true, nil)
	if err := app.ensureProviderUsable(ctx, payment.ProviderWxpay); err != nil {
		t.Fatalf("configured+enabled must pass: %v", err)
	}

	// enabled=false → 拒绝（且不查 configured）。
	if err := store.setEnabled(ctx, "wxpay", false); err != nil {
		t.Fatalf("disable wxpay: %v", err)
	}
	orig := providerStatusQuery
	t.Cleanup(func() { providerStatusQuery = orig })
	providerStatusQuery = func(_ *App, _ context.Context) (bool, bool, error) {
		t.Fatal("must not query configured when enabled=false")
		return false, false, nil
	}
	if err := app.ensureProviderUsable(ctx, payment.ProviderWxpay); err == nil {
		t.Fatal("disabled provider must be rejected")
	}

	// enabled(缺省) 但未 configured → 拒绝。
	stubProviderStatus(t, false, false, nil)
	if err := app.ensureProviderUsable(ctx, payment.ProviderAlipay); err == nil {
		t.Fatal("not-configured provider must be rejected")
	}

	// auth-service 出错 → 放行（不阻塞正常充值）。
	stubProviderStatus(t, false, false, errAuthClientUnset)
	if err := app.ensureProviderUsable(ctx, payment.ProviderAlipay); err != nil {
		t.Fatalf("auth-service error must allow through: %v", err)
	}
}

// TestHandleTenantRechargeMethods 过滤逻辑：缺租户报错 / 仅 enabled&&configured 入选 / auth-service 出错给空。
func TestHandleTenantRechargeMethods(t *testing.T) {
	app := newPaymentProvidersApp(t)
	store := newPaymentProviderStore(app.DB)
	ctx := context.Background()

	// 缺 Host 租户 → TENANT_NOT_FOUND。
	c, rec := newTenantReqCtx(http.MethodGet, "", nil, 0, nil)
	app.HandleTenantRechargeMethods(c)
	if r := decodeResp(t, rec); r.Success || r.Code != "TENANT_NOT_FOUND" {
		t.Fatalf("no tenant: success=%v code=%q, want TENANT_NOT_FOUND", r.Success, r.Code)
	}

	// wxpay configured、alipay 未 configured，无开关记录（缺省 enabled）→ 仅 [wxpay]。
	stubProviderStatus(t, true, false, nil)
	c, rec = newTenantReqCtx(http.MethodGet, "", &tenant.Tenant{ID: 1}, 0, nil)
	app.HandleTenantRechargeMethods(c)
	if got := decodeMethods(t, rec); !equalStrSlice(got, []string{"wxpay"}) {
		t.Fatalf("methods=%v want [wxpay]", got)
	}

	// 管理员禁用 wxpay → 即便 configured 也不可用 → []。
	if err := store.setEnabled(ctx, "wxpay", false); err != nil {
		t.Fatalf("disable wxpay: %v", err)
	}
	c, rec = newTenantReqCtx(http.MethodGet, "", &tenant.Tenant{ID: 1}, 0, nil)
	app.HandleTenantRechargeMethods(c)
	if got := decodeMethods(t, rec); len(got) != 0 {
		t.Fatalf("methods=%v want [] (wxpay disabled)", got)
	}

	// auth-service 出错 → 返回错误（非空成功）；前端据此 fail-open 回退两渠道，与 ensureProviderUsable 放行一致。
	stubProviderStatus(t, false, false, errAuthClientUnset)
	c, rec = newTenantReqCtx(http.MethodGet, "", &tenant.Tenant{ID: 1}, 0, nil)
	app.HandleTenantRechargeMethods(c)
	if r := decodeResp(t, rec); r.Success {
		t.Fatalf("auth-service error must return error (not empty success), got success=true body=%s", rec.Body.String())
	}
}

// TestHandleWalletRecharge_RejectsDisabledProvider 下单时禁用渠道被拒（PROVIDER_DISABLED，
// 且不触达 configured 查询与 CreateOrder）。
func TestHandleWalletRecharge_RejectsDisabledProvider(t *testing.T) {
	app := newPaymentProvidersApp(t)
	if err := newPaymentProviderStore(app.DB).setEnabled(context.Background(), "wxpay", false); err != nil {
		t.Fatalf("disable wxpay: %v", err)
	}
	// providerStatusQuery 不应被调用（enabled=false 先行拒绝）。
	orig := providerStatusQuery
	t.Cleanup(func() { providerStatusQuery = orig })
	providerStatusQuery = func(_ *App, _ context.Context) (bool, bool, error) {
		t.Fatal("must not query configured when provider disabled")
		return true, true, nil
	}

	c, rec := newTenantReqCtx(http.MethodPost, `{"amount_usd":10,"provider":"wxpay"}`, &tenant.Tenant{ID: 1}, 100, nil)
	app.HandleWalletRecharge(c)
	if r := decodeResp(t, rec); r.Success || r.Code != "PROVIDER_DISABLED" {
		t.Fatalf("disabled recharge: success=%v code=%q, want PROVIDER_DISABLED", r.Success, r.Code)
	}
}

// TestHandleWalletRecharge_RejectsUnconfiguredProvider 渠道 enabled（缺省）但未 configured → 拒绝。
func TestHandleWalletRecharge_RejectsUnconfiguredProvider(t *testing.T) {
	app := newPaymentProvidersApp(t)
	stubProviderStatus(t, false, false, nil) // 均未 configured

	c, rec := newTenantReqCtx(http.MethodPost, `{"amount_usd":10,"provider":"alipay"}`, &tenant.Tenant{ID: 1}, 100, nil)
	app.HandleWalletRecharge(c)
	if r := decodeResp(t, rec); r.Success || r.Code != "PROVIDER_DISABLED" {
		t.Fatalf("unconfigured recharge: success=%v code=%q, want PROVIDER_DISABLED", r.Success, r.Code)
	}
}

// TestHandleAdminListPaymentProviders configured 取自 seam、enabled 取自 store（缺省 true）。
func TestHandleAdminListPaymentProviders(t *testing.T) {
	app := newPaymentProvidersApp(t)
	if err := newPaymentProviderStore(app.DB).setEnabled(context.Background(), "alipay", false); err != nil {
		t.Fatalf("disable alipay: %v", err)
	}
	stubProviderStatus(t, true, false, nil) // wxpay configured、alipay 未配置

	c, rec := newTenantReqCtx(http.MethodGet, "", nil, 0, nil)
	app.HandleAdminListPaymentProviders(c)

	var r struct {
		Success bool `json:"success"`
		Data    struct {
			Wxpay  providerStatusOut `json:"wxpay"`
			Alipay providerStatusOut `json:"alipay"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	if !r.Success {
		t.Fatalf("want success, body=%s", rec.Body.String())
	}
	if !r.Data.Wxpay.Configured || !r.Data.Wxpay.Enabled {
		t.Fatalf("wxpay=%+v want configured+enabled", r.Data.Wxpay)
	}
	if r.Data.Alipay.Configured || r.Data.Alipay.Enabled {
		t.Fatalf("alipay=%+v want not-configured+disabled", r.Data.Alipay)
	}
}

// TestHandleAdminListPaymentProviders_AuthServiceDown auth-service 出错 → configured 全 false、不 500。
func TestHandleAdminListPaymentProviders_AuthServiceDown(t *testing.T) {
	app := newPaymentProvidersApp(t)
	stubProviderStatus(t, false, false, errAuthClientUnset)

	c, rec := newTenantReqCtx(http.MethodGet, "", nil, 0, nil)
	app.HandleAdminListPaymentProviders(c)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200 (must not 500 on auth-service error)", rec.Code)
	}
	var r struct {
		Success bool `json:"success"`
		Data    struct {
			Wxpay  providerStatusOut `json:"wxpay"`
			Alipay providerStatusOut `json:"alipay"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// configured 降级为 false，但 enabled 仍取自 store（缺省 true）。
	if r.Data.Wxpay.Configured || r.Data.Alipay.Configured {
		t.Fatalf("configured must be false on auth-service error, got %+v / %+v", r.Data.Wxpay, r.Data.Alipay)
	}
	if !r.Data.Wxpay.Enabled || !r.Data.Alipay.Enabled {
		t.Fatalf("enabled must default true, got %+v / %+v", r.Data.Wxpay, r.Data.Alipay)
	}
}

// TestHandleAdminSetPaymentProvider 设开关 + 落库；非法 provider 报错。
func TestHandleAdminSetPaymentProvider(t *testing.T) {
	app := newPaymentProvidersApp(t)

	// 设 wxpay enabled=false。
	c, rec := newTenantReqCtx(http.MethodPut, `{"enabled":false}`, nil, 0, gin.Params{{Key: "provider", Value: "wxpay"}})
	app.HandleAdminSetPaymentProvider(c)
	var r struct {
		Success bool `json:"success"`
		Data    struct {
			Provider string `json:"provider"`
			Enabled  bool   `json:"enabled"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	if !r.Success || r.Data.Provider != "wxpay" || r.Data.Enabled {
		t.Fatalf("set resp=%+v want {wxpay,false}", r.Data)
	}
	if newPaymentProviderStore(app.DB).providerEnabled(context.Background(), "wxpay") {
		t.Fatal("store must persist enabled=false")
	}

	// 非法 provider → PROVIDER_INVALID。
	c, rec = newTenantReqCtx(http.MethodPut, `{"enabled":true}`, nil, 0, gin.Params{{Key: "provider", Value: "paypal"}})
	app.HandleAdminSetPaymentProvider(c)
	if rr := decodeResp(t, rec); rr.Success || rr.Code != "PROVIDER_INVALID" {
		t.Fatalf("invalid provider: success=%v code=%q want PROVIDER_INVALID", rr.Success, rr.Code)
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
