package wallet

import (
	"context"
	"sync"

	"newapi-mt/internal/platform/appctx"
)

// contextWithTenant 构造携带 Principal(tenant,user) 的 ctx，供需要租户上下文的用例复用。
func contextWithTenant(tenantID, userID int64) context.Context {
	return appctx.WithPrincipal(context.Background(), appctx.Principal{
		TenantID: tenantID, UserID: userID, Role: appctx.RoleUser,
	})
}

// fakePricing 是 PricingService 的内存假实现（仅供单测）。
type fakePricing struct {
	// ratios: tenantID -> groupID -> 倍率。未命中返回 defaultRatio。
	ratios       map[int64]map[int64]float64
	defaultRatio float64
	err          error // 可注入，用于覆盖错误传播
	calls        int
}

func newFakePricing(defaultRatio float64) *fakePricing {
	return &fakePricing{ratios: map[int64]map[int64]float64{}, defaultRatio: defaultRatio}
}

func (f *fakePricing) set(tenantID, groupID int64, ratio float64) {
	if f.ratios[tenantID] == nil {
		f.ratios[tenantID] = map[int64]float64{}
	}
	f.ratios[tenantID][groupID] = ratio
}

func (f *fakePricing) GroupRatio(_ context.Context, tenantID, groupID int64) (float64, error) {
	f.calls++
	if f.err != nil {
		return 0, f.err
	}
	if byGroup, ok := f.ratios[tenantID]; ok {
		if r, ok := byGroup[groupID]; ok {
			return r, nil
		}
	}
	return f.defaultRatio, nil
}

// fakeEarnings 是 EarningSink 的内存假实现，记录收到的收益条目（并发安全）。
type fakeEarnings struct {
	mu      sync.Mutex
	entries []EarningEntry
	err     error // 可注入，用于覆盖错误传播
}

func newFakeEarnings() *fakeEarnings { return &fakeEarnings{} }

func (f *fakeEarnings) AddEarning(_ context.Context, e EarningEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.entries = append(f.entries, e)
	return nil
}

func (f *fakeEarnings) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.entries)
}

func (f *fakeEarnings) last() (EarningEntry, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.entries) == 0 {
		return EarningEntry{}, false
	}
	return f.entries[len(f.entries)-1], true
}
