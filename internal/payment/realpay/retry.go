package realpay

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/internal/payment"
)

// 同步 Prepay 三重边界（PAY-LAT-02）：overall + per-attempt + max attempts。
// 禁止回到 3×8s + 退避 ≈ 24.6s。
const (
	payCreateMaxAttempts = 2
	payCreatePerAttempt  = 5 * time.Second
	payCreateOverall     = 12 * time.Second
	payCreateRetryDelay  = 200 * time.Millisecond // 仅 pre-write 失败后短退避
	payQueryPerAttempt   = 4 * time.Second
	payQueryMaxAttempts  = 1 // 查单内部禁止乘法重试风暴；外层 5s/30s/60s 调度
)

// attemptClock 可注入的时钟/睡眠（测试用 fake）。
type attemptClock struct {
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error
}

func realClock() attemptClock {
	return attemptClock{
		now: time.Now,
		sleep: func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-t.C:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
}

// retryCreatePay 对 fn 做有界重试：仅 pre-write 瞬时错误可重试；写出后 unknown 立即返回。
// 每次 attempt 带 X-Pay-Attempt 由调用方在 HTTP 层设置；此处返回最后一次错误（已包装 Outcome）。
// onAttempt(attempt, max, err) 供观测（attempt 从 1 起）。
func retryCreatePay(
	ctx context.Context,
	clock attemptClock,
	fn func(tryCtx context.Context, attempt, maxAttempts int) error,
	onAttempt func(attempt, maxAttempts int, err error),
) error {
	if clock.now == nil {
		clock = realClock()
	}
	max := payCreateMaxAttempts
	overall, cancel := context.WithTimeout(ctx, payCreateOverall)
	defer cancel()

	var last error
	for attempt := 1; attempt <= max; attempt++ {
		if err := overall.Err(); err != nil {
			return payment.NewOutcomeError(payment.CreateOutcomeUnknown, "canceled", "context", err)
		}
		tryCtx, tryCancel := context.WithTimeout(overall, payCreatePerAttempt)
		err := fn(tryCtx, attempt, max)
		tryCancel()
		if onAttempt != nil {
			onAttempt(attempt, max, err)
		}
		if err == nil {
			return nil
		}
		last = err
		// context 取消：立即停
		if errors.Is(err, context.Canceled) || overall.Err() != nil {
			return payment.AsOutcome(err)
		}
		oe := payment.AsOutcome(err)
		if oe.Outcome == payment.CreateOutcomeDefinitiveReject {
			return oe
		}
		// 不可 Prepay 重试（可能已送达）
		if !payment.IsPreWriteRetryable(err) {
			return oe
		}
		if attempt == max {
			break
		}
		if sleepErr := clock.sleep(overall, payCreateRetryDelay); sleepErr != nil {
			return payment.NewOutcomeError(payment.CreateOutcomeUnknown, "canceled", "context", sleepErr)
		}
	}
	return payment.AsOutcome(last)
}

// isTransientPayErr 保留兼容旧测试与调用：瞬时网络错误。
// 注意：写出后的 timeout 也返回 true，但 retryCreatePay 用 IsPreWriteRetryable 收紧。
func isTransientPayErr(err error) bool {
	if err == nil {
		return false
	}
	oe := payment.AsOutcome(err)
	if oe.ErrorClass == "canceled" || oe.Outcome == payment.CreateOutcomeDefinitiveReject {
		return false
	}
	return oe.ErrorClass == "timeout" || oe.ErrorClass == "dns" || oe.ErrorClass == "connect" ||
		oe.ErrorClass == "connect_refused" || oe.ErrorClass == "tls" || oe.ErrorClass == "reset" ||
		oe.ErrorClass == "eof" || oe.ErrorClass == "unknown"
}

// retryTransientPay 兼容旧测试名：委托 retryCreatePay（无真实 sleep 的 clock 由测试注入时需改用 retryCreatePay）。
func retryTransientPay(ctx context.Context, attempts int, perTry, delay time.Duration, fn func(context.Context) error) error {
	// 映射到新三重边界：忽略旧 attempts/perTry 参数中的过大值，强制安全预算
	_ = attempts
	_ = perTry
	_ = delay
	return retryCreatePay(ctx, realClock(), func(tryCtx context.Context, attempt, maxAttempts int) error {
		return fn(tryCtx)
	}, nil)
}
