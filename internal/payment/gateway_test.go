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
	// 未知错误对外归一为 PAY_CREATE_UNKNOWN（不泄露内部、不标 failed）
	if got := apperr.CodeOf(err); got != CodeCreateOutcomeUnknown {
		t.Fatalf("code = %q, want %q (err=%v)", got, CodeCreateOutcomeUnknown, err)
	}
}

// TestCreateOrderUnknownKeepsCreated PAY-LAT-02：网络/未知错误不得 created→failed。
func TestCreateOrderUnknownKeepsCreated(t *testing.T) {
	repo := NewMemRepo()
	sdk := &fakeSDK{createErr: errors.New("sdk down"), verifyFn: okVerify()}
	g := NewGateway(repo, sdk, map[OrderType]OrderSink{OrderTypeRecharge: newFakeSink()},
		WithOrderNoFunc(func() string { return "RCG-UNKNOWN" }))

	_, err := g.CreateOrder(context.Background(), OrderInput{
		Type: OrderTypeRecharge, TenantID: 1, UserID: 1, Provider: ProviderWxpay, AmountUSD: 10,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := apperr.CodeOf(err); got != CodeCreateOutcomeUnknown {
		t.Fatalf("code = %q, want %q", got, CodeCreateOutcomeUnknown)
	}
	got, err := repo.GetByOrderNo(context.Background(), "RCG-UNKNOWN")
	if err != nil {
		t.Fatalf("order must be persisted: %v", err)
	}
	if got.Status != OrderCreated {
		t.Fatalf("status = %q, want created (not failed)", got.Status)
	}
}

// TestCreateOrderDefinitiveRejectMarksFailed 确定性平台拒绝 → failed。
func TestCreateOrderDefinitiveRejectMarksFailed(t *testing.T) {
	repo := NewMemRepo()
	sdk := &fakeSDK{
		createErr: NewOutcomeError(CreateOutcomeDefinitiveReject, "param_error", "response", errors.New("PARAM_ERROR")),
		verifyFn:  okVerify(),
	}
	g := NewGateway(repo, sdk, map[OrderType]OrderSink{OrderTypeRecharge: newFakeSink()},
		WithOrderNoFunc(func() string { return "RCG-REJECT" }))

	_, err := g.CreateOrder(context.Background(), OrderInput{
		Type: OrderTypeRecharge, TenantID: 1, UserID: 1, Provider: ProviderWxpay, AmountUSD: 10,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	got, err := repo.GetByOrderNo(context.Background(), "RCG-REJECT")
	if err != nil {
		t.Fatalf("order: %v", err)
	}
	if got.Status != OrderFailed {
		t.Fatalf("status = %q, want failed", got.Status)
	}
}

// TestCreateOrderPayURLPersistFails 平台成功但落库失败 → 不得返回空 QR 成功。
func TestCreateOrderPayURLPersistFails(t *testing.T) {
	repo := &failSetPayURLRepo{MemRepo: NewMemRepo()}
	sdk := NewStubPaySDK("s")
	g := NewGateway(repo, sdk, map[OrderType]OrderSink{OrderTypeRecharge: newFakeSink()},
		WithOrderNoFunc(func() string { return "RCG-NOPAYURL" }))

	o, err := g.CreateOrder(context.Background(), OrderInput{
		Type: OrderTypeRecharge, TenantID: 1, UserID: 1, Provider: ProviderWxpay, AmountUSD: 10, ActualPaid: 73,
	})
	// P0-4：返回 order（无 PayURL）+ 错误，供前端持有 order_no 轮询
	if err == nil || o == nil {
		t.Fatalf("want persist error and non-nil order, got o=%v err=%v", o, err)
	}
	if got := apperr.CodeOf(err); got != CodePayURLPersist {
		t.Fatalf("code = %q, want %q", got, CodePayURLPersist)
	}
	if o.PayURL != "" {
		t.Fatalf("must not return in-memory pay_url on persist failure")
	}
	// 订单仍 created，pay_url 空
	got, _ := repo.GetByOrderNo(context.Background(), "RCG-NOPAYURL")
	if got.Status != OrderCreated || got.PayURL != "" {
		t.Fatalf("want created empty pay_url, got status=%s pay_url=%q", got.Status, got.PayURL)
	}
}

// failSetPayURLRepo SetPayURL/Fenced 恒失败。
type failSetPayURLRepo struct {
	*MemRepo
}

func (r *failSetPayURLRepo) SetPayURL(ctx context.Context, orderNo, payURL string) error {
	return errors.New("db write failed")
}

func (r *failSetPayURLRepo) SetPayURLFenced(ctx context.Context, orderNo, payURL, claimToken string, allowedStates []CreateState) error {
	return errors.New("db write failed")
}

func (r *failSetPayURLRepo) FinishPrepayFenced(ctx context.Context, orderNo, token string, to CreateState, payURL string, nextQueryAt time.Time, errorClass, stage string, createAttempts int) (bool, error) {
	if payURL != "" {
		return false, errors.New("db write failed")
	}
	return r.MemRepo.FinishPrepayFenced(ctx, orderNo, token, to, payURL, nextQueryAt, errorClass, stage, createAttempts)
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
