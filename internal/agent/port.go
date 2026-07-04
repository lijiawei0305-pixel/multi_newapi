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
	// SetPayoutAccount 设置/修改代理收款账户（提现闭环补强 #1；经 PayoutAccount.Validate 校验，
	// 非法返回 PAYOUT_ACCOUNT_INVALID）。
	SetPayoutAccount(ctx context.Context, tenantID int64, p PayoutAccount) error
	// GetPayoutAccount 返回代理当前收款账户；found=false 表示尚未设置。
	GetPayoutAccount(ctx context.Context, tenantID int64) (PayoutAccount, bool, error)
}

// EarningSink 是收益入账下沉口，被 Billing/Wallet/TokenPlan 调用。
type EarningSink interface {
	// AddEarning 写收益日志并增可提现余额；按 (SourceType, SourceID) 幂等。
	// 来源含 recharge_spread|consume_commission|tokenplan_spread|tokenplan_commission|manual_adjustment。
	AddEarning(ctx context.Context, e EarningEntry) error
}

// WithdrawalService 是提现申请与审核（状态机 pending→approved→paid | pending→rejected）。
type WithdrawalService interface {
	// Request 提交提现：先校验代理已设置收款账户（未设返回 PAYOUT_ACCOUNT_REQUIRED，不冻结），
	// 再把当前收款账户快照进提现单，最后冻结可提现余额（→ frozen_withdraw_amount）；
	// 金额超额返回 WITHDRAW_INSUFFICIENT。
	Request(ctx context.Context, in WithdrawInput) (*Withdrawal, error)
	// Review 审核提现：approve=true 标 approved（**不动钱**，钱仍在 frozen，等 MarkPaid 才出账）；
	// approve=false 标 rejected（解冻退回）；非 pending 再审返回 WITHDRAW_NOT_PENDING。
	Review(ctx context.Context, id int64, approve bool, remark string) error
	// MarkPaid 标记已打款：approved→paid，扣减冻结资金（线下打款真正出账），记 payout_ref/paid_at。
	// payoutRef 必填（空白返回 PAYOUT_REF_REQUIRED）；非 approved（含并发已被标记）返回 WITHDRAW_NOT_APPROVED；
	// 不存在返回 WITHDRAW_NOT_FOUND。
	MarkPaid(ctx context.Context, id int64, payoutRef string) error
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
	// CreateWithdrawal 原子冻结可提现余额并建 pending 提现单（含调用方已填好的收款快照字段）；
	// 金额非正或超额返回 ErrWithdrawInsufficient。
	CreateWithdrawal(ctx context.Context, w *Withdrawal) error
	// GetWithdrawal 按 id 读取提现单；不存在返回 ErrWithdrawNotFound。
	GetWithdrawal(ctx context.Context, id int64) (*Withdrawal, error)
	// ResolveWithdrawal 原子迁移 pending→{approved,rejected} 并按新钱流规则移动资金：
	// approved **不动钱**（钱仍在 frozen，等 MarkWithdrawalPaid 才出账）；rejected 解冻退回可提现。
	// 非 pending 返回 ErrWithdrawNotPending，不存在返回 ErrWithdrawNotFound。
	ResolveWithdrawal(ctx context.Context, id int64, target WithdrawStatus, remark string) error
	// MarkWithdrawalPaid 原子迁移 approved→paid（CAS，WHERE status='approved'）并扣减冻结资金
	// （线下打款真正出账），记 payout_ref/paid_at。非 approved（含并发已被标记）返回
	// ErrWithdrawNotApproved；不存在返回 ErrWithdrawNotFound。
	MarkWithdrawalPaid(ctx context.Context, id int64, payoutRef string) error
	// GetPayoutAccount 读取代理收款账户；found=false 表示尚未设置。
	GetPayoutAccount(ctx context.Context, tenantID int64) (PayoutAccount, bool, error)
	// SetPayoutAccount 写入/更新代理收款账户（按 tenantID upsert；仅影响 payout_* 列，
	// 不触碰同一 profile 行上的 AgentParams 字段）。
	SetPayoutAccount(ctx context.Context, tenantID int64, p PayoutAccount) error
}
