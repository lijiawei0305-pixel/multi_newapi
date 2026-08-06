package realpay

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/internal/payment"
)

// 主备对冲：主域发出后 hedgeDelay 仍无结果，则并发备域；取先到者，另一路 cancel。
// 同 out_trade_no 幂等 → 无双单资金风险；两路都按「可能已写出」处理（失败一律 unknown）。

const defaultHedgeDelay = 700 * time.Millisecond

func hedgeEnabled() bool {
	v := strings.TrimSpace(os.Getenv("PAYMENT_HEDGE_ENABLED"))
	if v == "" {
		return true
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return true
	}
	return b
}

func hedgeDelay() time.Duration {
	v := strings.TrimSpace(os.Getenv("PAYMENT_HEDGE_DELAY_MS"))
	if v == "" {
		return defaultHedgeDelay
	}
	ms, err := strconv.Atoi(v)
	if err != nil || ms < 50 {
		return defaultHedgeDelay
	}
	return time.Duration(ms) * time.Millisecond
}

type hedgeResult struct {
	val string
	err error
	// backup 是否来自备域（观测）
	backup bool
}

// runHedged 对冲执行 fn(preferBackup)。
// fn 必须在 preferBackup=true 时走备域；两路均可能「已写出」，失败不得判 definitive（由 fn 内部分类）。
func runHedged(
	ctx context.Context,
	fn func(ctx context.Context, preferBackup bool) (string, error),
) (string, error) {
	if !hedgeEnabled() {
		return fn(ctx, false)
	}
	// 父 ctx 取消则两路停
	type out struct {
		s   string
		err error
		bak bool
	}
	ch := make(chan out, 2)
	var once sync.Once
	var winner out
	done := make(chan struct{})

	run := func(preferBackup bool) {
		cctx, cancel := context.WithCancel(ctx)
		defer cancel()
		// 与 sibling 联动：winner 产生后 cancel 由外层负责
		s, err := fn(cctx, preferBackup)
		select {
		case ch <- out{s: s, err: err, bak: preferBackup}:
		case <-done:
		}
	}

	// 主域立即
	go run(false)

	delay := hedgeDelay()
	timer := time.NewTimer(delay)
	defer timer.Stop()

	backupStarted := false
	for {
		select {
		case <-ctx.Done():
			close(done)
			return "", payment.NewOutcomeError(payment.CreateOutcomeUnknown, "canceled", "context", ctx.Err())
		case <-timer.C:
			if !backupStarted {
				backupStarted = true
				go run(true)
			}
		case r := <-ch:
			if r.err == nil && strings.TrimSpace(r.s) != "" {
				once.Do(func() {
					winner = r
					close(done)
				})
				return r.s, nil
			}
			// 失败：若另一路还在跑，等它；两路都失败则返回 unknown
			if backupStarted {
				// 可能已收齐两路，或再等一个
				select {
				case r2 := <-ch:
					if r2.err == nil && strings.TrimSpace(r2.s) != "" {
						return r2.s, nil
					}
					// 两路皆败：优先返回「可能已写出」的 unknown
					return "", preferUnknown(r.err, r2.err)
				case <-ctx.Done():
					return "", payment.AsOutcome(r.err)
				case <-time.After(50 * time.Millisecond):
					// 再给一点时间
					select {
					case r2 := <-ch:
						if r2.err == nil && strings.TrimSpace(r2.s) != "" {
							return r2.s, nil
						}
						return "", preferUnknown(r.err, r2.err)
					default:
						return "", payment.AsOutcome(r.err)
					}
				}
			}
			// 主域先败且尚未启动备域：立即备域（不再干等 delay）
			if !backupStarted {
				backupStarted = true
				timer.Stop()
				go run(true)
			}
			// 继续等备域
			_ = winner
		}
	}
}

func preferUnknown(a, b error) error {
	if a == nil {
		return payment.AsOutcome(b)
	}
	if b == nil {
		return payment.AsOutcome(a)
	}
	// 任一 definitive 仍返回 definitive（如双路同 NO_AUTH）
	if payment.IsDefinitiveReject(a) && payment.IsDefinitiveReject(b) {
		return a
	}
	if payment.IsDefinitiveReject(a) && !payment.IsDefinitiveReject(b) {
		return payment.AsOutcome(b)
	}
	if payment.IsDefinitiveReject(b) && !payment.IsDefinitiveReject(a) {
		return payment.AsOutcome(a)
	}
	return payment.AsOutcome(a)
}
