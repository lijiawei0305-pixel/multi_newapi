package payment

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReconcilePendingPrepay_IssuesCode local_created && pay_url=” → 驱动器补出码。
func TestReconcilePendingPrepay_IssuesCode(t *testing.T) {
	repo := NewMemRepo()
	now := time.Unix(1_700_000_000, 0)
	repo.now = func() time.Time { return now }
	sdk := &fakeSDK{payURL: "weixin://wxpay/bizpayurl?pr=driver1"}
	g := NewGateway(repo, sdk, map[OrderType]OrderSink{
		OrderTypeRecharge: newFakeSink(),
	}, WithClock(func() time.Time { return now }))

	ord := &PayOrder{
		OrderNo:     "RCG-DRIVE-1",
		Type:        OrderTypeRecharge,
		TenantID:    1,
		UserID:      2,
		Provider:    ProviderWxpay,
		AmountUSD:   1,
		ActualPaid:  7,
		Status:      OrderCreated,
		CreateState: CreateStateLocalCreated,
		ExpiresAt:   now.Add(2 * time.Hour),
		CreatedAt:   now.Add(-10 * time.Second),
		UpdatedAt:   now.Add(-10 * time.Second),
	}
	require.NoError(t, repo.Create(context.Background(), ord))

	ctx := WithBackgroundPrepay(context.Background())
	res, err := g.ReconcilePendingPrepay(ctx, 10)
	require.NoError(t, err)
	assert.Equal(t, 1, res.Scanned)
	require.Len(t, res.Reconciled, 1)
	assert.Equal(t, "RCG-DRIVE-1", res.Reconciled[0])

	got, err := repo.GetByOrderNo(context.Background(), "RCG-DRIVE-1")
	require.NoError(t, err)
	assert.Equal(t, "weixin://wxpay/bizpayurl?pr=driver1", got.PayURL)
	assert.Equal(t, CreateStateCredentialReady, got.CreateState)
	assert.Equal(t, OrderCreated, got.Status) // 仍待付，未 failed
}

// TestReconcilePendingPrepay_UnknownKeepsCreated 网络 unknown 不标 failed。
func TestReconcilePendingPrepay_UnknownKeepsCreated(t *testing.T) {
	repo := NewMemRepo()
	now := time.Unix(1_700_000_100, 0)
	repo.now = func() time.Time { return now }
	sdk := &fakeSDK{createErr: NewOutcomeError(CreateOutcomeUnknown, "timeout", "tls", errors.New("tls timeout"))}
	g := NewGateway(repo, sdk, map[OrderType]OrderSink{
		OrderTypeRecharge: newFakeSink(),
	}, WithClock(func() time.Time { return now }))

	ord := &PayOrder{
		OrderNo:     "RCG-DRIVE-UNK",
		Type:        OrderTypeRecharge,
		TenantID:    1,
		UserID:      2,
		Provider:    ProviderWxpay,
		AmountUSD:   1,
		ActualPaid:  7,
		Status:      OrderCreated,
		CreateState: CreateStateLocalCreated,
		ExpiresAt:   now.Add(time.Hour),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	require.NoError(t, repo.Create(context.Background(), ord))

	res, err := g.ReconcilePendingPrepay(WithBackgroundPrepay(context.Background()), 10)
	require.NoError(t, err)
	assert.Equal(t, 1, res.Scanned)
	assert.Empty(t, res.Reconciled)
	assert.Contains(t, res.Failed, "RCG-DRIVE-UNK")

	got, err := repo.GetByOrderNo(context.Background(), "RCG-DRIVE-UNK")
	require.NoError(t, err)
	assert.Equal(t, OrderCreated, got.Status)
	assert.NotEqual(t, OrderFailed, got.Status)
	assert.Equal(t, CreateStatePrepayUnknown, got.CreateState)
}

// countingSDK 记录 CreatePay 次数。
type countingSDK struct {
	n      atomic.Int32
	payURL string
	err    error
}

func (c *countingSDK) CreatePay(_ context.Context, req PayRequest) (*PayCredential, error) {
	c.n.Add(1)
	if c.err != nil {
		return nil, c.err
	}
	url := c.payURL
	if url == "" {
		url = "weixin://wxpay/bizpayurl?pr=" + req.OrderNo
	}
	return &PayCredential{PayURL: url}, nil
}

func (c *countingSDK) Verify(_ context.Context, _ Provider, _ []byte) (*CallbackInfo, error) {
	return nil, errors.New("unused")
}

// TestNOTPAY_AllowsReplaySameOutTradeNo prepay_unknown + query NOTPAY → local_created，可再 Prepay。
func TestNOTPAY_AllowsReplaySameOutTradeNo(t *testing.T) {
	repo := NewMemRepo()
	now := time.Unix(1_700_000_200, 0)
	repo.now = func() time.Time { return now }
	sdk := &countingSDK{}
	g := NewGateway(repo, sdk, map[OrderType]OrderSink{
		OrderTypeRecharge: newFakeSink(),
	}, WithClock(func() time.Time { return now }), WithErrorLogf(func(string, ...any) {}))

	ord := &PayOrder{
		OrderNo:        "RCG-REPLAY-1",
		Type:           OrderTypeRecharge,
		TenantID:       1,
		UserID:         2,
		Provider:       ProviderWxpay,
		AmountUSD:      1,
		ActualPaid:     7,
		Status:         OrderCreated,
		CreateState:    CreateStatePrepayUnknown,
		CreateAttempts: 1,
		PayURL:         "",
		ExpiresAt:      now.Add(time.Hour),
		NextQueryAt:    now.Add(-time.Second),
		CreatedAt:      now.Add(-time.Minute),
		UpdatedAt:      now.Add(-time.Minute),
	}
	require.NoError(t, repo.Create(context.Background(), ord))

	query := func(ctx context.Context, orderNo, provider string) (*QueryResult, error) {
		return &QueryResult{
			Provider:        ProviderWxpay,
			OrderNo:         orderNo,
			TradeState:      "NOTPAY",
			NormalizedState: TradeStateNotPay,
			Paid:            false,
			// BindingsOK 要求微信 mch/app 对齐
			MchID: "mch1", ExpectedMchID: "mch1",
			AppID: "app1", ExpectedAppID: "app1",
		}, nil
	}
	res, err := g.ReconcileDueQueries(context.Background(), 10, query)
	require.NoError(t, err)
	_ = res

	got, err := repo.GetByOrderNo(context.Background(), "RCG-REPLAY-1")
	require.NoError(t, err)
	assert.Equal(t, CreateStateLocalCreated, got.CreateState, "NOTPAY + empty pay_url → re-prepay allowed")

	// 驱动器同单号补出码
	sdk.payURL = "weixin://wxpay/bizpayurl?pr=replay"
	dres, err := g.ReconcilePendingPrepay(WithBackgroundPrepay(context.Background()), 10)
	require.NoError(t, err)
	require.Contains(t, dres.Reconciled, "RCG-REPLAY-1")
	got2, err := repo.GetByOrderNo(context.Background(), "RCG-REPLAY-1")
	require.NoError(t, err)
	assert.Equal(t, "weixin://wxpay/bizpayurl?pr=replay", got2.PayURL)
	assert.Equal(t, int32(1), sdk.n.Load())
}

// TestNOTPAY_ReplayCap 超过重放上限 → close_pending，不再出码。
func TestNOTPAY_ReplayCap(t *testing.T) {
	repo := NewMemRepo()
	now := time.Unix(1_700_000_300, 0)
	repo.now = func() time.Time { return now }
	g := NewGateway(repo, &fakeSDK{}, map[OrderType]OrderSink{
		OrderTypeRecharge: newFakeSink(),
	}, WithClock(func() time.Time { return now }), WithErrorLogf(func(string, ...any) {}))

	ord := &PayOrder{
		OrderNo:        "RCG-REPLAY-CAP",
		Type:           OrderTypeRecharge,
		TenantID:       1,
		UserID:         2,
		Provider:       ProviderWxpay,
		AmountUSD:      1,
		ActualPaid:     7,
		Status:         OrderCreated,
		CreateState:    CreateStatePrepayUnknown,
		CreateAttempts: maxPrepayReplay, // 已达上限
		PayURL:         "",
		ExpiresAt:      now.Add(time.Hour),
		NextQueryAt:    now.Add(-time.Second),
		CreatedAt:      now.Add(-time.Minute),
		UpdatedAt:      now.Add(-time.Minute),
	}
	require.NoError(t, repo.Create(context.Background(), ord))

	query := func(ctx context.Context, orderNo, provider string) (*QueryResult, error) {
		return &QueryResult{
			Provider: ProviderWxpay, OrderNo: orderNo,
			TradeState: "NOTPAY", NormalizedState: TradeStateNotPay,
			MchID: "mch1", ExpectedMchID: "mch1",
			AppID: "app1", ExpectedAppID: "app1",
		}, nil
	}
	_, err := g.ReconcileDueQueries(context.Background(), 10, query)
	require.NoError(t, err)

	got, err := repo.GetByOrderNo(context.Background(), "RCG-REPLAY-CAP")
	require.NoError(t, err)
	assert.Equal(t, CreateStateClosePending, got.CreateState)

	// 驱动器扫不到 close_pending
	list, err := repo.ListPendingPrepay(context.Background(), now, 10)
	require.NoError(t, err)
	for _, o := range list {
		assert.NotEqual(t, "RCG-REPLAY-CAP", o.OrderNo)
	}
}

// TestListPendingPrepay_SkipsWithQR 有码订单不进驱动器队列。
func TestListPendingPrepay_SkipsWithQR(t *testing.T) {
	repo := NewMemRepo()
	now := time.Now()
	repo.now = func() time.Time { return now }
	require.NoError(t, repo.Create(context.Background(), &PayOrder{
		OrderNo: "RCG-HAS-QR", Type: OrderTypeRecharge, TenantID: 1, UserID: 1,
		Provider: ProviderWxpay, AmountUSD: 1, ActualPaid: 1, Status: OrderCreated,
		CreateState: CreateStateLocalCreated, PayURL: "weixin://x", ExpiresAt: now.Add(time.Hour),
		CreatedAt: now, UpdatedAt: now,
	}))
	require.NoError(t, repo.Create(context.Background(), &PayOrder{
		OrderNo: "RCG-NEED", Type: OrderTypeRecharge, TenantID: 1, UserID: 1,
		Provider: ProviderWxpay, AmountUSD: 1, ActualPaid: 1, Status: OrderCreated,
		CreateState: CreateStateLocalCreated, PayURL: "", ExpiresAt: now.Add(time.Hour),
		CreatedAt: now, UpdatedAt: now,
	}))
	list, err := repo.ListPendingPrepay(context.Background(), now, 10)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "RCG-NEED", list[0].OrderNo)
}

// 确保测试文件引用 strings（日志审计串检查可选）
var _ = strings.Contains
