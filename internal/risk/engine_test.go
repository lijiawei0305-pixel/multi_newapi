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
	incr, get, setnx bool
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

// kvDelSpy 包装 KVCache：记录每次 Del 的入参（断言「只删该删的」），failNow 置真则 Del 返回 errBoom
// （模拟 Redis 抖动/只读，验证补偿删除失败时的行为）。并发安全（补偿回滚测试可能并发）。
type kvDelSpy struct {
	KVCache
	mu      sync.Mutex
	calls   [][]string
	failNow bool
}

func (k *kvDelSpy) Del(ctx context.Context, keys ...string) error {
	k.mu.Lock()
	k.calls = append(k.calls, append([]string(nil), keys...))
	k.mu.Unlock()
	if k.failNow {
		return errBoom
	}
	return k.KVCache.Del(ctx, keys...)
}

// delCalls 返回记录的 Del 调用快照（并发安全读）。
func (k *kvDelSpy) delCalls() [][]string {
	k.mu.Lock()
	defer k.mu.Unlock()
	out := make([][]string, len(k.calls))
	copy(out, k.calls)
	return out
}

// equalStrs 判断两个字符串切片顺序与内容完全相等：比对补偿 Del 入参 == 期望 claimed 键序。
// claimed 按 [user, realname, device] 顺序追加，故顺序有意义（用有序比对而非集合比对）。
func equalStrs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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

// ---- checkTrialLimit：补偿删除（Del）失败/入参守卫（audit F6 · Testing）----
// 生产 checkTrialLimit 判负/出错路径的补偿删除是「尽力而为」：_ = e.kv.Del(...) 显式丢弃错误
// （Del 失败仅多留痕，可经 ReleaseTrialLimit 后台解）。此前 3 个 KV 替身都未覆写 Del、全透传底层
// MemKVCache → 无法注入 Del 失败、也无法断言 Del 入参。下列用例用 kvDelSpy 补齐这两条守卫。

// Del 失败（Redis 抖动/只读）时，跨账号撞设备的败者仍返回业务错误 ErrPurchaseLimitExceeded，
// 而非把被吞掉的 errBoom 冒泡上来，且不 panic。
func TestCheckTrialLimit_DelFailureOnLoserStillReturnsBusinessError(t *testing.T) {
	// failNow 自构造起即置真；A 全维 SetNX 成功、不触发 Del，故不受影响——只有败者 B 会走补偿 Del。
	spy := &kvDelSpy{KVCache: NewMemKVCache(nil), failNow: true}
	e := NewEngine(spy)
	// A(7) 先占住共享设备键。
	assertCode(t, e.CheckPurchaseLimit(trialCtx("", "dev-1"), 7, trialPlan()), "")
	// B(8，不同 userID、同设备)判负：补偿 Del([user:8]) 因 failNow 返回 errBoom，但该错误被显式丢弃。
	err := e.CheckPurchaseLimit(trialCtx("", "dev-1"), 8, trialPlan())
	assertCode(t, err, CodePurchaseLimitExceeded)
	// 显式钉死：返回的是业务错误、绝非被吞的 Del 错误 errBoom。
	if errors.Is(err, errBoom) {
		t.Fatalf("Del failure must be swallowed, not surfaced; got %v", err)
	}
	// 补偿确实以「只本次 claimed」的键发起（[user:8]），未误删 A 的共享设备键。
	calls := spy.delCalls()
	if len(calls) != 1 || !equalStrs(calls[0], []string{trialKey("user", "8")}) {
		t.Fatalf("compensation Del = %v, want exactly one call [%s]", calls, trialKey("user", "8"))
	}
}

// 补偿删除只删「本次 SetNX 成功的键」（败者自己的），绝不误删赢家占用的键。
// n 个不同账号并发抢同一共享设备：SetNX 原子决胜恰 1 赢家，n-1 败者各自补偿删除自己的 user 维键
// （userK 置首且唯一 → 败者必在第 2 维 devK 判负、只 claim 了自己的 userK）。
func TestCheckTrialLimit_CompensationDeletesOnlyClaimedKeys(t *testing.T) {
	spy := &kvDelSpy{KVCache: NewMemKVCache(nil)} // 不注错：Del 记录入参并如实删除
	e := NewEngine(spy)
	const n = 4
	var success int64
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if e.CheckPurchaseLimit(trialCtx("", "dev-shared"), int64(100+i), trialPlan()) == nil {
				atomic.AddInt64(&success, 1)
			}
		}(i)
	}
	wg.Wait()
	if success != 1 {
		t.Fatalf("want exactly 1 winner, got %d", success)
	}

	devKey := trialKey("device", "dev-shared")
	userKeys := map[string]bool{}
	for i := 0; i < n; i++ {
		userKeys[trialKey("user", strconv.Itoa(100+i))] = true
	}
	// 聚合所有被补偿删除的键。
	deleted := map[string]bool{}
	for _, call := range spy.delCalls() {
		for _, k := range call {
			deleted[k] = true
		}
	}
	// ① 每个被删键都必须是某败者「自己的」user 维键（⊆ 败者 claimed）——绝不含共享 devK/realname/其它。
	for k := range deleted {
		if !userKeys[k] {
			t.Fatalf("compensated-deleted key %q is outside losers' own claimed user keys (deleted=%v)", k, deleted)
		}
	}
	// ② 赢家占用的共享设备键绝不被补偿删除（否则无关新账号可从已消耗设备再领 Trial）。
	if deleted[devKey] {
		t.Fatalf("winner's shared device key %q must never be compensated-deleted", devKey)
	}
	// ③ n-1 个败者全部完成补偿：被删的 user 维键恰为 n-1 个（同时守住「补偿 Del 真的发生了」，
	//    补偿被整段删除时此断言即变红）。
	if len(deleted) != n-1 {
		t.Fatalf("want exactly %d losers compensated, got %d deleted keys: %v", n-1, len(deleted), deleted)
	}
	// ④ 赢家的键（自己的 user 维键 + 共享设备键）必须仍在（绝不被误删）。
	var winnerUserKey string
	for k := range userKeys {
		if !deleted[k] {
			winnerUserKey = k
		}
	}
	if _, found, _ := spy.Get(context.Background(), winnerUserKey); !found {
		t.Fatalf("winner's own user key %q must survive", winnerUserKey)
	}
	if _, found, _ := spy.Get(context.Background(), devKey); !found {
		t.Fatalf("winner's shared device key %q must survive", devKey)
	}
}

// 循环中途某维 SetNX 报错时，补偿 Del 也失败（叠加两替身），仍返回原始 errBoom、不 panic。
// 取舍说明：kvSetNXErrOnKey 与 kvDelSpy(failNow) 均返回同一个 errBoom（后者必须为 errBoom 以供
// TestReleaseTrialLimit_DelErrorPropagates 断言），故无法从「错误身份」区分返回的是 SetNX 错还是
// Del 错；此处守卫是「补偿 Del 失败这条路径确实被走到（delCalls 有 record）、且函数仍返回 errBoom
// （非 nil / 非业务错误 / 非 panic）」——即被吞的 Del 错不改变返回契约。
func TestCheckTrialLimit_DelFailureOnMidLoopErrorStillReturnsOrigErr(t *testing.T) {
	inner := NewMemKVCache(nil)
	// 内层 kvSetNXErrOnKey：device 维 SetNX 报 errBoom（循环中途出错）；外层 kvDelSpy：补偿 Del 亦失败。
	spy := &kvDelSpy{
		KVCache: kvSetNXErrOnKey{KVCache: inner, failKey: trialKey("device", "dev-1")},
		failNow: true,
	}
	e := NewEngine(spy)
	// user:7、realname:ID-A 先 SetNX 成功 → device 维 SetNX 报错 → 补偿 Del([user:7, realname:ID-A]) 亦失败。
	err := e.CheckPurchaseLimit(trialCtx("ID-A", "dev-1"), 7, trialPlan())
	if !errors.Is(err, errBoom) {
		t.Fatalf("want errBoom (original error survives failed compensation Del), got %v", err)
	}
	// 证明确实走到「补偿 Del 也失败」这条路径：补偿以循环中途已 claim 的两键（按 [user, realname] 序）发起。
	calls := spy.delCalls()
	want := []string{trialKey("user", "7"), trialKey("realname", "ID-A")}
	if len(calls) != 1 || !equalStrs(calls[0], want) {
		t.Fatalf("compensation Del = %v, want exactly one call %v", calls, want)
	}
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

// ---- ReleaseTrialLimit：共享维度归属校验（PROBE-P3）----

func containsDim(list []string, dim string) bool {
	for _, d := range list {
		if d == dim {
			return true
		}
	}
	return false
}

// 守卫 PROBE-P3 修复的核心性质：realname/device 维度跨用户共享，释放请求里的维度值系
// 调用方自报、与 userID 零绑定——若释放不校验键值归属（占用者 userID），客服「给 B 释放」
// 会误删 A 合法占用的设备键，无关新账号 C 即可从已消耗设备再领 Trial（反刷维度被客服
// 通道洗掉）。探针谓词：释放 B 后 A 合法占用的 devK 仍在；C 从同设备领取仍被拒。
func TestReleaseTrialLimit_CrossUserOwnershipGuard(t *testing.T) {
	e := NewEngine(NewMemKVCache(nil))
	// A(7) 从设备 dev-D 合法消耗 Trial。
	assertCode(t, e.CheckPurchaseLimit(trialCtx("", "dev-D"), 7, trialPlan()), "")
	// B(8) 同设备被拒（败者补偿删除后 userK:8 不留痕；devK 归属仍是 A）。
	assertCode(t, e.CheckPurchaseLimit(trialCtx("", "dev-D"), 8, trialPlan()), CodePurchaseLimitExceeded)

	// 客服按 B 自报的 device_id 释放 B → device 维度归属校验不符，必须拒删。
	res, err := e.ReleaseTrialLimit(context.Background(), 8, PurchaseIdentity{DeviceID: "dev-D"}, false)
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if !containsDim(res.Skipped, "device") || containsDim(res.Released, "device") {
		t.Fatalf("device dim must be skipped (owned by A), got %+v", res)
	}
	if !containsDim(res.Released, "user") {
		t.Fatalf("user dim must always release, got %+v", res)
	}

	// 探针谓词①：A 占用的 devK 仍在——无关新账号 C(9) 从设备 dev-D 领取仍被拒。
	assertCode(t, e.CheckPurchaseLimit(trialCtx("", "dev-D"), 9, trialPlan()), CodePurchaseLimitExceeded)
	// B 换设备可再试（判负时 userK 已被补偿删除，本无残留；release 对 user 维恒报
	// Released 且 Del 幂等，键不存在也不报错）。
	assertCode(t, e.CheckPurchaseLimit(trialCtx("", "dev-B2"), 8, trialPlan()), "")
}

// 守卫 F4 修复的核心性质（audit · Medium）：force 只豁免「归属不可考」的遗留/脏值键，
// **绝不**豁免「另一真实用户的有效占用键」（值为其 userID）。否则防线（归属校验）与绕过
// 开关（force）握在同一只客服手里，一键 force:true 即可偷删他人合法反刷键，令无关新账号
// 从已消耗设备再领 Trial。探针谓词：以 B(8)+force 释放 A(7) 占用的 devK（值 "7"），device
// 维仍进 Skipped、devK 仍在、无关新账号 C(9) 从同设备领取仍被拒。
// 未修代码（if val != owner && !force）下 force 无差别绕过 → devK 被删、C 可再领 → 先红。
func TestReleaseTrialLimit_ForceCannotStealValidOwnerKey(t *testing.T) {
	kv := NewMemKVCache(nil)
	e := NewEngine(kv)
	// A(7) 从设备 dev-D 合法占用 Trial（devK 值 = "7"，是另一真实用户的有效占用）。
	assertCode(t, e.CheckPurchaseLimit(trialCtx("", "dev-D"), 7, trialPlan()), "")

	// 以 B(8) + force=true 释放 A 占用的 device 维度键：值 "7" 归属明确（另一真实用户），
	// force 也必须拒删（force 不是偷他人反刷键的后门）。
	res, err := e.ReleaseTrialLimit(context.Background(), 8, PurchaseIdentity{DeviceID: "dev-D"}, true)
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if !containsDim(res.Skipped, "device") || containsDim(res.Released, "device") {
		t.Fatalf("device dim must be skipped even with force (valid owner A=7), got %+v", res)
	}

	// 探针谓词①：A 占用的 devK 仍在。
	if _, found, _ := kv.Get(context.Background(), trialKey("device", "dev-D")); !found {
		t.Fatal("A's valid device key must survive force release attempted by another user")
	}
	// 探针谓词②：无关新账号 C(9) 从设备 dev-D 领取仍被拒。
	assertCode(t, e.CheckPurchaseLimit(trialCtx("", "dev-D"), 9, trialPlan()), CodePurchaseLimitExceeded)
}

// 真正的占用者释放自己的三维键 → 全部删除，可重新购买同一身份的 Trial
// （文档化的合法场景：点开收银台未付款、键已被自己消耗）。
func TestReleaseTrialLimit_OwnerFullRelease(t *testing.T) {
	e := NewEngine(NewMemKVCache(nil))
	assertCode(t, e.CheckPurchaseLimit(trialCtx("ID-A", "dev-D"), 7, trialPlan()), "")

	res, err := e.ReleaseTrialLimit(context.Background(), 7, PurchaseIdentity{RealNameID: "ID-A", DeviceID: "dev-D"}, false)
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	for _, dim := range []string{"user", "realname", "device"} {
		if !containsDim(res.Released, dim) {
			t.Fatalf("dim %s must release for owner, got %+v", dim, res)
		}
	}
	if len(res.Skipped) != 0 {
		t.Fatalf("owner release must skip nothing, got %+v", res)
	}
	// 三维全释放 → 同身份可重新购买。
	assertCode(t, e.CheckPurchaseLimit(trialCtx("ID-A", "dev-D"), 7, trialPlan()), "")
}

// 旧版遗留键（值为 "1"、无归属信息）：默认拒删（归属不可考 = fail-closed），
// force=true（客服人工核实后）才删除。
func TestReleaseTrialLimit_LegacyValueRequiresForce(t *testing.T) {
	kv := NewMemKVCache(nil)
	if ok, _ := kv.SetNX(context.Background(), trialKey("device", "dev-L"), "1", 0); !ok {
		t.Fatal("seed legacy key failed")
	}
	e := NewEngine(kv)

	res, err := e.ReleaseTrialLimit(context.Background(), 7, PurchaseIdentity{DeviceID: "dev-L"}, false)
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if !containsDim(res.Skipped, "device") {
		t.Fatalf("legacy key must be skipped without force, got %+v", res)
	}
	if _, found, _ := kv.Get(context.Background(), trialKey("device", "dev-L")); !found {
		t.Fatal("legacy key must survive non-force release")
	}

	res, err = e.ReleaseTrialLimit(context.Background(), 7, PurchaseIdentity{DeviceID: "dev-L"}, true)
	if err != nil {
		t.Fatalf("force release: %v", err)
	}
	if !containsDim(res.Released, "device") {
		t.Fatalf("force must release legacy key, got %+v", res)
	}
	if _, found, _ := kv.Get(context.Background(), trialKey("device", "dev-L")); found {
		t.Fatal("legacy key must be gone after force release")
	}
}

// 共享维度键不存在：幂等成功，不计入 Released/Skipped（与 checkTrialLimit 空维度口径对称）。
func TestReleaseTrialLimit_AbsentSharedKeysIdempotent(t *testing.T) {
	e := NewEngine(NewMemKVCache(nil))
	res, err := e.ReleaseTrialLimit(context.Background(), 7, PurchaseIdentity{RealNameID: "ID-X", DeviceID: "dev-X"}, false)
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if len(res.Skipped) != 0 || containsDim(res.Released, "realname") || containsDim(res.Released, "device") {
		t.Fatalf("absent keys must not appear in result, got %+v", res)
	}
	if !containsDim(res.Released, "user") {
		t.Fatalf("user dim must always release, got %+v", res)
	}
}

type getErrKV struct{ KVCache }

func (getErrKV) Get(ctx context.Context, key string) (string, bool, error) {
	return "", false, errBoom
}

func TestReleaseTrialLimit_GetErrorPropagates(t *testing.T) {
	e := NewEngine(getErrKV{NewMemKVCache(nil)})
	if _, err := e.ReleaseTrialLimit(context.Background(), 7, PurchaseIdentity{DeviceID: "dev-D"}, false); !errors.Is(err, errBoom) {
		t.Fatalf("want errBoom, got %v", err)
	}
}

// 覆盖 ReleaseTrialLimit 末尾**有返回值**的 Del 错误传播（与上方 GetErrorPropagates 互补：那条测
// Get 错、本条测 Del 错）。owner 先占好自己的 device 键（Get 校验值==owner → 进 keys 待删），释放时
// e.kv.Del(keys...) 因 failNow 返回 errBoom，必须向上传播（区别于 checkTrialLimit 里被吞的 Del）。
func TestReleaseTrialLimit_DelErrorPropagates(t *testing.T) {
	spy := &kvDelSpy{KVCache: NewMemKVCache(nil), failNow: true}
	e := NewEngine(spy)
	// owner(7) 全维 SetNX 成功、不触发 Del，故 failNow 不影响占用。
	assertCode(t, e.CheckPurchaseLimit(trialCtx("", "dev-D"), 7, trialPlan()), "")
	// 释放自己的键：末尾 Del 失败 → 该错误有返回值，必须传播。
	if _, err := e.ReleaseTrialLimit(context.Background(), 7, PurchaseIdentity{DeviceID: "dev-D"}, false); !errors.Is(err, errBoom) {
		t.Fatalf("want errBoom propagated from Del, got %v", err)
	}
}
