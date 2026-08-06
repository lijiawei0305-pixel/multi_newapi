package payment

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBreakerOpen_SyncCreateQueuedFast 熔断开路时同步 create <100ms 返回 queued（不调 SDK）。
func TestBreakerOpen_SyncCreateQueuedFast(t *testing.T) {
	repo := NewMemRepo()
	now := time.Unix(1_700_200_000, 0)
	repo.now = func() time.Time { return now }

	var sdkCalls atomic.Int32
	sdk := &countingSDK{payURL: "weixin://should-not-call"}
	// wrap counting
	wrapped := &callCountSDK{inner: sdk, n: &sdkCalls}

	// 注入「永远开路」熔断
	SetBreakerHooks(
		func() bool { return false }, // allow=false
		func() {},
		func() {},
		func() {},
	)
	t.Cleanup(func() {
		SetBreakerHooks(func() bool { return true }, func() {}, func() {}, func() {})
	})

	g := NewGateway(repo, wrapped, map[OrderType]OrderSink{
		OrderTypeRecharge: newFakeSink(),
	}, WithClock(func() time.Time { return now }))

	t0 := time.Now()
	o, err := g.CreateOrder(context.Background(), OrderInput{
		Type: OrderTypeRecharge, TenantID: 1, UserID: 2, Provider: ProviderWxpay,
		AmountUSD: 1, ActualPaid: 7, Subject: "breaker",
	})
	elapsed := time.Since(t0)
	require.NotNil(t, o)
	require.ErrorIs(t, err, ErrCreateOutcomeUnknown)
	assert.Equal(t, OrderCreated, o.Status)
	assert.Empty(t, o.PayURL)
	assert.Equal(t, int32(0), sdkCalls.Load(), "SDK must not be called when breaker open")
	assert.Less(t, elapsed, 100*time.Millisecond)
}

type callCountSDK struct {
	inner PaySDK
	n     *atomic.Int32
}

func (c *callCountSDK) CreatePay(ctx context.Context, req PayRequest) (*PayCredential, error) {
	c.n.Add(1)
	return c.inner.CreatePay(ctx, req)
}

func (c *callCountSDK) Verify(ctx context.Context, p Provider, raw []byte) (*CallbackInfo, error) {
	return c.inner.Verify(ctx, p, raw)
}
