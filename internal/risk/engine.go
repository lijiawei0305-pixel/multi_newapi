package risk

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/internal/platform/appctx"
)

// Engine 是 RiskEngine 的实现。KVCache 为必需依赖（限流/限购计数）；
// StatusChecker / IPAllowlist / RPMResolver / AlertSink 为可选（nil 即跳过对应校验/告警）。
type Engine struct {
	kv     KVCache
	clock  Clock
	status StatusChecker
	ips    IPAllowlist
	rpm    RPMResolver
	alerts AlertSink
	cfg    Config
}

// 编译期断言：*Engine 满足对外契约。
var _ RiskEngine = (*Engine)(nil)

// Option 配置 Engine 的可选依赖与参数。
type Option func(*Engine)

// WithClock 注入时钟（默认系统时钟）。
func WithClock(c Clock) Option { return func(e *Engine) { e.clock = c } }

// WithStatusChecker 注入状态校验（默认 nil = 跳过）。
func WithStatusChecker(s StatusChecker) Option { return func(e *Engine) { e.status = s } }

// WithIPAllowlist 注入 IP allowlist（默认 nil = 跳过）。
func WithIPAllowlist(a IPAllowlist) Option { return func(e *Engine) { e.ips = a } }

// WithRPMResolver 注入每主体 RPM 解析（默认 nil = 用 Config.DefaultRPM）。
func WithRPMResolver(r RPMResolver) Option { return func(e *Engine) { e.rpm = r } }

// WithAlertSink 注入告警下沉（默认 nil = 仅去重不下发）。
func WithAlertSink(a AlertSink) Option { return func(e *Engine) { e.alerts = a } }

// WithConfig 覆盖默认配置（自动 normalize 非法字段）。
func WithConfig(cfg Config) Option { return func(e *Engine) { e.cfg = cfg.normalize() } }

// NewEngine 构造风控引擎。kv 必需；其余依赖经 Option 注入，未注入则跳过/用默认。
func NewEngine(kv KVCache, opts ...Option) *Engine {
	e := &Engine{
		kv:    kv,
		clock: systemClock{},
		cfg:   DefaultConfig(),
	}
	for _, opt := range opts {
		opt(e)
	}
	if e.clock == nil {
		e.clock = systemClock{}
	}
	return e
}

// CheckCall 调用前风控，顺序：状态 → IP allowlist → RPM 限流（限流置末以免对被拒请求计数）。
func (e *Engine) CheckCall(ctx context.Context, p *appctx.Principal, rc CallContext) error {
	if p == nil {
		return ErrStatusForbidden
	}
	// 1) 租户/用户/Token 状态。
	if e.status != nil {
		ok, err := e.status.Active(ctx, p)
		if err != nil {
			return err
		}
		if !ok {
			return ErrStatusForbidden
		}
	}
	// 2) IP allowlist（未配置视为放行）。
	if e.ips != nil {
		ok, err := e.ips.Allowed(ctx, p, rc.ClientIP)
		if err != nil {
			return err
		}
		if !ok {
			return ErrIPNotAllowed
		}
	}
	// 3) RPM 固定窗口限流。
	return e.checkRPM(ctx, p)
}

// checkRPM 固定窗口计数限流：key 含窗口序号，首次命中设置 TTL，超限返回 RATE_LIMITED。
func (e *Engine) checkRPM(ctx context.Context, p *appctx.Principal) error {
	limit := e.cfg.DefaultRPM
	if e.rpm != nil {
		l, ok, err := e.rpm.RPMFor(ctx, p)
		if err != nil {
			return err
		}
		if ok {
			limit = l
		}
	}
	if limit <= 0 {
		return nil // 不限流
	}
	windowSec := int64(e.cfg.RateWindow / time.Second)
	if windowSec <= 0 {
		windowSec = 60
	}
	idx := e.clock.Now().Unix() / windowSec
	key := rpmKey(p.TenantID, p.UserID, idx)
	n, err := e.kv.Incr(ctx, key)
	if err != nil {
		return err
	}
	if n == 1 {
		// 首次命中该窗口：设置过期，窗口结束后计数自动回收。
		_ = e.kv.Expire(ctx, key, e.cfg.RateWindow)
	}
	if n > int64(limit) {
		return ErrRateLimited
	}
	return nil
}

// CheckPurchaseLimit Trial 三维去重（用户∪实名∪设备各 1 次，并发单赢家）；其它档按 PerUserLimit 限购。
func (e *Engine) CheckPurchaseLimit(ctx context.Context, userID int64, plan Plan) error {
	if plan.Trial {
		return e.checkTrialLimit(ctx, userID)
	}
	if plan.PerUserLimit <= 0 {
		return nil // 不限购
	}
	key := purchaseKey(plan.ID, userID)
	n, err := e.kv.Incr(ctx, key)
	if err != nil {
		return err
	}
	if n == 1 && e.cfg.PurchaseDedupTTL > 0 {
		_ = e.kv.Expire(ctx, key, e.cfg.PurchaseDedupTTL)
	}
	if n > int64(plan.PerUserLimit) {
		return ErrPurchaseLimitExceeded
	}
	return nil
}

// checkTrialLimit 实现 Trial 的用户∪实名∪设备三维去重（并发单赢家）：
// 三个维度各自用 SetNX 作决胜锁——任一维度已被占用（SetNX 返回 false）即拒。
// SetNX 是"不存在才写入"的原子 check-and-set，故并发下每个共享维度（同实名/同设备）
// 只有 1 个请求能占用成功，其余全部判负 → 天然单赢家。**不再先 Get 预检**：旧实现
// 的 Get 预检与 SetNX 之间无原子性，且只以 userK 作决胜锁（每用户 key 不同、跨账号不
// 串行化任何东西），realname/设备维度的 SetNX 返回值被丢弃 → 同设备/同实名的多账号
// 并发可各自拿到一份 Trial（绕过反刷，proposal §2.4）。此处令每个维度的 SetNX 返回值
// 都参与决胜，堵死该窗口。
//
// 键顺序 [用户, 实名, 设备] 有意为之：同一用户的并发重复请求在第一步 userK 即判负、
// 不留任何痕迹。判负/报错时把本次调用已占到的前序维度键补偿删除（Del 幂等）——只删自己
// 真正 SetNX 成功的键、绝不碰既得赢家的键，故单赢家性质不变（最后一个非空维度的持有者
// 恒胜出）。若不回滚，Trial 终身键（PurchaseDedupTTL=0）会把败者的 userK 与跨账号共享的
// realK 永久写死：家用共享设备/同实名多账号下，从未买到过 Trial 的无辜用户换干净设备、
// 换新账号都被永久拒（audit 2026-07-17 发现#3）。回滚后最坏情况只是并发同侪的一次瞬时
// 且自愈的误拒，严格优于永久误封；回滚 Del 本身失败的残留仍可经 ReleaseTrialLimit 后台释放。
//
// 实名/设备从 context 读取（请求级，经 WithPurchaseIdentity 注入），缺省维度跳过。
func (e *Engine) checkTrialLimit(ctx context.Context, userID int64) error {
	pi, _ := purchaseIdentityFrom(ctx)
	ttl := e.cfg.PurchaseDedupTTL

	userK := trialKey("user", strconv.FormatInt(userID, 10))
	var realK, devK string
	if pi.RealNameID != "" {
		realK = trialKey("realname", pi.RealNameID)
	}
	if pi.DeviceID != "" {
		devK = trialKey("device", pi.DeviceID)
	}

	// 逐维 SetNX 决胜：任一维度 SetNX 返回 false（已被占用）即拒。
	// userK 置首：同用户并发重复请求在此即判负，不会在实名/设备维度留痕。
	// 判负/报错即回滚 acquired（仅本次真正占到的键），败者不留任何永久残留。
	var acquired []string
	for _, k := range []string{userK, realK, devK} {
		if k == "" {
			continue
		}
		ok, err := e.kv.SetNX(ctx, k, "1", ttl)
		if err != nil || !ok {
			if len(acquired) > 0 {
				_ = e.kv.Del(ctx, acquired...)
			}
			if err != nil {
				return err
			}
			return ErrPurchaseLimitExceeded
		}
		acquired = append(acquired, k)
	}
	return nil
}

// 编译期断言：*Engine 亦满足后台释放契约（限购键补偿释放，见 port.go PurchaseLimitAdmin）。
var _ PurchaseLimitAdmin = (*Engine)(nil)

// ReleaseTrialLimit 释放某用户的 Trial 三维去重键（用户维度 + 传入的实名/设备维度）。
// 用途：后台补偿「点开收银台未付款即永久消耗 Trial 终身限购、无释放路径」的误占用——删掉对应维度键后，
// 该用户即可重新购买 Trial（RETRO 2026-07-16 · Critical）。pi 的实名/设备为空则只释放用户维度
// （与 checkTrialLimit 建键口径对称：空维度不建亦不删）。Del 幂等：键本就不存在也不报错。
func (e *Engine) ReleaseTrialLimit(ctx context.Context, userID int64, pi PurchaseIdentity) error {
	keys := []string{trialKey("user", strconv.FormatInt(userID, 10))}
	if pi.RealNameID != "" {
		keys = append(keys, trialKey("realname", pi.RealNameID))
	}
	if pi.DeviceID != "" {
		keys = append(keys, trialKey("device", pi.DeviceID))
	}
	return e.kv.Del(ctx, keys...)
}

// ReleasePurchaseLimit 释放某用户对某非 Trial 套餐的每用户限购计数键（purchaseKey）。
func (e *Engine) ReleasePurchaseLimit(ctx context.Context, planID, userID int64) error {
	return e.kv.Del(ctx, purchaseKey(planID, userID))
}

// NoteUsage used/limit 逼近阈值时打标告警；按 subID 去重（SetNX）确保只告警一次。
func (e *Engine) NoteUsage(ctx context.Context, subID int64, used, limit float64) {
	if limit <= 0 {
		return // 无月限额，无从计算占比
	}
	ratio := used / limit
	if ratio < e.cfg.AlertThreshold {
		return
	}
	// 去重：首次跨过阈值才真正下发。
	first, err := e.kv.SetNX(ctx, alertKey(subID), "1", 0)
	if err != nil || !first {
		return
	}
	if e.alerts != nil {
		e.alerts.Fire(ctx, Alert{
			SubscriptionID: subID,
			Used:           used,
			Limit:          limit,
			Ratio:          ratio,
			Threshold:      e.cfg.AlertThreshold,
			At:             e.clock.Now(),
		})
	}
}

// ---- 键命名（risk: 前缀，避免与其它模块的 KV 键冲突）----

func rpmKey(tenantID, userID, idx int64) string {
	return fmt.Sprintf("risk:rpm:%d:%d:%d", tenantID, userID, idx)
}

func trialKey(dim, id string) string {
	return fmt.Sprintf("risk:trial:%s:%s", dim, id)
}

func purchaseKey(planID, userID int64) string {
	return fmt.Sprintf("risk:purchase:%d:user:%d", planID, userID)
}

func alertKey(subID int64) string {
	return fmt.Sprintf("risk:alert:sub:%d", subID)
}

// ---- 购买身份（请求级，经 context 传入限购去重所需的实名/设备维度）----

// PurchaseIdentity 承载 Trial 限购去重所需的实名标识与设备指纹（CheckPurchaseLimit 签名仅含 userID，
// 故实名/设备经请求级 context 传入，detailed-design §2.13 三维去重）。
type PurchaseIdentity struct {
	// RealNameID 实名标识（同一身份证/手机号归一）；空表示未实名，跳过该维度。
	RealNameID string
	// DeviceID 设备指纹（UA+IP 等归一）；空表示无指纹，跳过该维度。
	DeviceID string
}

type purchaseIDKey struct{}

// WithPurchaseIdentity 在 context 上附加购买身份，供 CheckPurchaseLimit 读取实名/设备维度。
func WithPurchaseIdentity(ctx context.Context, pi PurchaseIdentity) context.Context {
	return context.WithValue(ctx, purchaseIDKey{}, pi)
}

// purchaseIdentityFrom 读取 context 中的购买身份；不存在返回零值 + false。
func purchaseIdentityFrom(ctx context.Context) (PurchaseIdentity, bool) {
	pi, ok := ctx.Value(purchaseIDKey{}).(PurchaseIdentity)
	return pi, ok
}
