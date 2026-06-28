package risk

import (
	"net/http"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

// 本模块错误码命名空间（detailed-design §2.13 / §6.4）。
// RATE_LIMITED / IP_NOT_ALLOWED / PURCHASE_LIMIT_EXCEEDED / STATUS_FORBIDDEN 见设计文档。
const (
	// CodeRateLimited 触发 RPM 限流（滑动/固定窗口计数超限）。
	CodeRateLimited = "RATE_LIMITED"
	// CodeIPNotAllowed 客户端 IP 不在调用主体的 allowlist 内。
	CodeIPNotAllowed = "IP_NOT_ALLOWED"
	// CodePurchaseLimitExceeded 触发套餐限购（Trial=用户∪实名∪设备各 1 次）。
	CodePurchaseLimitExceeded = "PURCHASE_LIMIT_EXCEEDED"
	// CodeStatusForbidden 调用主体（租户/用户/Token）非可调用状态。
	CodeStatusForbidden = "STATUS_FORBIDDEN"
)

var (
	// ErrRateLimited RPM 限流：窗口内请求数超过上限。
	ErrRateLimited = apperr.New(CodeRateLimited, "请求过于频繁，已触发限流", http.StatusTooManyRequests)
	// ErrIPNotAllowed 客户端 IP 不在 allowlist 内。
	ErrIPNotAllowed = apperr.New(CodeIPNotAllowed, "客户端 IP 不被允许", http.StatusForbidden)
	// ErrPurchaseLimitExceeded 套餐限购：超过 Trial（用户∪实名∪设备各 1 次）或其它档位上限。
	ErrPurchaseLimitExceeded = apperr.New(CodePurchaseLimitExceeded, "已超过该套餐的限购次数", http.StatusConflict)
	// ErrStatusForbidden 调用主体被禁用/冻结，禁止调用。
	ErrStatusForbidden = apperr.New(CodeStatusForbidden, "调用主体状态异常，禁止调用", http.StatusForbidden)
)
