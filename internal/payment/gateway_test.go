package payment

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

// newGateway 组装一个用 StubPaySDK + 两个 fakeSink 的网关，供下单/回调用例复用。
func newGateway(opts ...Option) (*Gateway, *MemRepo, *fakeSink, *fakeSink) {
	repo := NewMemRepo()
	recharge, sub := newFakeSink(), newFakeSink()
	sinks := map[OrderType]OrderSink{
		OrderTypeRecharge:     recharge,
		OrderTypeSubscription: sub,
	}
	g := NewGateway(repo, NewStubPaySDK("secret"), sinks, opts...)
	return g, repo, recharge, sub
}

func TestCreateOrderPersistsAndBackfills(t *testing.T) {
	ctx := context.Background()
	g, repo, _, _ := newGateway(WithNotifyBaseURL("https://api.test"))

	o, err := g.CreateOrder(ctx, OrderInput{
		Type: OrderTypeRecharge, TenantID: 7, UserID: 42, Provider: ProviderWxpay,
		AmountUSD: 100, ActualPaid: 120, GroupID: 3, Subject: "充值",
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if o.OrderNo == "" {
		t.Fatal("order_no must be generated")
	}
	if o.Status != OrderCreated {
		t.Fatalf("status = %q, want created", o.Status)
	}
	if o.NotifyURL != "https://api.test/api/pay/wechat/notify" {
		t.Fatalf("notify_url = %q", o.NotifyURL)
	}
	if o.PayURL == "" {
		t.Fatal("pay_url must be backfilled from SDK")
	}
	// 已落库，可按 order_no 查回，绑定 tenant/user。
	got, err := repo.GetByOrderNo(ctx, o.OrderNo)
	if err != nil {
		t.Fatalf("persisted Get: %v", err)
	}
	if got.TenantID != 7 || got.UserID != 42 {
		t.Fatalf("persisted binding = tenant %d user %d", got.TenantID, got.UserID)
	}
}

func TestCreateOrderAlipayNotifyPath(t *testing.T) {
	g, _, _, _ := newGateway(WithNotifyBaseURL("https://x"))
	o, err := g.CreateOrder(context.Background(), OrderInput{
		Type: OrderTypeSubscription, TenantID: 1, UserID: 1, Provider: ProviderAlipay, PlanID: 5, ActualPaid: 30,
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if o.NotifyURL != "https://x/api/pay/alipay/notify" {
		t.Fatalf("alipay notify_url = %q", o.NotifyURL)
	}
}

func TestCreateOrderRejectsInvalidInput(t *testing.T) {
	g, _, _, _ := newGateway()
	_, err := g.CreateOrder(context.Background(), OrderInput{Type: OrderTypeRecharge, UserID: 1, Provider: ProviderWxpay, AmountUSD: 1})
	if got := apperr.CodeOf(err); got != CodeOrderInvalid {
		t.Fatalf("code = %q, want %q", got, CodeOrderInvalid)
	}
}

func TestCreateOrderDuplicateOrderNo(t *testing.T) {
	// 固定生成器制造 order_no 冲突。
	g, _, _, _ := newGateway(WithOrderNoFunc(func() string { return "FIXED" }))
	in := OrderInput{Type: OrderTypeRecharge, TenantID: 1, UserID: 1, Provider: ProviderWxpay, AmountUSD: 10}
	if _, err := g.CreateOrder(context.Background(), in); err != nil {
		t.Fatalf("first CreateOrder: %v", err)
	}
	_, err := g.CreateOrder(context.Background(), in)
	if got := apperr.CodeOf(err); got != CodeOrderDuplicate {
		t.Fatalf("duplicate code = %q, want %q", got, CodeOrderDuplicate)
	}
}

func TestCreateOrderPropagatesSDKError(t *testing.T) {
	repo := NewMemRepo()
	sdk := &fakeSDK{createErr: errors.New("sdk down"), verifyFn: okVerify()}
	g := NewGateway(repo, sdk, map[OrderType]OrderSink{OrderTypeRecharge: newFakeSink()})

	_, err := g.CreateOrder(context.Background(), OrderInput{
		Type: OrderTypeRecharge, TenantID: 1, UserID: 1, Provider: ProviderWxpay, AmountUSD: 10,
	})
	if err == nil || err.Error() != "sdk down" {
		t.Fatalf("expected SDK error to propagate, got %v", err)
	}
}

// TestCreateOrderMarksFailedOnSDKError 锁定审计 M3：改为「先落 created 订单、再向平台下单」后，
// 平台下单失败 → 本地订单落库并置 failed（终态），非孤儿、不被 ReconcileStuckCreated 反复查单。
func TestCreateOrderMarksFailedOnSDKError(t *testing.T) {
	repo := NewMemRepo()
	sdk := &fakeSDK{createErr: errors.New("sdk down"), verifyFn: okVerify()}
	g := NewGateway(repo, sdk, map[OrderType]OrderSink{OrderTypeRecharge: newFakeSink()},
		WithOrderNoFunc(func() string { return "RCG-FAILED" }))

	if _, err := g.CreateOrder(context.Background(), OrderInput{
		Type: OrderTypeRecharge, TenantID: 1, UserID: 1, Provider: ProviderWxpay, AmountUSD: 10,
	}); err == nil {
		t.Fatal("expected SDK error")
	}
	got, err := repo.GetByOrderNo(context.Background(), "RCG-FAILED")
	if err != nil {
		t.Fatalf("order must be persisted (created-then-failed), got: %v", err)
	}
	if got.Status != OrderFailed {
		t.Fatalf("status = %q, want failed", got.Status)
	}
}

func TestCreateOrderUsesInjectedClock(t *testing.T) {
	fixed := time.Date(2026, 6, 28, 0, 0, 0, 0, time.UTC)
	g, _, _, _ := newGateway(WithClock(func() time.Time { return fixed }))
	o, err := g.CreateOrder(context.Background(), OrderInput{
		Type: OrderTypeRecharge, TenantID: 1, UserID: 1, Provider: ProviderWxpay, AmountUSD: 10,
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if !o.CreatedAt.Equal(fixed) {
		t.Fatalf("CreatedAt = %v, want %v", o.CreatedAt, fixed)
	}
}
