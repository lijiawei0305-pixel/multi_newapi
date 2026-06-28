package stats

import (
	"context"
	"errors"
	"sync"
	"testing"

	"newapi-mt/internal/platform/appctx"
	"newapi-mt/internal/platform/apperr"
)

// ---- 测试用 erroring 假实现（内存假实现总是成功，故错误传播用专门的失败桩）----

type errBillingReader struct{ err error }

func (e errBillingReader) TenantBillings(context.Context) ([]TenantBilling, error) {
	return nil, e.err
}
func (e errBillingReader) TenantBillingByID(context.Context, int64) (TenantBilling, error) {
	return TenantBilling{}, e.err
}

type errSubReader struct{ err error }

func (e errSubReader) ActiveSubscriptions(context.Context) ([]SubscriptionUsage, error) {
	return nil, e.err
}

func bg() context.Context { return context.Background() }

// ---- 构造与阈值 ----

func TestNewServiceNotNil(t *testing.T) {
	if NewService(NewMemBillingReader(), NewMemSubscriptionReader()) == nil {
		t.Fatal("NewService returned nil")
	}
}

func TestNewServiceWithThresholdClamping(t *testing.T) {
	cases := []struct {
		name  string
		in    float64
		alert float64 // 取一个 ratio=0.8 的订阅，是否应触发，间接反映生效阈值
		want  bool
	}{
		{"valid 0.9 not triggered at 0.8", 0.9, 0.8, false}, // 0.8 < 0.9 → 不预警
		{"valid 0.5 triggered at 0.8", 0.5, 0.8, true},      // 0.8 ≥ 0.5 → 预警
		{"zero falls back to default", 0, 0.8, true},        // 回退 0.8 → 0.8≥0.8 预警
		{"negative falls back", -1, 0.8, true},              // 回退 0.8
		{"too large falls back", 1.5, 0.8, true},            // 回退 0.8
		{"exactly 1 kept", 1.0, 1.0, true},                  // 阈值=1，ratio=1 → 预警
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			subs := NewMemSubscriptionReader()
			// used/limit = c.alert（如 8/10=0.8 或 10/10=1.0）
			subs.Add(SubscriptionUsage{SubscriptionID: 1, UsedUSD: c.alert * 10, MonthLimitUSD: 10})
			svc := NewServiceWithThreshold(NewMemBillingReader(), subs, c.in)
			got, err := svc.SubscriptionAlerts(bg())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if (len(got) == 1) != c.want {
				t.Fatalf("threshold %v: alerts=%d, want triggered=%v", c.in, len(got), c.want)
			}
		})
	}
}

// ---- AdminOverview ----

func TestAdminOverviewAggregates(t *testing.T) {
	br := NewMemBillingReader()
	br.Put(TenantBilling{TenantID: 1, Active: true, UserCount: 10, TokenCount: 20, Calls: 100, ChargedUSD: 5, RevenueUSD: 9, EarningUSD: 2})
	br.Put(TenantBilling{TenantID: 2, Active: true, Calls: 50, ChargedUSD: 2.5, EarningUSD: 1})
	br.Put(TenantBilling{TenantID: 3, Active: false, Calls: 7, ChargedUSD: 0.5, EarningUSD: 0.25}) // suspended/deleted
	svc := NewService(br, NewMemSubscriptionReader())

	got, err := svc.AdminOverview(bg())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := &AdminStats{
		TotalTenants:    3,
		ActiveTenants:   2,
		TotalCalls:      157,
		TotalChargedUSD: 8.0,
		TotalEarningUSD: 3.25,
	}
	if *got != *want {
		t.Fatalf("AdminOverview = %+v, want %+v", *got, *want)
	}
}

func TestAdminOverviewEmpty(t *testing.T) {
	svc := NewService(NewMemBillingReader(), NewMemSubscriptionReader())
	got, err := svc.AdminOverview(bg())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *got != (AdminStats{}) {
		t.Fatalf("empty overview should be all-zero, got %+v", *got)
	}
}

func TestAdminOverviewReaderErrorPropagates(t *testing.T) {
	sentinel := errors.New("billing read failed")
	svc := NewService(errBillingReader{err: sentinel}, NewMemSubscriptionReader())
	if _, err := svc.AdminOverview(bg()); !errors.Is(err, sentinel) {
		t.Fatalf("expected reader error to propagate, got %v", err)
	}
}

// ---- TenantOverview：范围与隔离 ----

func TestTenantOverviewScopedToTenant(t *testing.T) {
	br := NewMemBillingReader()
	br.Put(TenantBilling{TenantID: 1, UserCount: 11, TokenCount: 22, Calls: 33, RevenueUSD: 44, EarningUSD: 55})
	br.Put(TenantBilling{TenantID: 2, UserCount: 999, TokenCount: 999, Calls: 999, RevenueUSD: 999, EarningUSD: 999})
	svc := NewService(br, NewMemSubscriptionReader())

	got, err := svc.TenantOverview(bg(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := &TenantStats{TenantID: 1, UserCount: 11, TokenCount: 22, Calls: 33, RevenueUSD: 44, EarningUSD: 55}
	if *got != *want {
		t.Fatalf("TenantOverview(1) = %+v, want %+v (must not leak tenant 2)", *got, *want)
	}
}

func TestTenantOverviewUnknownTenantZero(t *testing.T) {
	svc := NewService(NewMemBillingReader(), NewMemSubscriptionReader())
	got, err := svc.TenantOverview(bg(), 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *got != (TenantStats{TenantID: 42}) {
		t.Fatalf("unknown tenant should yield zero stats with TenantID set, got %+v", *got)
	}
}

func TestTenantOverviewReaderErrorPropagates(t *testing.T) {
	sentinel := errors.New("billing read failed")
	svc := NewService(errBillingReader{err: sentinel}, NewMemSubscriptionReader())
	if _, err := svc.TenantOverview(bg(), 1); !errors.Is(err, sentinel) {
		t.Fatalf("expected reader error to propagate, got %v", err)
	}
}

// 防御性跨租户隔离：依 ctx 中 Principal 的角色/租户决定放行或 STATS_CROSS_TENANT。
func TestTenantOverviewCrossTenantGuard(t *testing.T) {
	br := NewMemBillingReader()
	br.Put(TenantBilling{TenantID: 1, Calls: 1})
	br.Put(TenantBilling{TenantID: 2, Calls: 2})
	svc := NewService(br, NewMemSubscriptionReader())

	cases := []struct {
		name      string
		ctx       context.Context
		tenantID  int64
		wantBlock bool
	}{
		{"no principal (internal) allowed", bg(), 2, false},
		{"admin sees any tenant", appctx.WithPrincipal(bg(), appctx.Principal{Role: appctx.RoleAdmin, TenantID: 1}), 2, false},
		{"agent owner sees own tenant", appctx.WithPrincipal(bg(), appctx.Principal{Role: appctx.RoleAgentOwner, TenantID: 2}), 2, false},
		{"agent owner blocked on other tenant", appctx.WithPrincipal(bg(), appctx.Principal{Role: appctx.RoleAgentOwner, TenantID: 1}), 2, true},
		{"user blocked on other tenant", appctx.WithPrincipal(bg(), appctx.Principal{Role: appctx.RoleUser, TenantID: 1}), 2, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := svc.TenantOverview(c.ctx, c.tenantID)
			if c.wantBlock {
				if !apperr.Is(err, "STATS_CROSS_TENANT") {
					t.Fatalf("want STATS_CROSS_TENANT, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("want allowed, got %v", err)
			}
		})
	}
}

// ---- SubscriptionAlerts：阈值边界 ----

func TestSubscriptionAlertsThresholdBoundary(t *testing.T) {
	subs := NewMemSubscriptionReader()
	subs.Add(SubscriptionUsage{SubscriptionID: 1, UsedUSD: 79, MonthLimitUSD: 100})  // 0.79 < 0.8 → 否
	subs.Add(SubscriptionUsage{SubscriptionID: 2, UsedUSD: 8, MonthLimitUSD: 10})    // 0.80 = 0.8 → 是（边界含等于）
	subs.Add(SubscriptionUsage{SubscriptionID: 3, UsedUSD: 81, MonthLimitUSD: 100})  // 0.81 > 0.8 → 是
	subs.Add(SubscriptionUsage{SubscriptionID: 4, UsedUSD: 100, MonthLimitUSD: 100}) // 1.00 → 是
	svc := NewService(NewMemBillingReader(), subs)

	got, err := svc.SubscriptionAlerts(bg())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	gotIDs := map[int64]bool{}
	for _, a := range got {
		gotIDs[a.SubscriptionID] = true
	}
	if gotIDs[1] {
		t.Errorf("sub 1 (0.79) must NOT alert at threshold 0.8")
	}
	for _, id := range []int64{2, 3, 4} {
		if !gotIDs[id] {
			t.Errorf("sub %d must alert at threshold 0.8", id)
		}
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 alerts, got %d: %+v", len(got), got)
	}
}

func TestSubscriptionAlertsRatioAndFields(t *testing.T) {
	subs := NewMemSubscriptionReader()
	subs.Add(SubscriptionUsage{SubscriptionID: 7, TenantID: 1001, UserID: 7, PlanCode: "max", UsedUSD: 9, MonthLimitUSD: 10})
	svc := NewService(NewMemBillingReader(), subs)

	got, err := svc.SubscriptionAlerts(bg())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(got))
	}
	want := SubAlert{SubscriptionID: 7, TenantID: 1001, UserID: 7, PlanCode: "max", UsedUSD: 9, MonthLimitUSD: 10, Ratio: 0.9}
	if got[0] != want {
		t.Fatalf("alert = %+v, want %+v", got[0], want)
	}
}

// 月限额非法/为 0 的安全处理：有消耗 → 视为已满(1.0)预警；无消耗 → 不预警；杜绝 +Inf/NaN。
func TestSubscriptionAlertsZeroLimit(t *testing.T) {
	subs := NewMemSubscriptionReader()
	subs.Add(SubscriptionUsage{SubscriptionID: 1, UsedUSD: 5, MonthLimitUSD: 0})  // 0 限额 + 有消耗 → 预警(1.0)
	subs.Add(SubscriptionUsage{SubscriptionID: 2, UsedUSD: 0, MonthLimitUSD: 0})  // 0 限额 + 无消耗 → 否
	subs.Add(SubscriptionUsage{SubscriptionID: 3, UsedUSD: 1, MonthLimitUSD: -5}) // 负限额 + 有消耗 → 预警(1.0)
	svc := NewService(NewMemBillingReader(), subs)

	got, err := svc.SubscriptionAlerts(bg())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 alerts (id 1,3), got %d: %+v", len(got), got)
	}
	for _, a := range got {
		if a.Ratio != 1.0 {
			t.Errorf("sub %d: ratio=%v, want capped 1.0", a.SubscriptionID, a.Ratio)
		}
	}
}

func TestSubscriptionAlertsEmptyReturnsNil(t *testing.T) {
	svc := NewService(NewMemBillingReader(), NewMemSubscriptionReader())
	got, err := svc.SubscriptionAlerts(bg())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("no subscriptions should yield nil alerts, got %+v", got)
	}
}

func TestSubscriptionAlertsReaderErrorPropagates(t *testing.T) {
	sentinel := errors.New("subscription read failed")
	svc := NewService(NewMemBillingReader(), errSubReader{err: sentinel})
	if _, err := svc.SubscriptionAlerts(bg()); !errors.Is(err, sentinel) {
		t.Fatalf("expected reader error to propagate, got %v", err)
	}
}

// ---- 内存假实现直接覆盖 ----

func TestMemBillingReaderPutOverwriteAndOrder(t *testing.T) {
	br := NewMemBillingReader()
	br.Put(TenantBilling{TenantID: 3, Calls: 3})
	br.Put(TenantBilling{TenantID: 1, Calls: 1})
	br.Put(TenantBilling{TenantID: 1, Calls: 99}) // 覆盖
	rows, err := br.TenantBillings(bg())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 || rows[0].TenantID != 1 || rows[1].TenantID != 3 {
		t.Fatalf("expected ascending [1,3], got %+v", rows)
	}
	if rows[0].Calls != 99 {
		t.Fatalf("Put should overwrite same tenant, Calls=%d want 99", rows[0].Calls)
	}
}

func TestMemSubscriptionReaderAddCopies(t *testing.T) {
	sr := NewMemSubscriptionReader()
	sr.Add(SubscriptionUsage{SubscriptionID: 1})
	out, _ := sr.ActiveSubscriptions(bg())
	out[0].SubscriptionID = 12345 // 篡改返回切片不应影响内部状态
	again, _ := sr.ActiveSubscriptions(bg())
	if again[0].SubscriptionID != 1 {
		t.Fatalf("ActiveSubscriptions must return a copy, internal mutated to %d", again[0].SubscriptionID)
	}
}

// 并发读写内存假实现 + service，配合 -race 检测数据竞争。
func TestConcurrentReadAggregateRace(t *testing.T) {
	br := NewMemBillingReader()
	sr := NewMemSubscriptionReader()
	svc := NewService(br, sr)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			br.Put(TenantBilling{TenantID: int64(n), Active: true, Calls: int64(n)})
			sr.Add(SubscriptionUsage{SubscriptionID: int64(n), UsedUSD: 9, MonthLimitUSD: 10})
			_, _ = svc.AdminOverview(bg())
			_, _ = svc.TenantOverview(bg(), int64(n))
			_, _ = svc.SubscriptionAlerts(bg())
		}(i)
	}
	wg.Wait()
}
