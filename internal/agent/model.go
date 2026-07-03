package agent

import (
	"strconv"
	"time"
)

// ---- 代理类型与参数（detailed-design §2.3 / proposal §5.2、§7）----

// AgentParams 是设代理时的参数（detailed-design §2.3：cost_price、package_discount、commission_ratio、level）。
type AgentParams struct {
	// CostPrice 代理进货成本价（¥），强校验 ≥ 0。
	CostPrice float64
	// PackageDiscount 套餐折扣倍率（如 0.9 = 9 折），∈[0,1]；
	// 并经 PricingGuard 校验不低于 DiscountFloor（击穿保护线被拒）。
	PackageDiscount float64
	// CommissionRatio 消耗分润比例，∈[0,1]。
	CommissionRatio float64
	// Level 代理等级，≥ 0。
	Level int
	// CanAPI 开放 API 能力占位（本期恒不 gate 任何东西，正交于 level；proposal「开放API」独立后续项目）。
	CanAPI bool
	// DiscountFloor 主站折扣/倍率保护下限：设代理时经 PricingGuard 校验 PackageDiscount ≥ DiscountFloor。
	// 本轮新增字段（design 的 params 仅列 4 项），见报告默认假设。
	DiscountFloor float64
}

// Validate 校验参数合法性；任一非法返回 ErrAgentTypeInvalid（AGENT_TYPE_INVALID）。
// 规则：成本价 ≥ 0、分润比例 ∈[0,1]、折扣倍率 ∈[0,1]、等级 ≥ 0。
func (p AgentParams) Validate() error {
	switch {
	case p.CostPrice < 0:
		return ErrAgentTypeInvalid
	case p.CommissionRatio < 0 || p.CommissionRatio > 1:
		return ErrAgentTypeInvalid
	case p.PackageDiscount < 0 || p.PackageDiscount > 1:
		return ErrAgentTypeInvalid
	case p.Level < 0:
		return ErrAgentTypeInvalid
	default:
		return nil
	}
}

// ---- 收益来源与条目（detailed-design §2.3 / proposal §7、§8.3）----

// EarningSource 是收益来源类型枚举。
type EarningSource string

const (
	// SourceRechargeSpread 充值差价（用户实付 − 代理成本）。
	SourceRechargeSpread EarningSource = "recharge_spread"
	// SourceConsumeCommission 消耗分润（用户调用消耗 × 分润比例）。
	SourceConsumeCommission EarningSource = "consume_commission"
	// SourceTokenplanSpread 套餐差价（零售价 − 代理成本价）。
	SourceTokenplanSpread EarningSource = "tokenplan_spread"
	// SourceTokenplanCommission 套餐内消耗分润。
	SourceTokenplanCommission EarningSource = "tokenplan_commission"
	// SourceManualAdjustment 人工调整（管理员修正，金额可正可负）。
	SourceManualAdjustment EarningSource = "manual_adjustment"
)

// Valid 判断是否为已知的合法收益来源。
func (s EarningSource) Valid() bool {
	switch s {
	case SourceRechargeSpread, SourceConsumeCommission,
		SourceTokenplanSpread, SourceTokenplanCommission, SourceManualAdjustment:
		return true
	default:
		return false
	}
}

// EarningEntry 是一条收益入账请求（被 Billing/Wallet/TokenPlan 经 EarningSink 调用）。
// 幂等键为 (SourceType, SourceID)：同键重复调用只入账一次。
type EarningEntry struct {
	// TenantID 收益归属租户（>0），同时是钱包键，天然跨租户隔离。
	TenantID int64
	// UserID 收益归属代理 owner 用户。
	UserID int64
	// SourceType 收益来源类型。
	SourceType EarningSource
	// SourceID 业务来源唯一标识（幂等键，不可为空）。
	SourceID string
	// Amount 收益金额（¥），增 withdrawable_balance 与 total_earned；manual_adjustment 可为负。
	Amount float64
	// Remark 备注。
	Remark string
	// CreatedAt 入账时间（零值由 repo 填充）。
	CreatedAt time.Time
}

// Validate 校验收益条目；非法返回 ErrEarningInvalid（EARNING_INVALID）。
func (e EarningEntry) Validate() error {
	if !e.SourceType.Valid() || e.SourceID == "" || e.TenantID <= 0 {
		return ErrEarningInvalid
	}
	return nil
}

// IdempotencyKey 返回 (TenantID, SourceType, SourceID) 复合幂等去重键。
// 含 TenantID 以保证跨租户隔离：不同租户即使复用同一 SourceID 也各自独立入账。
func (e EarningEntry) IdempotencyKey() string {
	return strconv.FormatInt(e.TenantID, 10) + "\x00" + string(e.SourceType) + "\x00" + e.SourceID
}

// ---- 代理钱包（detailed-design §2.3 / proposal §7：两类余额）----

// AgentWallet 是代理钱包：API 额度（调用扣减）+ 可提现余额（人民币收益）。
type AgentWallet struct {
	// TenantID 钱包归属租户（GetWallet 的键）。
	TenantID int64
	// UserID 代理 owner 用户。
	UserID int64
	// APIBalance 当前余额 / API 额度（调用扣减）。本模块只读展示，由 Wallet/Billing 维护。
	APIBalance float64
	// WithdrawableBalance 可提现余额（¥，代理人民币收益）。
	WithdrawableBalance float64
	// FrozenWithdrawAmount 提现冻结中金额（¥）。
	FrozenWithdrawAmount float64
	// TotalEarned 累计收益（¥，Σ 收益条目金额）。
	TotalEarned float64
	// UpdatedAt 最近更新时间。
	UpdatedAt time.Time
}

// ---- 提现状态机（detailed-design §2.3）----

// WithdrawStatus 是提现单状态。
type WithdrawStatus string

const (
	// WithdrawPending 已提交，可提现余额冻结中。
	WithdrawPending WithdrawStatus = "pending"
	// WithdrawApproved 审核通过 → 线下打款（终态）。
	WithdrawApproved WithdrawStatus = "approved"
	// WithdrawRejected 审核拒绝 → 解冻退回（终态）。
	WithdrawRejected WithdrawStatus = "rejected"
)

// Valid 判断是否为已知的合法提现状态。
func (s WithdrawStatus) Valid() bool {
	switch s {
	case WithdrawPending, WithdrawApproved, WithdrawRejected:
		return true
	default:
		return false
	}
}

// allowedWithdrawTransitions 编码 detailed-design §2.3 的状态机：
//
//	pending -> approved | rejected
//	approved / rejected 为终态（不可迁出）。
//
// 不在表内的迁移（含 same->same、任何 from 终态）均为非法 → WITHDRAW_NOT_PENDING。
var allowedWithdrawTransitions = map[WithdrawStatus]map[WithdrawStatus]bool{
	WithdrawPending:  {WithdrawApproved: true, WithdrawRejected: true},
	WithdrawApproved: {},
	WithdrawRejected: {},
}

// CanTransitionTo 报告从 s 迁移到 next 是否合法。
func (s WithdrawStatus) CanTransitionTo(next WithdrawStatus) bool {
	return allowedWithdrawTransitions[s][next]
}

// Withdrawal 是一笔提现单。
type Withdrawal struct {
	ID         int64
	TenantID   int64
	UserID     int64
	Amount     float64 // 提现金额（¥）
	Status     WithdrawStatus
	Remark     string    // 审核备注
	CreatedAt  time.Time // 提交时间
	UpdatedAt  time.Time // 最近更新时间
	ReviewedAt time.Time // 审核时间（approved/rejected 时填充）
}

// WithdrawInput 是提现申请入参。
type WithdrawInput struct {
	TenantID int64
	UserID   int64
	Amount   float64
	Remark   string
}
