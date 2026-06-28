package stats

import "context"

// ---- 对外接口（doc/detailed-design.md §2.12）----

// StatsService 是统计看板：只读聚合主站全局/代理本站数据，并对 tokenplan 订阅
// 满额逼近做预警（呼应 doc/proposal.md §2.4「防巨亏」）。
//
// 本服务只读、无副作用：不写库、不做扣费/状态变更。鉴权（管理员/代理 owner）
// 在 handler 前置（见报告 TODO）；TenantOverview 在 service 层附带一层防御性的
// 跨租户隔离校验（见 service.go）。
type StatsService interface {
	// AdminOverview 返回主站全局统计快照（全站租户数/活跃数/调用量/扣费/代理收益）。
	AdminOverview(ctx context.Context) (*AdminStats, error)
	// TenantOverview 返回单个租户的统计快照，严格限定在该租户范围内（隔离）。
	TenantOverview(ctx context.Context, tenantID int64) (*TenantStats, error)
	// SubscriptionAlerts 返回 used_usd/month_limit 逼近（≥ 阈值，默认 0.8）的订阅预警列表。
	SubscriptionAlerts(ctx context.Context) ([]SubAlert, error)
}

// ---- 对外 DTO ----

// AdminStats 是主站全局统计快照（doc/detailed-design.md §2.12）。
type AdminStats struct {
	// TotalTenants 全站租户数。
	TotalTenants int64
	// ActiveTenants 处于 active 状态的租户数（活跃数）。
	ActiveTenants int64
	// TotalCalls 全站调用量（次数）。
	TotalCalls int64
	// TotalChargedUSD 全站扣费总额（用户侧消耗，源自 tenant_billing_logs.charged_quota）。
	TotalChargedUSD float64
	// TotalEarningUSD 全站代理收益总额（充值差价 + 消耗分润 + tokenplan 差价/分润）。
	TotalEarningUSD float64
}

// TenantStats 是单个租户（代理本站）的统计快照，仅含本租户范围数据。
type TenantStats struct {
	// TenantID 该统计归属的租户。
	TenantID int64
	// UserCount 本站用户数。
	UserCount int64
	// TokenCount 本站 API Token(Key) 数。
	TokenCount int64
	// Calls 本站调用量（次数）。
	Calls int64
	// RevenueUSD 本站收入（用户实付/充值口径）。
	RevenueUSD float64
	// EarningUSD 本站代理收益（差价 + 分润）。
	EarningUSD float64
}

// SubAlert 是一条 tokenplan 订阅满额逼近预警（doc/proposal.md §2.4 / §16）。
type SubAlert struct {
	// SubscriptionID 订阅实例 id（user_subscriptions.id）。
	SubscriptionID int64
	// TenantID/UserID 归属租户与用户。
	TenantID int64
	UserID   int64
	// PlanCode 套餐代码（trial/mini/.../max）。
	PlanCode string
	// UsedUSD/MonthLimitUSD 已消耗与月度封顶。
	UsedUSD       float64
	MonthLimitUSD float64
	// Ratio 用量占比 used/limit（[0, 1+]；limit<=0 且有消耗时记为 1.0，见 usageRatio）。
	Ratio float64
}

// ---- 消费者定义接口（本模块声明其只读依赖，运行时由 cmd/main 注入实现）----
// 依据 doc/detailed-design.md §1.4 / §2.12：模块只 import 自己声明的接口，不 import 兄弟模块。
// 真实实现为只读 GORM 仓储（或读副本），对 tenant_billing_logs / user_subscriptions 等做
// COUNT/SUM/GROUP BY 聚合；本轮提供并发安全内存假实现（见 reader.go）。

// BillingReader 提供按租户汇总的只读计费投影（源自 tenant_billing_logs 及租户维度计数）。
type BillingReader interface {
	// TenantBillings 返回全站「每租户一行」的计费汇总，供 AdminOverview 跨租户聚合。
	TenantBillings(ctx context.Context) ([]TenantBilling, error)
	// TenantBillingByID 返回指定租户的计费汇总（隔离：只读该租户）。
	// 该租户无数据时返回零值汇总（Calls=0），以便看板显示 0，而非报错。
	TenantBillingByID(ctx context.Context, tenantID int64) (TenantBilling, error)
}

// SubscriptionReader 提供只读的订阅用量投影（源自 user_subscriptions，§6.19）。
type SubscriptionReader interface {
	// ActiveSubscriptions 返回全站 active 订阅的用量投影，供满额预警计算。
	// 仅 active（未过期、未耗尽）订阅纳入监控：终态订阅已自然停用，不再产生新增风险。
	ActiveSubscriptions(ctx context.Context) ([]SubscriptionUsage, error)
}

// ---- 只读投影类型（read-model；stats 本地定义，避免 import 兄弟模块）----

// TenantBilling 是单个租户的计费只读汇总投影。
//
// 真实实现来源：调用量/扣费/收益来自 tenant_billing_logs 与 agent_earning_logs 的
// 按租户聚合；UserCount/TokenCount 来自 tenant_users / tenant_tokens 计数；
// Active 来自 tenants.status。本轮由内存假实现填充（见 reader.go）。
type TenantBilling struct {
	// TenantID 租户 id（作为唯一聚合键）。
	TenantID int64
	// Active 租户是否处于 active 状态（用于全站活跃数统计）。
	Active bool
	// UserCount/TokenCount 本租户用户数与 Token 数。
	UserCount  int64
	TokenCount int64
	// Calls 本租户调用量（次数）。
	Calls int64
	// ChargedUSD 本租户用户侧消耗（扣费）总额。
	ChargedUSD float64
	// RevenueUSD 本租户收入（实付/充值口径）总额。
	RevenueUSD float64
	// EarningUSD 本租户代理收益（差价 + 分润）总额。
	EarningUSD float64
}

// SubscriptionUsage 是单条订阅的用量只读投影（满额预警的输入）。
type SubscriptionUsage struct {
	// SubscriptionID 订阅实例 id。
	SubscriptionID int64
	// TenantID/UserID 归属。
	TenantID int64
	UserID   int64
	// PlanCode 套餐代码。
	PlanCode string
	// UsedUSD/MonthLimitUSD 已消耗与月度封顶（USD）。
	UsedUSD       float64
	MonthLimitUSD float64
}
