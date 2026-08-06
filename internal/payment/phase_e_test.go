package payment

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClaimForQueryFencing 两实例 claim 只有一个成功；迟到 finish 0 行。
func TestClaimForQueryFencing(t *testing.T) {
	repo := NewMemRepo()
	now := time.Unix(1_700_000_000, 0)
	repo.now = func() time.Time { return now }
	require.NoError(t, repo.Create(context.Background(), &PayOrder{
		OrderNo: "RCG-claim", Type: OrderTypeRecharge, TenantID: 1, UserID: 1,
		Provider: ProviderWxpay, AmountUSD: 10, ActualPaid: 73, ActualPaidFen: 7300,
		Status: OrderCreated, NextQueryAt: now.Add(-time.Second),
		CreatedAt: now.Add(-time.Minute),
	}))

	tok1, ok1, err := repo.ClaimForQuery(context.Background(), "RCG-claim", now, now.Add(30*time.Second))
	require.NoError(t, err)
	require.True(t, ok1)
	require.NotEmpty(t, tok1)

	_, ok2, err := repo.ClaimForQuery(context.Background(), "RCG-claim", now, now.Add(30*time.Second))
	require.NoError(t, err)
	assert.False(t, ok2, "second claim while lease held must fail")

	// 过期后可重领
	later := now.Add(31 * time.Second)
	tok3, ok3, err := repo.ClaimForQuery(context.Background(), "RCG-claim", later, later.Add(30*time.Second))
	require.NoError(t, err)
	require.True(t, ok3)
	assert.NotEqual(t, tok1, tok3)

	// 旧 token finish 0 行（不改写 next）
	applied, err := repo.FinishQueryFenced(context.Background(), "RCG-claim", tok1, later.Add(time.Hour), 99, CreateStateClosePending, "NOTPAY")
	require.NoError(t, err)
	assert.False(t, applied)
	got, _ := repo.GetByOrderNo(context.Background(), "RCG-claim")
	assert.NotEqual(t, 99, got.QueryAttempts, "stale token must not update attempts")
	assert.Equal(t, tok3, got.QueryClaimToken)

	// 新 token finish 成功
	applied, err = repo.FinishQueryFenced(context.Background(), "RCG-claim", tok3, later.Add(5*time.Second), 2, CreateStateClosePending, "NOTPAY")
	require.NoError(t, err)
	assert.True(t, applied)
	got, _ = repo.GetByOrderNo(context.Background(), "RCG-claim")
	assert.Equal(t, 2, got.QueryAttempts)
	assert.Equal(t, CreateStateClosePending, got.CreateState)
	assert.Empty(t, got.QueryClaimToken)
}

// TestNotPayWithoutQRGoesClosePending 本地过期不直接 failed。
func TestNotPayWithoutQRGoesClosePending(t *testing.T) {
	g, repo, _, _ := newGateway()
	now := time.Unix(1_700_000_000, 0)
	g.now = func() time.Time { return now }
	repo.now = g.now
	require.NoError(t, repo.Create(context.Background(), &PayOrder{
		OrderNo: "RCG-notpay", Type: OrderTypeRecharge, TenantID: 1, UserID: 1,
		Provider: ProviderWxpay, AmountUSD: 10, ActualPaid: 73, ActualPaidFen: 7300,
		Status: OrderCreated, CreateState: CreateStatePrepayUnknown,
		NextQueryAt: now.Add(-time.Second), ExpiresAt: now.Add(-time.Hour), // 已本地过期
		CreatedAt: now.Add(-3 * time.Hour), PayURL: "",
	}))

	res, err := g.ReconcileDueQueries(context.Background(), 10, func(ctx context.Context, orderNo, provider string) (*QueryResult, error) {
		return &QueryResult{
			Provider: ProviderWxpay, OrderNo: orderNo, TradeState: "NOTPAY",
			NormalizedState: TradeStateNotPay, ExpectedMchID: "mch", ExpectedAppID: "app",
			MchID: "mch", AppID: "app",
		}, nil
	})
	require.NoError(t, err)
	assert.Empty(t, res.Expired, "must not expire NOTPAY to failed without CLOSED")
	got, _ := repo.GetByOrderNo(context.Background(), "RCG-notpay")
	assert.Equal(t, OrderCreated, got.Status)
	assert.Equal(t, CreateStateClosePending, got.CreateState)
}

// TestQueryPaidRequiresResponseOrderNo 响应 order_no 空则拒绝入账。
func TestQueryPaidRequiresResponseOrderNo(t *testing.T) {
	g, repo, sink, _ := newGateway()
	seedCreatedOrder(t, repo, "RCG-no-resp-no", OrderTypeRecharge)
	err := g.CreditFromQueryResult(context.Background(), "RCG-no-resp-no", &QueryResult{
		Provider: ProviderWxpay, OrderNo: "", Paid: true,
		TransactionID: "wx-1", PaidAmountFen: 7300, Currency: "CNY",
	})
	require.Error(t, err)
	assert.Equal(t, 0, sink.count())
}

// TestDualClaimOnlyOneQuery due-query 双 worker 只产生一次 provider query。
func TestDualClaimOnlyOneQuery(t *testing.T) {
	g, repo, _, _ := newGateway()
	now := time.Unix(1_700_000_000, 0)
	g.now = func() time.Time { return now }
	repo.now = g.now
	require.NoError(t, repo.Create(context.Background(), &PayOrder{
		OrderNo: "RCG-once", Type: OrderTypeRecharge, TenantID: 1, UserID: 1,
		Provider: ProviderWxpay, AmountUSD: 10, ActualPaid: 73, ActualPaidFen: 7300,
		Status: OrderCreated, NextQueryAt: now.Add(-time.Second),
		CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		PayURL: "weixin://wxpay/bizpayurl?pr=x",
	}))

	var calls int
	var mu sync.Mutex
	query := func(ctx context.Context, orderNo, provider string) (*QueryResult, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return &QueryResult{
			Provider: ProviderWxpay, OrderNo: orderNo, TradeState: "NOTPAY",
			NormalizedState: TradeStateNotPay, MchID: "mch", AppID: "app",
			ExpectedMchID: "mch", ExpectedAppID: "app",
		}, nil
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = g.ReconcileDueQueries(context.Background(), 10, query) }()
	go func() { defer wg.Done(); _, _ = g.ReconcileDueQueries(context.Background(), 10, query) }()
	wg.Wait()
	assert.Equal(t, 1, calls, "exactly one provider query under dual scanners")
}
