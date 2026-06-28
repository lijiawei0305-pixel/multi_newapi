package wallet

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/internal/platform/appctx"
	"github.com/QuantumNous/new-api/internal/platform/quota"
)

// --- 对外接口（detailed-design §2.6 的 Go 签名）---

// WalletService 提供充值/人工入账与兑换码兑换。
type WalletService interface {
	// Credit 充值/人工入账，绑 tenant_id+user_id；recharge 来源触发 recharge_spread 差价收益。
	Credit(ctx context.Context, in CreditInput) error
	// Redeem 兑换码入账（状态机 enabled->used）。tenant 维度自 ctx Principal 解析。
	Redeem(ctx context.Context, userID int64, code string) error
}

// WalletQuotaFactory 为某 Principal 产出一个钱包额度桶（quota.Source），供 QuotaRouter 选桶。
// 见 detailed-design §2.5/§2.6：Router 依赖的具体桶由本模块实现、main 注入，Billing 不 import 本包。
type WalletQuotaFactory interface {
	For(p appctx.Principal) quota.Source
}

// --- 消费者定义的依赖接口（本包声明，main 装配具体实现；见 detailed-design §1.4）---

// PricingService 是 Wallet 计算充值差价所需的「成本基准/倍率」来源。
// 仅取 GroupRatio 子集；真实实现由 pricing 模块提供（结构上兼容其 PricingService）。
type PricingService interface {
	// GroupRatio 返回租户下指定分组的有效倍率（无专属则回退默认）。
	GroupRatio(ctx context.Context, tenantID, groupID int64) (float64, error)
}

// EarningSink 接收代理收益记录（充值差价等）。由 Agent 模块实现并在 main 注入。
type EarningSink interface {
	AddEarning(ctx context.Context, e EarningEntry) error
}

// WalletRepo 是钱包持久化抽象（余额 + 兑换码）。本轮提供并发安全内存假实现（MemRepo）；
// 真实 GORM 实现（条件 UPDATE / 行锁、scopeByTenant、迁移）顺延（见报告 TODO）。
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
