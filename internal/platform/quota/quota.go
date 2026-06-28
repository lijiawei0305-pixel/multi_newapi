// Package quota 定义统一的「额度桶」契约：钱包桶（wallet）与套餐桶（tokenplan）
// 都实现 Source；RelayGateway/Billing 通过 Router 选桶。放在 platform 是为了让
// billing/wallet/tokenplan 共享同一份接口与值类型，避免各自定义导致组装期不兼容。
// 详见 doc/detailed-design.md §2.5。实现需保证 Charge 的并发原子性（§6.2 条件 UPDATE）。
package quota

import "context"

// Kind 标识桶来源。
type Kind string

const (
	KindWallet       Kind = "wallet"
	KindSubscription Kind = "subscription"
)

// Receipt 是一次扣费的结果。
type Receipt struct {
	Kind         Kind
	ChargedUSD   float64
	RemainingUSD float64 // 钱包=余额；套餐=月限额剩余
}

// Source 是统一额度桶抽象。Charge 必须原子；不足/超额/过期返回 apperr
// （QUOTA_INSUFFICIENT / SUBSCRIPTION_EXHAUSTED / SUBSCRIPTION_EXPIRED）。
type Source interface {
	Charge(ctx context.Context, costUSD float64) (Receipt, error)
	Balance(ctx context.Context) (float64, error)
}

// Router 为当前请求选择额度桶：有 active 套餐 → 套餐桶，否则钱包桶（独立计量不回退）。
type Router interface {
	Select(ctx context.Context, userID, tenantID int64) (Source, error)
}
