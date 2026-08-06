package payment

import (
	"math"
	"strings"
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

// OrderStatus 是支付订单状态机（detailed-design §2.8「见 §2.6 订单状态机」+ Phase D）：
//
//	created ──验签+CAS──▶ paid ──OnPaid 成功──▶ credited（终态）
//	   │                   │
//	   │                   └──OnPaid 失败──▶ created（回滚，允许网关重试再分发）
//	   └──平台明确未付/本地超时──────────────▶ failed
//	                                           │
//	                         后续可信已付事实 ──┘──▶ paid
//	cancelled（legacy）──可信已付事实──────────▶ paid ──▶ credited
//
// failed/cancelled 是本地基于当时事实作出的停止扫描状态，不得压过支付平台后续给出的可信已付事实。
// order_no 唯一约束 + {created,failed,cancelled}→paid 的原子 CAS 共同实现「重复回调幂等、不重复入账」。
//
// 注意：created 同时承载「尚未 Prepay / 结果未知 / 待付有 QR」——通过 PayURL 是否为空 + LastErrorClass
// 区分；不得假设「重复 Prepay 返回原 code_url」。
type OrderStatus string

const (
	// OrderCreated 已下单待支付（初始态；亦承载 outcome_unknown 恢复）。
	OrderCreated OrderStatus = "created"
	// OrderPaid 已验签、CAS 占位成功、入账进行中（瞬态）。
	OrderPaid OrderStatus = "paid"
	// OrderCredited 入账完成（终态）。
	OrderCredited OrderStatus = "credited"
	// OrderFailed 支付失败 / 平台明确失败（本地终态，可被可信已付事实恢复）。
	OrderFailed OrderStatus = "failed"
	// OrderCancelled 历史取消态（生产 legacy）；不得批量改写，仅允许严格可信已付 → paid。
	OrderCancelled OrderStatus = "cancelled"
)

// Valid 判断是否为已知状态。
func (s OrderStatus) Valid() bool {
	switch s {
	case OrderCreated, OrderPaid, OrderCredited, OrderFailed, OrderCancelled:
		return true
	default:
		return false
	}
}

// IsTerminal 判断是否为日常扫描终态（credited / failed / cancelled）。
// failed/cancelled 仍可被后续可信已付事实恢复到 paid。
func (s OrderStatus) IsTerminal() bool {
	return s == OrderCredited || s == OrderFailed || s == OrderCancelled
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
	case OrderCancelled:
		// legacy：仅严格可信已付可恢复。
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
	Type           OrderType // 必填：recharge | subscription
	TenantID       int64     // 必填：归属租户
	UserID         int64     // 必填：下单用户
	Provider       Provider  // 必填：wxpay | alipay
	AmountUSD      float64   // recharge：入账额度（USD）；subscription 可为 0
	ActualPaid     float64   // 用户实付（收益币种；差价基准）
	GroupID        int64     // recharge：用户分组（算充值差价用）
	PlanID         int64     // subscription：套餐 ID
	Subject        string    // 订单描述（透传支付平台）
	Reference      string    // 可选：外部业务引用（如 tokenplan 购买票据）
	IdempotencyKey string    // 可选：客户端支付意图幂等键（网络重试复用同一 key）
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

// CreateState 是支付创建/恢复维度状态（与结算 Status 正交，Phase E）。
// 不得只靠 created + pay_url 是否为空猜测。
type CreateState string

const (
	CreateStateLocalCreated     CreateState = "local_created"
	CreateStatePrepayInflight   CreateState = "prepay_inflight"
	CreateStateCredentialReady  CreateState = "credential_ready"
	CreateStatePrepayUnknown    CreateState = "prepay_unknown"
	CreateStateClosePending     CreateState = "close_pending"
	CreateStateCloseConfirming  CreateState = "close_confirming"
	CreateStateProviderClosed   CreateState = "provider_closed"
	CreateStateReplaced         CreateState = "replaced"
	CreateStateDefinitiveReject CreateState = "definitive_reject"
)

// ProviderTradeState 规范化平台查单状态。
type ProviderTradeState string

const (
	TradeStateSuccess       ProviderTradeState = "SUCCESS"
	TradeStateNotPay        ProviderTradeState = "NOTPAY"
	TradeStateClosed        ProviderTradeState = "CLOSED"
	TradeStateRefund        ProviderTradeState = "REFUND"
	TradeStateRevoked       ProviderTradeState = "REVOKED"
	TradeStateUserPaying    ProviderTradeState = "USERPAYING"
	TradeStatePayError      ProviderTradeState = "PAYERROR"
	TradeStateOrderNotExist ProviderTradeState = "ORDER_NOT_EXIST"
	TradeStateUnknown       ProviderTradeState = "UNKNOWN"
)

// PayOrder 是落库的支付订单（order_no 唯一）+ 下单返回给前端的支付凭据。
// Phase E：intent/attempt 分离——RootOrderNo 指向支付意图根；ReplacesOrderNo 唯一约束兜底单 child。
type PayOrder struct {
	OrderNo               string      // 全局唯一订单号（单次 provider attempt）
	Type                  OrderType   // recharge | subscription
	TenantID              int64       // 归属租户
	UserID                int64       // 下单用户
	Provider              Provider    // wxpay | alipay
	AmountUSD             float64     // 入账额度（USD）
	ActualPaid            float64     // 用户实付（元，报表兼容字段）
	ActualPaidFen         int64       // 用户实付（分，精确比对；0=历史行未回填）
	GroupID               int64       // recharge 分组
	PlanID                int64       // subscription 套餐
	Subject               string      // 订单描述
	Reference             string      // 外部业务引用
	IdempotencyKey        string      // 客户端支付意图幂等键（空=未使用；仅 root 持有）
	ProviderTransactionID string      // 支付机构交易号（空=未支付/未回填）
	Status                OrderStatus // 结算状态机
	CreateState           CreateState // 创建/恢复状态
	RootOrderNo           string      // 支付意图根（首 attempt 等于 OrderNo）
	ReplacesOrderNo       string      // 本单替换的父 attempt（空=非 replacement）
	AttemptNo             int         // 意图内 attempt 序号（从 1）
	ActiveOrderNo         string      // 仅 root：当前可支付 attempt
	ProviderTradeState    string      // 最近一次权威查单规范化状态
	NotifyURL             string      // 回填的异步回调地址
	PayURL                string      // 支付平台返回的支付跳转/二维码内容（TEXT，可超 512）
	ExpiresAt             time.Time   // 与平台 TimeExpire 对齐的待付有效期
	ProviderPaidAt        time.Time   // 支付机构确认时间（零值=未确认）
	CreditedAt            time.Time   // 本站入账完成时间
	CallbackReceivedAt    time.Time   // 首次收到成功回调时间
	NextQueryAt           time.Time   // 下次主动查单计划时间（零值=不调度）
	QueryAttempts         int         // 已主动查单次数
	QueryClaimToken       string      // 查单 fencing token
	QueryClaimUntil       time.Time   // 查单租约截止
	RecoveryClaimToken    string      // 恢复 fencing token
	RecoveryClaimUntil    time.Time   // 恢复租约截止
	NextRecoveryAt        time.Time   // 下次恢复调度
	LastErrorClass        string      // 最近一次创建/网络错误类别（低基数）
	LastNetworkStage      string      // 最近一次失败阶段 dns/connect/tls/...
	CreateAttempts        int         // 已向平台发起 CreatePay 的次数
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// toPaidOrder 把订单快照 + 回调信息组装成分发给 OrderSink 的 PaidOrder。
func (o *PayOrder) toPaidOrder(info *CallbackInfo, now time.Time) PaidOrder {
	ref := o.Reference
	if info != nil && info.TxnID != "" {
		ref = info.TxnID // 优先用平台交易号作收益/审计引用
	}
	root := o.RootOrderNo
	if root == "" {
		root = o.OrderNo
	}
	fen := OrderActualPaidFen(o)
	return PaidOrder{
		OrderNo:       o.OrderNo,
		RootOrderNo:   root,
		Type:          o.Type,
		TenantID:      o.TenantID,
		UserID:        o.UserID,
		Provider:      o.Provider,
		AmountUSD:     o.AmountUSD,
		ActualPaid:    o.ActualPaid,
		ActualPaidFen: fen,
		GroupID:       o.GroupID,
		PlanID:        o.PlanID,
		Reference:     ref,
		PaidAt:        now,
	}
}

// PaidOrder 是验签+幂等通过后，分发给 OrderSink（Wallet / TokenPlan）的入账契约。
// Phase F：携带 root/intent 与整数分，sink 不得再 float 精确比较。
type PaidOrder struct {
	OrderNo       string    // attempt 订单号
	RootOrderNo   string    // 支付意图根
	Type          OrderType // 分发依据
	TenantID      int64     // 入账绑定租户
	UserID        int64     // 入账绑定用户
	Provider      Provider  // 支付渠道
	AmountUSD     float64   // 展示/兼容；权威入账以 Quota 整数（sink 内算）+ ActualPaidFen
	ActualPaid    float64   // 展示兼容
	ActualPaidFen int64     // 权威实付分
	GroupID       int64     // recharge：用户分组
	PlanID        int64     // subscription：套餐 ID
	Reference     string    // 平台交易号 / 外部引用（收益幂等键来源）
	PaidAt        time.Time // 入账时间
}

// PayRequest 是 PaySDK.CreatePay 的入参（向支付平台下单）。
type PayRequest struct {
	Provider   Provider
	OrderNo    string
	AmountUSD  float64
	ActualPaid float64
	Subject    string
	NotifyURL  string
	ExpiresAt  time.Time // 与 DB expires_at 对齐；微信 TimeExpire / 支付宝 TimeoutExpress
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
	PaidAmount float64 // 平台回传金额（元）；与库内订单金额比对
	TxnID      string  // 平台交易号
}

// QueryResult 是主动查单的结构化结果（PAY-FACT-01 / Phase F）。
// 字段必须来自平台**响应**，不得把请求参数回填冒充。
type QueryResult struct {
	Provider        Provider
	OrderNo         string             // 响应 out_trade_no（非请求拷贝；NOT_EXIST 可为空）
	TradeState      string             // 平台原始状态
	NormalizedState ProviderTradeState // 规范化
	Paid            bool
	Closed          bool
	NotExist        bool
	TransactionID   string
	PaidAmountFen   int64
	Currency        string // 响应币种；SUCCESS 必须为 CNY
	ProviderPaidAt  time.Time
	MchID           string // 响应 mchid
	AppID           string // 响应 appid
	ExpectedMchID   string // 本站凭据（请求上下文，非响应冒充）
	ExpectedAppID   string
	// AuthorityVerified：响应无 seller/app 字段时，适配器在 SDK 验签/商户上下文可信后置 true。
	AuthorityVerified bool
}

// QueryPaidOK 报告是否具备严格入账所需的全部事实（Phase F：禁止空字段放行）。
// NormalizedState 必须为 SUCCESS；空值不得通过。
func (q *QueryResult) QueryPaidOK() bool {
	if q == nil || !q.Paid || q.NotExist || q.Closed {
		return false
	}
	if q.NormalizedState != TradeStateSuccess {
		return false
	}
	if q.Provider == "" {
		return false
	}
	if strings.TrimSpace(q.OrderNo) == "" || strings.TrimSpace(q.TransactionID) == "" {
		return false
	}
	if q.PaidAmountFen <= 0 {
		return false
	}
	// currency 必须存在且为 CNY；缺失不得伪造为已验证
	if !strings.EqualFold(strings.TrimSpace(q.Currency), "CNY") {
		return false
	}
	// 微信：mchid 与 appid 必须同时完整匹配（禁止任一匹配即放行）
	if q.Provider == ProviderWxpay {
		if strings.TrimSpace(q.ExpectedMchID) == "" || strings.TrimSpace(q.MchID) == "" || q.MchID != q.ExpectedMchID {
			return false
		}
		if strings.TrimSpace(q.ExpectedAppID) == "" || strings.TrimSpace(q.AppID) == "" || q.AppID != q.ExpectedAppID {
			return false
		}
		return true
	}
	// 支付宝：签名+业务成功后的 AuthorityVerified，或 app 完整匹配
	if q.AuthorityVerified {
		return true
	}
	if strings.TrimSpace(q.ExpectedAppID) == "" || strings.TrimSpace(q.AppID) == "" || q.AppID != q.ExpectedAppID {
		return false
	}
	return true
}

// BindingsOK 关单/替换前校验：有响应体时 order_no 与商户绑定必须严格。
// NOT_EXIST 无订单体：仅 provider 上下文可信即可。
func (q *QueryResult) BindingsOK(localOrderNo string, provider Provider) bool {
	if q == nil {
		return false
	}
	if q.NotExist {
		return provider != "" && (q.Provider == "" || q.Provider == provider)
	}
	if q.Provider != "" && provider != "" && q.Provider != provider {
		return false
	}
	// 有响应体时 order_no 必须非空且匹配
	if strings.TrimSpace(q.OrderNo) == "" || q.OrderNo != localOrderNo {
		return false
	}
	if q.Provider == ProviderWxpay {
		if strings.TrimSpace(q.ExpectedMchID) == "" || strings.TrimSpace(q.MchID) == "" || q.MchID != q.ExpectedMchID {
			return false
		}
		if strings.TrimSpace(q.ExpectedAppID) == "" || strings.TrimSpace(q.AppID) == "" || q.AppID != q.ExpectedAppID {
			return false
		}
		return true
	}
	// 支付宝：响应无 seller 时依赖 AuthorityVerified
	if q.AuthorityVerified {
		return true
	}
	if strings.TrimSpace(q.ExpectedAppID) != "" && (strings.TrimSpace(q.AppID) == "" || q.AppID != q.ExpectedAppID) {
		return false
	}
	return strings.TrimSpace(q.ExpectedAppID) == "" || q.AppID == q.ExpectedAppID
}

// validAmount 拒绝 NaN / ±Inf，作为金额入口的防御校验（与 wallet 一致）。
func validAmount(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

// YuanToFen 将人民币元转为整数分（四舍五入到最近分）。
func YuanToFen(yuan float64) int64 {
	if !validAmount(yuan) {
		return 0
	}
	return int64(math.Round(yuan * 100))
}

// FenToYuan 将整数分转为元（仅展示；比对请用分）。
func FenToYuan(fen int64) float64 {
	return float64(fen) / 100
}

// OrderActualPaidFen 返回订单权威实付分：优先 ActualPaidFen，否则从 ActualPaid 推导。
func OrderActualPaidFen(o *PayOrder) int64 {
	if o == nil {
		return 0
	}
	if o.ActualPaidFen > 0 {
		return o.ActualPaidFen
	}
	return YuanToFen(o.ActualPaid)
}

// DefaultQRValidity 微信 Native 二维码默认有效窗口（与对账过期口径一致）。
const DefaultQRValidity = 2 * time.Hour

// Query schedule offsets from CreatedAt（PAY-REC-01：5s / 30s / 60s，之后 5min 扫尾）。
var queryScheduleOffsets = []time.Duration{
	5 * time.Second,
	30 * time.Second,
	60 * time.Second,
}

// NextQueryAtForAttempt 根据已完成查单次数与创建时间计算下次查单时刻。
// attempts=0 → CreatedAt+5s；1 → +30s；2 → +60s；之后 now+5min（调用方传 now）。
func NextQueryAtForAttempt(createdAt time.Time, attempts int, now time.Time) time.Time {
	if attempts < 0 {
		attempts = 0
	}
	if attempts < len(queryScheduleOffsets) {
		return createdAt.Add(queryScheduleOffsets[attempts])
	}
	return now.Add(5 * time.Minute)
}
