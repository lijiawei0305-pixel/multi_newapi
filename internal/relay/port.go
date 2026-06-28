package relay

import (
	"context"

	"github.com/QuantumNous/new-api/internal/platform/appctx"
)

// ---- 错误码（relay 命名空间，apperr.Code）----
// relay 是纯编排层：除上游转发失败统一标 UPSTREAM_ERROR 外，其余错误码均由各依赖
// （Identity / Risk / ModelPermission / Billing）构造并**原样上浮**，本包不改写、不吞码。
// 详见 doc/detailed-design.md §2.11（错误码：透传各依赖错误码 + UPSTREAM_ERROR）。
const (
	// CodeUpstreamError 上游渠道池转发失败（网络/渠道/非 AppError 的底层错误）。
	CodeUpstreamError = "UPSTREAM_ERROR"
	// CodeModelNotAllowed 模型不在 Token 允许清单。由 ModelPermission 实现构造，
	// relay 仅透传；此处保留常量供装配/断言方统一命名（参考 billing.CodeModelNotAllowed）。
	CodeModelNotAllowed = "MODEL_NOT_ALLOWED"
)

// ---- 编排 DTO ----

// RelayRequest 是 /v1/* 入口的统一请求 DTO。后续 Gin handler（薄封装）从 *gin.Context
// 解析后构造它并交给 Gateway.Handle 编排；UpstreamPool.Forward 据此转发上游。
type RelayRequest struct {
	// RawToken 客户端 Authorization 原始令牌（可含 "Bearer " 前缀，由 Authenticator 归一）。
	RawToken string
	// Model 目标模型名（模型权限校验 + 风控 + 计费）。
	Model string
	// Endpoint 兼容入口路径，如 /v1/chat/completions（多入口共用本编排，预留）。
	Endpoint string
	// Body 透传上游的原始请求体（JSON）；编排逻辑不解析。
	Body []byte
	// Stream 是否流式（透传上游；编排逻辑不区分）。
	Stream bool
	// RequestID 调用链/幂等键；落计费日志与风控审计。
	RequestID string
	// ClientIP 客户端 IP（风控 IP allowlist / 指纹）。
	ClientIP string
}

// CallContext 是风控 RiskEngine.CheckCall 的入参（detailed-design §2.13）：
// 携带本次调用的模型/入口/来源供状态、RPM、IP allowlist、并发校验。
type CallContext struct {
	Model     string
	Endpoint  string
	ClientIP  string
	RequestID string
}

// Usage 是上游返回的 token 用量（计费基数）。
type Usage struct {
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
}

// RelayResponse 是上游转发结果，原样回送客户端。
type RelayResponse struct {
	StatusCode int
	Body       []byte
	Headers    map[string]string
}

// ChargeRequest 是交给 Billing 的一次调用计费输入（relay 侧消费者视图，仅含 Billing.Charge
// 所需的最小集；真实 Billing 内部再做桶路由/倍率/日志/分润，relay 不感知，§3.1）。
type ChargeRequest struct {
	RequestID        string
	UserID           int64
	TenantID         int64
	Model            string
	PromptTokens     int64
	CompletionTokens int64
}

// ChargeResult 是一次扣费成功的结果摘要（relay 仅透传，可选回写计费响应头）。
type ChargeResult struct {
	// BucketKind 实际扣费桶来源（"wallet" / "subscription"）；string 化以免耦合 platform/quota。
	BucketKind string
	// ChargedUSD 实际扣减额；RemainingUSD 扣后桶剩余（钱包=余额；套餐=月限额剩余）。
	ChargedUSD   float64
	RemainingUSD float64
}

// ---- 消费者定义接口（本模块声明其依赖，运行时由 cmd/main 注入兄弟模块实现）----
// 依据 detailed-design §1.4：relay 只 import 自己声明的接口，**不 import 兄弟业务模块**，
// 编译期零直接耦合，便于全 mock 单测（§2.11）。

// Authenticator 鉴权：原始令牌 → 请求级身份 Principal。由 Identity 模块实现。
type Authenticator interface {
	AuthenticateToken(ctx context.Context, raw string) (*appctx.Principal, error)
}

// AccessGuard 租户守卫：校验当前租户处于 active。由 Identity 模块实现。
type AccessGuard interface {
	RequireTenantActive(ctx context.Context, tenantID int64) error
}

// RiskEngine 调用前风控：状态 / RPM / IP allowlist / 并发。由 RiskControl 模块实现。
type RiskEngine interface {
	CheckCall(ctx context.Context, p *appctx.Principal, rc CallContext) error
}

// ModelPermission 模型权限：校验 Token model_allowlist。不允许返回 MODEL_NOT_ALLOWED。
type ModelPermission interface {
	Check(ctx context.Context, p *appctx.Principal, model string) error
}

// Billing 调用计费：算成本 → 选桶 → 原子扣减 → 写日志 → 触发分润。由 Billing 模块实现。
// 桶不足/超额/过期错误原样上浮，relay 不回退（独立计量，§8.1）。
type Billing interface {
	Charge(ctx context.Context, req ChargeRequest) (*ChargeResult, error)
}

// UpstreamPool 上游渠道池转发（复用 New API，§1.5）。由适配器实现。
type UpstreamPool interface {
	Forward(ctx context.Context, req RelayRequest) (RelayResponse, Usage, error)
}
