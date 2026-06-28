package agent

import "context"

// withdrawalService 是 WithdrawalService 的实现，依赖 AgentRepo 的原子冻结/迁移。
type withdrawalService struct {
	repo AgentRepo
}

// NewWithdrawalService 构造 WithdrawalService。
func NewWithdrawalService(repo AgentRepo) WithdrawalService {
	return &withdrawalService{repo: repo}
}

// Request 提交提现申请：冻结可提现余额（→ frozen_withdraw_amount）。
// 金额非正或超过可提现余额返回 WITHDRAW_INSUFFICIENT。
func (s *withdrawalService) Request(ctx context.Context, in WithdrawInput) (*Withdrawal, error) {
	if in.Amount <= 0 {
		return nil, ErrWithdrawInsufficient
	}
	w := &Withdrawal{
		TenantID: in.TenantID,
		UserID:   in.UserID,
		Amount:   in.Amount,
		Status:   WithdrawPending,
		Remark:   in.Remark,
	}
	if err := s.repo.CreateWithdrawal(ctx, w); err != nil {
		return nil, err // 含 ErrWithdrawInsufficient
	}
	return w, nil
}

// Review 审核提现：approve=true → approved（线下打款，扣减冻结）；
// approve=false → rejected（解冻退回可提现余额）。
// 非 pending 再审返回 WITHDRAW_NOT_PENDING；提现单不存在返回 WITHDRAW_NOT_FOUND。
func (s *withdrawalService) Review(ctx context.Context, id int64, approve bool, remark string) error {
	target := WithdrawRejected
	if approve {
		target = WithdrawApproved
	}
	return s.repo.ResolveWithdrawal(ctx, id, target, remark)
}
