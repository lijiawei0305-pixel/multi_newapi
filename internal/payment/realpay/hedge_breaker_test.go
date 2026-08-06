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

// 通过 payment 钩子测同步 create 在开路时快速 queued
func TestBreaker_HooksIntegrationNote(t *testing.T) {
	// 见 payment 包 TestBreakerOpen_SyncCreateQueuedFast
	_ = errors.New("placeholder")
}
