package billing

import (
	"context"
	"sync"

	"newapi-mt/internal/platform/quota"
)

// recorder 记录跨 mock 的调用顺序，用于断言「扣费 → 日志 → 分润」编排次序。
// 并发安全，支撑 -race 用例。
type recorder struct {
	mu     sync.Mutex
	events []string
}

func (r *recorder) add(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, s)
}

func (r *recorder) seq() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.events))
	copy(out, r.events)
	return out
}

// ---- fakeModelCatalog ----

type fakeModelCatalog struct {
	in, out float64
	err     error
}

func (f fakeModelCatalog) Price(_ context.Context, _ string) (float64, float64, error) {
	if f.err != nil {
		return 0, 0, f.err
	}
	return f.in, f.out, nil
}

// ---- fakeSource：quota.Source 的可注入 mock（成功/不足/超额/过期）----

type fakeSource struct {
	rec           *recorder
	kind          quota.Kind
	remaining     float64
	chargedReturn float64 // 0 → 回显请求额；非 0 → 作为桶权威扣费额返回
	err           error

	mu       sync.Mutex
	requests []float64 // 每次 Charge 收到的请求额
}

func (f *fakeSource) Charge(_ context.Context, costUSD float64) (quota.Receipt, error) {
	if f.rec != nil {
		f.rec.add("charge")
	}
	f.mu.Lock()
	f.requests = append(f.requests, costUSD)
	f.mu.Unlock()
	if f.err != nil {
		return quota.Receipt{}, f.err
	}
	charged := f.chargedReturn
	if charged == 0 {
		charged = costUSD
	}
	kind := f.kind
	if kind == "" {
		kind = quota.KindWallet
	}
	return quota.Receipt{Kind: kind, ChargedUSD: charged, RemainingUSD: f.remaining}, nil
}

func (f *fakeSource) Balance(_ context.Context) (float64, error) { return f.remaining, nil }

func (f *fakeSource) chargeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *fakeSource) lastRequest() (float64, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		return 0, false
	}
	return f.requests[len(f.requests)-1], true
}

// ---- fakeCallLogWriter ----

type fakeCallLogWriter struct {
	rec *recorder
	err error

	mu      sync.Mutex
	entries []CallLogEntry
}

func (f *fakeCallLogWriter) Write(_ context.Context, e CallLogEntry) error {
	if f.rec != nil {
		f.rec.add("log")
	}
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	f.entries = append(f.entries, e)
	f.mu.Unlock()
	return nil
}

func (f *fakeCallLogWriter) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.entries)
}

func (f *fakeCallLogWriter) last() (CallLogEntry, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.entries) == 0 {
		return CallLogEntry{}, false
	}
	return f.entries[len(f.entries)-1], true
}

// ---- fakeEarningSink ----

type fakeEarningSink struct {
	rec *recorder
	err error

	mu      sync.Mutex
	entries []EarningEntry
}

func (f *fakeEarningSink) AddEarning(_ context.Context, e EarningEntry) error {
	if f.rec != nil {
		f.rec.add("earn")
	}
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	f.entries = append(f.entries, e)
	f.mu.Unlock()
	return nil
}

func (f *fakeEarningSink) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.entries)
}

func (f *fakeEarningSink) last() (EarningEntry, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.entries) == 0 {
		return EarningEntry{}, false
	}
	return f.entries[len(f.entries)-1], true
}

// ---- fakeRouter：隔离 BillingService 单测，直接注入桶/错误 ----

type fakeRouter struct {
	src quota.Source
	err error

	gotUser   int64
	gotTenant int64
	calls     int
}

func (f *fakeRouter) Select(_ context.Context, userID, tenantID int64) (quota.Source, error) {
	f.calls++
	f.gotUser = userID
	f.gotTenant = tenantID
	if f.err != nil {
		return nil, f.err
	}
	return f.src, nil
}

// ---- QuotaRouter 依赖的 mock ----

type fakeSubChecker struct {
	active  bool
	err     error
	gotUser int64
	calls   int
}

func (f *fakeSubChecker) HasActive(_ context.Context, userID int64) (bool, error) {
	f.calls++
	f.gotUser = userID
	return f.active, f.err
}

type fakeWalletFactory struct {
	src       quota.Source
	err       error
	calls     int
	gotUser   int64
	gotTenant int64
}

func (f *fakeWalletFactory) WalletSource(_ context.Context, userID, tenantID int64) (quota.Source, error) {
	f.calls++
	f.gotUser = userID
	f.gotTenant = tenantID
	return f.src, f.err
}

type fakeSubFactory struct {
	src       quota.Source
	err       error
	calls     int
	gotUser   int64
	gotTenant int64
}

func (f *fakeSubFactory) SubscriptionSource(_ context.Context, userID, tenantID int64) (quota.Source, error) {
	f.calls++
	f.gotUser = userID
	f.gotTenant = tenantID
	return f.src, f.err
}
