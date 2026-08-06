package payment

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"syscall"
)

// CreateOutcome 是 CreatePay / 网络调用的结构化结果（PAY-LAT-02）。
// 禁止再把 CreatePay 简化为 success/error 后一律 created→failed。
type CreateOutcome string

const (
	// CreateOutcomeSuccess 平台明确成功并返回支付凭据。
	CreateOutcomeSuccess CreateOutcome = "success"
	// CreateOutcomeDefinitiveReject 平台明确拒绝（参数/权限/商户配置等），可安全 failed。
	CreateOutcomeDefinitiveReject CreateOutcome = "definitive_reject"
	// CreateOutcomeUnknown 请求可能已送达或结果不确定；不得 created→failed。
	CreateOutcomeUnknown CreateOutcome = "outcome_unknown"
)

// OutcomeError 包装错误并携带结果分类与观测字段。
type OutcomeError struct {
	Outcome     CreateOutcome
	ErrorClass  string // 低基：timeout/tls/dns/connect/reset/eof/http_4xx/http_5xx/canceled/...
	Stage       string // dns/connect/tls/write/ttfb/roundtrip/...
	Attempt     int
	MaxAttempts int
	HTTPStatus  int
	Err         error
}

func (e *OutcomeError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return string(e.Outcome)
}

func (e *OutcomeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// AsOutcome 提取 OutcomeError；无包装时按保守策略归类为 unknown。
func AsOutcome(err error) *OutcomeError {
	if err == nil {
		return nil
	}
	var oe *OutcomeError
	if errors.As(err, &oe) && oe != nil {
		return oe
	}
	return &OutcomeError{
		Outcome:    CreateOutcomeUnknown,
		ErrorClass: classifyErrorClass(err),
		Stage:      "unknown",
		Err:        err,
	}
}

// IsOutcomeUnknown 报告是否为结果未知。
func IsOutcomeUnknown(err error) bool {
	oe := AsOutcome(err)
	return oe != nil && oe.Outcome == CreateOutcomeUnknown
}

// IsDefinitiveReject 报告是否为确定性拒绝。
func IsDefinitiveReject(err error) bool {
	oe := AsOutcome(err)
	return oe != nil && oe.Outcome == CreateOutcomeDefinitiveReject
}

// NewOutcomeError 构造分类错误。
func NewOutcomeError(outcome CreateOutcome, class, stage string, err error) *OutcomeError {
	return &OutcomeError{
		Outcome:    outcome,
		ErrorClass: class,
		Stage:      stage,
		Err:        err,
	}
}

// ClassifyNetworkError 将底层网络错误归类为 definitive_reject / outcome_unknown。
// wroteRequest=true 表示请求体/头可能已写出 → 一律 unknown（除非 context.Canceled）。
func ClassifyNetworkError(err error, wroteRequest bool) *OutcomeError {
	if err == nil {
		return nil
	}
	if errors.Is(err, contextCanceledSentinel) || errors.Is(err, errContextCanceled) {
		return NewOutcomeError(CreateOutcomeUnknown, "canceled", "context", err)
	}
	if errors.Is(err, context.Canceled) {
		return NewOutcomeError(CreateOutcomeUnknown, "canceled", "context", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		class := "timeout"
		if wroteRequest {
			return NewOutcomeError(CreateOutcomeUnknown, class, "ttfb", err)
		}
		return NewOutcomeError(CreateOutcomeUnknown, class, "roundtrip", err)
	}

	class := classifyErrorClass(err)
	stage := stageFromClass(class)

	// 写出后一律 unknown
	if wroteRequest {
		return NewOutcomeError(CreateOutcomeUnknown, class, stage, err)
	}

	// 写出前：永久 DNS / 明确拒绝连接仍视为 unknown 保守？文档：
	// DNS/连接在请求写出前明确失败 → 可视为未送达候选，但仍须保守。
	// 对可重试的 pre-write 瞬时错误标 unknown（允许重试）；确定性参数错误由上层标 reject。
	switch class {
	case "canceled":
		return NewOutcomeError(CreateOutcomeUnknown, class, stage, err)
	case "tls_cert", "tls_hostname":
		// 证书/主机名问题：重试无意义，但也可能是中间人——标 definitive 会 failed 订单；
		// 文档要求 x509/hostname 不重试。对订单：definitive_reject（配置/环境问题）。
		return NewOutcomeError(CreateOutcomeDefinitiveReject, class, stage, err)
	default:
		// timeout/dns/connect/reset 写出前 → unknown（可短重试或查单）
		return NewOutcomeError(CreateOutcomeUnknown, class, stage, err)
	}
}

// ClassifyHTTPStatus 根据 HTTP 状态与业务码分类。
// 确定性 4xx 业务（参数/权限）→ definitive_reject；5xx/429/502 等 → unknown。
func ClassifyHTTPStatus(status int, apiCode string, wroteRequest bool) *OutcomeError {
	code := strings.ToUpper(strings.TrimSpace(apiCode))
	// 微信 OUT_TRADE_NO_USED：商户订单号已存在 → 结果未知（须查单，禁止当新失败）
	if code == "OUT_TRADE_NO_USED" {
		return NewOutcomeError(CreateOutcomeUnknown, "out_trade_no_used", "response",
			fmt.Errorf("platform: OUT_TRADE_NO_USED"))
	}
	// 确定性业务拒绝
	switch code {
	case "PARAM_ERROR", "INVALID_REQUEST", "APPID_MCHID_NOT_MATCH", "MCH_NOT_EXISTS",
		"NO_AUTH", "SIGN_ERROR", "ACCOUNT_ERROR", "RULELIMIT":
		return NewOutcomeError(CreateOutcomeDefinitiveReject, "platform_"+strings.ToLower(code), "response",
			fmt.Errorf("platform definitive: %s status=%d", code, status))
	}
	if status >= 400 && status < 500 && status != 429 && status != 408 {
		// 多数 4xx 视为 definitive，除非 408/429
		return NewOutcomeError(CreateOutcomeDefinitiveReject, fmt.Sprintf("http_%d", status), "response",
			fmt.Errorf("http %d code=%s", status, code))
	}
	if status == 0 {
		return NewOutcomeError(CreateOutcomeUnknown, "no_status", "response",
			fmt.Errorf("empty http status code=%s", code))
	}
	// 5xx / 429 / 408 / 网络半响应
	return NewOutcomeError(CreateOutcomeUnknown, fmt.Sprintf("http_%d", status), "response",
		fmt.Errorf("http %d code=%s", status, code))
}

// errContextCanceled 供 errors.Is 使用。
var errContextCanceled = errors.New("context canceled")

// contextCanceledSentinel 避免与 context 包循环——在 classify 里用 errors.Is(err, context.Canceled) 从调用方传。
// 这里用字符串/包装检测。
var contextCanceledSentinel = errContextCanceled

func classifyErrorClass(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	if errors.Is(err, errContextCanceled) || strings.Contains(msg, "context canceled") {
		return "canceled"
	}
	if strings.Contains(msg, "context deadline") || strings.Contains(msg, "deadline exceeded") {
		return "timeout"
	}
	if strings.Contains(msg, "x509") || strings.Contains(msg, "certificate") {
		if strings.Contains(msg, "hostname") || strings.Contains(msg, "not valid for") {
			return "tls_hostname"
		}
		return "tls_cert"
	}
	if strings.Contains(msg, "tls") || strings.Contains(msg, "handshake") {
		return "tls"
	}
	if strings.Contains(msg, "no such host") || strings.Contains(msg, "dns") || strings.Contains(msg, "server misbehaving") {
		return "dns"
	}
	if strings.Contains(msg, "connection refused") {
		return "connect_refused"
	}
	if strings.Contains(msg, "connection reset") || errors.Is(err, syscall.ECONNRESET) {
		return "reset"
	}
	if strings.Contains(msg, "eof") || errors.Is(err, net.ErrClosed) {
		return "eof"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if opErr.Timeout() {
			return "timeout"
		}
		return "connect"
	}
	if strings.Contains(msg, "timeout") {
		return "timeout"
	}
	return "unknown"
}

func stageFromClass(class string) string {
	switch class {
	case "dns":
		return "dns"
	case "connect", "connect_refused":
		return "connect"
	case "tls", "tls_cert", "tls_hostname":
		return "tls"
	case "timeout":
		return "roundtrip"
	case "eof", "reset":
		return "io"
	case "canceled":
		return "context"
	default:
		return "roundtrip"
	}
}

// IsPreWriteRetryable 写出前瞬时错误才可短重试（同一 out_trade_no）。
func IsPreWriteRetryable(err error) bool {
	oe := AsOutcome(err)
	if oe == nil || oe.Outcome == CreateOutcomeDefinitiveReject {
		return false
	}
	if oe.ErrorClass == "canceled" || oe.ErrorClass == "tls_cert" || oe.ErrorClass == "tls_hostname" {
		return false
	}
	// 已写出后的 unknown 不重试 Prepay
	if oe.Stage == "write" || oe.Stage == "ttfb" || oe.Stage == "response" {
		return false
	}
	switch oe.ErrorClass {
	case "timeout", "dns", "connect", "connect_refused", "tls", "reset", "eof", "unknown":
		// timeout 若发生在 ttfb 之后不应进这里；保守：仅 dns/connect/tls 写出前
		return oe.ErrorClass == "dns" || oe.ErrorClass == "connect" || oe.ErrorClass == "connect_refused" ||
			oe.ErrorClass == "tls" || (oe.ErrorClass == "timeout" && (oe.Stage == "connect" || oe.Stage == "tls" || oe.Stage == "dns"))
	default:
		return false
	}
}

// HTTPStatusOf 尽量从错误中解析状态码。
func HTTPStatusOf(err error) int {
	if err == nil {
		return 0
	}
	oe := AsOutcome(err)
	if oe != nil && oe.HTTPStatus > 0 {
		return oe.HTTPStatus
	}
	// 兼容 net/http
	var he interface{ StatusCode() int }
	if errors.As(err, &he) {
		return he.StatusCode()
	}
	return 0
}

// MarkWrote 标记请求已写出（将 pre-write 可重试转为 unknown 不可 Prepay 重试）。
func MarkWrote(err error) error {
	if err == nil {
		return nil
	}
	oe := AsOutcome(err)
	if oe.Stage == "" || oe.Stage == "dns" || oe.Stage == "connect" || oe.Stage == "tls" {
		cp := *oe
		cp.Stage = "write"
		cp.Outcome = CreateOutcomeUnknown
		if cp.Err == nil {
			cp.Err = err
		}
		return &cp
	}
	return err
}

// Ensure http import used for docs / future status helpers.
var _ = http.StatusOK
