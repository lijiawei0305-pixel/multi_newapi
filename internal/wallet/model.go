package wallet

import (
	"math"
	"time"
)

// CreditSource 标识一次入账的来源，决定是否计算充值差价。
type CreditSource string

const (
	// SourceRecharge 用户付费充值 —— 触发代理「充值差价」收益（recharge_spread）。
	SourceRecharge CreditSource = "recharge"
	// SourceManual 管理员人工入账（线上支付未通时兜底）—— 不计差价收益。
	SourceManual CreditSource = "manual"
)

// EarningRechargeSpread 是充值差价收益的 source_type（写入 agent_earning_logs）。
// 见 doc/proposal.md §7、detailed-design §2.3 的 EarningSink 约定。
const EarningRechargeSpread = "recharge_spread"

// CreditInput 是 WalletService.Credit 的入参。
//
// 入账强制绑定 tenant_id+user_id（多租户隔离根，见 detailed-design §1.3 / §14）。
// 注意两类金额单位不同：CreditedUSD 是入账到用户 API 余额的额度（USD，与 quota 桶一致）；
// ActualPaid 是用户实付的真实货币（与代理可提现收益同币种，如 ¥），用作充值差价基准。
type CreditInput struct {
	TenantID    int64        // 必填：归属租户
	UserID      int64        // 必填：入账目标用户
	CreditedUSD float64      // 入账到用户 API 余额的额度（USD）
	ActualPaid  float64      // 用户实付（收益币种，如 ¥；差价基准；人工入账填 0）
	GroupID     int64        // 用户分组（取分组倍率算成本基准）
	Source      CreditSource // recharge=计差价；manual=不计
	Reference   string       // 关联订单号/来源标识（审计 + 收益幂等键来源）
}

// EarningEntry 是写给 EarningSink 的一条代理收益记录（本包消费者侧定义）。
// 字段对齐 detailed-design §2.3 的 agent_earning_logs 语义；Agent 模块以自己的
// EarningEntry 实现 AddEarning，main 处适配，二者编译期零耦合。
type EarningEntry struct {
	TenantID   int64
	UserID     int64
	SourceType string  // 如 recharge_spread
	AmountUSD  float64 // 收益金额（USD）
	Reference  string  // 关联来源
}

// RedemptionStatus 是兑换码状态机的类型常量（detailed-design §2.6）。
type RedemptionStatus string

const (
	// RedemptionEnabled 可用（初始态）。
	RedemptionEnabled RedemptionStatus = "enabled"
	// RedemptionUsed 已兑换（终态）。
	RedemptionUsed RedemptionStatus = "used"
	// RedemptionDisabled 管理员/代理禁用（终态）。
	RedemptionDisabled RedemptionStatus = "disabled"
)

// Valid 判断是否为已知合法状态值。
func (s RedemptionStatus) Valid() bool {
	switch s {
	case RedemptionEnabled, RedemptionUsed, RedemptionDisabled:
		return true
	default:
		return false
	}
}

// RedemptionCode 是兑换码实体（对应 proposal §6 `agent_redemption_codes`，绑 tenant_id）。
// 代理建码时「从自己额度预扣」（proposal §5.2），故兑换入账不再计充值差价。
type RedemptionCode struct {
	ID           int64
	TenantID     int64
	Code         string
	AmountUSD    float64          // 兑换面额（入账到用户余额）
	Status       RedemptionStatus // enabled -> used
	ExpireAt     time.Time        // 零值表示永不过期
	UsedByUserID int64
	UsedAt       time.Time
	CreatedAt    time.Time
}

// redeemableError 按状态机判定该码当前能否兑换，返回精确错误码：
//
//	used                     -> REDEEM_CODE_USED
//	disabled / 未知状态        -> REDEEM_CODE_INVALID
//	enabled 且已过期           -> REDEEM_CODE_INVALID（过期归类为 invalid）
//	enabled 且未过期           -> nil（可兑换）
//
// 真正的「翻牌」原子性由 Repo 的 CAS（UseRedemption）保证（detailed-design §6.2）。
func (c *RedemptionCode) redeemableError(now time.Time) error {
	switch c.Status {
	case RedemptionUsed:
		return ErrRedeemCodeUsed
	case RedemptionEnabled:
		if !c.ExpireAt.IsZero() && !now.Before(c.ExpireAt) { // now >= ExpireAt
			return ErrRedeemCodeInvalid
		}
		return nil
	default: // disabled / 非法状态
		return ErrRedeemCodeInvalid
	}
}

// rechargeSpread 计算充值差价（代理盈利一）。
//
// 业务规则（doc/proposal.md §7）：差价 = 用户实付 − 代理成本。
// 成本基准由分组倍率决定（detailed-design §2.6「按组倍率算充值差价」）：
//
//	代理成本 = 实付 / 分组倍率      （倍率为分组溢价倍率）
//	差价     = 实付 − 代理成本 = 实付 × (1 − 1/倍率)
//
// 直觉：溢价组（倍率>1）代理获得正差价；普通组（倍率=1）差价为 0；
// 倍率<=0 视为无效配置 → 代理成本=实付，差价 0（防御，不产生收益）。
//
// 注意：本公式为 proposal 未给出精确成本口径下的【默认假设】，已在交付报告标注 [待确认]。
// 全部差价语义集中于此纯函数，便于审阅与替换。
func rechargeSpread(paidUSD, groupRatio float64) float64 {
	if !validAmount(paidUSD) || !validAmount(groupRatio) || groupRatio <= 0 {
		return 0
	}
	cost := paidUSD / groupRatio
	return paidUSD - cost
}

// validAmount 拒绝 NaN / ±Inf，作为金额入口的防御校验。
func validAmount(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
