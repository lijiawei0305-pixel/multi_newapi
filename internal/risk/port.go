package risk

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/internal/platform/appctx"
)

// ---- 对外接口（detailed-design §2.13）----

// RiskEngine 是风控对外契约：调用前校验、套餐限购、满额逼近告警。
// 由 *Engine 实现；RelayGateway 调用 CheckCall、TokenPlan 调用 CheckPurchaseLimit、Stats/计量调用 NoteUsage。
type RiskEngine interface {
	// CheckCall 调用前风控：租户/用户/Token 状态 + IP allowlist + RPM 限流（并发安全）。
	// 失败返回 STATUS_FORBIDDEN / IP_NOT_ALLOWED / RATE_LIMITED。
	CheckCall(ctx context.Context, p *appctx.Principal, rc CallContext) error
	// CheckPurchaseLimit Trial=用户∪实名∪设备各 1 次（三维去重，并发单赢家）；
	// 其它档按 Plan.PerUserLimit 限购。超限返回 PURCHASE_LIMIT_EXCEEDED。
	CheckPurchaseLimit(ctx context.Context, userID int64, plan Plan) error
	// NoteUsage used/limit 逼近阈值（默认 0.8）打标告警，按 subID 去重避免重复告警。
	NoteUsage(ctx context.Context, subID int64, used, limit float64)
}

// CallContext 是 CheckCall 的入参（对齐 relay.CallContext，detailed-design §2.13）：
// 携带本次调用的模型/入口/来源 IP/请求号，供状态、IP allowlist、RPM 限流使用。
type CallContext struct {
	Model     string
	Endpoint  string
	ClientIP  string
	RequestID string
}

// Plan 是 CheckPurchaseLimit 所需的本地最小套餐视图。
// 依据 detailed-design §1.4 不 import 兄弟模块 tokenplan，仅声明限购判定所需字段。
type Plan struct {
	// ID 套餐主键，用于非 Trial 档的限购计数键。
	ID int64
	// Code 套餐代码（如 "trial"、"mini"），便于审计/日志。
	Code string
	// Trial 是否引流体验款：需最强限购（用户∪实名∪设备各 1 次，proposal §2.4）。
	Trial bool
	// PerUserLimit 非 Trial 档的每用户购买上限；<=0 表示不限购。
	PerUserLimit int
}

// Alert 是 NoteUsage 逼近阈值时下发的告警条目。
type Alert struct {
	SubscriptionID int64
	Used           float64
	Limit          float64
	Ratio          float64   // used/limit
	Threshold      float64   // 触发阈值（如 0.8）
	At             time.Time // 触发时间（注入时钟）
}

// ---- 配置 ----

// Config 是风控引擎的可调参数。零值经 normalize 回退到 DefaultConfig 的安全默认。
type Config struct {
	// DefaultRPM 默认每窗口请求上限；<=0 表示不限流（由 RPMResolver 覆盖具体主体）。
	DefaultRPM int
	// RateWindow 限流窗口长度，默认 1 分钟（固定窗口计数）。
	RateWindow time.Duration
	// AlertThreshold NoteUsage 告警阈值占比，默认 0.8。
	AlertThreshold float64
	// PurchaseDedupTTL 限购去重键 TTL；<=0 表示永不过期（Trial 终身限购）。
	PurchaseDedupTTL time.Duration
}

// DefaultConfig 返回安全默认配置。
func DefaultConfig() Config {
	return Config{
		DefaultRPM:       0, // 默认不限流；真实部署经 RPMResolver 注入每 Token RPM
		RateWindow:       time.Minute,
		AlertThreshold:   0.8,
		PurchaseDedupTTL: 0, // Trial 终身限购，键不过期
	}
}

// normalize 用合法默认补齐非法/缺省字段。
func (c Config) normalize() Config {
	if c.RateWindow <= 0 {
		c.RateWindow = time.Minute
	}
	if c.AlertThreshold <= 0 || c.AlertThreshold > 1 {
		c.AlertThreshold = 0.8
	}
	return c
}

// ---- 消费者定义接口（本包声明其依赖，运行时由 cmd/main 注入实现）----
// 依据 detailed-design §1.4：模块只 import 自己声明的接口，不 import 兄弟业务模块。

// KVCache 是限流/限购计数与去重的键值抽象（detailed-design §2.13 依赖：KVCache）。
// 本轮提供并发安全内存假实现 MemKVCache；真实 Redis 适配顺延（见报告 TODO）。
// 约定：Incr 原子自增，SetNX“不存在才写入”，二者并发安全以保证限流计数与限购单赢家。
type KVCache interface {
	// Incr 原子自增 key 的计数并返回新值；key 不存在视为从 0 自增到 1。
	Incr(ctx context.Context, key string) (int64, error)
	// Get 读取 SetNX 写入的值；found=false 表示 key 不存在或已过期。
	Get(ctx context.Context, key string) (val string, found bool, err error)
	// SetNX 仅当 key 不存在时写入 val 并返回 true；已存在返回 false。ttl<=0 表示不过期。
	SetNX(ctx context.Context, key, val string, ttl time.Duration) (bool, error)
	// Expire 为已存在的 key 设置/刷新过期时长；ttl<=0 表示清除过期。
	Expire(ctx context.Context, key string, ttl time.Duration) error
}

// Clock 是可注入时钟，便于限流窗口与去重 TTL 的确定性单测（detailed-design §2.13 单测策略）。
type Clock interface {
	Now() time.Time
}

// StatusChecker 校验调用主体（租户/用户/Token）是否处于可调用状态（消费者定义接口）。
// 返回 false 触发 STATUS_FORBIDDEN。可注入 nil 跳过（早期装配/无需状态校验场景）。
// 真实实现据 RiskRepo/Identity 判定封禁；本轮提供内存假实现 MemStatusChecker。
type StatusChecker interface {
	Active(ctx context.Context, p *appctx.Principal) (bool, error)
}

// IPAllowlist 校验客户端 IP 是否在主体 allowlist 内（消费者定义接口）。
// 主体未配置 allowlist 视为不限制（返回 true）。不允许触发 IP_NOT_ALLOWED。
// 可注入 nil 跳过。本轮提供内存假实现 MemIPAllowlist（支持精确 IP + CIDR）。
type IPAllowlist interface {
	Allowed(ctx context.Context, p *appctx.Principal, ip string) (bool, error)
}

// RPMResolver 返回某主体适用的每窗口请求上限（消费者定义接口）。
// ok=false 回退 Config.DefaultRPM。可注入 nil（恒用默认）。真实实现据 Token/用户组配置解析。
type RPMResolver interface {
	RPMFor(ctx context.Context, p *appctx.Principal) (limit int, ok bool, err error)
}

// AlertSink 接收 NoteUsage 的满额逼近告警（消费者定义接口）。可注入 nil（仅去重、不下发）。
// 真实实现写告警表/通知；本轮提供内存假实现 MemAlertSink。
type AlertSink interface {
	Fire(ctx context.Context, a Alert)
}
