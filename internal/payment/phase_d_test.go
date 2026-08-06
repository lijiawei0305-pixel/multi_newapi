package payment

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestQueryCreditRejectsPlaceholderTxn 禁止 "query"/"reconcile" 伪交易号入账（P0-1）。
func TestQueryCreditRejectsPlaceholderTxn(t *testing.T) {
	g, repo, sink, _ := newGateway()
	seedCreatedOrder(t, repo, "RCG-ph", OrderTypeRecharge)

	for _, bad := range []string{"query", "reconcile", "QUERY", ""} {
		err := g.CreditPaidOrder(context.Background(), "RCG-ph", bad, 73)
		require.Error(t, err, "placeholder %q", bad)
		assert.Equal(t, CodePaymentFactInvalid, apperr.CodeOf(err), bad)
	}
	assert.Equal(t, 0, sink.count())
}

// TestTwoQueryPaidOrdersDifferentTxn 同一 provider 连续两笔真实 txn 均可入账（P0-1 唯一索引）。
func TestTwoQueryPaidOrdersDifferentTxn(t *testing.T) {
	g, repo, sink, _ := newGateway()
	seedCreatedOrder(t, repo, "RCG-q1", OrderTypeRecharge)
	seedCreatedOrder(t, repo, "RCG-q2", OrderTypeRecharge)

	qr1 := &QueryResult{
		Provider: ProviderWxpay, OrderNo: "RCG-q1", Paid: true,
		TransactionID: "wx-txn-001", PaidAmountFen: 7300, Currency: "CNY",
		NormalizedState: TradeStateSuccess, MchID: "m", AppID: "a", ExpectedMchID: "m", ExpectedAppID: "a",
	}
	qr2 := &QueryResult{
		Provider: ProviderWxpay, OrderNo: "RCG-q2", Paid: true,
		TransactionID: "wx-txn-002", PaidAmountFen: 7300, Currency: "CNY",
		NormalizedState: TradeStateSuccess, MchID: "m", AppID: "a", ExpectedMchID: "m", ExpectedAppID: "a",
	}
	require.NoError(t, g.CreditFromQueryResult(context.Background(), "RCG-q1", qr1))
	require.NoError(t, g.CreditFromQueryResult(context.Background(), "RCG-q2", qr2))
	assert.Equal(t, 2, sink.count())

	o1, _ := repo.GetByOrderNo(context.Background(), "RCG-q1")
	o2, _ := repo.GetByOrderNo(context.Background(), "RCG-q2")
	assert.Equal(t, "wx-txn-001", o1.ProviderTransactionID)
	assert.Equal(t, "wx-txn-002", o2.ProviderTransactionID)
	assert.True(t, o1.CallbackReceivedAt.IsZero(), "query path must not set callback_received_at")
	assert.True(t, o2.CallbackReceivedAt.IsZero())
}

// TestQueryCreditRejectsMissingAmountOrMismatch 缺金额/金额不符拒绝入账。
func TestQueryCreditRejectsMissingAmountOrMismatch(t *testing.T) {
	g, repo, sink, _ := newGateway()
	seedCreatedOrder(t, repo, "RCG-amt", OrderTypeRecharge)

	err := g.CreditFromQueryResult(context.Background(), "RCG-amt", &QueryResult{
		Provider: ProviderWxpay, OrderNo: "RCG-amt", Paid: true,
		TransactionID: "wx-1", PaidAmountFen: 0, Currency: "CNY",
		NormalizedState: TradeStateSuccess, MchID: "m", AppID: "a", ExpectedMchID: "m", ExpectedAppID: "a",
	})
	require.Error(t, err)
	assert.Equal(t, 0, sink.count())

	err = g.CreditFromQueryResult(context.Background(), "RCG-amt", &QueryResult{
		Provider: ProviderWxpay, OrderNo: "RCG-amt", Paid: true,
		TransactionID: "wx-1", PaidAmountFen: 9999, Currency: "CNY",
		NormalizedState: TradeStateSuccess, MchID: "m", AppID: "a", ExpectedMchID: "m", ExpectedAppID: "a",
	})
	assert.Equal(t, CodeAmountMismatch, apperr.CodeOf(err))
	assert.Equal(t, 0, sink.count())
}

// TestCallbackSetsCallbackReceivedAt 仅回调路径写 callback_received_at。
func TestCallbackSetsCallbackReceivedAt(t *testing.T) {
	g, repo, _, _ := newGateway()
	seedCreatedOrder(t, repo, "RCG-cb", OrderTypeRecharge)
	require.NoError(t, g.CreditPaidOrderWithProvider(context.Background(), "RCG-cb", ProviderWxpay, "wx-cb-1", 73, true))
	got, _ := repo.GetByOrderNo(context.Background(), "RCG-cb")
	assert.False(t, got.CallbackReceivedAt.IsZero())
}

// blockingSink 阻塞 OnPaid 直到 release，用于并发 credited-before-ledger 测试（P0-2）。
type blockingSink struct {
	mu       sync.Mutex
	entered  chan struct{}
	release  chan struct{}
	failOnce atomic.Bool
	calls    atomic.Int32
}

func newBlockingSink() *blockingSink {
	return &blockingSink{
		entered: make(chan struct{}, 8),
		release: make(chan struct{}),
	}
}

func (s *blockingSink) OnPaid(ctx context.Context, o PaidOrder) error {
	s.calls.Add(1)
	select {
	case s.entered <- struct{}{}:
	default:
	}
	if s.failOnce.Load() {
		return errors.New("sink failed")
	}
	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TestNoCreditedBeforeLedgerConcurrent 阻塞 sink + 两并发执行者：任意时点不得 credited-before-ledger。
func TestNoCreditedBeforeLedgerConcurrent(t *testing.T) {
	repo := NewMemRepo()
	bs := newBlockingSink()
	g := NewGateway(repo, NewStubPaySDK("s"), map[OrderType]OrderSink{OrderTypeRecharge: bs})
	seedCreatedOrder(t, repo, "RCG-race", OrderTypeRecharge)

	var wg sync.WaitGroup
	wg.Add(2)
	errs := make(chan error, 2)
	go func() {
		defer wg.Done()
		errs <- g.CreditPaidOrder(context.Background(), "RCG-race", "txn-race", 73)
	}()
	// 等 winner 进入 sink
	select {
	case <-bs.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("winner never entered sink")
	}
	// 第二调用者：应看到 paid，不得 MarkCredited
	go func() {
		defer wg.Done()
		errs <- g.CreditPaidOrder(context.Background(), "RCG-race", "txn-race", 73)
	}()
	time.Sleep(50 * time.Millisecond)
	mid, _ := repo.GetByOrderNo(context.Background(), "RCG-race")
	assert.Equal(t, OrderPaid, mid.Status, "while sink running must stay paid, not credited")
	assert.NotEqual(t, OrderCredited, mid.Status)

	close(bs.release)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	final, _ := repo.GetByOrderNo(context.Background(), "RCG-race")
	assert.Equal(t, OrderCredited, final.Status)
	assert.Equal(t, int32(1), bs.calls.Load(), "only CAS winner runs sink")
}

// TestFailingSinkNeverCredits sink 失败不得 credited。
func TestFailingSinkNeverCredits(t *testing.T) {
	repo := NewMemRepo()
	bs := newBlockingSink()
	bs.failOnce.Store(true)
	// 立即释放：OnPaid 会直接返回 fail
	close(bs.release)
	g := NewGateway(repo, NewStubPaySDK("s"), map[OrderType]OrderSink{OrderTypeRecharge: bs})
	seedCreatedOrder(t, repo, "RCG-fail-sink", OrderTypeRecharge)

	err := g.CreditPaidOrder(context.Background(), "RCG-fail-sink", "txn-fs", 73)
	require.Error(t, err)
	got, _ := repo.GetByOrderNo(context.Background(), "RCG-fail-sink")
	assert.Equal(t, OrderCreated, got.Status, "must roll back to created")
	assert.NotEqual(t, OrderCredited, got.Status)
}

// TestCreateUnknownReturnsOrder  outcome_unknown 返回 order 供前端轮询（P0-4）。
func TestCreateUnknownReturnsOrder(t *testing.T) {
	repo := NewMemRepo()
	sdk := &fakeSDK{createErr: NewOutcomeError(CreateOutcomeUnknown, "timeout", "ttfb", errors.New("timeout"))}
	g := NewGateway(repo, sdk, map[OrderType]OrderSink{OrderTypeRecharge: newFakeSink()},
		WithOrderNoFunc(func() string { return "RCG-UNK" }))
	o, err := g.CreateOrder(context.Background(), OrderInput{
		Type: OrderTypeRecharge, TenantID: 1, UserID: 1, Provider: ProviderWxpay,
		AmountUSD: 10, ActualPaid: 73, IdempotencyKey: "idem-unk-1",
	})
	require.Error(t, err)
	assert.Equal(t, CodeCreateOutcomeUnknown, apperr.CodeOf(err))
	require.NotNil(t, o)
	assert.Equal(t, "RCG-UNK", o.OrderNo)
	assert.Equal(t, OrderCreated, o.Status)
	assert.Empty(t, o.PayURL)
}

// TestIdempotencyConflictDifferentAmount 同 key 不同金额 → 冲突（P0-5）。
func TestIdempotencyConflictDifferentAmount(t *testing.T) {
	g, _, _, _ := newGateway()
	_, err := g.CreateOrder(context.Background(), OrderInput{
		Type: OrderTypeRecharge, TenantID: 1, UserID: 1, Provider: ProviderWxpay,
		AmountUSD: 10, ActualPaid: 73, IdempotencyKey: "idem-amt",
	})
	require.NoError(t, err)
	_, err = g.CreateOrder(context.Background(), OrderInput{
		Type: OrderTypeRecharge, TenantID: 1, UserID: 1, Provider: ProviderWxpay,
		AmountUSD: 20, ActualPaid: 146, IdempotencyKey: "idem-amt",
	})
	assert.Equal(t, CodeIdempotencyConflict, apperr.CodeOf(err))
}

// TestLegacyCancelledCanCredit  legacy cancelled + 严格已付事实可恢复（P1-4）。
func TestLegacyCancelledCanCredit(t *testing.T) {
	g, repo, sink, _ := newGateway()
	require.NoError(t, repo.Create(context.Background(), &PayOrder{
		OrderNo: "RCG-cancel", Type: OrderTypeRecharge, TenantID: 1, UserID: 42,
		Provider: ProviderWxpay, AmountUSD: 10, ActualPaid: 73, ActualPaidFen: 7300,
		Status: OrderCancelled,
	}))
	require.NoError(t, g.CreditPaidOrder(context.Background(), "RCG-cancel", "wx-late", 73))
	assert.Equal(t, 1, sink.count())
	got, _ := repo.GetByOrderNo(context.Background(), "RCG-cancel")
	assert.Equal(t, OrderCredited, got.Status)
}

// TestStuckPaidRequiresRealTxn stuck-paid 无真实 txn 不得伪造入账。
func TestStuckPaidRequiresRealTxn(t *testing.T) {
	g, repo, sink, _ := newGateway()
	require.NoError(t, repo.Create(context.Background(), &PayOrder{
		OrderNo: "RCG-stuck-notxn", Type: OrderTypeRecharge, TenantID: 1, UserID: 42,
		Provider: ProviderWxpay, AmountUSD: 10, ActualPaid: 73, ActualPaidFen: 7300,
		Status: OrderPaid, UpdatedAt: time.Unix(1000, 0),
		// 无 ProviderTransactionID
	}))
	res, err := g.ReconcileStuckPaid(context.Background(), time.Unix(2000, 0))
	require.NoError(t, err)
	assert.Equal(t, 0, len(res.Reconciled))
	assert.Contains(t, res.Failed, "RCG-stuck-notxn")
	assert.Equal(t, 0, sink.count())
}
