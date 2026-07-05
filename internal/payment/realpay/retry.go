package realpay

import (
	"context"
	"errors"
	"net"
	"time"
)

// 出站下单（微信 Prepay / 支付宝下单）重试参数。
//
// 背景：本环境到微信/支付宝 API 为跨境访问，时延间歇性偏高，单次下单可能挂到超时
// （实测 TLS 握手 0.5~9s，偶尔整次 >30s，用户表现为「点击微信支付超时」）。下单按
// out_trade_no 幂等（同单号 → 同 code_url/跳转），故对「瞬时网络/超时」错误做有限次
// 短超时重试，把「偶发一次卡死」变成「快速重试后成功」，而不改变入账语义。
const (
	payCreateAttempts   = 3                      // 最多尝试次数
	payCreatePerTry     = 8 * time.Second        // 每次尝试的独立超时（短于外层 http.Client 的 30s）
	payCreateRetryDelay = 300 * time.Millisecond // 尝试间退避
)

// retryTransientPay 用独立的短超时 ctx 反复执行 fn，直到成功、遇到非瞬时错误或用尽次数。
//
// fn 必须幂等（下单按 out_trade_no 幂等，重试安全）。仅对 isTransientPayErr 判定为瞬时的
// 错误重试；业务错误（参数非法等）快速失败、原样返回。父 ctx 取消 → 立即停止并返回其错误。
func retryTransientPay(ctx context.Context, attempts int, perTry, delay time.Duration, fn func(context.Context) error) error {
	if attempts < 1 {
		attempts = 1
	}
	var err error
	for i := 0; i < attempts; i++ {
		tryCtx, cancel := context.WithTimeout(ctx, perTry)
		err = fn(tryCtx)
		cancel()
		if err == nil || !isTransientPayErr(err) {
			return err
		}
		if i == attempts-1 {
			break // 最后一次不再退避
		}
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return err
}

// isTransientPayErr 判断错误是否为可重试的瞬时网络错误（超时 / 连接层失败）。
// 超时（含 context.DeadlineExceeded、net 超时）、拨号/连接层错误（net.OpError）→ 可重试；
// 业务错误、context.Canceled（调用方主动放弃）→ 不重试。
func isTransientPayErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) { // 拨号失败 / 连接重置 / TLS 建连失败等
		return true
	}
	return false
}
