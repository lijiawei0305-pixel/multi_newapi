package tokenplan

import (
	"math"
	"time"
)

// ---- 时钟（可注入，detailed-design §6.2 到期惰性校验需可控时间）----

// Clock 抽象当前时间，便于到期/计量用例注入假时钟，避免被测逻辑直接写死 time.Now。
type Clock interface {
	Now() time.Time
}

// systemClock 是默认真实时钟。
type systemClock struct{}

// Now 返回当前真实时间。
func (systemClock) Now() time.Time { return time.Now() }

// orSystemClock 在未注入时钟时回退到真实时钟。
func orSystemClock(c Clock) Clock {
	if c == nil {
		return systemClock{}
	}
	return c
}

// ---- 套餐定义（proposal §6.17 token_plans / §8.2）----

// PlanStatus 是主站套餐上下架状态。
type PlanStatus string

const (
	// PlanEnabled 套餐启用（可被代理上架/用户购买）。
	PlanEnabled PlanStatus = "enabled"
	// PlanDisabled 套餐停用（主站下架，禁止新购）。
	PlanDisabled PlanStatus = "disabled"
)

// Valid 判断是否为已知合法状态。
func (s PlanStatus) Valid() bool {
	return s == PlanEnabled || s == PlanDisabled
}

// IsEnabled 报告套餐是否处于启用态。
func (s PlanStatus) IsEnabled() bool { return s == PlanEnabled }

// Plan 是主站套餐定义（proposal §6.17）。价格类字段单位为 ¥（人民币，代理收益币种），
// MonthLimitUSD 为月度上游额度封顶（USD，与 quota 桶口径一致）。
type Plan struct {
	ID              int64
	Code            string  // trial / mini / solo / lite / pro / max
	Name            string  // 展示名
	BasePrice       float64 // 主站官方售价（¥/30 天）
	AnchorPrice     float64 // 原价（营销划线锚点，仅展示，不参与计费）
	DiscountLabel   string  // 折扣角标，如 "-94%"
	Multiplier      float64 // 计量倍率，默认 1.0（x1，不叠分组倍率）
	MonthLimitUSD   float64 // 月度上游额度封顶（USD）
	ValidDays       int     // 有效期天数，默认 30
	UpstreamCostEst float64 // 上游成本估算（¥，P&L 参考）
	AgentCostPrice  float64 // 主站给代理的进货成本价（¥，差价收益基准）
	MinPrice        float64 // 代理零售价保护线（¥，retail 不得低于此）
	IsRecommended   bool    // 营销：推荐/热门（二期展示用，一期预留）
	Badge           string  // 营销角标文案（二期预留）
	Sort            int     // 排序
	Status          PlanStatus
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// PlanInput 是管理员创建/更新套餐的入参（PlanCatalog）。
type PlanInput struct {
	Code            string
	Name            string
	BasePrice       float64
	AnchorPrice     float64
	DiscountLabel   string
	Multiplier      float64
	MonthLimitUSD   float64
	ValidDays       int
	UpstreamCostEst float64
	AgentCostPrice  float64
	MinPrice        float64
	IsRecommended   bool
	Badge           string
	Sort            int
	Status          PlanStatus
}

// Validate 校验套餐入参合法性；任一非法返回 ErrPlanInputInvalid（PLAN_INPUT_INVALID）。
// 规则：code 非空、各金额非负且有限、月限额>0、有效期>0、倍率>0、保护线≥进货价、状态合法。
func (in PlanInput) Validate() error {
	switch {
	case in.Code == "" || in.Name == "":
		return ErrPlanInputInvalid
	case !validAmount(in.BasePrice) || in.BasePrice < 0:
		return ErrPlanInputInvalid
	case !validAmount(in.AnchorPrice) || in.AnchorPrice < 0:
		return ErrPlanInputInvalid
	case !validAmount(in.MonthLimitUSD) || in.MonthLimitUSD <= 0:
		return ErrPlanInputInvalid
	case !validAmount(in.Multiplier) || in.Multiplier <= 0:
		return ErrPlanInputInvalid
	case in.ValidDays <= 0:
		return ErrPlanInputInvalid
	case !validAmount(in.AgentCostPrice) || in.AgentCostPrice < 0:
		return ErrPlanInputInvalid
	case !validAmount(in.MinPrice) || in.MinPrice < in.AgentCostPrice:
		return ErrPlanInputInvalid
	case !in.Status.Valid():
		return ErrPlanInputInvalid
	default:
		return nil
	}
}

// toPlan 把入参物化为新套餐实体（不含 ID/时间，由 Repo 回填）。
func (in PlanInput) toPlan() *Plan {
	return &Plan{
		Code:            in.Code,
		Name:            in.Name,
		BasePrice:       in.BasePrice,
		AnchorPrice:     in.AnchorPrice,
		DiscountLabel:   in.DiscountLabel,
		Multiplier:      in.Multiplier,
		MonthLimitUSD:   in.MonthLimitUSD,
		ValidDays:       in.ValidDays,
		UpstreamCostEst: in.UpstreamCostEst,
		AgentCostPrice:  in.AgentCostPrice,
		MinPrice:        in.MinPrice,
		IsRecommended:   in.IsRecommended,
		Badge:           in.Badge,
		Sort:            in.Sort,
		Status:          in.Status,
	}
}

// ---- 代理上架（proposal §6.18 tenant_token_plans）----

// TenantPlan 是代理对某套餐的上架与定价记录（UNIQUE(tenant_id, plan_id)）。
type TenantPlan struct {
	ID          int64
	TenantID    int64
	PlanID      int64
	Enabled     bool    // 是否上架（退出=false）
	RetailPrice float64 // 代理零售价（¥），强校验 >= Plan.MinPrice
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// TenantPlanView 是代理视角的套餐展示（套餐定义 + 本租户上架态/零售价）。
type TenantPlanView struct {
	Plan        Plan
	Listed      bool    // 该租户是否存在上架记录
	Enabled     bool    // 是否上架启用
	RetailPrice float64 // 代理零售价；未上架时回退 BasePrice
}

// ---- 订阅实例与状态机（proposal §6.19 / detailed-design §2.7）----

// SubStatus 是订阅实例状态。
type SubStatus string

const (
	// SubActive 生效中（可计量）。
	SubActive SubStatus = "active"
	// SubExhausted 月额度用尽（终态，需手动重购）。
	SubExhausted SubStatus = "exhausted"
	// SubExpired 已过期（expire_at < now，终态）。
	SubExpired SubStatus = "expired"
	// SubRefunded 已退款（终态；一期不支持退款，仅保留字段，见 §7 默认 #5）。
	SubRefunded SubStatus = "refunded"
)

// Valid 判断是否为已知合法状态。
func (s SubStatus) Valid() bool {
	switch s {
	case SubActive, SubExhausted, SubExpired, SubRefunded:
		return true
	default:
		return false
	}
}

// allowedSubTransitions 编码 detailed-design §2.7 的订阅状态机：
//
//	active -> exhausted | expired | refunded
//	exhausted / expired / refunded 为终态（不可迁出）。
//
// 不在表内的迁移（含 same->same、任何 from 终态）均为非法。
var allowedSubTransitions = map[SubStatus]map[SubStatus]bool{
	SubActive:    {SubExhausted: true, SubExpired: true, SubRefunded: true},
	SubExhausted: {},
	SubExpired:   {},
	SubRefunded:  {},
}

// CanTransitionTo 报告从 s 迁移到 next 是否合法。
func (s SubStatus) CanTransitionTo(next SubStatus) bool {
	return allowedSubTransitions[s][next]
}

// Subscription 是用户购买的套餐实例（proposal §6.19 user_subscriptions）。
type Subscription struct {
	ID             int64
	TenantID       int64
	UserID         int64
	PlanID         int64
	PurchasedPrice float64 // 实付（¥）
	MonthLimitUSD  float64 // 购买时快照（USD）
	UsedUSD        float64 // 已消耗（USD，原子累加）
	Status         SubStatus
	StartAt        time.Time
	ExpireAt       time.Time // StartAt + ValidDays
	SourceOrderID  string    // 关联支付订单（幂等键）
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// isExpiredAt 报告订阅在 now 是否已到期（expire_at 非零且 now >= expire_at）。
func (s *Subscription) isExpiredAt(now time.Time) bool {
	return !s.ExpireAt.IsZero() && !now.Before(s.ExpireAt)
}

// RemainingUSD 返回当前月额度剩余（month_limit - used，下限 0）。
func (s *Subscription) RemainingUSD() float64 {
	return remaining(s.MonthLimitUSD, s.UsedUSD)
}

// PendingPurchase 是下单后、支付激活前暂存的购买意图（按 OrderID 索引），
// 供 ActivateFromPayment(orderID) 凭订单号还原出订阅快照。对应一条待支付订单。
type PendingPurchase struct {
	OrderID        string
	TenantID       int64
	UserID         int64
	PlanID         int64
	RetailPrice    float64 // 实付（¥）= 代理零售价
	AgentCostPrice float64 // 进货成本价（¥），算差价收益
	MonthLimitUSD  float64 // 月限额快照（USD）
	ValidDays      int     // 有效期快照
}

// UsageLog 是套餐内一次计量记录（proposal §6.20 subscription_usage_logs）。
// 一期 Meter 仅携带 costUSD；model/request_id 等富字段顺延（见报告 TODO）。
type UsageLog struct {
	ID              int64
	SubscriptionID  int64
	TenantID        int64
	UserID          int64
	Model           string
	UpstreamCostUSD float64
	RequestID       string
	CreatedAt       time.Time
}

// ---- 购买/支付/风控/收益 的入参与值类型 ----

// PurchaseInput 是 SubscriptionService.Purchase 的入参。
type PurchaseInput struct {
	TenantID   int64
	UserID     int64
	PlanID     int64
	DeviceID   string // 设备指纹（Trial 限购维度之一）
	RealNameID string // 实名标识（Trial 限购维度之一）
	// DiscountRatio 该租户代理的全线折扣系数（doc/agent-wholesale-discount.md）：>0 时代理套餐进货价 =
	// plan.BasePrice × DiscountRatio；<=0（主站/未设代理）回退 plan.AgentCostPrice（现状）。由 http 层解析后传入。
	DiscountRatio float64
}

// PurchaseTicket 是下单结果（含支付跳转信息），用户据此完成支付。
type PurchaseTicket struct {
	OrderID   string
	PayURL    string
	AmountCNY float64
	PlanID    int64
}

// OrderType 标识下单类型（与 detailed-design §2.8 Payment 对齐）。
type OrderType string

// OrderTypeSubscription 套餐订阅订单（区别于钱包 recharge）。
const OrderTypeSubscription OrderType = "subscription"

// OrderInput 是向 PaymentGateway 下单的入参。
type OrderInput struct {
	TenantID  int64
	UserID    int64
	Type      OrderType
	AmountCNY float64
	Reference string // 业务引用（如 plan code）
	Subject   string // 订单标题
}

// PayOrder 是 PaymentGateway 下单返回。
type PayOrder struct {
	OrderID string
	PayURL  string
}

// PurchaseLimitCheck 是向 RiskEngine 询问限购的入参（Trial：用户∪实名∪设备 各 1 次）。
type PurchaseLimitCheck struct {
	TenantID   int64
	UserID     int64
	PlanID     int64
	PlanCode   string
	DeviceID   string
	RealNameID string
}

// EarningSource 标识一条代理收益来源（与 agent 模块收益枚举字符串对齐）。
type EarningSource string

const (
	// EarningTokenplanSpread 套餐差价（零售价 − 代理进货价）。
	EarningTokenplanSpread EarningSource = "tokenplan_spread"
	// EarningTokenplanCommission 套餐内消耗分润（一期不在本模块产出，预留枚举）。
	EarningTokenplanCommission EarningSource = "tokenplan_commission"
)

// EarningEntry 是写给 EarningSink 的一条代理收益记录（消费者侧定义，main 适配 agent.EarningEntry）。
// 幂等键为 (SourceType, SourceID)：同订单重复激活只入账一次。
type EarningEntry struct {
	TenantID   int64
	UserID     int64
	SourceType EarningSource
	SourceID   string // 幂等键来源（取 OrderID）
	Amount     float64
	Reference  string
}

// ---- 纯函数（集中金额语义，便于审阅）----

// tokenplanSpread 计算套餐差价收益 = 零售价 − 代理进货价（proposal §8.3）。
// 非法金额或非正差价返回 0（不产生收益）。
func tokenplanSpread(retail, agentCost float64) float64 {
	if !validAmount(retail) || !validAmount(agentCost) {
		return 0
	}
	if d := retail - agentCost; d > 0 {
		return d
	}
	return 0
}

// remaining 返回额度剩余 = limit − used，下限 0。
func remaining(limit, used float64) float64 {
	if r := limit - used; r > 0 {
		return r
	}
	return 0
}

// validAmount 拒绝 NaN / ±Inf，作为金额入口的防御校验。
func validAmount(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
