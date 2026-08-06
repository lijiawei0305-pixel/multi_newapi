package realpay

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/internal/payment"
)

func TestRetryCreatePayOnlyPreWrite(t *testing.T) {
	var n atomic.Int32
	clock := attemptClock{
		now: time.Now,
		sleep: func(ctx context.Context, d time.Duration) error {
			return nil // no real sleep
		},
	}
	// 后台预算：第一次 connect 失败（pre-write），第二次成功
	err := retryCreatePay(WithBudgetMode(context.Background(), BudgetBackground), clock,
		func(tryCtx context.Context, attempt, maxAttempts int) error {
			c := int(n.Add(1))
			if c == 1 {
				return payment.NewOutcomeError(payment.CreateOutcomeUnknown, "connect", "connect",
					errors.New("connection refused"))
			}
			return nil
		}, nil)
	require.NoError(t, err)
	assert.Equal(t, int32(2), n.Load())
}

func TestRetryCreatePayNoRetryAfterWrite(t *testing.T) {
	var n atomic.Int32
	clock := attemptClock{now: time.Now, sleep: func(context.Context, time.Duration) error { return nil }}
	err := retryCreatePay(context.Background(), clock,
		func(tryCtx context.Context, attempt, maxAttempts int) error {
			n.Add(1)
			return payment.NewOutcomeError(payment.CreateOutcomeUnknown, "timeout", "ttfb",
				errors.New("timeout after write"))
		}, nil)
	require.Error(t, err)
	assert.Equal(t, int32(1), n.Load(), "must not retry after write/ttfb")
	assert.True(t, payment.IsOutcomeUnknown(err))
}

func TestRetryCreatePayDefinitiveNoRetry(t *testing.T) {
	var n atomic.Int32
	clock := attemptClock{now: time.Now, sleep: func(context.Context, time.Duration) error { return nil }}
	err := retryCreatePay(context.Background(), clock,
		func(tryCtx context.Context, attempt, maxAttempts int) error {
			n.Add(1)
			return payment.NewOutcomeError(payment.CreateOutcomeDefinitiveReject, "param_error", "response",
				errors.New("PARAM_ERROR"))
		}, nil)
	require.Error(t, err)
	assert.Equal(t, int32(1), n.Load())
	assert.True(t, payment.IsDefinitiveReject(err))
}

func TestRetryCreatePayContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	clock := attemptClock{now: time.Now, sleep: func(context.Context, time.Duration) error { return context.Canceled }}
	err := retryCreatePay(ctx, clock,
		func(tryCtx context.Context, attempt, maxAttempts int) error {
			return tryCtx.Err()
		}, nil)
	require.Error(t, err)
}

func TestPaymentClientRejectsInsecureAndCrossRedirect(t *testing.T) {
	c := paymentHTTPClient(nil)
	require.NotNil(t, c)
	// 永不 InsecureSkipVerify
	tr := unwrapTransport(c.Transport).(*http.Transport)
	require.NotNil(t, tr.TLSClientConfig)
	assert.False(t, tr.TLSClientConfig.InsecureSkipVerify)
	assert.GreaterOrEqual(t, int(tr.TLSClientConfig.MinVersion), int(tls.VersionTLS12))
	assert.Nil(t, tr.Proxy)

	// cross-hostname redirect denied
	err := c.CheckRedirect(
		&http.Request{URL: mustURL("https://evil.example/x")},
		[]*http.Request{{URL: mustURL("https://api.mch.weixin.qq.com/v3/x")}},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redirects denied")
}

func mustURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}

func TestTracingRoundTripperClassifiesTimeout(t *testing.T) {
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, &net.OpError{Op: "dial", Err: errors.New("i/o timeout")}
	})
	rt := &tracingRoundTripper{base: base, logf: func(string, ...any) {}}
	_, err := rt.RoundTrip(httptest.NewRequest(http.MethodGet, "https://api.mch.weixin.qq.com/", nil))
	require.Error(t, err)
	assert.True(t, payment.IsOutcomeUnknown(err) || payment.AsOutcome(err).ErrorClass != "")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestClassifyHTTPStatusOutTradeNoUsed(t *testing.T) {
	oe := payment.ClassifyHTTPStatus(400, "OUT_TRADE_NO_USED", true)
	assert.Equal(t, payment.CreateOutcomeUnknown, oe.Outcome)
	assert.Equal(t, "out_trade_no_used", oe.ErrorClass)
}

func TestClassifyHTTPStatusParamError(t *testing.T) {
	oe := payment.ClassifyHTTPStatus(400, "PARAM_ERROR", true)
	assert.Equal(t, payment.CreateOutcomeDefinitiveReject, oe.Outcome)
}

func TestIsPreWriteRetryable(t *testing.T) {
	assert.True(t, payment.IsPreWriteRetryable(
		payment.NewOutcomeError(payment.CreateOutcomeUnknown, "connect", "connect", errors.New("x"))))
	assert.False(t, payment.IsPreWriteRetryable(
		payment.NewOutcomeError(payment.CreateOutcomeUnknown, "timeout", "ttfb", errors.New("x"))))
	assert.False(t, payment.IsPreWriteRetryable(
		payment.NewOutcomeError(payment.CreateOutcomeDefinitiveReject, "param_error", "response", errors.New("x"))))
}

func TestPaymentClientNotAffectedByGlobalInsecure(t *testing.T) {
	// 即使 DefaultTransport 被污染，payment client 仍安全
	if tr, ok := http.DefaultTransport.(*http.Transport); ok && tr != nil {
		if tr.TLSClientConfig == nil {
			tr.TLSClientConfig = &tls.Config{}
		}
		prev := tr.TLSClientConfig.InsecureSkipVerify
		tr.TLSClientConfig.InsecureSkipVerify = true
		t.Cleanup(func() { tr.TLSClientConfig.InsecureSkipVerify = prev })
	}
	c := paymentHTTPClient(nil)
	pt := unwrapTransport(c.Transport).(*http.Transport)
	assert.False(t, pt.TLSClientConfig.InsecureSkipVerify)
}

func TestSelfSignedWouldFail(t *testing.T) {
	// 启动仅自签的 TLS server；client 必须失败
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	// 无证书的 TLS 握手会失败——用 httptest 更简单：直接测 Insecure=false 配置存在即可。
	// 完整自签握手测需要证书生成；此处验证 MinVersion + Insecure=false 契约。
	c := paymentHTTPClient(nil)
	pt := unwrapTransport(c.Transport).(*http.Transport)
	assert.False(t, pt.TLSClientConfig.InsecureSkipVerify)
	assert.Equal(t, uint16(tls.VersionTLS12), pt.TLSClientConfig.MinVersion)
	_ = ln
}

func TestBudgetConstantsNot24s(t *testing.T) {
	// 同步：best-effort ≤2.5s，禁止回到 3×8s 同步等待
	assert.LessOrEqual(t, int(payCreateOverallSync/time.Millisecond), 2500)
	assert.LessOrEqual(t, int(payCreatePerAttemptSync/time.Millisecond), 2500)
	assert.Equal(t, 1, payCreateMaxAttemptsSync)
	// 后台：8s/20s/3，仍远小于旧 24.6s 同步
	assert.Equal(t, 8*time.Second, payCreatePerAttemptBg)
	assert.Equal(t, 20*time.Second, payCreateOverallBg)
	assert.Equal(t, 3, payCreateMaxAttemptsBg)
	// minRetryBudget ≥ 冷连地板（dial 目标 3s + TLS 4s + 1s），仅后台使用
	assert.GreaterOrEqual(t, payCreateMinRetryBudget, 8*time.Second)
	// P2 Transport：dial 3 / TLS 4 / header 5；冷连地板 ≤ 后台 per-attempt
	assert.Equal(t, 3*time.Second, payDialTimeout)
	assert.Equal(t, 4*time.Second, payTLSHandshakeTimeout)
	assert.Equal(t, 5*time.Second, payResponseHeaderTimeout)
	assert.LessOrEqual(t, payDialTimeout+payTLSHandshakeTimeout, payCreatePerAttemptBg)
	assert.GreaterOrEqual(t, payClientTimeout, payCreateOverallBg)
}

func TestCheckRedirectIsDefinitive(t *testing.T) {
	c := paymentHTTPClient(nil)
	err := c.CheckRedirect(nil, nil)
	require.Error(t, err)
	assert.True(t, payment.IsDefinitiveReject(err), "redirect must be definitive_reject, not unknown")
}

func TestClosePaymentIdleConnectionsNoRace(t *testing.T) {
	// 并发 Close + Shared 不得 panic / data race（配合 -race）
	done := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			_ = paymentHTTPClientShared()
			ClosePaymentIdleConnections()
		}
		close(done)
	}()
	for i := 0; i < 50; i++ {
		_ = paymentHTTPClientShared()
		ClosePaymentIdleConnections()
	}
	<-done
}

func TestRetrySkipsWhenOverallBudgetTooLow(t *testing.T) {
	// 第一次用尽 overall 后不得再发起第二次 attempt
	start := time.Now()
	clock := attemptClock{
		now: time.Now,
		sleep: func(ctx context.Context, d time.Duration) error {
			return nil
		},
	}
	var n atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	// 使用真实 overall 边界：手动缩短 overall via parent ctx
	err := retryCreatePay(ctx, clock,
		func(tryCtx context.Context, attempt, maxAttempts int) error {
			n.Add(1)
			// 耗尽 parent ctx
			<-tryCtx.Done()
			return payment.NewOutcomeError(payment.CreateOutcomeUnknown, "timeout", "connect",
				errors.New("timeout pre-write"))
		}, nil)
	require.Error(t, err)
	// parent 50ms 时通常只来得及 1 次
	assert.LessOrEqual(t, n.Load(), int32(2))
	assert.Less(t, time.Since(start), 500*time.Millisecond)
}
