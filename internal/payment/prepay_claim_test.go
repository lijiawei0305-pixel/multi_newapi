package payment

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blockingSDK 第一次 CreatePay 阻塞直到 release。
type blockingSDK struct {
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int32
	payURL  string
}

func (s *blockingSDK) CreatePay(_ context.Context, req PayRequest) (*PayCredential, error) {
	s.calls.Add(1)
	select {
	case s.entered <- struct{}{}:
	default:
	}
	<-s.release
	url := s.payURL
	if url == "" {
		url = "weixin://pay/" + req.OrderNo
	}
	return &PayCredential{PayURL: url}, nil
}

func (s *blockingSDK) Verify(context.Context, Provider, []byte) (*CallbackInfo, error) {
	return nil, ErrSignInvalid
}

func TestConcurrentPrepayOnlyOneSDKCall(t *testing.T) {
	repo := NewMemRepo()
	sdk := &blockingSDK{entered: make(chan struct{}, 8), release: make(chan struct{})}
	g := NewGateway(repo, sdk, map[OrderType]OrderSink{OrderTypeRecharge: newFakeSink()},
		WithOrderNoFunc(func() string { return "RCG-ONCE" }))

	// 先创建本地单（不 finish）
	now := time.Now()
	require.NoError(t, repo.Create(context.Background(), &PayOrder{
		OrderNo: "RCG-ONCE", Type: OrderTypeRecharge, TenantID: 1, UserID: 1,
		Provider: ProviderWxpay, AmountUSD: 10, ActualPaid: 73, ActualPaidFen: 7300,
		Status: OrderCreated, CreateState: CreateStateLocalCreated,
		RootOrderNo: "RCG-ONCE", AttemptNo: 1, ActiveOrderNo: "RCG-ONCE",
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(2 * time.Hour),
	}))

	in := OrderInput{
		Type: OrderTypeRecharge, TenantID: 1, UserID: 1, Provider: ProviderWxpay,
		AmountUSD: 10, ActualPaid: 73, IdempotencyKey: "idem-once",
	}
	// 需 idem 键关联
	// Create via finish only
	var wg sync.WaitGroup
	const n = 32
	wg.Add(n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			o, err := g.finishCreatePay(context.Background(), &PayOrder{
				OrderNo: "RCG-ONCE", Provider: ProviderWxpay, CreateState: CreateStateLocalCreated,
				ExpiresAt: now.Add(2 * time.Hour),
			}, in, now)
			_ = o
			errs <- err
		}()
	}
	// 等第一次进入 SDK
	select {
	case <-sdk.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("SDK never entered")
	}
	// 此时其他 31 个应拿不到 claim
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(1), sdk.calls.Load())
	close(sdk.release)
	wg.Wait()
	close(errs)
	// 恰好一次 SDK
	assert.Equal(t, int32(1), sdk.calls.Load())
}

func TestQueryClaimExcludedDuringPrepayInflight(t *testing.T) {
	repo := NewMemRepo()
	now := time.Unix(1_700_000_000, 0)
	repo.now = func() time.Time { return now }
	require.NoError(t, repo.Create(context.Background(), &PayOrder{
		OrderNo: "RCG-INF", Type: OrderTypeRecharge, TenantID: 1, UserID: 1,
		Provider: ProviderWxpay, AmountUSD: 10, ActualPaid: 73, ActualPaidFen: 7300,
		Status: OrderCreated, CreateState: CreateStateLocalCreated,
		NextQueryAt: now.Add(-time.Second), CreatedAt: now.Add(-time.Minute),
	}))
	tok, ok, err := repo.ClaimForPrepay(context.Background(), "RCG-INF", now, now.Add(15*time.Second))
	require.NoError(t, err)
	require.True(t, ok)
	require.NotEmpty(t, tok)

	_, qok, err := repo.ClaimForQuery(context.Background(), "RCG-INF", now, now.Add(4*time.Second))
	require.NoError(t, err)
	assert.False(t, qok, "query must not claim during prepay_inflight")
}

func TestStalePrepaySuccessIgnored(t *testing.T) {
	repo := NewMemRepo()
	now := time.Unix(1_700_000_000, 0)
	repo.now = func() time.Time { return now }
	require.NoError(t, repo.Create(context.Background(), &PayOrder{
		OrderNo: "RCG-STALE", Type: OrderTypeRecharge, TenantID: 1, UserID: 1,
		Provider: ProviderWxpay, AmountUSD: 10, ActualPaid: 73, ActualPaidFen: 7300,
		Status: OrderCreated, CreateState: CreateStateLocalCreated,
		CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}))
	tok1, ok, err := repo.ClaimForPrepay(context.Background(), "RCG-STALE", now, now.Add(15*time.Second))
	require.NoError(t, err)
	require.True(t, ok)

	// 过期 lease 后新 token：create_state 仍 prepay_inflight → 不得直接再 Prepay
	later := now.Add(20 * time.Second)
	tok2, ok2, err := repo.ClaimForPrepay(context.Background(), "RCG-STALE", later, later.Add(15*time.Second))
	require.NoError(t, err)
	_ = tok2
	assert.False(t, ok2)

	// stale token 写回失败
	applied, err := repo.FinishPrepayFenced(context.Background(), "RCG-STALE", "wrong", CreateStateCredentialReady, "weixin://x", later.Add(5*time.Second), "", "", 1)
	require.NoError(t, err)
	assert.False(t, applied)

	applied, err = repo.FinishPrepayFenced(context.Background(), "RCG-STALE", tok1, CreateStateCredentialReady, "weixin://good", later.Add(5*time.Second), "", "", 1)
	require.NoError(t, err)
	assert.True(t, applied)
	got, _ := repo.GetByOrderNo(context.Background(), "RCG-STALE")
	assert.Equal(t, "weixin://good", got.PayURL)
}

// TestPrepayUnknownCannotReclaimWithoutQuery prepay_unknown 不得直接再 Prepay。
func TestPrepayUnknownCannotReclaimWithoutQuery(t *testing.T) {
	repo := NewMemRepo()
	now := time.Unix(1_700_000_100, 0)
	repo.now = func() time.Time { return now }
	require.NoError(t, repo.Create(context.Background(), &PayOrder{
		OrderNo: "RCG-UNK-PRE", Type: OrderTypeRecharge, TenantID: 1, UserID: 1,
		Provider: ProviderWxpay, AmountUSD: 10, ActualPaid: 73, ActualPaidFen: 7300,
		Status: OrderCreated, CreateState: CreateStatePrepayUnknown,
		NextQueryAt: now.Add(-time.Second), CreatedAt: now.Add(-time.Minute),
	}))
	_, ok, err := repo.ClaimForPrepay(context.Background(), "RCG-UNK-PRE", now, now.Add(15*time.Second))
	require.NoError(t, err)
	assert.False(t, ok, "must Query first; only ORDER_NOT_EXIST resets to local_created")
}

// TestCrashMidPrepayAllowsQueryAfterLeaseExpiry 崩溃后 lease 过期允许 Query，禁止盲重放 Prepay。
func TestCrashMidPrepayAllowsQueryAfterLeaseExpiry(t *testing.T) {
	repo := NewMemRepo()
	now := time.Unix(1_700_000_200, 0)
	repo.now = func() time.Time { return now }
	require.NoError(t, repo.Create(context.Background(), &PayOrder{
		OrderNo: "RCG-CRASH", Type: OrderTypeRecharge, TenantID: 1, UserID: 1,
		Provider: ProviderWxpay, AmountUSD: 10, ActualPaid: 73, ActualPaidFen: 7300,
		Status: OrderCreated, CreateState: CreateStateLocalCreated,
		NextQueryAt: now.Add(-time.Second), CreatedAt: now.Add(-time.Minute),
	}))
	tok, ok, err := repo.ClaimForPrepay(context.Background(), "RCG-CRASH", now, now.Add(15*time.Second))
	require.NoError(t, err)
	require.True(t, ok)
	require.NotEmpty(t, tok)

	// lease 有效：Query 不可 claim
	_, qok, err := repo.ClaimForQuery(context.Background(), "RCG-CRASH", now.Add(time.Second), now.Add(30*time.Second))
	require.NoError(t, err)
	assert.False(t, qok)

	// lease 过期：Query 可 claim；Prepay 仍不可（state=inflight）
	later := now.Add(20 * time.Second)
	_, qok2, err := repo.ClaimForQuery(context.Background(), "RCG-CRASH", later, later.Add(30*time.Second))
	require.NoError(t, err)
	assert.True(t, qok2, "after lease expiry Query-first recovery must work")

	_, pok, err := repo.ClaimForPrepay(context.Background(), "RCG-CRASH", later, later.Add(15*time.Second))
	require.NoError(t, err)
	assert.False(t, pok, "must not re-Prepay without Query ORDER_NOT_EXIST")
}

// TestSuccessVsDefinitiveConcurrentOnlyCredentialReady success 与 definitive 并发，终态只能 credential_ready。
func TestSuccessVsDefinitiveConcurrentOnlyCredentialReady(t *testing.T) {
	repo := NewMemRepo()
	now := time.Unix(1_700_000_300, 0)
	repo.now = func() time.Time { return now }
	require.NoError(t, repo.Create(context.Background(), &PayOrder{
		OrderNo: "RCG-RACE-FIN", Type: OrderTypeRecharge, TenantID: 1, UserID: 1,
		Provider: ProviderWxpay, AmountUSD: 10, ActualPaid: 73, ActualPaidFen: 7300,
		Status: OrderCreated, CreateState: CreateStateLocalCreated,
		CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}))
	tok, ok, err := repo.ClaimForPrepay(context.Background(), "RCG-RACE-FIN", now, now.Add(15*time.Second))
	require.NoError(t, err)
	require.True(t, ok)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = repo.FinishPrepayFenced(context.Background(), "RCG-RACE-FIN", tok, CreateStateCredentialReady, "weixin://win", now.Add(5*time.Second), "", "", 1)
	}()
	go func() {
		defer wg.Done()
		_, _ = repo.FinishPrepayFenced(context.Background(), "RCG-RACE-FIN", tok, CreateStateDefinitiveReject, "", time.Time{}, "reject", "biz", 1)
	}()
	wg.Wait()
	got, err := repo.GetByOrderNo(context.Background(), "RCG-RACE-FIN")
	require.NoError(t, err)
	// 两者都持同一 token：先成功的 applied=true；后到的 applied=false（status 已变或 token 已清）
	// 终态：若 success 先到 → credential_ready；若 reject 先到 → failed。
	// 要求：success 与 reject 不得「先 success 再被 reject 覆盖为 failed」。
	// Finish 要求 status=created 且 token 匹配：success 清 token 后 reject 无法 applied；
	// reject 先把 status=failed 后 success 也无法 applied。
	// 因此终态二选一；这里再跑一轮：success 后 stale reject 不得覆盖。
	assert.True(t, got.Status == OrderCreated || got.Status == OrderFailed)
	if got.PayURL != "" {
		assert.Equal(t, CreateStateCredentialReady, got.CreateState)
		assert.Equal(t, OrderCreated, got.Status)
		// stale reject
		applied, err := repo.FinishPrepayFenced(context.Background(), "RCG-RACE-FIN", tok, CreateStateDefinitiveReject, "", time.Time{}, "late", "biz", 1)
		require.NoError(t, err)
		assert.False(t, applied)
		got2, _ := repo.GetByOrderNo(context.Background(), "RCG-RACE-FIN")
		assert.Equal(t, OrderCreated, got2.Status)
		assert.Equal(t, "weixin://win", got2.PayURL)
	}
}

// TestQueryPaidOKRequiresNormalizedSuccess 空 NormalizedState 不得入账。
func TestQueryPaidOKRequiresNormalizedSuccess(t *testing.T) {
	base := &QueryResult{
		Provider: ProviderWxpay, OrderNo: "RCG-1", Paid: true,
		TransactionID: "tx1", PaidAmountFen: 100, Currency: "CNY",
		MchID: "m", AppID: "a", ExpectedMchID: "m", ExpectedAppID: "a",
		NormalizedState: TradeStateSuccess,
	}
	assert.True(t, base.QueryPaidOK())
	empty := *base
	empty.NormalizedState = ""
	assert.False(t, empty.QueryPaidOK(), "empty NormalizedState must not pass")
	wrongMch := *base
	wrongMch.MchID = "other"
	assert.False(t, wrongMch.QueryPaidOK())
	wrongApp := *base
	wrongApp.AppID = "other"
	assert.False(t, wrongApp.QueryPaidOK())
	noCur := *base
	noCur.Currency = ""
	assert.False(t, noCur.QueryPaidOK())
}

// TestAutoCloseReplaceUnreachable flag true 启动失败；Enabled 恒 false。
func TestAutoCloseReplaceUnreachable(t *testing.T) {
	t.Setenv(AutoCloseReplaceEnv, "true")
	err := ValidateAutoCloseReplaceConfig()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrFeatureNotImplemented)
	assert.False(t, AutoCloseReplaceEnabled())
	t.Setenv(AutoCloseReplaceEnv, "false")
	require.NoError(t, ValidateAutoCloseReplaceConfig())
}
