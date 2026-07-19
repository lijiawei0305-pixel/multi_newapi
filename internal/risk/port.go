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

// DefaultDeviceDedupTTL 是 Trial 限购**设备维度**去重键的默认有界 TTL（24 小时）。
// 见 Config.DeviceDedupTTL 注释：device 维绝不用终身键（audit R1）。
const DefaultDeviceDedupTTL = 24 * time.Hour

// Config 是风控引擎的可调参数。零值经 normalize 回退到 DefaultConfig 的安全默认。
type Config struct {
	// DefaultRPM 默认每窗口请求上限；<=0 表示不限流（由 RPMResolver 覆盖具体主体）。
	DefaultRPM int
	// RateWindow 限流窗口长度，默认 1 分钟（固定窗口计数）。
	RateWindow time.Duration
	// AlertThreshold NoteUsage 告警阈值占比，默认 0.8。
	AlertThreshold float64
	// PurchaseDedupTTL 限购去重键 TTL；<=0 表示永不过期。用于**用户维度与实名维度**——
	// 同一账号 / 同一自然人不该无限领 Trial，故这两维保留终身键。
	PurchaseDedupTTL time.Duration
	// DeviceDedupTTL 是 Trial 限购**设备维度**去重键的 TTL，**必须有界、绝不终身**。
	// 设备维度由服务端从粗粒度、跨真人共享的 ClientIP（运营商 CGNAT / NAT 出口）派生
	// （见 mtwire.deviceFingerprint）；若沿用 PurchaseDedupTTL=0 的终身键，则同一出口 IP
	// 的首个买家占键后，其余共享该 IP 的真实账号会被**永久**连坐拒绝 Trial、换账号换设备
	// 都无效，只能人工客服解套（audit R1 · High · 已 live 生产）。有界 TTL 仍能拦「同 IP
	// 短时批量刷」这一真实滥用，而长期误伤到点自动过期自愈。<=0 经 normalize 回落
	// DefaultDeviceDedupTTL（24h），**绝不**落成终身——这样生产（wire.go 仅注入 DefaultRPM）
	// 也自动拿到有界 TTL。
	DeviceDedupTTL time.Duration
}

// DefaultConfig 返回安全默认配置。
func DefaultConfig() Config {
	return Config{
		DefaultRPM:       0, // 默认不限流；真实部署经 RPMResolver 注入每 Token RPM
		RateWindow:       time.Minute,
		AlertThreshold:   0.8,
		PurchaseDedupTTL: 0,                     // 用户/实名维度：终身限购，键不过期
		DeviceDedupTTL:   DefaultDeviceDedupTTL, // 设备维度：有界 TTL，绝不终身（audit R1）
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
	if c.DeviceDedupTTL <= 0 {
		c.DeviceDedupTTL = DefaultDeviceDedupTTL // device 维绝不终身（audit R1）：<=0 一律回落有界默认
	}
	return c
}

// ---- 消费者定义接口（本包声明其依赖，运行时由 cmd/main 注入实现）----
// 依据 detailed-design §1.4：模块只 import 自己声明的接口，不 import 兄弟业务模块。

// KVCache 是限流/限购计数与去重的键值抽象（detailed-design §2.13 依赖：KVCache）。
// MemKVCache 用于确定性测试/单进程回退，RedisKVCache 用于多副本生产部署。
// 约定：Incr/Decr 均为原子计数操作，SetNX“不存在才写入”；三者并发安全以保证限流计数、
// 限购补偿与去重单赢家。Decr 到 0 时删除 key，避免留下无意义的零值永久键。
type KVCache interface {
	// Incr 原子自增 key 的计数并返回新值；key 不存在视为从 0 自增到 1。
	Incr(ctx context.Context, key string) (int64, error)
	// Decr 原子递减已存在的计数并返回不小于 0 的新值；key 不存在返回 0。
	// 递减结果 <=0 时在同一原子操作内删除 key。
	Decr(ctx context.Context, key string) (int64, error)
	// Get 读取 SetNX 写入的值；found=false 表示 key 不存在或已过期。
	Get(ctx context.Context, key string) (val string, found bool, err error)
	// SetNX 仅当 key 不存在时写入 val 并返回 true；已存在返回 false。ttl<=0 表示不过期。
	SetNX(ctx context.Context, key, val string, ttl time.Duration) (bool, error)
	// Expire 为已存在的 key 设置/刷新过期时长；ttl<=0 表示清除过期。
	Expire(ctx context.Context, key string, ttl time.Duration) error
	// Del 删除一个或多个 key（幂等：不存在的 key 忽略，不报错；空列表 no-op）。
	// 补上此前结构性缺失的**释放原语**：KVCache 曾只增（Incr/SetNX）不删 → 被误占用的限购去重键
	// （用户点开收银台未付款即消耗、TTL=0 永久）在类型层面无法释放，后台零手段（RETRO 2026-07-16 · Critical）。
	Del(ctx context.Context, keys ...string) error
}

// PurchaseLimitAdmin 暴露限购去重键的**后台释放**能力，由 *Engine 实现。
// 用途：补偿「Purchase 在下单前即 SetNX 写永久去重键、KVCache 无 Del → 犹豫关单即永久消耗、
// 后台无手段可解」的误占用（RETRO 2026-07-16 · Critical）。经带鉴权 + 审计日志的后台端点调用，
// 替代客服直连无密码/无卷/无审计的 Redis 删键。
type PurchaseLimitAdmin interface {
	// ReleaseTrialLimit 释放某用户的 Trial 三维去重键（用户维度 + 传入的实名/设备维度），
	// 使其可重新购买 Trial。实名/设备为空则只释放用户维度（与 checkTrialLimit 建键口径对称）。
	//
	// 共享维度（realname/device）带**归属校验**：键值记录占用者 userID，值不符（含遗留
	// 无归属值）默认拒删、计入 Skipped——防止按调用方自报的 realname/device 误删他人
	// 合法占用的反刷键。force=true 绕过归属校验（仅限客服人工核实的遗留键，须审计留痕）。
	ReleaseTrialLimit(ctx context.Context, userID int64, pi PurchaseIdentity, force bool) (TrialReleaseResult, error)
	// ReleasePurchaseLimit 释放某用户对某非 Trial 套餐的每用户限购计数键。
	ReleasePurchaseLimit(ctx context.Context, planID, userID int64) error
}

// PurchaseLimitCompensator 只归还本次非 Trial CheckPurchaseLimit 成功占用的一次计数。
// 它与后台 ReleasePurchaseLimit（整键清零）刻意分开，避免失败订单的补偿误删同用户其它成功购买。
type PurchaseLimitCompensator interface {
	RollbackPurchaseLimit(ctx context.Context, planID, userID int64) error
}

// TrialReleaseResult 是 ReleaseTrialLimit 的逐维度结果：让后台端点能如实回报
// 「哪些维度真的释放了、哪些因归属不符被拒」，而非笼统的 released=true。
type TrialReleaseResult struct {
	// Released 实际删除的维度名（"user"/"realname"/"device"）。user 恒在列
	//（该维度键按 userID 建键、无跨用户共享，恒删且 Del 幂等）。
	Released []string
	// Skipped 因键值归属 != userID 而拒删的共享维度名（含遗留无归属键；force 可绕过）。
	Skipped []string
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
