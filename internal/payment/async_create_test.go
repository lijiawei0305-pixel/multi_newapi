package payment

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// slowSDK CreatePay 阻塞到 ctx 取消，模拟跨境慢链路。
type slowSDK struct{}

func (slowSDK) CreatePay(ctx context.Context, req PayRequest) (*PayCredential, error) {
	<-ctx.Done()
	return nil, NewOutcomeError(CreateOutcomeUnknown, "timeout", "roundtrip", ctx.Err())
}

func (slowSDK) Verify(context.Context, Provider, []byte) (*CallbackInfo, error) {
	return nil, errors.New("unused")
}

// TestCreateOrder_BestEffortTimeout_ReturnsUnknownNotFailed
// 同步 best-effort 超时 → ErrCreateOutcomeUnknown + order_no，订单保持 created/local_created|prepay_unknown，不得 failed。
func TestCreateOrder_BestEffortTimeout_ReturnsUnknownNotFailed(t *testing.T) {
	repo := NewMemRepo()
	now := time.Unix(1_700_100_000, 0)
	repo.now = func() time.Time { return now }
	g := NewGateway(repo, slowSDK{}, map[OrderType]OrderSink{
		OrderTypeRecharge: newFakeSink(),
	}, WithClock(func() time.Time { return now }))

	// 用短 deadline 模拟同步 2.5s（测试不真等 2.5s）
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	t0 := time.Now()
	o, err := g.CreateOrder(ctx, OrderInput{
		Type: OrderTypeRecharge, TenantID: 1, UserID: 2, Provider: ProviderWxpay,
		AmountUSD: 1, ActualPaid: 7, Subject: "t",
	})
	elapsed := time.Since(t0)
	require.NotNil(t, o)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrCreateOutcomeUnknown) || AsOutcome(err) != nil)
	assert.NotEmpty(t, o.OrderNo)
	assert.Equal(t, OrderCreated, o.Status)
	assert.NotEqual(t, OrderFailed, o.Status)
	assert.Less(t, elapsed, 2*time.Second, "must not hang on long sync budget")

	got, gerr := repo.GetByOrderNo(context.Background(), o.OrderNo)
	require.NoError(t, gerr)
	assert.Equal(t, OrderCreated, got.Status)
	assert.Empty(t, got.PayURL)
	// finishCreatePay 在 SDK 超时后写 prepay_unknown 或因 claim 后 unknown
	assert.True(t,
		got.CreateState == CreateStatePrepayUnknown ||
			got.CreateState == CreateStateLocalCreated ||
			got.CreateState == CreateStatePrepayInflight,
		"create_state=%s", got.CreateState)
}
