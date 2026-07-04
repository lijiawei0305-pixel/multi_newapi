package agent

import (
	"context"
	"strings"
)

// withdrawalService 是 WithdrawalService 的实现，依赖 AgentRepo 的原子冻结/迁移。
type withdrawalService struct {
	repo AgentRepo
}

// NewWithdrawalService 构造 WithdrawalService。
func NewWithdrawalService(repo AgentRepo) WithdrawalService {
	return &withdrawalService{repo: repo}
}

// Request 提交提现申请：① 校验金额为正；② 校验代理已设置收款账户（未设 → PAYOUT_ACCOUNT_REQUIRED，
// 不冻结分毫，提现闭环补强 #1）；③ 把当前收款账户整份快照进提现单（记录不可变）；
// ④ 冻结可提现余额（→ frozen_withdraw_amount）。金额非正或超过可提现余额返回 WITHDRAW_INSUFFICIENT。
func (s *withdrawalService) Request(ctx context.Context, in WithdrawInput) (*Withdrawal, error) {
	if in.Amount <= 0 {
		return nil, ErrWithdrawInsufficient
	}
	payout, found, err := s.repo.GetPayoutAccount(ctx, in.TenantID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrPayoutAccountRequired
	}
	w := &Withdrawal{
		TenantID:      in.TenantID,
		UserID:        in.UserID,
		Amount:        in.Amount,
		Status:        WithdrawPending,
		Remark:        in.Remark,
		PayoutMethod:  payout.Method,
		PayoutAccount: payout.Account,
		PayoutName:    payout.Name,
		PayoutBank:    payout.Bank,
	}
	if err := s.repo.CreateWithdrawal(ctx, w); err != nil {
		return nil, err // 含 ErrWithdrawInsufficient
	}
	return w, nil
}

// Review 审核提现：approve=true → approved（**不动钱**，仅记决策——钱仍在 frozen，
// 等 MarkPaid 才真正出账，提现闭环补强 #2 钱流调整）；approve=false → rejected（解冻退回可提现余额）。
// 非 pending 再审返回 WITHDRAW_NOT_PENDING；提现单不存在返回 WITHDRAW_NOT_FOUND。
func (s *withdrawalService) Review(ctx context.Context, id int64, approve bool, remark string) error {
	target := WithdrawRejected
	if approve {
		target = WithdrawApproved
	}
	return s.repo.ResolveWithdrawal(ctx, id, target, remark)
}

// MarkPaid 标记已打款：approved→paid，扣减冻结资金（线下打款真正出账）+ 记 payout_ref/paid_at
// （提现闭环补强 #2）。payoutRef 必填（空白返回 PAYOUT_REF_REQUIRED）；非 approved（含并发已被
// 标记）返回 WITHDRAW_NOT_APPROVED；不存在返回 WITHDRAW_NOT_FOUND。
func (s *withdrawalService) MarkPaid(ctx context.Context, id int64, payoutRef string) error {
	if strings.TrimSpace(payoutRef) == "" {
		return ErrPayoutRefRequired
	}
	return s.repo.MarkWithdrawalPaid(ctx, id, payoutRef)
}
