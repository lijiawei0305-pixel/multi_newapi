package tokenplan

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/internal/platform/appctx"
	"github.com/QuantumNous/new-api/internal/platform/quota"
)

// ---- 对外接口（detailed-design §2.7 的 Go 签名）----

// PlanCatalog 是管理员套餐 CRUD。
type PlanCatalog interface {
	// Create 新建套餐；入参非法返回 PLAN_INPUT_INVALID。
	Create(ctx context.Context, in PlanInput) (*Plan, error)
	// Update 全量更新套餐；不存在返回 PLAN_NOT_FOUND，入参非法返回 PLAN_INPUT_INVALID。
	Update(ctx context.Context, id int64, in PlanInput) error
	// Get 读取单个套餐；不存在返回 PLAN_NOT_FOUND。
	Get(ctx context.Context, id int64) (*Plan, error)
	// List 列出全部套餐（按 Sort 升序）。
	List(ctx context.Context) ([]Plan, error)
}

// PlanRetailService 是代理上架/改价（经 PricingGuard 校验 retail>=min_price）。
type PlanRetailService interface {
	// ListForTenant 返回代理视角的套餐列表（套餐定义 + 本租户上架态/零售价）。
	ListForTenant(ctx context.Context, tenantID int64) ([]TenantPlanView, error)
	// SetListing 上架/退出并设零售价；零售价击穿保护线返回 RETAIL_BELOW_MIN，
	// 套餐停用返回 PLAN_DISABLED，套餐不存在返回 PLAN_NOT_FOUND。
	SetListing(ctx context.Context, tenantID, planID int64, enabled bool, retail float64) error
}

// SubscriptionService 是用户购买与月度计量（detailed-design §2.7）。
type SubscriptionService interface {
	// Purchase 校验上架/限购后下单（type=subscription）：套餐未上架 PLAN_NOT_LISTED、
	// 停用 PLAN_DISABLED、限购 PURCHASE_LIMIT_EXCEEDED。返回支付凭据。
	Purchase(ctx context.Context, in PurchaseInput) (*PurchaseTicket, error)
	// ReleasePurchaseClaim 供上层（HandlePurchase）在 Purchase 成功返回后、支付凭据创建失败时
	// 归还本次占键（覆盖 Purchase 函数内 defer 触不到的 CreatePay 失败点，audit F2）。
	ReleasePurchaseClaim(ctx context.Context, in PurchaseLimitCheck) error
	// ActivateFromPayment 凭订单号幂等创建 active 实例（同 orderID 多次只建一个）并触发
	// tokenplan_spread 收益；订单不存在返回 SUBSCRIPTION_NOT_FOUND。
	ActivateFromPayment(ctx context.Context, orderID string) (*Subscription, error)
	// GetActive 返回用户当前 active 订阅（惰性过期）；无则 SUBSCRIPTION_NOT_FOUND。
	GetActive(ctx context.Context, userID int64) (*Subscription, error)
	// HasActive 报告用户是否存在 active 套餐（供 billing.SubscriptionChecker 选桶）。
	HasActive(ctx context.Context, userID int64) (bool, error)
	// Meter 原子累加 used_usd（§6.2 条件 UPDATE）；超 month_limit 置 exhausted 返回
	// SUBSCRIPTION_EXHAUSTED，已过期返回 SUBSCRIPTION_EXPIRED。
	Meter(ctx context.Context, subID int64, costUSD float64) error
}

// SubscriptionQuotaFactory 为请求产出套餐额度桶（quota.Source），供 QuotaRouter 选桶。
type SubscriptionQuotaFactory interface {
	// For 匹配 detailed-design §2.7：有 active 套餐→(套餐桶, true)，否则 (nil, false)。
	For(p appctx.Principal) (quota.Source, bool)
	// SubscriptionSource 匹配 billing.SubscriptionSourceFactory（ctx 感知）：
	// 返回绑定到用户当前 active 订阅的额度桶；无 active 返回 SUBSCRIPTION_EXPIRED。
	SubscriptionSource(ctx context.Context, userID, tenantID int64) (quota.Source, error)
}

// ---- 消费者定义的依赖接口（本包声明，main 装配具体实现；见 detailed-design §1.4）----
// 本包只 import 自己声明的接口与 platform/{quota,apperr,appctx}，不 import 兄弟业务模块。

// PlanRepo 是套餐定义与代理上架的持久化抽象。本轮提供并发安全内存假实现（MemRepo）；
// 生产 GORM 实现（迁移 / UNIQUE(tenant_id,plan_id) / scopeByTenant）位于 gormrepo 子包。
type PlanRepo interface {
	// CreatePlan 入库并回填 p.ID / 时间戳。
	CreatePlan(ctx context.Context, p *Plan) error
	// UpdatePlan 全量更新；不存在返回 ErrPlanNotFound。
	UpdatePlan(ctx context.Context, id int64, in PlanInput) error
	// GetPlan 按 id 读取；不存在返回 ErrPlanNotFound。
	GetPlan(ctx context.Context, id int64) (*Plan, error)
	// ListPlans 返回全部套餐（按 Sort 升序）。
	ListPlans(ctx context.Context) ([]Plan, error)
	// GetListing 读取某租户对某套餐的上架记录；不存在返回 (nil, nil)。
	GetListing(ctx context.Context, tenantID, planID int64) (*TenantPlan, error)
	// UpsertListing 按 (tenant_id, plan_id) 唯一键 upsert 上架记录，回填 ID/时间戳。
	UpsertListing(ctx context.Context, tp *TenantPlan) error
	// ListListings 返回某租户全部上架记录。
	ListListings(ctx context.Context, tenantID int64) ([]TenantPlan, error)
}

// SubscriptionRepo 是订阅实例、待支付订单与计量的持久化抽象。
//
// 实现约定：Meter 与 ActivateFromOrder 必须**原子**完成读-判定-写（detailed-design §6.2
// 条件 UPDATE / 行锁），以保证并发下不击穿 month_limit、同 orderID 只建一个实例。
type SubscriptionRepo interface {
	// SavePendingPurchase 暂存下单意图（按 OrderID）。
	SavePendingPurchase(ctx context.Context, p *PendingPurchase) error
	// GetPendingPurchase 凭订单号取回购买意图；不存在返回 ErrSubscriptionNotFound。
	GetPendingPurchase(ctx context.Context, orderID string) (*PendingPurchase, error)
	// ActivateFromOrder 幂等创建：同 SourceOrderID 已存在则把既有实例回填进 sub 并返回
	// created=false；否则入库回填 sub.ID 返回 created=true。
	ActivateFromOrder(ctx context.Context, sub *Subscription) (created bool, err error)
	// GetActiveByUser 返回用户当前 active 订阅（按 now 惰性过期）；无 active 返回 (nil, nil)。
	GetActiveByUser(ctx context.Context, userID int64, now time.Time) (*Subscription, error)
	// GetByID 按 id 读取订阅；不存在返回 ErrSubscriptionNotFound。
	GetByID(ctx context.Context, id int64) (*Subscription, error)
	// Meter 原子条件累加：仅当 active 且未过期且 used+cost<=month_limit 才累加并写计量日志；
	// 超额置 exhausted 返回 ErrSubscriptionExhausted，过期置 expired 返回 ErrSubscriptionExpired。
	// 返回累加后的 used_usd。
	Meter(ctx context.Context, subID int64, costUSD float64, now time.Time) (newUsed float64, err error)
}

// PricingGuard 是设零售价时的成本保护校验（消费者定义接口）。
// 签名对齐 pricing.PricingGuard.ValidateRetailPrice，真实实现由 pricing 包提供并在 main 注入；
// 本包仅声明所需的最小契约，不 import 兄弟模块。
type PricingGuard interface {
	// ValidateRetailPrice 校验零售价满足 retail >= cost*(1+minMargin)；低于返回守卫错误。
	ValidateRetailPrice(retail, cost, minMargin float64) error
}

// PaymentGateway 是下单网关（消费者定义接口）。签名对齐 detailed-design §2.8 Payment。
type PaymentGateway interface {
	// CreateOrder 创建支付订单（type=subscription）并返回支付凭据。
	CreateOrder(ctx context.Context, in OrderInput) (*PayOrder, error)
}

// AtomicPurchaseGateway 是生产 GORM 支付桥可选实现的更强契约：把本地 SUB 订单与完整购买快照放在
// 同一数据库事务提交。Purchase 会优先使用本接口；纯内存/外部网关仍可只实现 PaymentGateway，走
// 既有顺序路径。实现必须为 pending 回填最终 OrderID，任一步失败不得留下孤立 pending 订单。
type AtomicPurchaseGateway interface {
	CreateOrderWithPending(ctx context.Context, in OrderInput, pending *PendingPurchase) (*PayOrder, error)
}

// RiskEngine 是限购校验（消费者定义接口）。Trial 等限购口径见 §7 默认 #4。
type RiskEngine interface {
	// CheckPurchaseLimit 触发限购返回 PURCHASE_LIMIT_EXCEEDED；放行返回 nil。
	CheckPurchaseLimit(ctx context.Context, in PurchaseLimitCheck) error
	// ReleasePurchaseClaim 归还本次已通过 CheckPurchaseLimit 的限购占用（补偿：下单/支付凭据
	// 创建失败时调用）。Trial 只删归属本用户的维度键；非 Trial 原子递减本次计数，不能整键清零。
	ReleasePurchaseClaim(ctx context.Context, in PurchaseLimitCheck) error
}

// EarningSink 接收代理收益记录（tokenplan_spread）。由 Agent 模块实现并在 main 注入。
type EarningSink interface {
	// AddEarning 写收益日志并增可提现余额；按 (SourceType, SourceID) 幂等。
	AddEarning(ctx context.Context, e EarningEntry) error
}
