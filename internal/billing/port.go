package billing

import (
	"context"

	"newapi-mt/internal/platform/quota"
)

// ---- 错误码（apperr.Code 命名空间：QUOTA / SUBSCRIPTION / MODEL）----
// 详见 doc/detailed-design.md §2.5。这些码由额度桶（wallet/tokenplan）在 Charge 时
// 构造并原样上浮；Billing 只声明常量供组装/断言复用，不在本包吞码或改写。
const (
	// CodeQuotaInsufficient 钱包桶余额不足。
	CodeQuotaInsufficient = "QUOTA_INSUFFICIENT"
	// CodeSubscriptionExhausted 套餐桶月度额度用尽（终态，需手动重购）。
	CodeSubscriptionExhausted = "SUBSCRIPTION_EXHAUSTED"
	// CodeSubscriptionExpired 套餐桶已过期（expire_at < now）。
	CodeSubscriptionExpired = "SUBSCRIPTION_EXPIRED"
	// CodeModelNotAllowed 模型不在允许清单/未定价。模型权限主判在 RelayGateway/Pricing；
	// 本包仅在 ModelCatalog 返回该类错误时原样上浮，保留常量以备装配方统一命名。
	CodeModelNotAllowed = "MODEL_NOT_ALLOWED"
)

// EarningSource 标识一条代理收益的来源（与 doc/detailed-design.md §2.3 收益枚举对齐）。
type EarningSource string

const (
	// SourceConsumeCommission 调用消耗分润：用户每次扣费后按代理分润比例入账。
	SourceConsumeCommission EarningSource = "consume_commission"
)

// ChargeRequest 是一次调用计费的输入（由 RelayGateway 在转发拿到 usage 后填充）。
//
// 计费口径（doc/proposal.md §7 / §8.1，doc/detailed-design.md §2.5）：
//
//	upstream_cost = PromptTokens*inPrice + CompletionTokens*outPrice   // ModelCatalog 官方价
//	charged       = upstream_cost * Multiplier                          // 计费额（扣减桶）
//
// 其中 Multiplier 为计费倍率：本期默认 x1（上游成本价直计，不叠分组倍率，见 §7 默认假设 #2）。
type ChargeRequest struct {
	// RequestID 调用链/幂等键；落计费日志与分润条目，便于审计与去重。可选。
	RequestID string
	// UserID/TenantID 当前请求身份（供 quota.Router 选桶与日志归属）。
	UserID   int64
	TenantID int64
	// Model 上游模型名（向 ModelCatalog 查官方价）。
	Model string
	// PromptTokens/CompletionTokens 上游用量。
	PromptTokens     int64
	CompletionTokens int64
	// Multiplier 计费倍率；<=0 视为默认 1.0（x1）。真实分组倍率由 Pricing/Relay 决定后传入。
	Multiplier float64
	// GroupKey 可选：分组键，落 tenant_billing_logs.group_key 供统计。
	GroupKey string
}

// BillingResult 是一次扣费成功后的结果摘要。
type BillingResult struct {
	// BucketKind 实际扣费的桶来源（wallet / subscription）。
	BucketKind quota.Kind
	// UpstreamCostUSD 上游成本（官方价 × 用量）。
	UpstreamCostUSD float64
	// ChargedUSD 实际扣减额（取桶回执，权威）。
	ChargedUSD float64
	// GrossProfitUSD 毛利 = ChargedUSD - UpstreamCostUSD。
	GrossProfitUSD float64
	// RemainingUSD 扣费后桶剩余（钱包=余额；套餐=月限额剩余）。
	RemainingUSD float64
}

// CallLogEntry 是一条计费日志（落 tenant_billing_logs）。
type CallLogEntry struct {
	RequestID        string
	TenantID         int64
	UserID           int64
	Model            string
	PromptTokens     int64
	CompletionTokens int64
	BucketKind       quota.Kind
	UpstreamCostUSD  float64
	ChargedUSD       float64
	GrossProfitUSD   float64
	GroupKey         string
}

// EarningEntry 是一条代理收益记录（本包仅产出 consume_commission）。
//
// AmountUSD 为本次计费消耗额（charged_quota）作为分润基数；Billing 不持有分润比例，
// 由 Agent 侧按该用户/代理的比例换算为实际入账收益，保持模块解耦（§2.3）。
type EarningEntry struct {
	TenantID  int64
	UserID    int64
	Source    EarningSource
	AmountUSD float64
	Model     string
	// RefKey 关联计费日志的幂等/审计引用键（取 ChargeRequest.RequestID）。
	RefKey string
}

// ---- 对外接口（doc/detailed-design.md §2.5）----

// BillingService 是计费中枢：算上游成本 → 路由桶 → 原子扣减 → 写计费日志 → 触发消耗分润。
type BillingService interface {
	// Charge 执行一次调用计费。桶不足/超额/过期错误原样上浮，且不写日志/分润（独立计量不回退）。
	Charge(ctx context.Context, req ChargeRequest) (*BillingResult, error)
}

// ---- 消费者定义接口（本模块声明其依赖，运行时由 cmd/main 注入实现）----
// 依据 doc/detailed-design.md §1.4：模块只 import 自己声明的接口，不 import 兄弟模块。
// 钱包桶 / 套餐桶（quota.Source 实现）分别由 Wallet / TokenPlan 提供并注入。

// ModelCatalog 提供模型官方价（只读复用 New API 模型与价，见 §1.5）。
type ModelCatalog interface {
	// Price 返回某模型的输入/输出单价（USD，与用量口径一致）；未定价/不可用返回 error。
	Price(ctx context.Context, model string) (inPrice, outPrice float64, err error)
}

// EarningSink 接收代理收益（由 Agent 模块实现）。
type EarningSink interface {
	// AddEarning 追加一条收益（幂等以 RefKey 去重，见 §2.3）。
	AddEarning(ctx context.Context, e EarningEntry) error
}

// CallLogWriter 落计费日志（由日志/计费仓储实现，日志条目增 tenant_id，见 §1.5）。
type CallLogWriter interface {
	// Write 写入一条计费日志。
	Write(ctx context.Context, entry CallLogEntry) error
}

// SubscriptionChecker 查询用户是否存在 active 套餐（由 TokenPlan 模块实现），供 QuotaRouter 选桶。
type SubscriptionChecker interface {
	// HasActive 用户存在 active（未耗尽且未过期）套餐时返回 true。
	HasActive(ctx context.Context, userID int64) (bool, error)
}

// WalletSourceFactory 为当前请求产出钱包额度桶（quota.Source 由 Wallet 模块实现）。
type WalletSourceFactory interface {
	WalletSource(ctx context.Context, userID, tenantID int64) (quota.Source, error)
}

// SubscriptionSourceFactory 为当前请求产出套餐额度桶（quota.Source 由 TokenPlan 模块实现）。
type SubscriptionSourceFactory interface {
	SubscriptionSource(ctx context.Context, userID, tenantID int64) (quota.Source, error)
}
