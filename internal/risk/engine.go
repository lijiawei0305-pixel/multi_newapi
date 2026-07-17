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
// 一个键都未占、连补偿都无需触发；跨账号撞实名/撞设备的败者则对**本次已抢占成功**的
// 维度做补偿删除（claimed 回滚）——被删键值都是本人 userID（刚由本次 SetNX 写入），
// 不会误删赢家或他人的占用。不补偿的历史版本会把败者的 userK 永久烧掉：设备指纹按
// ClientIP+UA 派生（粗粒度），共享出口 IP 的无辜用户一次碰撞即终身买不了 Trial、只能
// 走客服释放。曾以「KVCache 无删除原语」论证补偿不可能（3dd6b4f），该前提在同批 Del
// 落地（714aa6d，port.go）后即不成立，勿再引用。补偿是尽力而为：Del 失败仅多留痕
// （fail-closed 方向，可经 ReleaseTrialLimit 后台解）；SetNX 成功→Del 之间键被管理员
// 释放又被他人重占的误删窗口，与 ReleaseTrialLimit 的 Get→Del 竞态同理接受（须管理员
// 操作恰挤进微秒级间隙）。
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
	// 键值写占用者 userID（非无意义的 "1"）：realname/device 维度跨用户共享，
	// 后台释放（ReleaseTrialLimit）必须能校验「这把键真是该用户占的」，否则
	// 客服按 B 自报的 device_id 释放会误删 A 合法占用的键 → 新账号可从已消耗
	// 设备再领 Trial，反刷维度被客服通道洗掉。
	owner := strconv.FormatInt(userID, 10)
	var claimed []string // 本次已抢占成功的键：判负/出错时补偿删除（Del 空列表为 no-op）
	for _, k := range []string{userK, realK, devK} {
		if k == "" {
			continue
		}
		ok, err := e.kv.SetNX(ctx, k, owner, ttl)
		if err != nil {
			_ = e.kv.Del(ctx, claimed...)
			return err
		}
		if !ok {
			_ = e.kv.Del(ctx, claimed...)
			return ErrPurchaseLimitExceeded
		}
		claimed = append(claimed, k)
	}
	return nil
}

// 编译期断言：*Engine 亦满足后台释放契约（限购键补偿释放，见 port.go PurchaseLimitAdmin）。
var _ PurchaseLimitAdmin = (*Engine)(nil)

// ReleaseTrialLimit 释放某用户的 Trial 三维去重键（用户维度 + 传入的实名/设备维度）。
// 用途：后台补偿「点开收银台未付款即永久消耗 Trial 终身限购、无释放路径」的误占用——删掉对应维度键后，
// 该用户即可重新购买 Trial（RETRO 2026-07-16 · Critical）。pi 的实名/设备为空则只释放用户维度
// （与 checkTrialLimit 建键口径对称：空维度不建亦不删）。Del 幂等：键本就不存在也不报错。
//
// 归属校验（默认拒删，force 显式绕过）：realname/device 维度**跨用户共享**，键值即占用者
// userID（checkTrialLimit 写入）。释放前逐键 Get 校验值==userID——不符（含旧版无归属的
// 遗留值 "1"）即跳过并计入 Skipped，防止「客服按 B 自报的 device_id 释放 B」误删 A 合法
// 占用的键、令新账号可从已消耗设备再领 Trial。force=true 跳过归属校验（唯一合法用途：
// 释放遗留 "1" 键——归属不可考但客服已人工核实；调用方必须审计留痕）。用户维度键
// （trialKey("user", userID)）本身按 userID 建键、无跨用户共享问题，恒删。
//
// Get→Del 非原子：窗口内键被并发释放又被他人 SetNX 重占时会误删新占用。该窗口仅在
// 「两个管理员并发释放同一键 + 恰有购买挤进微秒级间隙」时存在，且本端点为人工低频
// 客服操作，接受此竞态；热路径决胜（checkTrialLimit）仍完全建立在 SetNX 原子返回值上。
func (e *Engine) ReleaseTrialLimit(ctx context.Context, userID int64, pi PurchaseIdentity, force bool) (TrialReleaseResult, error) {
	owner := strconv.FormatInt(userID, 10)
	res := TrialReleaseResult{}
	keys := []string{trialKey("user", owner)}
	res.Released = append(res.Released, "user")

	shared := []struct{ dim, id string }{
		{"realname", pi.RealNameID},
		{"device", pi.DeviceID},
	}
	for _, s := range shared {
		if s.id == "" {
			continue
		}
		k := trialKey(s.dim, s.id)
		val, found, err := e.kv.Get(ctx, k)
		if err != nil {
			return TrialReleaseResult{}, err
		}
		if !found {
			continue // 键不存在：无需释放（幂等），不计入任何列表
		}
		if val != owner && !force {
			res.Skipped = append(res.Skipped, s.dim)
			continue
		}
		keys = append(keys, k)
		res.Released = append(res.Released, s.dim)
	}
	if err := e.kv.Del(ctx, keys...); err != nil {
		return TrialReleaseResult{}, err
	}
	return res, nil
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
// 铁律：这两个字段必须由**服务端派生**，绝不承载客户端自报值——反滥用维度若由被监管方自报，
// 即可被随机化绕过、或被填入受害者标识反向武器化（RETRO 2026-07-17）。注入点见
// mtwire.HandlePurchase：DeviceID 由 deviceFingerprint(c) 从 ClientIP+User-Agent 派生。
type PurchaseIdentity struct {
	// RealNameID 实名标识（同一身份证/手机号归一）；空表示未实名，跳过该维度。
	// **当前恒空**：暂无可信的服务端实名来源（无 KYC 装配），故实名维度未启用；
	// 待接入可信身份系统后，须由 authenticated user 的服务端记录派生，绝不读客户端串。
	RealNameID string
	// DeviceID 设备指纹（UA+IP 等归一，服务端派生）；空表示无信号，跳过该维度。
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
