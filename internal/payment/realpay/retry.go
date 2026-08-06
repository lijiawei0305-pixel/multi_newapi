package realpay

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/internal/payment"
)

// Prepay 预算：同步 best-effort 与后台驱动器解耦。
//
// 同步（用户请求）：短 overall，超时即 202 queued，交后台继续。
// 后台（P0-B 驱动器）：更宽 per-attempt/overall/attempts，不阻塞用户。
//
// 禁止回到 3×8s 同步长等待。
const (
	// 同步（P1-A）：单次 best-effort ≈2.5s
	payCreateMaxAttemptsSync = 1
	payCreatePerAttemptSync  = 2500 * time.Millisecond
	payCreateOverallSync     = 2500 * time.Millisecond

	// 后台（P0-B）
	payCreateMaxAttemptsBg = 3
	payCreatePerAttemptBg  = 8 * time.Second
	payCreateOverallBg     = 20 * time.Second

	// 兼容旧名：默认指向同步预算（测试/未带 context 的路径）
	payCreateMaxAttempts = payCreateMaxAttemptsSync
	payCreatePerAttempt  = payCreatePerAttemptSync
	payCreateOverall     = payCreateOverallSync

	// 第二次 attempt 至少需要 dial+TLS+1s 余量（P2：不再用低于冷连地板的 2.5s）
	// 同步仅 1 attempt 时此值不参与；后台 3 attempt 时用。
	payCreateMinRetryBudget = 3*time.Second + 4*time.Second + 1*time.Second // dial+TLS 目标 + 1s
	payCreateRetryDelay     = 150 * time.Millisecond                        // 仅 pre-write 失败后短退避
	payQueryPerAttempt      = 4 * time.Second
	payQueryMaxAttempts     = 1 // 查单内部禁止乘法重试风暴；外层 5s/30s/60s 调度
)

// BudgetMode 选择 Prepay 重试预算。
type BudgetMode int

const (
	// BudgetSync 用户请求路径：best-effort 短预算。
	BudgetSync BudgetMode = iota
	// BudgetBackground 对账/驱动器：宽预算。
	BudgetBackground
)

type ctxKeyBudgetMode struct{}

// WithBudgetMode 将预算模式写入 context（Gateway → SDK → retryCreatePay）。
func WithBudgetMode(ctx context.Context, m BudgetMode) context.Context {
	return context.WithValue(ctx, ctxKeyBudgetMode{}, m)
}

func budgetModeFrom(ctx context.Context) BudgetMode {
	if ctx == nil {
		return BudgetSync
	}
	if v, ok := ctx.Value(ctxKeyBudgetMode{}).(BudgetMode); ok {
		return v
	}
	return BudgetSync
}

type createBudget struct {
	maxAttempts    int
	perAttempt     time.Duration
	overall        time.Duration
	minRetryBudget time.Duration
}

func budgetFor(mode BudgetMode) createBudget {
	switch mode {
	case BudgetBackground:
		return createBudget{
			maxAttempts:    payCreateMaxAttemptsBg,
			perAttempt:     payCreatePerAttemptBg,
			overall:        payCreateOverallBg,
			minRetryBudget: payCreateMinRetryBudget,
		}
	default:
		return createBudget{
			maxAttempts:    payCreateMaxAttemptsSync,
			perAttempt:     payCreatePerAttemptSync,
			overall:        payCreateOverallSync,
			minRetryBudget: 0, // 同步单次，无二次 attempt
		}
	}
}

// prepayDriverEnabled 默认开启；PAYMENT_PREPAY_DRIVER_ENABLED=0/false 关闭。
func prepayDriverEnabled() bool {
	v := strings.TrimSpace(os.Getenv("PAYMENT_PREPAY_DRIVER_ENABLED"))
	if v == "" {
		return true
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return true
	}
	return b
}

// PrepayDriverEnabled 导出给 mtwire 对账循环。
func PrepayDriverEnabled() bool { return prepayDriverEnabled() }

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
// 预算由 context BudgetMode 决定（同步 / 后台）。
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
	b := budgetFor(budgetModeFrom(ctx))
	max := b.maxAttempts
	overall, cancel := context.WithTimeout(ctx, b.overall)
	defer cancel()

	deadline, hasDeadline := overall.Deadline()
	var last error
	for attempt := 1; attempt <= max; attempt++ {
		if err := overall.Err(); err != nil {
			return payment.NewOutcomeError(payment.CreateOutcomeUnknown, "canceled", "context", err)
		}
		// 剩余 overall 不足时跳过重试，避免「半截 attempt」空等
		if attempt > 1 && hasDeadline && b.minRetryBudget > 0 {
			remain := time.Until(deadline)
			if remain < b.minRetryBudget {
				return payment.AsOutcome(last)
			}
		}
		tryCtx, tryCancel := context.WithTimeout(overall, b.perAttempt)
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
