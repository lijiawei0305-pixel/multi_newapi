package realpay

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/internal/payment"
)

func TestHedge_PrimaryHangBackupWins(t *testing.T) {
	t.Setenv("PAYMENT_HEDGE_ENABLED", "true")
	t.Setenv("PAYMENT_HEDGE_DELAY_MS", "100")
	var primary, backup atomic.Int32
	start := time.Now()
	url, err := runHedged(context.Background(), func(ctx context.Context, preferBackup bool) (string, error) {
		if preferBackup {
			backup.Add(1)
			select {
			case <-time.After(50 * time.Millisecond):
				return "weixin://backup", nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		primary.Add(1)
		// 主域 hang 到 ctx cancel
		<-ctx.Done()
		return "", payment.NewOutcomeError(payment.CreateOutcomeUnknown, "timeout", "ttfb", ctx.Err())
	})
	elapsed := time.Since(start)
	require.NoError(t, err)
	assert.Equal(t, "weixin://backup", url)
	assert.Equal(t, int32(1), primary.Load())
	assert.Equal(t, int32(1), backup.Load())
	// ≈ hedgeDelay(100ms) + backup(50ms) ≈ 150ms，远小于主域 hang
	assert.Less(t, elapsed, 800*time.Millisecond)
	assert.GreaterOrEqual(t, elapsed, 100*time.Millisecond)
}

func TestHedge_BothSuccessNoDoubleBill(t *testing.T) {
	t.Setenv("PAYMENT_HEDGE_ENABLED", "true")
	t.Setenv("PAYMENT_HEDGE_DELAY_MS", "30")
	var n atomic.Int32
	url, err := runHedged(context.Background(), func(ctx context.Context, preferBackup bool) (string, error) {
		c := n.Add(1)
		// 两路都成功且同 URL（同 out_trade_no 幂等）
		select {
		case <-time.After(20 * time.Millisecond):
			return "weixin://same", nil
		case <-ctx.Done():
			// 败者被 cancel 仍可能「已写出」
			if c >= 1 {
				return "weixin://same", nil
			}
			return "", ctx.Err()
		}
	})
	require.NoError(t, err)
	assert.Equal(t, "weixin://same", url)
	// 至多两路
	assert.LessOrEqual(t, n.Load(), int32(2))
}

func TestHedge_Disabled_PrimaryOnly(t *testing.T) {
	t.Setenv("PAYMENT_HEDGE_ENABLED", "false")
	var n atomic.Int32
	url, err := runHedged(context.Background(), func(ctx context.Context, preferBackup bool) (string, error) {
		n.Add(1)
		if preferBackup {
			t.Fatal("backup should not run when hedge disabled")
		}
		return "weixin://primary", nil
	})
	require.NoError(t, err)
	assert.Equal(t, "weixin://primary", url)
	assert.Equal(t, int32(1), n.Load())
}

func TestBreaker_OpenAfterNUnknown(t *testing.T) {
	t.Setenv("PAYMENT_BREAKER_ENABLED", "true")
	t.Setenv("PAYMENT_BREAKER_N", "3")
	t.Setenv("PAYMENT_BREAKER_T_SEC", "2")
	ResetBreakerForTest()

	require.True(t, BreakerAllowSync())
	BreakerRecordUnknown()
	BreakerRecordUnknown()
	require.True(t, BreakerAllowSync(), "not yet open")
	BreakerRecordUnknown() // 第 3 次 → open
	require.True(t, BreakerIsOpen())
	// 开路后同步不允许（半开前）
	assert.False(t, BreakerAllowSync())
}

func TestBreaker_SuccessResets(t *testing.T) {
	t.Setenv("PAYMENT_BREAKER_ENABLED", "true")
	t.Setenv("PAYMENT_BREAKER_N", "2")
	ResetBreakerForTest()
	BreakerRecordUnknown()
	BreakerRecordSuccess()
	BreakerRecordUnknown()
	// streak reset after success：仍未开路
	assert.False(t, BreakerIsOpen())
	assert.True(t, BreakerAllowSync())
}

// 主域确定性拒绝（NO_AUTH）+ 备域 hang：必须原样返回 definitive，绝不降级成 unknown，
// 且不得再等备域（用户应立刻看到「商户无权限」而非「正在核实」）。
func TestHedge_PrimaryDefinitiveShortCircuits(t *testing.T) {
	t.Setenv("PAYMENT_HEDGE_ENABLED", "true")
	t.Setenv("PAYMENT_HEDGE_DELAY_MS", "100")
	var backup atomic.Int32
	start := time.Now()
	_, err := runHedged(context.Background(), func(ctx context.Context, preferBackup bool) (string, error) {
		if preferBackup {
			backup.Add(1)
			<-ctx.Done()
			return "", payment.NewOutcomeError(payment.CreateOutcomeUnknown, "timeout", "ttfb", ctx.Err())
		}
		return "", payment.NewOutcomeError(payment.CreateOutcomeDefinitiveReject, "platform_no_auth", "response",
			errors.New("NO_AUTH"))
	})
	elapsed := time.Since(start)
	require.Error(t, err)
	assert.True(t, payment.IsDefinitiveReject(err), "definitive 不得被降级为 unknown")
	assert.Equal(t, "platform_no_auth", payment.AsOutcome(err).ErrorClass)
	// 主域立即拒绝 → 备域根本不该启动，也不该等满 hedgeDelay
	assert.Equal(t, int32(0), backup.Load(), "definitive 后不得再打备域")
	assert.Less(t, elapsed, 100*time.Millisecond)
}

// 主域 hang + 备域确定性拒绝：同样返回 definitive（支付机构已应答）。
func TestHedge_BackupDefinitiveShortCircuits(t *testing.T) {
	t.Setenv("PAYMENT_HEDGE_ENABLED", "true")
	t.Setenv("PAYMENT_HEDGE_DELAY_MS", "30")
	_, err := runHedged(context.Background(), func(ctx context.Context, preferBackup bool) (string, error) {
		if preferBackup {
			return "", payment.NewOutcomeError(payment.CreateOutcomeDefinitiveReject, "platform_param_error", "response",
				errors.New("PARAM_ERROR"))
		}
		<-ctx.Done()
		return "", payment.NewOutcomeError(payment.CreateOutcomeUnknown, "timeout", "ttfb", ctx.Err())
	})
	require.Error(t, err)
	assert.True(t, payment.IsDefinitiveReject(err))
	assert.Equal(t, "platform_param_error", payment.AsOutcome(err).ErrorClass)
}

// 两路都是网络类失败：保持 unknown（资金语义不得改变）。
func TestHedge_BothUnknownStaysUnknown(t *testing.T) {
	t.Setenv("PAYMENT_HEDGE_ENABLED", "true")
	t.Setenv("PAYMENT_HEDGE_DELAY_MS", "20")
	_, err := runHedged(context.Background(), func(ctx context.Context, preferBackup bool) (string, error) {
		return "", payment.NewOutcomeError(payment.CreateOutcomeUnknown, "tls", "tls", errors.New("handshake timeout"))
	})
	require.Error(t, err)
	assert.True(t, payment.IsOutcomeUnknown(err))
	assert.False(t, payment.IsDefinitiveReject(err))
}

// 主域网络失败时应立即启动备域，不干等 hedgeDelay。
func TestHedge_PrimaryFailStartsBackupImmediately(t *testing.T) {
	t.Setenv("PAYMENT_HEDGE_ENABLED", "true")
	t.Setenv("PAYMENT_HEDGE_DELAY_MS", "5000") // 故意很大：若干等则测试会超时失败
	start := time.Now()
	url, err := runHedged(context.Background(), func(ctx context.Context, preferBackup bool) (string, error) {
		if preferBackup {
			return "weixin://backup", nil
		}
		return "", payment.NewOutcomeError(payment.CreateOutcomeUnknown, "connect", "connect", errors.New("refused"))
	})
	require.NoError(t, err)
	assert.Equal(t, "weixin://backup", url)
	assert.Less(t, time.Since(start), 1*time.Second, "主域失败后不得干等 hedgeDelay")
}
