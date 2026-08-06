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

// runHedged 对冲执行 fn(preferBackup)：主域立即发出，hedgeDelay 后（或主域先失败时立即）
// 并发备域，取先到的成功结果，另一路由 ctx cancel。
//
// 语义规则（P1 修复）：
//   - 任一路返回 definitive_reject 立即短路返回该错误：definitive 表示支付机构**已应答并拒绝**
//     （NO_AUTH / 参数错 / 签名错 / 证书错），主备两域面向同一商户配置，结论一致且与资金无关。
//     绝不可降级成 unknown——否则用户看到「正在核实」而非「商户无权限」，
//     后台驱动器还会对永久失败的配置错误反复重试，并把 unknown 计数喂给熔断器。
//   - 其余失败一律保持 unknown（可能已写出），保留原有资金语义。
//   - 同 out_trade_no 幂等 → 两路同时到达微信也不会产生双单。
func runHedged(
	ctx context.Context,
	fn func(ctx context.Context, preferBackup bool) (string, error),
) (string, error) {
	if !hedgeEnabled() {
		return fn(ctx, false)
	}

	type out struct {
		s   string
		err error
		bak bool
	}

	// 任一路胜出即 cancel 另一路
	hctx, cancelAll := context.WithCancel(ctx)
	defer cancelAll()

	ch := make(chan out, 2) // 缓冲 2：提前返回也不会泄漏 goroutine
	run := func(preferBackup bool) {
		s, err := fn(hctx, preferBackup)
		ch <- out{s: s, err: err, bak: preferBackup}
	}

	go run(false)

	timer := time.NewTimer(hedgeDelay())
	defer timer.Stop()

	var firstErr error
	pending := 1
	backupStarted := false

	for pending > 0 {
		select {
		case <-ctx.Done():
			return "", payment.NewOutcomeError(payment.CreateOutcomeUnknown, "canceled", "context", ctx.Err())

		case <-timer.C:
			if !backupStarted {
				backupStarted = true
				pending++
				go run(true)
			}

		case r := <-ch:
			pending--
			if r.err == nil && strings.TrimSpace(r.s) != "" {
				return r.s, nil
			}
			// 确定性拒绝：短路，不再启动备域、不再等待另一路
			if payment.IsDefinitiveReject(r.err) {
				return "", payment.AsOutcome(r.err)
			}
			if firstErr == nil && r.err != nil {
				firstErr = r.err
			}
			if !backupStarted {
				// 主域已失败且非 definitive：立即起备域，不再干等 hedgeDelay
				backupStarted = true
				pending++
				timer.Stop()
				go run(true)
			}
		}
	}

	if firstErr == nil {
		firstErr = payment.NewOutcomeError(payment.CreateOutcomeUnknown, "empty_result", "response",
			errors.New("hedge: no usable result from primary/backup"))
	}
	return "", payment.AsOutcome(firstErr)
}
