package authservice

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/internal/payment"
)

// mainSiteStub 模拟主站 /api/internal/order/paid，记录收到的入账回调。
type mainSiteStub struct {
	mu        sync.Mutex
	calls     []string  // 收到的 order_no（按序）
	secrets   []string  // 每次的共享密钥头
	amounts   []float64 // 每次的 paid_amount（反篡改金额校验透传值）
	providers []string  // 每次的 provider
	status    int       // 返回状态（默认 200）
}

func (m *mainSiteStub) handler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		OrderNo    string  `json:"order_no"`
		TxnID      string  `json:"txn_id"`
		PaidAmount float64 `json:"paid_amount"`
		Provider   string  `json:"provider"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	m.mu.Lock()
	m.calls = append(m.calls, body.OrderNo)
	m.secrets = append(m.secrets, r.Header.Get(internalSecretHeader))
	m.amounts = append(m.amounts, body.PaidAmount)
	m.providers = append(m.providers, body.Provider)
	st := m.status
	m.mu.Unlock()
	if st == 0 {
		st = http.StatusOK
	}
	w.WriteHeader(st)
	_, _ = w.Write([]byte(`{"success":true}`))
}

func (m *mainSiteStub) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func newTestServer(t *testing.T, callbackURL string) *Server {
	t.Helper()
	var cfg Config
	cfg.Server.Addr = ":0"
	cfg.Server.PublicBaseURL = "https://example.test"
	cfg.Mock = true
	cfg.USDToCNYRate = 7.3
	cfg.SignSecret = "sign-secret"
	cfg.Internal.CallbackURL = callbackURL
	cfg.Internal.SharedSecret = "shared-secret"
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return srv
}

func createOrder(t *testing.T, srv *Server, orderNo, provider string) {
	t.Helper()
	body, _ := json.Marshal(createOrderRequest{
		OrderNo: orderNo, Provider: provider, AmountCNY: 73, AmountUSD: 10, Subject: "test",
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/order", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create order status = %d, body=%s", w.Code, w.Body.String())
	}
}

// TestMockConfirmForwardsToMainSite mock 确认 → 验签→幂等→调主站入账（带共享密钥头）。
func TestMockConfirmForwardsToMainSite(t *testing.T) {
	main := &mainSiteStub{}
	ts := httptest.NewServer(http.HandlerFunc(main.handler))
	defer ts.Close()
	srv := newTestServer(t, ts.URL)

	createOrder(t, srv, "RCG-ok", "wxpay")

	form := url.Values{"order": {"RCG-ok"}}
	req := httptest.NewRequest(http.MethodPost, "/auth/mock/confirm", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("confirm status = %d, body=%s", w.Code, w.Body.String())
	}

	if main.count() != 1 {
		t.Fatalf("main site called %d times, want 1", main.count())
	}
	if main.calls[0] != "RCG-ok" {
		t.Fatalf("forwarded order_no = %q, want RCG-ok", main.calls[0])
	}
	if main.secrets[0] != "shared-secret" {
		t.Fatalf("forwarded secret = %q, want shared-secret", main.secrets[0])
	}
	// 反篡改透传：paid_amount 与下单 AmountCNY 一致、provider 正确（供主站金额校验）。
	if main.amounts[0] != 73 {
		t.Fatalf("forwarded paid_amount = %v, want 73", main.amounts[0])
	}
	if main.providers[0] != "wxpay" {
		t.Fatalf("forwarded provider = %q, want wxpay", main.providers[0])
	}
}

// TestNotifyRejectsBadSignature 伪造/错签回调一律拒（不入账、不 forward）。
func TestNotifyRejectsBadSignature(t *testing.T) {
	main := &mainSiteStub{}
	ts := httptest.NewServer(http.HandlerFunc(main.handler))
	defer ts.Close()
	srv := newTestServer(t, ts.URL)
	createOrder(t, srv, "RCG-badsig", "wxpay")

	// 手工构造一个签名错误的回调原文（sign 字段乱填）。
	bad := `{"order_no":"RCG-badsig","success":true,"amount":73,"txn_id":"x","sign":"deadbeef"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/wxpay/notify", strings.NewReader(bad))
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad-sign notify status = %d, want 400", w.Code)
	}
	if main.count() != 0 {
		t.Fatalf("main site must not be called on bad signature, got %d", main.count())
	}
}

// TestNotifyIdempotentSingleForward 重复回调只入账一次（auth-service 侧 created→paid CAS 守门）。
func TestNotifyIdempotentSingleForward(t *testing.T) {
	main := &mainSiteStub{}
	ts := httptest.NewServer(http.HandlerFunc(main.handler))
	defer ts.Close()
	srv := newTestServer(t, ts.URL)
	createOrder(t, srv, "RCG-dup", "alipay")

	// 用 SDK 合成一条合法回调，重复投递三次。
	raw := srv.sdk.Encode(payment.CallbackInfo{
		Provider: payment.ProviderAlipay, OrderNo: "RCG-dup", Success: true, PaidAmount: 73, TxnID: "MOCK-RCG-dup",
	})
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/auth/alipay/notify", strings.NewReader(string(raw)))
		w := httptest.NewRecorder()
		srv.Router().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("notify #%d status = %d", i, w.Code)
		}
	}
	if main.count() != 1 {
		t.Fatalf("main site called %d times, want exactly 1 (idempotent)", main.count())
	}
}

// TestCreateOrderReturnsPayURL 下单返回 mock 支付凭据（确认页 URL）。
func TestCreateOrderReturnsPayURL(t *testing.T) {
	srv := newTestServer(t, "http://main.test/api/internal/order/paid")
	body, _ := json.Marshal(createOrderRequest{OrderNo: "RCG-url", Provider: "wxpay", AmountCNY: 73, AmountUSD: 10})
	req := httptest.NewRequest(http.MethodPost, "/auth/order", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			PayURL string `json:"pay_url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Success || !strings.Contains(resp.Data.PayURL, "/auth/mock/pay?order=RCG-url") {
		t.Fatalf("pay_url = %q", resp.Data.PayURL)
	}
}
