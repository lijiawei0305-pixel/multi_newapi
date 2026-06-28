package payment

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

// seedCreatedOrder 直接落一条 created 订单（绕过下单流程），供入账用例复用。
func seedCreatedOrder(t *testing.T, repo *MemRepo, no string, typ OrderType) {
	t.Helper()
	if err := repo.Create(context.Background(), &PayOrder{
		OrderNo: no, Type: typ, TenantID: 1, UserID: 42, Provider: ProviderWxpay,
		AmountUSD: 10, ActualPaid: 73, Status: OrderCreated,
	}); err != nil {
		t.Fatalf("seed order: %v", err)
	}
}

// TestCreditPaidOrderConcurrentCreditsOnce 是核心强幂等用例：
// 并发对同一 order_no 发起入账，OrderSink 只应被调用一次（CAS created→paid 只有一个胜者）。
func TestCreditPaidOrderConcurrentCreditsOnce(t *testing.T) {
	g, repo, recharge, _ := newGateway()
	const orderNo = "RCG-concurrent"
	seedCreatedOrder(t, repo, orderNo, OrderTypeRecharge)

	const n = 64
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_ = g.CreditPaidOrder(context.Background(), orderNo, "txn-x")
		}()
	}
	wg.Wait()

	if got := recharge.count(); got != 1 {
		t.Fatalf("sink called %d times, want exactly 1 (idempotent credit)", got)
	}
	final, _ := repo.GetByOrderNo(context.Background(), orderNo)
	if final.Status != OrderCredited {
		t.Fatalf("final status = %q, want credited", final.Status)
	}
}

// TestCreditPaidOrderRepeatShortCircuits 重复入账（串行）只入账一次并短路成功。
func TestCreditPaidOrderRepeatShortCircuits(t *testing.T) {
	g, repo, recharge, _ := newGateway()
	const orderNo = "RCG-repeat"
	seedCreatedOrder(t, repo, orderNo, OrderTypeRecharge)

	if err := g.CreditPaidOrder(context.Background(), orderNo, "t1"); err != nil {
		t.Fatalf("first credit: %v", err)
	}
	if err := g.CreditPaidOrder(context.Background(), orderNo, "t2"); err != nil {
		t.Fatalf("second credit must short-circuit success, got %v", err)
	}
	if got := recharge.count(); got != 1 {
		t.Fatalf("sink called %d times, want 1", got)
	}
}

// TestCreditPaidOrderUnknownOrder 未知订单号 → ORDER_NOT_FOUND。
func TestCreditPaidOrderUnknownOrder(t *testing.T) {
	g, _, _, _ := newGateway()
	err := g.CreditPaidOrder(context.Background(), "nope", "t")
	if got := apperr.CodeOf(err); got != CodeOrderNotFound {
		t.Fatalf("code = %q, want %q", got, CodeOrderNotFound)
	}
}

// TestCreditPaidOrderSinkErrorRollsBack OnPaid 失败 → 回滚 paid→created，可重试。
func TestCreditPaidOrderSinkErrorRollsBack(t *testing.T) {
	repo := NewMemRepo()
	recharge := newFakeSink()
	recharge.err = errors.New("quota service down")
	g := NewGateway(repo, NewStubPaySDK("s"), map[OrderType]OrderSink{OrderTypeRecharge: recharge})
	const orderNo = "RCG-rollback"
	seedCreatedOrder(t, repo, orderNo, OrderTypeRecharge)

	if err := g.CreditPaidOrder(context.Background(), orderNo, "t"); err == nil {
		t.Fatal("expected sink error to propagate")
	}
	got, _ := repo.GetByOrderNo(context.Background(), orderNo)
	if got.Status != OrderCreated {
		t.Fatalf("status = %q, want rolled back to created", got.Status)
	}

	// 上游重试：sink 恢复后应能成功入账。
	recharge.err = nil
	if err := g.CreditPaidOrder(context.Background(), orderNo, "t-retry"); err != nil {
		t.Fatalf("retry credit: %v", err)
	}
	if recharge.count() != 1 {
		t.Fatalf("retry sink count = %d, want 1", recharge.count())
	}
}

// TestNewOrderNoPrefixed 订单号带业务前缀且唯一。
func TestNewOrderNoPrefixed(t *testing.T) {
	a := NewOrderNo(OrderNoPrefixRecharge)
	b := NewOrderNo(OrderNoPrefixRecharge)
	if !strings.HasPrefix(a, "RCG") {
		t.Fatalf("order_no %q missing RCG prefix", a)
	}
	if a == b {
		t.Fatal("order_no must be unique across calls")
	}
	if !strings.HasPrefix(NewOrderNo(OrderNoPrefixSubscription), "SUB") {
		t.Fatal("subscription prefix mismatch")
	}
}
