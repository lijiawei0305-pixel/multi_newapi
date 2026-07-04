package agent

import (
	"strconv"
	"strings"
	"time"
)

// ---- 代理类型与参数（detailed-design §2.3 / proposal §5.2、§7）----

// AgentParams 是设代理时的参数（detailed-design §2.3：cost_price、package_discount、commission_ratio、level）。
type AgentParams struct {
	// UserID 代理 owner 用户 ID（>0）。建代理时写入 agent_profiles.user_id，供代理列表解析「所属用户」；
	// owner 建后不可改——SetAgentType 的 OnConflict 不把 user_id 列入更新集，故更新代理不动它。
	UserID int64
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
	// BottomPriceRatio 消耗计费底价倍率（spec agent-tiering §9.7；管理员按代理设，与上面 PackageDiscount/
	// DiscountFloor 完全独立——那两个服务 tokenplan 套餐折扣保护线，这个服务「模型消耗」计费的四档价格
	// 阶梯下限）。0 = 未配置（HandleAgentSetGroupRatio 的地板回退平台基准，见 internal/mtwire/distribution.go
	// consumeFloorRatio，Task 12）；>0 = 该代理卖价（tenant_groups 覆盖倍率）的下限，同时是差价入账公式
	// （Task 13 creditRatioMarkup）的减数——两处必须同一口径，否则记账错误（见 consumeFloorRatio 注释）。
	// 跨全部模型分组统一一个比例（不逐分组设——底价是「对该代理的批发折扣比例」，与逐模型定价的 ModelRatio
	// 相乘即天然逐模型生效，无需再逐分组重复配置）。
	BottomPriceRatio float64
	// DiscountRatio 全线批发折扣系数（doc/agent-wholesale-discount.md）：管理员按代理设一个系数（如 0.8），
	// >0 时全线生效——消耗侧代理成本 = 主站分组基准 × DiscountRatio（相对缩放，见 mtwire.consumeFloorRatio）；
	// 套餐侧代理进货价 = 套餐主站价 BasePrice × DiscountRatio（见 tokenplan.Purchase）。<=0 = 未设/无折扣，
	// 两侧均回退现状行为（消耗回退 BottomPriceRatio→平台基准；套餐回退 plan.AgentCostPrice）。
	// 按用户明确要求「不做任何成本保护」：不接 PricingGuard、无上下限，仅拒绝 <0 的非法值。
	DiscountRatio float64
}

// Validate 校验参数合法性；任一非法返回 ErrAgentParamsInvalid（AGENT_TYPE_INVALID）。
// 规则：成本价 ≥ 0、分润比例 ∈[0,1]、折扣倍率 ∈[0,1]、等级 ≥ 0。
func (p AgentParams) Validate() error {
	switch {
	case p.CostPrice < 0:
		return ErrAgentParamsInvalid
	case p.CommissionRatio < 0 || p.CommissionRatio > 1:
		return ErrAgentParamsInvalid
	case p.PackageDiscount < 0 || p.PackageDiscount > 1:
		return ErrAgentParamsInvalid
	case p.Level < 0:
		return ErrAgentParamsInvalid
	case p.BottomPriceRatio < 0:
		return ErrAgentParamsInvalid
	case p.DiscountRatio < 0:
		return ErrAgentParamsInvalid
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
	// SourceRatioMarkup 差价入账（L1/独立档：卖价高于底价的部分，按官方 token×ModelRatio 折算；
	// spec agent-tiering §9.4）。
	SourceRatioMarkup EarningSource = "ratio_markup"
	// SourceManualAdjustment 人工调整（管理员修正，金额可正可负）。
	SourceManualAdjustment EarningSource = "manual_adjustment"
)

// Valid 判断是否为已知的合法收益来源。
func (s EarningSource) Valid() bool {
	switch s {
	case SourceRechargeSpread, SourceConsumeCommission,
		SourceTokenplanSpread, SourceTokenplanCommission, SourceRatioMarkup, SourceManualAdjustment:
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

// ---- 收款账户（detailed-design 提现闭环补强 #1）----

// PayoutMethod 是代理收款方式枚举。
type PayoutMethod string

const (
	// PayoutAlipay 支付宝收款。
	PayoutAlipay PayoutMethod = "alipay"
	// PayoutBank 银行卡收款。
	PayoutBank PayoutMethod = "bank"
)

// Valid 判断是否为已知的合法收款方式。
func (m PayoutMethod) Valid() bool {
	switch m {
	case PayoutAlipay, PayoutBank:
		return true
	default:
		return false
	}
}

// PayoutAccount 是代理的收款账户（提现打款目标）。代理在「收款账户」设置里自助填写/修改；
// 申请提现时整份快照进 Withdrawal（记录不可变，见下方 Withdrawal.Payout* 字段）。
type PayoutAccount struct {
	// Method 收款方式：alipay | bank。
	Method PayoutMethod
	// Account 收款账号（支付宝账号 / 银行卡号）。
	Account string
	// Name 实名（收款人姓名）。
	Name string
	// Bank 开户行；仅 Method==PayoutBank 时必填，alipay 下应为空。
	Bank string
}

// Validate 校验收款账户合法性：方式∈{alipay,bank}、账号与姓名非空、bank 方式下开户行必填。
// 非法返回 ErrPayoutAccountInvalid（PAYOUT_ACCOUNT_INVALID）。
func (p PayoutAccount) Validate() error {
	switch {
	case !p.Method.Valid():
		return ErrPayoutAccountInvalid
	case strings.TrimSpace(p.Account) == "":
		return ErrPayoutAccountInvalid
	case strings.TrimSpace(p.Name) == "":
		return ErrPayoutAccountInvalid
	case p.Method == PayoutBank && strings.TrimSpace(p.Bank) == "":
		return ErrPayoutAccountInvalid
	default:
		return nil
	}
}

// IsZero 判断收款账户是否为「尚未设置」的零值（GetPayoutAccount 据此报告 found）。
func (p PayoutAccount) IsZero() bool {
	return p == PayoutAccount{}
}

// ---- 提现状态机（detailed-design §2.3；paid + 收款快照见提现闭环补强 #1/#2）----

// WithdrawStatus 是提现单状态。
type WithdrawStatus string

const (
	// WithdrawPending 已提交，可提现余额冻结中。
	WithdrawPending WithdrawStatus = "pending"
	// WithdrawApproved 审核通过，等待线下打款——**不再是终态**：approve 本身不动钱
	// （钱仍在 frozen），须再经 mark-paid（→ paid）才真正出账。
	WithdrawApproved WithdrawStatus = "approved"
	// WithdrawPaid 已打款（终态）：mark-paid 扣减冻结后的资金真正离开系统状态，
	// 连带记录 payout_ref（打款单号/凭证）与 paid_at。
	WithdrawPaid WithdrawStatus = "paid"
	// WithdrawRejected 审核拒绝 → 解冻退回（终态）。
	WithdrawRejected WithdrawStatus = "rejected"
)

// Valid 判断是否为已知的合法提现状态。
func (s WithdrawStatus) Valid() bool {
	switch s {
	case WithdrawPending, WithdrawApproved, WithdrawPaid, WithdrawRejected:
		return true
	default:
		return false
	}
}

// allowedWithdrawTransitions 编码提现闭环补强 #2 的状态机：
//
//	pending  -> approved | rejected
//	approved -> paid            （approved 不再是终态）
//	paid / rejected 为终态（不可迁出）。
//
// 不在表内的迁移（含 same->same、任何终态迁出）均为非法：
// pending/approved 分支迁移非法 → WITHDRAW_NOT_PENDING；approved->paid 分支非法 → WITHDRAW_NOT_APPROVED
// （由调用方 ResolveWithdrawal / MarkWithdrawalPaid 各自的 CAS 语境决定具体错误码）。
var allowedWithdrawTransitions = map[WithdrawStatus]map[WithdrawStatus]bool{
	WithdrawPending:  {WithdrawApproved: true, WithdrawRejected: true},
	WithdrawApproved: {WithdrawPaid: true},
	WithdrawPaid:     {},
	WithdrawRejected: {},
}

// CanTransitionTo 报告从 s 迁移到 next 是否合法。
func (s WithdrawStatus) CanTransitionTo(next WithdrawStatus) bool {
	return allowedWithdrawTransitions[s][next]
}

// Withdrawal 是一笔提现单。
type Withdrawal struct {
	ID       int64
	TenantID int64
	UserID   int64
	Amount   float64 // 提现金额（¥）
	Status   WithdrawStatus
	Remark   string // 审核备注（驳回理由等）

	// PayoutMethod/PayoutAccount/PayoutName/PayoutBank 是申请提现那一刻从代理收款账户
	// （PayoutAccount）整份快照下来的打款目标（提现闭环补强 #1）：记录不可变，日后代理修改收款账户
	// 不影响已提交的历史单——管理员审核/打款时据此转账。
	PayoutMethod  PayoutMethod
	PayoutAccount string
	PayoutName    string
	PayoutBank    string
	// PayoutRef 打款单号/凭证（mark-paid 时填，空=尚未打款）。
	PayoutRef string

	CreatedAt  time.Time // 提交时间
	UpdatedAt  time.Time // 最近更新时间
	ReviewedAt time.Time // 审核时间（approved/rejected 时填充）
	// PaidAt 标记已打款时间（mark-paid 时填充；零值=尚未打款）。
	PaidAt time.Time
}

// WithdrawInput 是提现申请入参。
type WithdrawInput struct {
	TenantID int64
	UserID   int64
	Amount   float64
	Remark   string
}
