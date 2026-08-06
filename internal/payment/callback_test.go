package payment

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

// seedOrder 直接在 repo 预置一个 created 订单，返回其 order_no。
func seedOrder(t *testing.T, repo *MemRepo, typ OrderType) string {
	t.Helper()
	no := "PAY-" + string(typ)
	o := &PayOrder{
		OrderNo: no, Type: typ, TenantID: 7, UserID: 42, Provider: ProviderWxpay,
		AmountUSD: 100, ActualPaid: 120, GroupID: 3, PlanID: 9, Status: OrderCreated,
	}
	if err := repo.Create(context.Background(), o); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return no
}

// signedRaw 用 StubPaySDK 生成一条对应 order_no 的合法回调原文。
func signedRaw(orderNo string, success bool) []byte {
	return NewStubPaySDK("secret").Encode(CallbackInfo{OrderNo: orderNo, Success: success, PaidAmount: 120, TxnID: "txn-" + orderNo})
}

func TestCallbackRechargeDispatchesAndCredits(t *testing.T) {
	ctx := context.Background()
	g, repo, recharge, sub := newGateway()
	no := seedOrder(t, repo, OrderTypeRecharge)

	if err := g.HandleWxpay(ctx, signedRaw(no, true)); err != nil {
		t.Fatalf("HandleWxpay: %v", err)
	}
	// 分发到 recharge sink，绑定 tenant/user/amount，type=recharge。
	if recharge.count() != 1 || sub.count() != 0 {
		t.Fatalf("dispatch counts recharge=%d sub=%d, want 1/0", recharge.count(), sub.count())
	}
	po, _ := recharge.last()
	if po.Type != OrderTypeRecharge || po.TenantID != 7 || po.UserID != 42 || po.AmountUSD != 100 || po.ActualPaid != 120 {
		t.Fatalf("paid order = %+v", po)
	}
	if po.Reference != "txn-"+no {
		t.Fatalf("reference = %q, want platform txn id", po.Reference)
	}
	// 订单置 credited 终态。
	got, _ := repo.GetByOrderNo(ctx, no)
	if got.Status != OrderCredited {
		t.Fatalf("status = %q, want credited", got.Status)
	}
}

func TestCallbackSubscriptionDispatchesToSubSink(t *testing.T) {
	ctx := context.Background()
	g, repo, recharge, sub := newGateway()
	no := seedOrder(t, repo, OrderTypeSubscription)
	// seedOrder 默认 ProviderWxpay，须与回调渠道一致（PAY-FACT-01 渠道绑定）。
	if err := g.HandleWxpay(ctx, signedRaw(no, true)); err != nil {
		t.Fatalf("HandleWxpay: %v", err)
	}
	if sub.count() != 1 || recharge.count() != 0 {
		t.Fatalf("dispatch counts sub=%d recharge=%d, want 1/0", sub.count(), recharge.count())
	}
	po, _ := sub.last()
	if po.Type != OrderTypeSubscription || po.OrderNo != no || po.PlanID != 9 {
		t.Fatalf("paid order = %+v", po)
	}
}

func TestCallbackRejectsBadSign(t *testing.T) {
	ctx := context.Background()
	g, repo, recharge, _ := newGateway()
	no := seedOrder(t, repo, OrderTypeRecharge)

	bad := []byte(`{"order_no":"` + no + `","success":true,"amount":120,"sign":"forged"}`)
	if got := apperr.CodeOf(g.HandleWxpay(ctx, bad)); got != CodeSignInvalid {
		t.Fatalf("bad sign code = %q, want %q", got, CodeSignInvalid)
	}
	// 验签失败 → 不入账，订单仍 created。
	if recharge.count() != 0 {
		t.Fatal("must not credit on bad sign")
	}
	got, _ := repo.GetByOrderNo(ctx, no)
	if got.Status != OrderCreated {
		t.Fatalf("status = %q, want created", got.Status)
	}
}

func TestCallbackUnknownOrder(t *testing.T) {
	g, _, recharge, sub := newGateway()
	err := g.HandleWxpay(context.Background(), signedRaw("GHOST", true))
	if got := apperr.CodeOf(err); got != CodeOrderNotFound {
		t.Fatalf("unknown order code = %q, want %q", got, CodeOrderNotFound)
	}
	if recharge.count() != 0 || sub.count() != 0 {
		t.Fatal("must not dispatch for unknown order")
	}
}

func TestCallbackMalformedReportsCallbackInvalid(t *testing.T) {
	g, _, _, _ := newGateway()
	if got := apperr.CodeOf(g.HandleWxpay(context.Background(), []byte("garbage"))); got != CodeCallbackInvalid {
		t.Fatalf("malformed code = %q, want %q", got, CodeCallbackInvalid)
	}
}

func TestCallbackDuplicateIsIdempotent(t *testing.T) {
	ctx := context.Background()
	g, repo, recharge, _ := newGateway()
	no := seedOrder(t, repo, OrderTypeRecharge)
	raw := signedRaw(no, true)

	if err := g.HandleWxpay(ctx, raw); err != nil {
		t.Fatalf("first callback: %v", err)
	}
	// 第二次相同回调：短路成功，不重复入账。
	if err := g.HandleWxpay(ctx, raw); err != nil {
		t.Fatalf("duplicate callback should succeed (idempotent), got %v", err)
	}
	if recharge.count() != 1 {
		t.Fatalf("OnPaid called %d times, want exactly 1 (no double credit)", recharge.count())
	}
	got, _ := repo.GetByOrderNo(ctx, no)
	if got.Status != OrderCredited {
		t.Fatalf("status = %q, want credited", got.Status)
	}
}

func TestCallbackConcurrentCreditsOnce(t *testing.T) {
	ctx := context.Background()
	// 用可控 SDK，避免每次回调都做 HMAC，专注并发幂等；raw 即 order_no。
	repo := NewMemRepo()
	sink := newFakeSink()
	g := NewGateway(repo, &fakeSDK{verifyFn: okVerify()}, map[OrderType]OrderSink{OrderTypeRecharge: sink})
	no := seedOrder(t, repo, OrderTypeRecharge)

	const n = 64
	var wg sync.WaitGroup
	var errCount int32
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			if err := g.HandleWxpay(ctx, []byte(no)); err != nil {
				atomic.AddInt32(&errCount, 1)
			}
		}()
	}
	wg.Wait()

	if errCount != 0 {
		t.Fatalf("concurrent callbacks returned %d errors, want 0", errCount)
	}
	if sink.count() != 1 {
		t.Fatalf("OnPaid called %d times under concurrency, want exactly 1", sink.count())
	}
	got, _ := repo.GetByOrderNo(ctx, no)
	if got.Status != OrderCredited {
		t.Fatalf("status = %q, want credited", got.Status)
	}
}

func TestCallbackOnPaidErrorRollsBackAndRetrySucceeds(t *testing.T) {
	ctx := context.Background()
	g, repo, recharge, _ := newGateway()
	no := seedOrder(t, repo, OrderTypeRecharge)
	recharge.err = errors.New("wallet down")

	// 入账失败 → 错误上浮，订单回滚到 created（不丢账，可重试）。
	if err := g.HandleWxpay(ctx, signedRaw(no, true)); err == nil || err.Error() != "wallet down" {
		t.Fatalf("expected OnPaid error to propagate, got %v", err)
	}
	got, _ := repo.GetByOrderNo(ctx, no)
	if got.Status != OrderCreated {
		t.Fatalf("status after failed credit = %q, want created (rolled back)", got.Status)
	}

	// 网关重试：sink 恢复 → 成功入账一次。
	recharge.err = nil
	if err := g.HandleWxpay(ctx, signedRaw(no, true)); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if recharge.count() != 1 {
		t.Fatalf("after retry OnPaid count = %d, want 1", recharge.count())
	}
	got, _ = repo.GetByOrderNo(ctx, no)
	if got.Status != OrderCredited {
		t.Fatalf("status after retry = %q, want credited", got.Status)
	}
}

func TestCallbackPaymentFailedMarksFailed(t *testing.T) {
	ctx := context.Background()
	g, repo, recharge, _ := newGateway()
	no := seedOrder(t, repo, OrderTypeRecharge)

	// 平台明确失败（success=false）→ 订单 failed，不入账，对外 ack（nil）。
	if err := g.HandleWxpay(ctx, signedRaw(no, false)); err != nil {
		t.Fatalf("failed-payment callback should ack, got %v", err)
	}
	if recharge.count() != 0 {
		t.Fatal("must not credit on failed payment")
	}
	got, _ := repo.GetByOrderNo(ctx, no)
	if got.Status != OrderFailed {
		t.Fatalf("status = %q, want failed", got.Status)
	}
}

func TestCallbackUnknownTypeIsDefensive(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	// 仅注册 recharge sink；预置一个 subscription 订单制造「无对应 sink」。
	g := NewGateway(repo, &fakeSDK{verifyFn: okVerify()}, map[OrderType]OrderSink{OrderTypeRecharge: newFakeSink()})
	o := &PayOrder{
		OrderNo: "PAYX", Type: OrderTypeSubscription, TenantID: 1, UserID: 1,
		Provider: ProviderWxpay, ActualPaid: 120, ActualPaidFen: 12000, Status: OrderCreated,
	}
	_ = repo.Create(ctx, o)

	if got := apperr.CodeOf(g.HandleWxpay(ctx, []byte("PAYX"))); got != CodeOrderTypeUnknown {
		t.Fatalf("code = %q, want %q", got, CodeOrderTypeUnknown)
	}
	// 防御回滚：订单不应卡在 paid。
	got, _ := repo.GetByOrderNo(ctx, "PAYX")
	if got.Status != OrderCreated {
		t.Fatalf("status = %q, want created (rolled back)", got.Status)
	}
}
