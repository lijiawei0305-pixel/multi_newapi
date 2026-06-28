package risk

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"newapi-mt/internal/platform/appctx"
	"newapi-mt/internal/platform/apperr"
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

func TestCheckPurchaseLimit_TrialGetErrorPropagates(t *testing.T) {
	kv := kvErrOn{KVCache: NewMemKVCache(nil), get: true}
	e := NewEngine(kv)
	if err := e.CheckPurchaseLimit(context.Background(), 7, trialPlan()); !errors.Is(err, errBoom) {
		t.Fatalf("want errBoom, got %v", err)
	}
}

func TestCheckPurchaseLimit_TrialSetNXErrorPropagates(t *testing.T) {
	kv := kvErrOn{KVCache: NewMemKVCache(nil), setnx: true}
	e := NewEngine(kv)
	if err := e.CheckPurchaseLimit(context.Background(), 7, trialPlan()); !errors.Is(err, errBoom) {
		t.Fatalf("want errBoom, got %v", err)
	}
}

// 并发同用户购买 Trial：恰好 1 个成功（SetNX 决胜锁，-race 通过）。
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
