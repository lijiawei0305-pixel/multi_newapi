package mtwire

// 充值订单状态查询端点单测（目标③ 修复「付完款不跳转」：微信扫码支付后前端轮询本端点探活）。
//
// 覆盖：已支付 → paid:true；待支付 → paid:false；**越权（另一用户查询）→ 404，不泄露订单存在与否**
// （最重要的一条——HARD constraint 越权红线）；缺 order_no → 400。
//
// 复用既有测试基建：apiResp/decodeResp（distribution_test.go）、testCtx/setID（ticket_http_test.go）
// 均为同包 mtwire，无需重复定义。Gateway 装配参照 recharge_test.go 的内存假实现风格
// （payment.NewMemRepo + payment.NewStubPaySDK），无需真实 DB。

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/payment"
)

// newRechargeStatusApp 装配仅含 RechargeGateway 的最小 App：内存 repo + 占位 SDK，
// 供 order-status 端点单测直接摆订单状态，不依赖真实 DB/sinks。
func newRechargeStatusApp() (*App, *payment.MemRepo) {
	gin.SetMode(gin.TestMode)
	repo := payment.NewMemRepo()
	sdk := payment.NewStubPaySDK("recharge-status-test-secret")
	gw := payment.NewGateway(repo, sdk, map[payment.OrderType]payment.OrderSink{})
	return &App{RechargeGateway: gw}, repo
}

// seedRechargeOrder 经 Gateway.CreateOrder 落一笔 created 态充值订单（归属 userID），返回 order_no。
func seedRechargeOrder(t *testing.T, app *App, userID int64) string {
	t.Helper()
	ord, err := app.RechargeGateway.CreateOrder(context.Background(), payment.OrderInput{
		Type:       payment.OrderTypeRecharge,
		TenantID:   1,
		UserID:     userID,
		Provider:   payment.ProviderWxpay,
		AmountUSD:  10,
		ActualPaid: 73,
	})
	if err != nil {
		t.Fatalf("seed recharge order: %v", err)
	}
	return ord.OrderNo
}

type rechargeStatusOut struct {
	Paid   bool   `json:"paid"`
	Status string `json:"status"`
}

// TestRechargeStatusPaid：已支付订单 + 本人查询 → paid:true, status:"paid"。
func TestRechargeStatusPaid(t *testing.T) {
	app, repo := newRechargeStatusApp()
	orderNo := seedRechargeOrder(t, app, 100)
	if ok, err := repo.CompareAndSetStatus(context.Background(), orderNo, payment.OrderCreated, payment.OrderPaid); err != nil || !ok {
		t.Fatalf("advance order to paid: ok=%v err=%v", ok, err)
	}

	c, w := testCtx("GET", "/api/tenant/wallet/recharge/status?order_no="+orderNo, "")
	setID(c, 100)
	app.HandleWalletRechargeStatus(c)

	r := decodeResp(t, w)
	if !r.Success {
		t.Fatalf("paid status request failed: code=%s body=%s", r.Code, w.Body.String())
	}
	var out rechargeStatusOut
	if err := json.Unmarshal(r.Data, &out); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if !out.Paid || out.Status != string(payment.OrderPaid) {
		t.Fatalf("paid status = %+v, want paid=true status=%q", out, payment.OrderPaid)
	}
}

// TestRechargeStatusCreatedNotPaid：待支付订单 + 本人查询 → paid:false, status:"created"。
func TestRechargeStatusCreatedNotPaid(t *testing.T) {
	app, _ := newRechargeStatusApp()
	orderNo := seedRechargeOrder(t, app, 100)

	c, w := testCtx("GET", "/api/tenant/wallet/recharge/status?order_no="+orderNo, "")
	setID(c, 100)
	app.HandleWalletRechargeStatus(c)

	r := decodeResp(t, w)
	if !r.Success {
		t.Fatalf("created status request failed: code=%s body=%s", r.Code, w.Body.String())
	}
	var out rechargeStatusOut
	if err := json.Unmarshal(r.Data, &out); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if out.Paid || out.Status != string(payment.OrderCreated) {
		t.Fatalf("created status = %+v, want paid=false status=%q", out, payment.OrderCreated)
	}
}

// TestRechargeStatusIsolation：订单归属用户 100，用户 200 查询同一 order_no → 404，
// 且不得返回区别于「订单不存在」的信号（越权红线，HARD constraint 中最重要的一条）。
func TestRechargeStatusIsolation(t *testing.T) {
	app, _ := newRechargeStatusApp()
	orderNo := seedRechargeOrder(t, app, 100) // owner = 100

	c, w := testCtx("GET", "/api/tenant/wallet/recharge/status?order_no="+orderNo, "")
	setID(c, 200) // 另一用户尝试查询
	app.HandleWalletRechargeStatus(c)

	r := decodeResp(t, w)
	if r.Success {
		t.Fatalf("cross-user status query succeeded, want isolation failure: %s", w.Body.String())
	}
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-user status HTTP = %d, want %d (isolation must not leak via status code)", w.Code, http.StatusNotFound)
	}
	if r.Code != payment.CodeOrderNotFound {
		t.Fatalf("cross-user status code = %q, want %q (must look identical to a missing order)", r.Code, payment.CodeOrderNotFound)
	}

	// 对照组：真正不存在的订单号也必须给出完全相同的 404 + code，验证「不存在」与「越权」不可区分。
	c2, w2 := testCtx("GET", "/api/tenant/wallet/recharge/status?order_no=NOSUCHORDER", "")
	setID(c2, 200)
	app.HandleWalletRechargeStatus(c2)
	r2 := decodeResp(t, w2)
	if w2.Code != w.Code || r2.Code != r.Code {
		t.Fatalf("isolation response (%d %s) distinguishable from not-found response (%d %s)", w.Code, r.Code, w2.Code, r2.Code)
	}
}

// TestRechargeStatusMissingOrderNo：缺 order_no 查询参数 → 400。
func TestRechargeStatusMissingOrderNo(t *testing.T) {
	app, _ := newRechargeStatusApp()

	c, w := testCtx("GET", "/api/tenant/wallet/recharge/status", "")
	setID(c, 100)
	app.HandleWalletRechargeStatus(c)

	r := decodeResp(t, w)
	if r.Success {
		t.Fatalf("missing order_no succeeded, want 400: %s", w.Body.String())
	}
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing order_no HTTP = %d, want 400", w.Code)
	}
}
