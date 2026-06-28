package wallet

import (
	"context"

	"newapi-mt/internal/platform/appctx"
	"newapi-mt/internal/platform/quota"
)

// quotaFactory 是 WalletQuotaFactory 的实现，为每个 Principal 绑定一个钱包桶。
type quotaFactory struct {
	repo WalletRepo
}

// 编译期断言。
var (
	_ WalletQuotaFactory = (*quotaFactory)(nil)
	_ quota.Source       = (*walletQuota)(nil)
)

// NewQuotaFactory 构造 WalletQuotaFactory，供 QuotaRouter 在无 active 套餐时选钱包桶。
func NewQuotaFactory(repo WalletRepo) WalletQuotaFactory {
	return &quotaFactory{repo: repo}
}

// For 返回绑定到 (tenant,user) 的钱包额度桶。
func (f *quotaFactory) For(p appctx.Principal) quota.Source {
	return &walletQuota{repo: f.repo, tenantID: p.TenantID, userID: p.UserID}
}

// walletQuota 是 quota.Source 的钱包实现：余额即额度，扣减走条件 UPDATE（§6.2）。
type walletQuota struct {
	repo     WalletRepo
	tenantID int64
	userID   int64
}

// Charge 条件扣减：balance-cost>=0 才扣，否则 QUOTA_INSUFFICIENT。原子性由 Repo 保证。
// 返回 quota.Receipt{Kind: wallet, ChargedUSD, RemainingUSD=新余额}。
func (w *walletQuota) Charge(ctx context.Context, costUSD float64) (quota.Receipt, error) {
	if !validAmount(costUSD) || costUSD < 0 {
		return quota.Receipt{}, ErrAmountInvalid
	}
	remaining, err := w.repo.ChargeBalance(ctx, w.tenantID, w.userID, costUSD)
	if err != nil {
		return quota.Receipt{}, err // ErrQuotaInsufficient
	}
	return quota.Receipt{
		Kind:         quota.KindWallet,
		ChargedUSD:   costUSD,
		RemainingUSD: remaining,
	}, nil
}

// Balance 返回当前钱包余额（USD）。
func (w *walletQuota) Balance(ctx context.Context) (float64, error) {
	return w.repo.Balance(ctx, w.tenantID, w.userID)
}
