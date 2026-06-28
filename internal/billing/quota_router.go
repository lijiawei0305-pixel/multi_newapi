package billing

import (
	"context"

	"newapi-mt/internal/platform/quota"
)

// quotaRouter 是 quota.Router 的实现：双桶路由（钱包桶 / 套餐桶）。
//
// 选桶规则（doc/detailed-design.md §2.5 / doc/proposal.md §8.1）——独立计量不回退：
//   - 用户存在 active 套餐 → 套餐桶；
//   - 否则 → 钱包桶。
//
// 决策只发生一次：套餐桶后续若返回超额/过期，由 Billing 原样上浮、不在此回退到钱包。
type quotaRouter struct {
	subs   SubscriptionChecker
	wallet WalletSourceFactory
	sub    SubscriptionSourceFactory
}

// 编译期确认 quotaRouter 满足共享契约 quota.Router。
var _ quota.Router = (*quotaRouter)(nil)

// NewQuotaRouter 用套餐判定器与两个桶工厂构造 QuotaRouter。
// wallet/sub 的具体 quota.Source 实现由 Wallet/TokenPlan 模块在 cmd/main 注入。
func NewQuotaRouter(subs SubscriptionChecker, wallet WalletSourceFactory, sub SubscriptionSourceFactory) quota.Router {
	return &quotaRouter{subs: subs, wallet: wallet, sub: sub}
}

// Select 为当前请求选择额度桶。
func (r *quotaRouter) Select(ctx context.Context, userID, tenantID int64) (quota.Source, error) {
	active, err := r.subs.HasActive(ctx, userID)
	if err != nil {
		return nil, err // 基础设施错误，原样上浮（不吞码）
	}
	if active {
		return r.sub.SubscriptionSource(ctx, userID, tenantID)
	}
	return r.wallet.WalletSource(ctx, userID, tenantID)
}
