package payment

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateOrderIdempotencyKeyReusesPending(t *testing.T) {
	repo := NewMemRepo()
	sdk := NewStubPaySDK("s")
	g := NewGateway(repo, sdk, map[OrderType]OrderSink{})
	in := OrderInput{
		Type: OrderTypeRecharge, TenantID: 1, UserID: 2, Provider: ProviderWxpay,
		AmountUSD: 10, ActualPaid: 73, IdempotencyKey: "idem-1",
	}
	o1, err := g.CreateOrder(context.Background(), in)
	require.NoError(t, err)
	require.NotEmpty(t, o1.PayURL)
	assert.Equal(t, int64(7300), o1.ActualPaidFen)
	assert.False(t, o1.NextQueryAt.IsZero())

	o2, err := g.CreateOrder(context.Background(), in)
	require.NoError(t, err)
	assert.Equal(t, o1.OrderNo, o2.OrderNo, "same idempotency key must reuse order")
}

func TestCreateOrderIdempotencyConcurrentSingleOrder(t *testing.T) {
	repo := NewMemRepo()
	var createCount int
	var mu sync.Mutex
	sdk := &fakeSDK{
		verifyFn: okVerify(),
		payURL:   "https://pay.fake/x",
	}
	// count CreatePay via wrapping - use stub and count via race on Create
	g := NewGateway(repo, sdk, map[OrderType]OrderSink{})
	const key = "idem-concurrent"
	const n = 16
	var wg sync.WaitGroup
	nos := make(chan string, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			o, err := g.CreateOrder(context.Background(), OrderInput{
				Type: OrderTypeRecharge, TenantID: 1, UserID: 2, Provider: ProviderWxpay,
				AmountUSD: 5, ActualPaid: 36.5, IdempotencyKey: key,
			})
			if err == nil && o != nil {
				mu.Lock()
				createCount++
				mu.Unlock()
				nos <- o.OrderNo
			}
		}()
	}
	wg.Wait()
	close(nos)
	unique := map[string]struct{}{}
	for no := range nos {
		unique[no] = struct{}{}
	}
	assert.Len(t, unique, 1, "concurrent same key must produce one order_no")
}

func TestCreditRejectsProviderTxnConflict(t *testing.T) {
	g, repo, sink, _ := newGateway()
	seedCreatedOrder(t, repo, "RCG-a", OrderTypeRecharge)
	seedCreatedOrder(t, repo, "RCG-b", OrderTypeRecharge)

	require.NoError(t, g.CreditPaidOrder(context.Background(), "RCG-a", "txn-shared", 73))
	assert.Equal(t, 1, sink.count())

	err := g.CreditPaidOrder(context.Background(), "RCG-b", "txn-shared", 73)
	require.Error(t, err)
	// 可能是冲突或金额——RCG-b 也是 73；主要断言未第二次入账
	assert.Equal(t, 1, sink.count(), "conflicting txn must not credit second order")
}

func TestReconcileDueQueriesCreditsPaid(t *testing.T) {
	g, repo, sink, _ := newGateway()
	now := time.Unix(1_700_000_000, 0)
	g.now = func() time.Time { return now }
	repo.now = g.now

	ord := &PayOrder{
		OrderNo: "RCG-due", Type: OrderTypeRecharge, TenantID: 1, UserID: 42,
		Provider: ProviderWxpay, AmountUSD: 10, ActualPaid: 73, ActualPaidFen: 7300,
		Status: OrderCreated, NextQueryAt: now.Add(-time.Second),
		CreatedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute),
		ExpiresAt: now.Add(time.Hour),
	}
	require.NoError(t, repo.Create(context.Background(), ord))

	res, err := g.ReconcileDueQueries(context.Background(), 10, func(ctx context.Context, orderNo, provider string) (*QueryResult, error) {
		return paidQueryResult(orderNo, 7300), nil
	})
	require.NoError(t, err)
	assert.Equal(t, 1, res.Scanned)
	assert.Contains(t, res.Reconciled, "RCG-due")
	assert.Equal(t, 1, sink.count())
	got, _ := repo.GetByOrderNo(context.Background(), "RCG-due")
	assert.Equal(t, OrderCredited, got.Status)
}

func TestAmountMatchesFenExact(t *testing.T) {
	ord := &PayOrder{ActualPaid: 10.01, ActualPaidFen: 1001}
	assert.True(t, amountMatches(10.01, ord))
	assert.False(t, amountMatches(10.02, ord))
	assert.True(t, amountMatchesFen(1001, 1001))
	assert.False(t, amountMatchesFen(1002, 1001))
}
