package agent

import "context"

// ---- 对外接口（detailed-design §2.3）----

// AgentService 管理代理类型/参数与代理钱包查询。
//
// 键统一为 tenantID（决策：代理=User+Tenant 1:1，agent_profiles 以 tenant_id 为主键）。
// owner 用户与租户的映射落在 tenants.owner_user_id，本模块只认 tenantID，天然跨租户隔离。
type AgentService interface {
	// SetAgentType 设代理成本价/折扣/分润/等级/can_api（经 PricingGuard 校验）。
	// 参数非法返回 AGENT_TYPE_INVALID；折扣击穿保护线时原样上浮守卫错误。
	SetAgentType(ctx context.Context, tenantID int64, p AgentParams) error
	// GetWallet 返回租户维度的代理钱包（API 额度 / 可提现 / 累计收益）。
	GetWallet(ctx context.Context, tenantID int64) (*AgentWallet, error)
	// AgentLevel 返回该租户代理档位（能力 gate 的唯一真相）；非代理租户返回 0（非错误）。
	AgentLevel(ctx context.Context, tenantID int64) (int, error)
}

// EarningSink 是收益入账下沉口，被 Billing/Wallet/TokenPlan 调用。
type EarningSink interface {
	// AddEarning 写收益日志并增可提现余额；按 (SourceType, SourceID) 幂等。
	// 来源含 recharge_spread|consume_commission|tokenplan_spread|tokenplan_commission|manual_adjustment。
	AddEarning(ctx context.Context, e EarningEntry) error
}

// WithdrawalService 是提现申请与审核（状态机 pending→approved/rejected）。
type WithdrawalService interface {
	// Request 提交提现：冻结可提现余额（→ frozen_withdraw_amount）；超额返回 WITHDRAW_INSUFFICIENT。
	Request(ctx context.Context, in WithdrawInput) (*Withdrawal, error)
	// Review 审核提现：approve=true 标 approved（线下打款）；approve=false 解冻退回；
	// 非 pending 再审返回 WITHDRAW_NOT_PENDING。
	Review(ctx context.Context, id int64, approve bool, remark string) error
}

// ---- 消费者定义接口（本模块声明其依赖，运行时由 cmd/main 注入实现）----
// 依据 detailed-design §1.4：模块只 import 自己声明的接口，不 import 兄弟模块。

// PricingGuard 是设代理折扣/倍率时所需的成本保护校验（消费者定义接口）。
// 签名对齐 pricing.PricingGuard.ValidateGroupRatio，真实实现由 pricing 包提供并在 main 注入；
// 本包仅声明所需的最小契约，不 import 兄弟模块。
type PricingGuard interface {
	// ValidateGroupRatio 校验折扣/倍率不低于主站保护下限 floor；低于即击穿保护线。
	ValidateGroupRatio(ratio, floor float64) error
}

// AgentRepo 是 Agent 模块的持久化依赖（消费者定义接口）。
// 本轮提供并发安全的内存假实现 MemRepo；GORM 真实实现顺延（见报告 TODO）。
//
// 实现约定：AppendEarning / CreateWithdrawal / ResolveWithdrawal 必须**原子**完成
// 读-改-写（detailed-design §6.2 条件 UPDATE / 行锁），以保证并发下的幂等与金额守恒。
type AgentRepo interface {
	// SetAgentType 持久化代理资料（按 tenantID 主键 upsert）。
	SetAgentType(ctx context.Context, tenantID int64, p AgentParams) error
	// GetAgentType 读取代理资料；found=false 表示该租户尚未设代理。
	GetAgentType(ctx context.Context, tenantID int64) (p AgentParams, found bool, err error)
	// GetWallet 返回租户钱包（不存在则返回该租户的零值钱包，不报错）。
	GetWallet(ctx context.Context, tenantID int64) (*AgentWallet, error)
	// AppendEarning 幂等入账：同 (SourceType, SourceID) 已存在则 applied=false 且不重复增余额。
	AppendEarning(ctx context.Context, e EarningEntry) (applied bool, err error)
	// CreateWithdrawal 原子冻结可提现余额并建 pending 提现单；金额非正或超额返回 ErrWithdrawInsufficient。
	CreateWithdrawal(ctx context.Context, w *Withdrawal) error
	// GetWithdrawal 按 id 读取提现单；不存在返回 ErrWithdrawNotFound。
	GetWithdrawal(ctx context.Context, id int64) (*Withdrawal, error)
	// ResolveWithdrawal 原子迁移提现单状态并移动资金：approved=扣冻结（线下打款），rejected=解冻退回；
	// 非 pending 返回 ErrWithdrawNotPending，不存在返回 ErrWithdrawNotFound。
	ResolveWithdrawal(ctx context.Context, id int64, target WithdrawStatus, remark string) error
}
