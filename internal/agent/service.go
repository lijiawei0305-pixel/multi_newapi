package agent

import "context"

// agentService 是 AgentService 的实现，依赖 AgentRepo 与（可选）PricingGuard。
type agentService struct {
	repo  AgentRepo
	guard PricingGuard
}

// NewService 构造 AgentService。
// guard 可为 nil：跳过成本保护线校验（便于早期装配 / 不需要保护线的场景）。
func NewService(repo AgentRepo, guard PricingGuard) AgentService {
	return &agentService{repo: repo, guard: guard}
}

// SetAgentType 校验代理参数后落库；参数非法返回 AGENT_TYPE_INVALID。
// 折扣若击穿主站保护线，则原样上浮 PricingGuard 的错误码（如 RATIO_BELOW_FLOOR）。
func (s *agentService) SetAgentType(ctx context.Context, tenantID int64, p AgentParams) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if s.guard != nil {
		if err := s.guard.ValidateGroupRatio(p.PackageDiscount, p.DiscountFloor); err != nil {
			return err // 跨模块错误原样上浮（detailed-design §6.4）
		}
	}
	return s.repo.SetAgentType(ctx, tenantID, p)
}

// GetWallet 返回租户维度的代理钱包。键为 tenantID，天然跨租户隔离。
func (s *agentService) GetWallet(ctx context.Context, tenantID int64) (*AgentWallet, error) {
	return s.repo.GetWallet(ctx, tenantID)
}

// AgentLevel 返回租户代理档位（0=普通 / 1=独立）；未设代理的租户返回 0（非错误），供能力 gate 使用。
func (s *agentService) AgentLevel(ctx context.Context, tenantID int64) (int, error) {
	p, found, err := s.repo.GetAgentType(ctx, tenantID)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, nil
	}
	return p.Level, nil
}
