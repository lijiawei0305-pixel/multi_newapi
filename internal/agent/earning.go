package agent

import "context"

// earningSink 是 EarningSink 的实现，依赖 AgentRepo 完成幂等入账。
type earningSink struct {
	repo AgentRepo
}

// NewEarningSink 构造 EarningSink，供 Billing/Wallet/TokenPlan 经接口注入。
func NewEarningSink(repo AgentRepo) EarningSink {
	return &earningSink{repo: repo}
}

// AddEarning 校验条目合法性后委托 repo 幂等入账：
// 同 (SourceType, SourceID) 重复调用不重复增余额（幂等返回 nil）。
func (s *earningSink) AddEarning(ctx context.Context, e EarningEntry) error {
	if err := e.Validate(); err != nil {
		return err
	}
	_, err := s.repo.AppendEarning(ctx, e)
	return err
}
