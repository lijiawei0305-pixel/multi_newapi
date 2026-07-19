package payment

import (
	"math"
	"time"
)

// Provider 标识支付渠道（微信 / 支付宝）。回调入口与 notify_url 路径据此区分。
type Provider string

const (
	// ProviderWxpay 微信支付。回调 POST /api/pay/wechat/notify。
	ProviderWxpay Provider = "wxpay"
	// ProviderAlipay 支付宝。回调 POST /api/pay/alipay/notify。
	ProviderAlipay Provider = "alipay"
)

// Valid 判断是否为已知支付渠道。
func (p Provider) Valid() bool {
	switch p {
	case ProviderWxpay, ProviderAlipay:
		return true
	default:
		return false
	}
}

// NotifyPath 返回当前进程承载的渠道异步回调固定路径。未知渠道返回空串。
func (p Provider) NotifyPath() string {
	switch p {
	case ProviderWxpay:
		return "/api/pay/wechat/notify"
	case ProviderAlipay:
		return "/api/pay/alipay/notify"
	default:
		return ""
	}
}

// OrderType 标识订单用途，决定回调入账分发到哪个 OrderSink（detailed-design §2.8）。
type OrderType string

const (
	// OrderTypeRecharge 钱包充值订单 —— 入账分发到 Wallet.Credit（§3.3）。
	OrderTypeRecharge OrderType = "recharge"
	// OrderTypeSubscription tokenplan 套餐订单 —— 入账分发到 TokenPlan.ActivateFromPayment（§3.2）。
	OrderTypeSubscription OrderType = "subscription"
)

// Valid 判断是否为已知订单类型。
func (t OrderType) Valid() bool {
	switch t {
	case OrderTypeRecharge, OrderTypeSubscription:
		return true
	default:
		return false
	}
}

// OrderStatus 是支付订单状态机（detailed-design §2.8「见 §2.6 订单状态机」）：
//
//	created ──验签+CAS──▶ paid ──OnPaid 成功──▶ credited（终态）
//	   │                   │
//	   │                   └──OnPaid 失败──▶ created（回滚，允许网关重试再分发）
//	   └──平台明确未付/本地超时──────────────▶ failed
//	                                           │
//	                         后续可信已付事实 ──┘──▶ paid
//
// failed 是本地基于当时事实作出的停止扫描状态，不得压过支付平台后续给出的可信已付事实；因此允许
// failed→paid 恢复。order_no 唯一约束 + {created,failed}→paid 的原子 CAS 共同实现「重复回调幂等、
// 不重复入账」。
type OrderStatus string

const (
	// OrderCreated 已下单待支付（初始态）。
	OrderCreated OrderStatus = "created"
	// OrderPaid 已验签、CAS 占位成功、入账进行中（瞬态）。
	OrderPaid OrderStatus = "paid"
	// OrderCredited 入账完成（终态）。
	OrderCredited OrderStatus = "credited"
	// OrderFailed 支付失败 / 平台明确失败（终态）。
	OrderFailed OrderStatus = "failed"
)

// Valid 判断是否为已知状态。
func (s OrderStatus) Valid() bool {
	switch s {
	case OrderCreated, OrderPaid, OrderCredited, OrderFailed:
		return true
	default:
		return false
	}
}

// IsTerminal 判断是否为日常扫描终态（credited / failed）。failed 仍可被后续可信已付事实恢复到 paid；
// 此处的「终态」只表示普通未付对账不再自动推进。
func (s OrderStatus) IsTerminal() bool {
	return s == OrderCredited || s == OrderFailed
}

// CanTransitionTo 表达状态机的合法迁移；非法迁移由 Repo 的 CAS 自然拒绝。
// 该方法集中描述状态机供审阅，并被 model_test 覆盖。
func (s OrderStatus) CanTransitionTo(to OrderStatus) bool {
	switch s {
	case OrderCreated:
		return to == OrderPaid || to == OrderFailed
	case OrderPaid:
		// credited=入账成功；created=入账失败回滚；failed=作废。
		return to == OrderCredited || to == OrderCreated || to == OrderFailed
	case OrderFailed:
		// 本地过期/失败判断不得覆盖支付平台后续给出的可信已付事实。
		return to == OrderPaid
	default: // credited 为绝对终态
		return false
	}
}

// OrderInput 是 PaymentGateway.CreateOrder 的入参（由 Wallet 充值或 TokenPlan 购买流程填充）。
//
// 订单强制绑定 tenant_id+user_id（多租户隔离根，detailed-design §1.3 / §14）。
// 两类金额单位不同（与 wallet.CreditInput 对齐）：AmountUSD 是入账到用户 API 余额的额度（USD）；
// ActualPaid 是用户实付真实货币（收益币种，如 ¥），作充值差价基准。
type OrderInput struct {
	Type       OrderType // 必填：recharge | subscription
	TenantID   int64     // 必填：归属租户
	UserID     int64     // 必填：下单用户
	Provider   Provider  // 必填：wxpay | alipay
	AmountUSD  float64   // recharge：入账额度（USD）；subscription 可为 0
	ActualPaid float64   // 用户实付（收益币种；差价基准）
	GroupID    int64     // recharge：用户分组（算充值差价用）
	PlanID     int64     // subscription：套餐 ID
	Subject    string    // 订单描述（透传支付平台）
	Reference  string    // 可选：外部业务引用（如 tokenplan 购买票据）
}

// validate 校验下单入参；任一不合法返回 ErrOrderInvalid（PAY_ORDER_INVALID）。
func (in OrderInput) validate() error {
	if in.TenantID <= 0 || in.UserID <= 0 {
		return ErrOrderInvalid // 必带 tenant_id+user_id
	}
	if !in.Type.Valid() || !in.Provider.Valid() {
		return ErrOrderInvalid
	}
	if !validAmount(in.AmountUSD) || in.AmountUSD < 0 ||
		!validAmount(in.ActualPaid) || in.ActualPaid < 0 {
		return ErrOrderInvalid
	}
	switch in.Type {
	case OrderTypeRecharge:
		if in.AmountUSD <= 0 { // 充值必须有正入账额度
			return ErrOrderInvalid
		}
	case OrderTypeSubscription:
		if in.PlanID <= 0 { // 套餐订单必须绑定套餐
			return ErrOrderInvalid
		}
	}
	return nil
}

// PayOrder 是落库的支付订单（order_no 唯一）+ 下单返回给前端的支付凭据。
type PayOrder struct {
	OrderNo    string      // 全局唯一订单号（幂等键）
	Type       OrderType   // recharge | subscription
	TenantID   int64       // 归属租户
	UserID     int64       // 下单用户
	Provider   Provider    // wxpay | alipay
	AmountUSD  float64     // 入账额度（USD）
	ActualPaid float64     // 用户实付（收益币种）
	GroupID    int64       // recharge 分组
	PlanID     int64       // subscription 套餐
	Subject    string      // 订单描述
	Reference  string      // 外部业务引用
	Status     OrderStatus // 状态机
	NotifyURL  string      // 回填的异步回调地址
	PayURL     string      // 支付平台返回的支付跳转/二维码内容
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// toPaidOrder 把订单快照 + 回调信息组装成分发给 OrderSink 的 PaidOrder。
func (o *PayOrder) toPaidOrder(info *CallbackInfo, now time.Time) PaidOrder {
	ref := o.Reference
	if info != nil && info.TxnID != "" {
		ref = info.TxnID // 优先用平台交易号作收益/审计引用
	}
	return PaidOrder{
		OrderNo:    o.OrderNo,
		Type:       o.Type,
		TenantID:   o.TenantID,
		UserID:     o.UserID,
		Provider:   o.Provider,
		AmountUSD:  o.AmountUSD,
		ActualPaid: o.ActualPaid,
		GroupID:    o.GroupID,
		PlanID:     o.PlanID,
		Reference:  ref,
		PaidAt:     now,
	}
}

// PaidOrder 是验签+幂等通过后，分发给 OrderSink（Wallet / TokenPlan）的入账契约。
// 本类型由 payment 定义；Wallet / TokenPlan 实现 OrderSink 时 import 本包（payment 不反向 import）。
type PaidOrder struct {
	OrderNo    string    // 订单号（= TokenPlan.ActivateFromPayment 的 orderID）
	Type       OrderType // 分发依据
	TenantID   int64     // 入账绑定租户
	UserID     int64     // 入账绑定用户
	Provider   Provider  // 支付渠道
	AmountUSD  float64   // recharge：入账额度（USD）
	ActualPaid float64   // recharge：实付（充值差价基准）
	GroupID    int64     // recharge：用户分组
	PlanID     int64     // subscription：套餐 ID
	Reference  string    // 平台交易号 / 外部引用（收益幂等键来源）
	PaidAt     time.Time // 入账时间
}

// PayRequest 是 PaySDK.CreatePay 的入参（向支付平台下单）。
type PayRequest struct {
	Provider   Provider
	OrderNo    string
	AmountUSD  float64
	ActualPaid float64
	Subject    string
	NotifyURL  string
}

// PayCredential 是支付平台下单返回（mock：占位支付链接/二维码内容）。
type PayCredential struct {
	PayURL string // 支付跳转 URL 或二维码内容
	Raw    string // 平台原始返回（审计；mock 留空）
}

// CallbackInfo 是 PaySDK 验签后从回调原文解析出的结构化信息。
type CallbackInfo struct {
	Provider   Provider
	OrderNo    string  // 商户订单号（幂等定位）
	Success    bool    // 交易是否成功（平台 trade_status）
	PaidAmount float64 // 平台回传金额；公网回调和可信入账路径都会与库内订单金额比对
	TxnID      string  // 平台交易号
}

// validAmount 拒绝 NaN / ±Inf，作为金额入口的防御校验（与 wallet 一致）。
func validAmount(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
