package authservice

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHealthzProvidersMock mock 模式 /auth/healthz 返回 providers，wxpay/alipay 均 true
// （mock 下单走确认页，不需真实凭据，故视为已配置）。
func TestHealthzProvidersMock(t *testing.T) {
	srv := newTestServer(t, "http://main.test/api/internal/order/paid")

	req := httptest.NewRequest(http.MethodGet, "/auth/healthz", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("healthz status=%d, body=%s", w.Code, w.Body.String())
	}

	var r struct {
		Success   bool `json:"success"`
		Mock      bool `json:"mock"`
		Providers struct {
			Wxpay  bool `json:"wxpay"`
			Alipay bool `json:"alipay"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	if !r.Success || !r.Mock {
		t.Fatalf("want success+mock, got %+v", r)
	}
	if !r.Providers.Wxpay || !r.Providers.Alipay {
		t.Fatalf("mock mode must report both providers configured=true, got %+v", r.Providers)
	}
}
