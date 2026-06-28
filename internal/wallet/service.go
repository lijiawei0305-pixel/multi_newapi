package wallet

import (
	"context"
	"time"

	"newapi-mt/internal/platform/appctx"
)

// walletService 是 WalletService 的实现。依赖以接口注入，便于单测（mock Pricing/Earning/Repo）。
type walletService struct {
	repo     WalletRepo
	pricing  PricingService
	earnings EarningSink
	now      func() time.Time
}

// 编译期断言：walletService 实现 WalletService。
var _ WalletService = (*walletService)(nil)

// NewService 组装 WalletService。
func NewService(repo WalletRepo, pricing PricingService, earnings EarningSink) WalletService {
	return &walletService{
		repo:     repo,
		pricing:  pricing,
		earnings: earnings,
		now:      time.Now,
	}
}

// Credit 入账流程（detailed-design §2.6 / §3.3）：
//
//	校验参数（必带 tenant_id+user_id、金额合法）
//	  → 增用户余额（CreditedUSD）
//	  → recharge 来源：算充值差价 = 实付 − 代理成本 → AddEarning(recharge_spread)
//
// 真实实现需把「入账 + 差价」放进同一事务（platform/txn.WithTx），本轮内存假实现按序执行（见报告 TODO）。
func (s *walletService) Credit(ctx context.Context, in CreditInput) error {
	if in.TenantID <= 0 || in.UserID <= 0 {
		return ErrRechargeOrderInvalid // 入账必带 tenant_id+user_id
	}
	if !validAmount(in.CreditedUSD) || in.CreditedUSD < 0 ||
		!validAmount(in.ActualPaid) || in.ActualPaid < 0 {
		return ErrAmountInvalid
	}

	if err := s.repo.AddBalance(ctx, in.TenantID, in.UserID, in.CreditedUSD); err != nil {
		return err
	}

	// 仅付费充值计差价；人工入账（manual）不产生代理收益。
	if in.Source != SourceRecharge {
		return nil
	}
	ratio, err := s.pricing.GroupRatio(ctx, in.TenantID, in.GroupID)
	if err != nil {
		return err
	}
	spread := rechargeSpread(in.ActualPaid, ratio)
	if spread <= 0 {
		return nil // 普通组/无溢价 → 无差价收益
	}
	return s.earnings.AddEarning(ctx, EarningEntry{
		TenantID:   in.TenantID,
		UserID:     in.UserID,
		SourceType: EarningRechargeSpread,
		AmountUSD:  spread,
		Reference:  in.Reference,
	})
}

// Redeem 兑换码兑换（detailed-design §2.6）：
//
//	解析租户（ctx Principal）→ 查码 → 状态机校验 → 原子 CAS(enabled->used) → 入账面额到余额
//
// 错误：不存在/禁用/过期 → REDEEM_CODE_INVALID；已用（含并发败者）→ REDEEM_CODE_USED。
func (s *walletService) Redeem(ctx context.Context, userID int64, code string) error {
	tenantID := appctx.TenantID(ctx) // 0 表示无租户上下文 → 后续查码自然 invalid
	rc, err := s.repo.GetRedemption(ctx, tenantID, code)
	if err != nil {
		return err // ErrRedeemCodeInvalid
	}
	now := s.now()
	if err := rc.redeemableError(now); err != nil {
		return err // used / invalid（含过期、禁用）
	}
	ok, err := s.repo.UseRedemption(ctx, rc.ID, userID, now)
	if err != nil {
		return err
	}
	if !ok {
		return ErrRedeemCodeUsed // 并发竞态败者
	}
	return s.repo.AddBalance(ctx, tenantID, userID, rc.AmountUSD)
}
