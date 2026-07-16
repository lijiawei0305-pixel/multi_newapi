package wallet

import (
	"context"
	"time"
)

// WalletRepo 是钱包持久化抽象（余额 + 兑换码）。内存假实现见 repo.go（MemRepo），
// 真实 GORM 实现（条件 UPDATE / 行锁、scopeByTenant、迁移）见 gormrepo。
//
// 说明：本包早期还设计过 WalletService / WalletQuotaFactory / PricingService / EarningSink
// 一套「独立钱包计费引擎」接口（service.go / quota_wallet.go），但生产 /v1 计费实际走原生
// 订阅/额度桶，该套接口与实现从未装配（0 装配点引用），已于 2026-07-16 作为死码整体移除。
// 本包如今只承载「兑换码模型 + 余额/兑换持久化契约」这一段仍被 mtwire 使用的活口径。
type WalletRepo interface {
	// Balance 返回用户当前 API 余额（USD）；账户不存在视为 0。
	Balance(ctx context.Context, tenantID, userID int64) (float64, error)
	// AddBalance 入账：余额 += deltaUSD（账户不存在则创建）。
	AddBalance(ctx context.Context, tenantID, userID int64, deltaUSD float64) error
	// ChargeBalance 条件原子扣减：仅当 balance-cost>=0 才扣减并返回新余额；
	// 否则返回 ErrQuotaInsufficient 且余额不变（模拟 §6.2 条件 UPDATE）。
	ChargeBalance(ctx context.Context, tenantID, userID int64, costUSD float64) (remainingUSD float64, err error)
	// GetRedemption 按租户+码查兑换码；不存在返回 ErrRedeemCodeInvalid。
	GetRedemption(ctx context.Context, tenantID int64, code string) (*RedemptionCode, error)
	// UseRedemption 原子 CAS：仅当状态仍为 enabled 才翻为 used 并记录使用者/时间。
	// ok=false 表示已被并发用掉（状态非 enabled），由调用方据此返回 REDEEM_CODE_USED。
	UseRedemption(ctx context.Context, id, userID int64, now time.Time) (ok bool, err error)
}
