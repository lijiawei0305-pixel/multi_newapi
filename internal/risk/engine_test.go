package risk

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/platform/appctx"
	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

// ---- 测试辅助 ----

// manualClock 是可手动推进的注入时钟（并发安全，供 -race 测试）。
type manualClock struct {
	mu sync.Mutex
	t  time.Time
}

func newManualClock() *manualClock {
	return &manualClock{t: time.Unix(1_700_000_000, 0)}
}

func (c *manualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *manualClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// assertCode 断言 err 的 apperr 错误码等于 wantCode；wantCode=="" 表示期望无错误。
func assertCode(t *testing.T, err error, wantCode string) {
	t.Helper()
	if wantCode == "" {
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		return
	}
	if got := apperr.CodeOf(err); got != wantCode {
		t.Fatalf("error code = %q, want %q (err=%v)", got, wantCode, err)
	}
}

var errBoom = errors.New("boom")

// kvErrOn 包装一个真实 KVCache，对选定方法注入错误，用于测试错误透传。
type kvErrOn struct {
	KVCache
	incr, get, setnx, expire bool
}

func (k kvErrOn) Incr(ctx context.Context, key string) (int64, error) {
	if k.incr {
		return 0, errBoom
	}
	return k.KVCache.Incr(ctx, key)
}

func (k kvErrOn) Get(ctx context.Context, key string) (string, bool, error) {
	if k.get {
		return "", false, errBoom
	}
	return k.KVCache.Get(ctx, key)
}

func (k kvErrOn) SetNX(ctx context.Context, key, val string, ttl time.Duration) (bool, error) {
	if k.setnx {
		return false, errBoom
	}
	return k.KVCache.SetNX(ctx, key, val, ttl)
}

// kvSetNXErrOnKey 包装真实 KVCache，仅对指定 key 的 SetNX 注入错误（模拟多维决胜循环中途某一维报错）。
type kvSetNXErrOnKey struct {
	KVCache
	failKey string
}

func (k kvSetNXErrOnKey) SetNX(ctx context.Context, key, val string, ttl time.Duration) (bool, error) {
	if key == k.failKey {
		return false, errBoom
	}
	return k.KVCache.SetNX(ctx, key, val, ttl)
}

// kvSetNXErrOnce 包装真实 KVCache，仅第 at 次 SetNX 调用报错一次（模拟 Redis 瞬时抖动后恢复）。
type kvSetNXErrOnce struct {
	KVCache
	calls int
	at    int
}

func (k *kvSetNXErrOnce) SetNX(ctx context.Context, key, val string, ttl time.Duration) (bool, error) {
	k.calls++
	if k.calls == k.at {
		return false, errBoom
	}
	return k.KVCache.SetNX(ctx, key, val, ttl)
}

type errStatus struct{}

func (errStatus) Active(context.Context, *appctx.Principal) (bool, error) { return false, errBoom }

type errIP struct{}

func (errIP) Allowed(context.Context, *appctx.Principal, string) (bool, error) {
	return false, errBoom
}

type errRPM struct{}

func (errRPM) RPMFor(context.Context, *appctx.Principal) (int, bool, error) { return 0, false, errBoom }

func principal() *appctx.Principal {
	return &appctx.Principal{UserID: 7, TenantID: 1001, Role: appctx.RoleUser}
}

// ---- CheckCall：状态 ----

func TestCheckCall_NilPrincipalForbidden(t *testing.T) {
	e := NewEngine(NewMemKVCache(nil))
	assertCode(t, e.CheckCall(context.Background(), nil, CallContext{}), CodeStatusForbidden)
}

func TestCheckCall_NoCheckersPasses(t *testing.T) {
	e := NewEngine(NewMemKVCache(nil))
	assertCode(t, e.CheckCall(context.Background(), principal(), CallContext{ClientIP: "1.2.3.4"}), "")
}

func TestCheckCall_BannedUserForbidden(t *testing.T) {
	sc := NewMemStatusChecker()
	sc.BanUser(7)
	e := NewEngine(NewMemKVCache(nil), WithStatusChecker(sc))
	assertCode(t, e.CheckCall(context.Background(), principal(), CallContext{}), CodeStatusForbidden)
}

func TestCheckCall_BannedTenantForbidden(t *testing.T) {
	sc := NewMemStatusChecker()
	sc.BanTenant(1001)
	e := NewEngine(NewMemKVCache(nil), WithStatusChecker(sc))
	assertCode(t, e.CheckCall(context.Background(), principal(), CallContext{}), CodeStatusForbidden)
}

func TestCheckCall_StatusErrorPropagates(t *testing.T) {
	e := NewEngine(NewMemKVCache(nil), WithStatusChecker(errStatus{}))
	if err := e.CheckCall(context.Background(), principal(), CallContext{}); !errors.Is(err, errBoom) {
		t.Fatalf("want errBoom, got %v", err)
	}
}

// 状态校验短路于 IP/RPM 之前：封禁用户即便 IP 不合法、RPM=1，也始终返回 STATUS_FORBIDDEN。
func TestCheckCall_StatusShortCircuitsBeforeRPM(t *testing.T) {
	sc := NewMemStatusChecker()
	sc.BanUser(7)
	e := NewEngine(NewMemKVCache(nil), WithStatusChecker(sc),
		WithConfig(Config{DefaultRPM: 1, RateWindow: time.Minute}))
	for i := 0; i < 3; i++ {
		assertCode(t, e.CheckCall(context.Background(), principal(), CallContext{}), CodeStatusForbidden)
	}
}

// ---- CheckCall：IP allowlist ----

func TestCheckCall_IPAllowlist(t *testing.T) {
	al := NewMemIPAllowlist()
	al.Allow(7, "10.0.0.0/8")
	al.Allow(7, "203.0.113.5")
	e := NewEngine(NewMemKVCache(nil), WithIPAllowlist(al))

	cases := []struct {
		name, ip, code string
	}{
		{"cidr match", "10.1.2.3", ""},
		{"exact match", "203.0.113.5", ""},
		{"miss", "8.8.8.8", CodeIPNotAllowed},
		{"unparseable", "not-an-ip", CodeIPNotAllowed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertCode(t, e.CheckCall(context.Background(), principal(), CallContext{ClientIP: c.ip}), c.code)
		})
	}
}

func TestCheckCall_IPAllowlistUnconfiguredPasses(t *testing.T) {
	al := NewMemIPAllowlist()
	al.Allow(99, "10.0.0.0/8") // 另一个用户配置；user 7 无配置 → 放行
	e := NewEngine(NewMemKVCache(nil), WithIPAllowlist(al))
	assertCode(t, e.CheckCall(context.Background(), principal(), CallContext{ClientIP: "8.8.8.8"}), "")
}

func TestMemIPAllowlist_RejectsBadRule(t *testing.T) {
	al := NewMemIPAllowlist()
	if al.Allow(7, "garbage") {
		t.Fatal("expected bad rule rejected")
	}
}

func TestCheckCall_IPErrorPropagates(t *testing.T) {
	e := NewEngine(NewMemKVCache(nil), WithIPAllowlist(errIP{}))
	if err := e.CheckCall(context.Background(), principal(), CallContext{ClientIP: "1.2.3.4"}); !errors.Is(err, errBoom) {
		t.Fatalf("want errBoom, got %v", err)
	}
}

// ---- CheckCall：RPM 限流 ----

func TestCheckCall_RPMFixedWindow(t *testing.T) {
	clk := newManualClock()
	e := NewEngine(NewMemKVCache(clk), WithClock(clk),
		WithConfig(Config{DefaultRPM: 3, RateWindow: time.Minute}))
	p := principal()
	// 同窗口前 3 次放行，第 4 次限流。
	for i := 0; i < 3; i++ {
		assertCode(t, e.CheckCall(context.Background(), p, CallContext{}), "")
	}
	assertCode(t, e.CheckCall(context.Background(), p, CallContext{}), CodeRateLimited)

	// 推进一个窗口 → 计数重置，重新放行。
	clk.Advance(time.Minute)
	assertCode(t, e.CheckCall(context.Background(), p, CallContext{}), "")
}

func TestCheckCall_RPMZeroMeansUnlimited(t *testing.T) {
	e := NewEngine(NewMemKVCache(nil), WithConfig(Config{DefaultRPM: 0}))
	p := principal()
	for i := 0; i < 100; i++ {
		assertCode(t, e.CheckCall(context.Background(), p, CallContext{}), "")
	}
}

func TestCheckCall_RPMResolverOverridesDefault(t *testing.T) {
	clk := newManualClock()
	res := NewMemRPMResolver()
	res.Set(7, 2) // user 7 限 2 RPM，覆盖默认 100
	e := NewEngine(NewMemKVCache(clk), WithClock(clk), WithRPMResolver(res),
		WithConfig(Config{DefaultRPM: 100, RateWindow: time.Minute}))
	p := principal()
	assertCode(t, e.CheckCall(context.Background(), p, CallContext{}), "")
	assertCode(t, e.CheckCall(context.Background(), p, CallContext{}), "")
	assertCode(t, e.CheckCall(context.Background(), p, CallContext{}), CodeRateLimited)
}

func TestCheckCall_RPMResolverFallbackToDefault(t *testing.T) {
	clk := newManualClock()
	res := NewMemRPMResolver() // user 7 未配置 → 回退默认 1
	e := NewEngine(NewMemKVCache(clk), WithClock(clk), WithRPMResolver(res),
		WithConfig(Config{DefaultRPM: 1, RateWindow: time.Minute}))
	p := principal()
	assertCode(t, e.CheckCall(context.Background(), p, CallContext{}), "")
	assertCode(t, e.CheckCall(context.Background(), p, CallContext{}), CodeRateLimited)
}

func TestCheckCall_RPMResolverErrorPropagates(t *testing.T) {
	e := NewEngine(NewMemKVCache(nil), WithRPMResolver(errRPM{}))
	if err := e.CheckCall(context.Background(), principal(), CallContext{}); !errors.Is(err, errBoom) {
		t.Fatalf("want errBoom, got %v", err)
	}
}

func TestCheckCall_RPMIncrErrorPropagates(t *testing.T) {
	kv := kvErrOn{KVCache: NewMemKVCache(nil), incr: true}
	e := NewEngine(kv, WithConfig(Config{DefaultRPM: 5}))
	if err := e.CheckCall(context.Background(), principal(), CallContext{}); !errors.Is(err, errBoom) {
		t.Fatalf("want errBoom, got %v", err)
	}
}

// 限流计数在并发下精确：100 并发、上限 10 → 恰好 10 次放行。
func TestCheckCall_RPMConcurrentCountExact(t *testing.T) {
	clk := newManualClock()
	e := NewEngine(NewMemKVCache(clk), WithClock(clk),
		WithConfig(Config{DefaultRPM: 10, RateWindow: time.Minute}))
	p := principal()
	const n = 100
	var pass int64
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e.CheckCall(context.Background(), p, CallContext{}) == nil {
				atomic.AddInt64(&pass, 1)
			}
		}()
	}
	wg.Wait()
	if pass != 10 {
		t.Fatalf("want exactly 10 passes, got %d", pass)
	}
}

// ---- CheckPurchaseLimit：Trial 三维去重 ----

func trialCtx(realName, device string) context.Context {
	return WithPurchaseIdentity(context.Background(), PurchaseIdentity{RealNameID: realName, DeviceID: device})
}

func trialPlan() Plan { return Plan{ID: 1, Code: "trial", Trial: true} }

func TestCheckPurchaseLimit_TrialUserDedup(t *testing.T) {
	e := NewEngine(NewMemKVCache(nil))
	assertCode(t, e.CheckPurchaseLimit(context.Background(), 7, trialPlan()), "")
	assertCode(t, e.CheckPurchaseLimit(context.Background(), 7, trialPlan()), CodePurchaseLimitExceeded)
}

func TestCheckPurchaseLimit_TrialRealNameDedup(t *testing.T) {
	e := NewEngine(NewMemKVCache(nil))
	// 用户 7 用实名 ID-A 购买成功。
	assertCode(t, e.CheckPurchaseLimit(trialCtx("ID-A", ""), 7, trialPlan()), "")
	// 不同用户 8 但同实名 ID-A → 拒。
	assertCode(t, e.CheckPurchaseLimit(trialCtx("ID-A", ""), 8, trialPlan()), CodePurchaseLimitExceeded)
}

func TestCheckPurchaseLimit_TrialDeviceDedup(t *testing.T) {
	e := NewEngine(NewMemKVCache(nil))
	assertCode(t, e.CheckPurchaseLimit(trialCtx("", "dev-1"), 7, trialPlan()), "")
	// 不同用户 8 同设备 dev-1 → 拒。
	assertCode(t, e.CheckPurchaseLimit(trialCtx("", "dev-1"), 8, trialPlan()), CodePurchaseLimitExceeded)
}

func TestCheckPurchaseLimit_TrialDistinctIdentitiesPass(t *testing.T) {
	e := NewEngine(NewMemKVCache(nil))
	assertCode(t, e.CheckPurchaseLimit(trialCtx("ID-A", "dev-1"), 7, trialPlan()), "")
	// 全维度不同 → 放行。
	assertCode(t, e.CheckPurchaseLimit(trialCtx("ID-B", "dev-2"), 8, trialPlan()), "")
}

func TestCheckPurchaseLimit_TrialSetNXErrorPropagates(t *testing.T) {
	kv := kvErrOn{KVCache: NewMemKVCache(nil), setnx: true}
	e := NewEngine(kv)
	if err := e.CheckPurchaseLimit(context.Background(), 7, trialPlan()); !errors.Is(err, errBoom) {
		t.Fatalf("want errBoom, got %v", err)
	}
}

// 败者残留回归（audit 2026-07-17 发现#3）：B 在共享设备维度判负时，此前已占的 userK 必须回滚——
// 否则 Trial TTL=0 下 B 换干净设备也被永久拒（从未买到过却报「超过限购次数」）。
func TestCheckPurchaseLimit_TrialLoserLeavesNoResidue(t *testing.T) {
	kv := NewMemKVCache(nil)
	e := NewEngine(kv)
	// A（user 7）占用 dev-1。
	assertCode(t, e.CheckPurchaseLimit(trialCtx("", "dev-1"), 7, trialPlan()), "")
	// B（user 8）同设备判负。
	assertCode(t, e.CheckPurchaseLimit(trialCtx("", "dev-1"), 8, trialPlan()), CodePurchaseLimitExceeded)
	// 败者不留痕：B 的用户维度键必须已回滚。
	if _, found, _ := kv.Get(context.Background(), trialKey("user", "8")); found {
		t.Fatal("loser's user-dim key must be rolled back")
	}
	// 回滚只删自己占到的键：赢家 A 的设备维度键必须仍在。
	if _, found, _ := kv.Get(context.Background(), trialKey("device", "dev-1")); !found {
		t.Fatal("winner's device-dim key must survive loser rollback")
	}
	// B 换干净设备 dev-2 → 放行。
	assertCode(t, e.CheckPurchaseLimit(trialCtx("", "dev-2"), 8, trialPlan()), "")
}

// 共享实名维度不被败者污染（audit 2026-07-17 发现#3 PROBE-P1b）：自然人 R 在被占设备上判负后，
// 其实名键必须回滚——否则 R 换新账号+新设备也被永久拒（实名键按自然人计，株连其全部账号）。
func TestCheckPurchaseLimit_TrialLoserDoesNotPoisonRealName(t *testing.T) {
	kv := NewMemKVCache(nil)
	e := NewEngine(kv)
	// A（user 7）占用 dev-1。
	assertCode(t, e.CheckPurchaseLimit(trialCtx("", "dev-1"), 7, trialPlan()), "")
	// R（user 8，实名 ID-R）在 dev-1 判负。
	assertCode(t, e.CheckPurchaseLimit(trialCtx("ID-R", "dev-1"), 8, trialPlan()), CodePurchaseLimitExceeded)
	// 实名维度未被污染。
	if _, found, _ := kv.Get(context.Background(), trialKey("realname", "ID-R")); found {
		t.Fatal("loser's realname-dim key must be rolled back")
	}
	// 同一自然人换新账号（user 9）+ 新设备 → 放行。
	assertCode(t, e.CheckPurchaseLimit(trialCtx("ID-R", "dev-2"), 9, trialPlan()), "")
}

// 中途维度 SetNX 报错同样回滚已占维度（错误路径不留痕）。
func TestCheckPurchaseLimit_TrialErrorMidLoopRollsBack(t *testing.T) {
	inner := NewMemKVCache(nil)
	e := NewEngine(kvSetNXErrOnKey{KVCache: inner, failKey: trialKey("device", "dev-1")})
	if err := e.CheckPurchaseLimit(trialCtx("ID-A", "dev-1"), 7, trialPlan()); !errors.Is(err, errBoom) {
		t.Fatalf("want errBoom, got %v", err)
	}
	if _, found, _ := inner.Get(context.Background(), trialKey("user", "7")); found {
		t.Fatal("user-dim key must be rolled back on mid-loop error")
	}
	if _, found, _ := inner.Get(context.Background(), trialKey("realname", "ID-A")); found {
		t.Fatal("realname-dim key must be rolled back on mid-loop error")
	}
}

// PROBE-P6（audit 2026-07-17 发现#4）：Redis 瞬时抖动（第 2 次 SetNX 报错一次）不得永久吃掉终身 Trial——
// 错误如实传播（保留新码相对旧码「不丢错误」的改进），已占维度回滚，抖动恢复后同身份重试成功。
// 无回滚时该场景为：SetNX(userK) 成功→realK 抖动报错→HTTP 500→订单从未创建→重试永久 PURCHASE_LIMIT_EXCEEDED。
func TestCheckPurchaseLimit_TrialMidLoopErrorHealthyRetrySucceeds(t *testing.T) {
	kv := &kvSetNXErrOnce{KVCache: NewMemKVCache(nil), at: 2}
	e := NewEngine(kv)
	// 第 1 次购买：userK 写入成功，realK 遇抖动报错 → 错误透传。
	if err := e.CheckPurchaseLimit(trialCtx("ID-A", "dev-1"), 7, trialPlan()); !errors.Is(err, errBoom) {
		t.Fatalf("want errBoom, got %v", err)
	}
	// 抖动恢复后同身份重试 → 放行（零订单零付款的用户不得被烧毁资格）。
	assertCode(t, e.CheckPurchaseLimit(trialCtx("ID-A", "dev-1"), 7, trialPlan()), "")
}

// 并发同用户购买 Trial：恰好 1 个成功（SetNX 决胜锁，-race 通过）。
// 注意：本测试所有 goroutine 同 userID，userK 本身即同一把锁，旧实现（仅 userK 决胜）
// 也能过——跨账号性质由下方 CrossAccountSingleWinner 守卫。
func TestCheckPurchaseLimit_TrialConcurrentSingleWinner(t *testing.T) {
	e := NewEngine(NewMemKVCache(nil))
	const n = 64
	var success int64
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e.CheckPurchaseLimit(trialCtx("ID-A", "dev-1"), 7, trialPlan()) == nil {
				atomic.AddInt64(&success, 1)
			}
		}()
	}
	wg.Wait()
	if success != 1 {
		t.Fatalf("want exactly 1 winner, got %d", success)
	}
}

// barrierGetKV 在指定键的 Get 上设会合屏障：先执行内层 Get 把结果攥在手里，
// **然后**等到 need 个调用方全部完成该键的读取（或超时兜底，防实现不调 Get 时
// 死锁）才放行返回。用途：确定性地撑开「Get 预检读到结果 → SetNX 落痕」之间的
// 竞态窗口——若实现的跨账号决胜依赖非原子的 Get 预检，屏障保证所有请求都基于
// "未占用"的读取结果继续前进、各自胜出；原子 SetNX 决胜的实现根本不调 Get，
// 屏障零干预。屏障必须放在内层 Get **之后**：若放在之前，放行后最快的 goroutine
// 会先落痕、其余请求的内层 Get 仍能看到占用而被拒，窗口重新闭合（实测如此）。
// 普通并发压测同样抓不住这个窗口（内存 KV 下窗口极窄，单轮几乎总是 1 个赢家）。
type barrierGetKV struct {
	KVCache
	key     string
	need    int
	mu      sync.Mutex
	arrived int
	release chan struct{}
}

func (b *barrierGetKV) Get(ctx context.Context, key string) (string, bool, error) {
	val, found, err := b.KVCache.Get(ctx, key)
	if key == b.key {
		b.mu.Lock()
		b.arrived++
		if b.arrived == b.need {
			close(b.release)
		}
		b.mu.Unlock()
		select {
		case <-b.release:
		case <-time.After(time.Second):
		}
	}
	return val, found, err
}

// 并发跨账号共享单一维度购买 Trial：恰好 1 个成功（-race 通过）。
// 守卫 3dd6b4f 修复的核心性质：旧实现仅以 userK 作决胜锁、丢弃实名/设备维度的
// SetNX 返回值——不同 userID 的多账号共享同一设备/实名时可各自拿到一份 Trial
// （多账号 Trial 农场）。上方 SingleWinner 测试全员同 userID，userK 本身即同一把
// 锁，旧实现也能过、守不住此性质。本测试每个 goroutine 用不同 userID（userK 互不
// 竞争）、仅共享实名或设备一个维度，并用 barrierGetKV 强制并发同时通过 Get 预检
// （若有）；赢家数 >1 即该修复被还原/破坏。
func TestCheckPurchaseLimit_TrialConcurrentCrossAccountSingleWinner(t *testing.T) {
	cases := []struct {
		name      string
		sharedKey string
		ctx       func(i int) context.Context
	}{
		{"shared-device", trialKey("device", "dev-shared"),
			func(i int) context.Context { return trialCtx("ID-"+strconv.Itoa(i), "dev-shared") }},
		{"shared-realname", trialKey("realname", "ID-shared"),
			func(i int) context.Context { return trialCtx("ID-shared", "dev-"+strconv.Itoa(i)) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const n = 4
			kv := &barrierGetKV{KVCache: NewMemKVCache(nil), key: tc.sharedKey, need: n, release: make(chan struct{})}
			e := NewEngine(kv)
			var success int64
			var wg sync.WaitGroup
			for i := 0; i < n; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					if e.CheckPurchaseLimit(tc.ctx(i), int64(100+i), trialPlan()) == nil {
						atomic.AddInt64(&success, 1)
					}
				}(i)
			}
			wg.Wait()
			if success != 1 {
				t.Fatalf("want exactly 1 winner, got %d", success)
			}
		})
	}
}

// ---- CheckPurchaseLimit：非 Trial 档 ----

func TestCheckPurchaseLimit_NonTrialUnlimited(t *testing.T) {
	e := NewEngine(NewMemKVCache(nil))
	plan := Plan{ID: 2, Code: "mini", PerUserLimit: 0}
	for i := 0; i < 5; i++ {
		assertCode(t, e.CheckPurchaseLimit(context.Background(), 7, plan), "")
	}
}

func TestCheckPurchaseLimit_NonTrialPerUserLimit(t *testing.T) {
	clk := newManualClock()
	e := NewEngine(NewMemKVCache(clk), WithClock(clk),
		WithConfig(Config{PurchaseDedupTTL: time.Hour}))
	plan := Plan{ID: 2, Code: "mini", PerUserLimit: 2}
	assertCode(t, e.CheckPurchaseLimit(context.Background(), 7, plan), "")
	assertCode(t, e.CheckPurchaseLimit(context.Background(), 7, plan), "")
	assertCode(t, e.CheckPurchaseLimit(context.Background(), 7, plan), CodePurchaseLimitExceeded)
}

func TestCheckPurchaseLimit_NonTrialIncrErrorPropagates(t *testing.T) {
	kv := kvErrOn{KVCache: NewMemKVCache(nil), incr: true}
	e := NewEngine(kv)
	plan := Plan{ID: 2, PerUserLimit: 1}
	if err := e.CheckPurchaseLimit(context.Background(), 7, plan); !errors.Is(err, errBoom) {
		t.Fatalf("want errBoom, got %v", err)
	}
}

// ---- NoteUsage：满额逼近告警 ----

func TestNoteUsage_FiresOnceAtThreshold(t *testing.T) {
	clk := newManualClock()
	sink := NewMemAlertSink()
	e := NewEngine(NewMemKVCache(clk), WithClock(clk), WithAlertSink(sink))

	// 恰好 0.8（>=阈值）→ 触发。
	e.NoteUsage(context.Background(), 42, 80, 100)
	// 再次（更高占比）→ 去重，不重复告警。
	e.NoteUsage(context.Background(), 42, 95, 100)

	alerts := sink.Alerts()
	if len(alerts) != 1 {
		t.Fatalf("want exactly 1 alert, got %d", len(alerts))
	}
	a := alerts[0]
	if a.SubscriptionID != 42 || a.Used != 80 || a.Limit != 100 || a.Threshold != 0.8 {
		t.Fatalf("unexpected alert: %+v", a)
	}
	if a.Ratio != 0.8 {
		t.Fatalf("ratio = %v, want 0.8", a.Ratio)
	}
	if !a.At.Equal(clk.Now()) {
		t.Fatalf("alert time = %v, want clock %v", a.At, clk.Now())
	}
}

func TestNoteUsage_BelowThresholdNoAlert(t *testing.T) {
	sink := NewMemAlertSink()
	e := NewEngine(NewMemKVCache(nil), WithAlertSink(sink))
	e.NoteUsage(context.Background(), 42, 79, 100) // 0.79 < 0.8
	if got := len(sink.Alerts()); got != 0 {
		t.Fatalf("want no alert, got %d", got)
	}
}

func TestNoteUsage_ZeroLimitNoPanic(t *testing.T) {
	sink := NewMemAlertSink()
	e := NewEngine(NewMemKVCache(nil), WithAlertSink(sink))
	e.NoteUsage(context.Background(), 42, 10, 0) // limit<=0 → 跳过
	if got := len(sink.Alerts()); got != 0 {
		t.Fatalf("want no alert, got %d", got)
	}
}

func TestNoteUsage_CustomThreshold(t *testing.T) {
	sink := NewMemAlertSink()
	e := NewEngine(NewMemKVCache(nil), WithAlertSink(sink),
		WithConfig(Config{AlertThreshold: 0.5}))
	e.NoteUsage(context.Background(), 42, 60, 100) // 0.6 >= 0.5 → 触发
	if got := len(sink.Alerts()); got != 1 {
		t.Fatalf("want 1 alert, got %d", got)
	}
}

func TestNoteUsage_NilSinkNoPanic(t *testing.T) {
	e := NewEngine(NewMemKVCache(nil)) // 无 AlertSink
	e.NoteUsage(context.Background(), 42, 90, 100)
	// 仅验证不 panic；去重键已写入。
	if first, _ := e.kv.SetNX(context.Background(), alertKey(42), "1", 0); first {
		t.Fatal("alert dedup key should have been set on first NoteUsage")
	}
}

func TestNoteUsage_ConcurrentSingleAlert(t *testing.T) {
	clk := newManualClock()
	sink := NewMemAlertSink()
	e := NewEngine(NewMemKVCache(clk), WithClock(clk), WithAlertSink(sink))
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.NoteUsage(context.Background(), 42, 90, 100)
		}()
	}
	wg.Wait()
	if got := len(sink.Alerts()); got != 1 {
		t.Fatalf("want exactly 1 alert under concurrency, got %d", got)
	}
}

// ---- Config / 构造 ----

func TestConfigNormalize(t *testing.T) {
	c := Config{RateWindow: -1, AlertThreshold: 5}.normalize()
	if c.RateWindow != time.Minute || c.AlertThreshold != 0.8 {
		t.Fatalf("normalize failed: %+v", c)
	}
}

func TestNewEngine_NilClockDefaults(t *testing.T) {
	e := NewEngine(NewMemKVCache(nil), WithClock(nil))
	if e.clock == nil {
		t.Fatal("clock must default when nil option passed")
	}
}

func TestErrorCodesHTTP(t *testing.T) {
	cases := []struct {
		err  *apperr.AppError
		code string
		http int
	}{
		{ErrRateLimited, CodeRateLimited, http.StatusTooManyRequests},
		{ErrIPNotAllowed, CodeIPNotAllowed, http.StatusForbidden},
		{ErrPurchaseLimitExceeded, CodePurchaseLimitExceeded, http.StatusConflict},
		{ErrStatusForbidden, CodeStatusForbidden, http.StatusForbidden},
	}
	for _, c := range cases {
		if c.err.Code != c.code || c.err.HTTP != c.http {
			t.Fatalf("err %+v, want code=%s http=%d", c.err, c.code, c.http)
		}
	}
}
