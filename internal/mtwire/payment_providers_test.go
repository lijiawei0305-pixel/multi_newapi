package mtwire

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/payment"
	"github.com/QuantumNous/new-api/internal/tenant"
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
	app := &App{}

	// 缺 Host 租户 → TENANT_NOT_FOUND。
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
