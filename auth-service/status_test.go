package authservice

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// doStatus 发起查单请求，返回 HTTP code + 解析出的 paid/status。
func doStatus(t *testing.T, srv *Server, orderNo, secret string) (code int, paid bool, status string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/auth/order/status?order_no="+url.QueryEscape(orderNo), nil)
	if secret != "" {
		req.Header.Set(internalSecretHeader, secret)
	}
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	var body struct {
		Data struct {
			Paid   bool   `json:"paid"`
			Status string `json:"status"`
		} `json:"data"`
	}
	_ = json.NewDecoder(w.Body).Decode(&body)
	return w.Code, body.Data.Paid, body.Data.Status
}

// TestOrderStatusQuery 查单端点（②）：created→未付; 确认后→已付; 错密钥→401; 未知单→404。
// 这是主站 SUB 卡单对账（③）的依据：pending 套餐单查到 paid 才补激活。
func TestOrderStatusQuery(t *testing.T) {
	main := &mainSiteStub{}
	ms := httptest.NewServer(http.HandlerFunc(main.handler))
	defer ms.Close()
	srv := newTestServer(t, ms.URL)
	createOrder(t, srv, "SUB-st", "alipay")

	// 1) created → 未付
	if code, paid, _ := doStatus(t, srv, "SUB-st", "shared-secret"); code != http.StatusOK || paid {
		t.Fatalf("created: code=%d paid=%v, want 200/unpaid", code, paid)
	}

	// 2) 确认支付 → 已付（确认页合成回调→created→paid→forward 入账）
	form := url.Values{"order": {"SUB-st"}}
	cr := httptest.NewRequest(http.MethodPost, "/auth/mock/confirm", strings.NewReader(form.Encode()))
	cr.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.Router().ServeHTTP(httptest.NewRecorder(), cr)

	if code, paid, status := doStatus(t, srv, "SUB-st", "shared-secret"); code != http.StatusOK || !paid {
		t.Fatalf("after confirm: code=%d paid=%v status=%q, want 200/paid", code, paid, status)
	}

	// 3) 错密钥 → 401
	if code, _, _ := doStatus(t, srv, "SUB-st", "wrong-secret"); code != http.StatusUnauthorized {
		t.Fatalf("wrong secret → %d, want 401", code)
	}

	// 4) 未知单 → 404
	if code, _, _ := doStatus(t, srv, "nope", "shared-secret"); code != http.StatusNotFound {
		t.Fatalf("unknown order → %d, want 404", code)
	}
}
