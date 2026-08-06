package payment

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// expireBeforeCreditRepo 确定性制造「Credit 已读 created、对账先写 failed、随后 created→paid CAS 失败」
// 的边界竞态。旧实现会把该 CAS 失败直接当幂等成功，导致真实付款永久未入账。
type expireBeforeCreditRepo struct {
	*MemRepo
	once sync.Once
}

func (r *expireBeforeCreditRepo) CompareAndSetStatus(ctx context.Context, orderNo string, from, to OrderStatus) (bool, error) {
	if from == OrderCreated && to == OrderPaid {
		r.once.Do(func() {
			_, _ = r.MemRepo.CompareAndSetStatus(ctx, orderNo, OrderCreated, OrderFailed)
		})
	}
	return r.MemRepo.CompareAndSetStatus(ctx, orderNo, from, to)
}

// seedCreatedOrder 直接落一条 created 订单（绕过下单流程），供入账用例复用。
func seedCreatedOrder(t *testing.T, repo *MemRepo, no string, typ OrderType) {
	t.Helper()
	if err := repo.Create(context.Background(), &PayOrder{
		OrderNo: no, Type: typ, TenantID: 1, UserID: 42, Provider: ProviderWxpay,
		AmountUSD: 10, ActualPaid: 73, ActualPaidFen: 7300, Status: OrderCreated,
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
			_ = g.CreditPaidOrder(context.Background(), orderNo, "txn-x", 73)
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

	if err := g.CreditPaidOrder(context.Background(), orderNo, "t1", 73); err != nil {
		t.Fatalf("first credit: %v", err)
	}
	if err := g.CreditPaidOrder(context.Background(), orderNo, "t1", 73); err != nil {
		t.Fatalf("second credit must short-circuit success, got %v", err)
	}
	if got := recharge.count(); got != 1 {
		t.Fatalf("sink called %d times, want 1", got)
	}
}

// TestCreditPaidOrderUnknownOrder 未知订单号 → ORDER_NOT_FOUND。
func TestCreditPaidOrderUnknownOrder(t *testing.T) {
	g, _, _, _ := newGateway()
	err := g.CreditPaidOrder(context.Background(), "nope", "t", 73)
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

	if err := g.CreditPaidOrder(context.Background(), orderNo, "t", 73); err == nil {
		t.Fatal("expected sink error to propagate")
	}
	got, _ := repo.GetByOrderNo(context.Background(), orderNo)
	if got.Status != OrderCreated {
		t.Fatalf("status = %q, want rolled back to created", got.Status)
	}

	// 上游重试：sink 恢复后应能成功入账。
	recharge.err = nil
	if err := g.CreditPaidOrder(context.Background(), orderNo, "t-retry", 73); err != nil {
		t.Fatalf("retry credit: %v", err)
	}
	if recharge.count() != 1 {
		t.Fatalf("retry sink count = %d, want 1", recharge.count())
	}
}

// TestCreditPaidOrderAmountMismatch 回传金额与库内订单不一致 → PAY_AMOUNT_MISMATCH，不入账、不动状态。
func TestCreditPaidOrderAmountMismatch(t *testing.T) {
	g, repo, recharge, _ := newGateway()
	const orderNo = "RCG-amount"
	seedCreatedOrder(t, repo, orderNo, OrderTypeRecharge) // ActualPaid=73

	err := g.CreditPaidOrder(context.Background(), orderNo, "t", 99) // 回传 99 ≠ 73
	if got := apperr.CodeOf(err); got != CodeAmountMismatch {
		t.Fatalf("code = %q, want %q", got, CodeAmountMismatch)
	}
	if recharge.count() != 0 {
		t.Fatalf("sink must not be called on amount mismatch, got %d", recharge.count())
	}
	got, _ := repo.GetByOrderNo(context.Background(), orderNo)
	if got.Status != OrderCreated {
		t.Fatalf("status = %q, want created (unchanged)", got.Status)
	}

	// 金额一致（容差内）→ 正常入账。
	if err := g.CreditPaidOrder(context.Background(), orderNo, "t2", 73); err != nil {
		t.Fatalf("matching amount credit: %v", err)
	}
	if recharge.count() != 1 {
		t.Fatalf("sink count = %d, want 1", recharge.count())
	}
}

func TestCreditPaidOrderRevivesOrderFailedByConcurrentExpiry(t *testing.T) {
	base := NewMemRepo()
	repo := &expireBeforeCreditRepo{MemRepo: base}
	sink := newFakeSink()
	g := NewGateway(repo, NewStubPaySDK("secret"), map[OrderType]OrderSink{OrderTypeRecharge: sink})
	const orderNo = "RCG-expiry-race"
	seedCreatedOrder(t, base, orderNo, OrderTypeRecharge)

	err := g.CreditPaidOrder(context.Background(), orderNo, "txn-paid", 73)
	require.NoError(t, err)
	assert.Equal(t, 1, sink.count(), "verified payment must be credited exactly once")
	final, err := base.GetByOrderNo(context.Background(), orderNo)
	require.NoError(t, err)
	assert.Equal(t, OrderCredited, final.Status)
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
