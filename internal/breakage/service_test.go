/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

package breakage

import (
	"context"
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/alert"
)

func bg() context.Context { return context.Background() }

const day = int64(86400)

// ============================================================================
// 包内 fake Repo（记录入参 + 可注错误 / 桩返回）
// ============================================================================

type fakeRepo struct {
	// 桩返回
	overview Overview
	detail   []DetailRow
	total    int64
	trend    []SnapshotPoint
	collect  []Snapshot

	// 注错（非 nil 则对应方法返回该错误）
	overviewErr error
	detailErr   error
	trendErr    error
	collectErr  error
	upsertErr   error

	// 捕获最近一次入参（断言透传）
	gotTenantID  *int64
	gotNow       int64
	gotFilter    Filter
	gotStart     int64
	gotEnd       int64
	upserted     []Snapshot
	collectCalls int
	upsertCalls  int
}

func (f *fakeRepo) Overview(_ context.Context, tenantID *int64, now int64) (Overview, error) {
	f.gotTenantID, f.gotNow = tenantID, now
	if f.overviewErr != nil {
		return Overview{}, f.overviewErr
	}
	return f.overview, nil
}

func (f *fakeRepo) Detail(_ context.Context, filter Filter, now int64) ([]DetailRow, int64, error) {
	f.gotFilter, f.gotNow = filter, now
	if f.detailErr != nil {
		return nil, 0, f.detailErr
	}
	return f.detail, f.total, nil
}

func (f *fakeRepo) UpsertSnapshots(_ context.Context, rows []Snapshot) error {
	f.upsertCalls++
	f.upserted = rows
	return f.upsertErr
}

func (f *fakeRepo) SnapshotTrend(_ context.Context, tenantID *int64, start, end int64) ([]SnapshotPoint, error) {
	f.gotTenantID, f.gotStart, f.gotEnd = tenantID, start, end
	if f.trendErr != nil {
		return nil, f.trendErr
	}
	return f.trend, nil
}

func (f *fakeRepo) CollectSnapshots(_ context.Context, tenantID *int64, now int64) ([]Snapshot, error) {
	f.collectCalls++
	f.gotTenantID, f.gotNow = tenantID, now
	if f.collectErr != nil {
		return nil, f.collectErr
	}
	return f.collect, nil
}

// ============================================================================
// 包内 fake AlertSink（记录分发的告警 + 可注错误）
// ============================================================================

type fakeSink struct {
	dispatched []alert.Alert
	err        error
}

func (s *fakeSink) Dispatch(_ context.Context, a alert.Alert) error {
	s.dispatched = append(s.dispatched, a)
	return s.err
}

// enabledCfg 返回启用态告警配置（阈值默认走 DefaultThresholdPct）。
func enabledCfg(threshold float64) alert.Config {
	return alert.Config{Enabled: true, ThresholdPct: threshold}
}

func ptr(v int64) *int64 { return &v }

// ============================================================================
// round2 / breakagePct / validateRange / validateAlertLevel 纯函数
// ============================================================================

func TestRound2(t *testing.T) {
	// 注意：避开 x.xx5 的 IEEE754 表示歧义（如 1.005*100 实为 100.4999…）——只断言无歧义样本。
	cases := []struct {
		in, want float64
	}{
		{1.004, 1.0},      // 第 3 位 <5 向下
		{1.006, 1.01},     // 第 3 位 >5 向上
		{-1.006, -1.01},   // 负数向上（远零）
		{0, 0},            // 零
		{2.5, 2.5},        // 已 2 位不变
		{123.456, 123.46}, // 第 3 位 6 → 进位
		{7.891, 7.89},     // 第 3 位 1 → 舍
	}
	for _, c := range cases {
		if got := round2(c.in); got != c.want {
			t.Errorf("round2(%v) = %v, want %v", c.in, got, c.want)
		}
	}
	// 幂等
	if round2(round2(1.006)) != round2(1.006) {
		t.Error("round2 not idempotent")
	}
}

func TestBreakagePct(t *testing.T) {
	cases := []struct {
		name          string
		unused, limit float64
		want          float64
	}{
		{"normal 50%", 5, 10, 50},
		{"full 100%", 10, 10, 100},
		{"zero limit with unused -> 100", 3, 0, 100},
		{"zero limit no unused -> 0", 0, 0, 0},
		{"negative limit with unused -> 100", 4, -1, 100},
		{"negative limit no unused -> 0", 0, -1, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := breakagePct(c.unused, c.limit); got != c.want {
				t.Errorf("breakagePct(%v,%v) = %v, want %v", c.unused, c.limit, got, c.want)
			}
		})
	}
}

func TestValidateRange(t *testing.T) {
	cases := []struct {
		name       string
		start, end int64
		wantErr    bool
	}{
		{"both zero -> skip", 0, 0, false},
		{"start zero -> skip", 0, 100, false},
		{"end zero -> skip", 100, 0, false},
		{"valid same", 100, 100, false},
		{"valid within 366d", 0 + 1, 1 + maxRangeSeconds, false},
		{"start > end", 200, 100, true},
		{"span exactly 366d ok", 1, 1 + maxRangeSeconds, false},
		{"span over 366d", 1, 2 + maxRangeSeconds, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateRange(c.start, c.end)
			if c.wantErr && !errors.Is(err, ErrRangeInvalid) {
				t.Errorf("validateRange(%d,%d) err=%v, want ErrRangeInvalid", c.start, c.end, err)
			}
			if !c.wantErr && err != nil {
				t.Errorf("validateRange(%d,%d) unexpected err=%v", c.start, c.end, err)
			}
		})
	}
}

func TestValidateAlertLevel(t *testing.T) {
	for _, l := range []AlertLevel{AlertNone, AlertWarn, AlertCritical, AlertExhausted} {
		if err := validateAlertLevel(l); err != nil {
			t.Errorf("validateAlertLevel(%q) unexpected err=%v", l, err)
		}
	}
	if err := validateAlertLevel(AlertLevel("bogus")); !errors.Is(err, ErrAlertLevelInvalid) {
		t.Errorf("validateAlertLevel(bogus) err=%v, want ErrAlertLevelInvalid", err)
	}
}

// ============================================================================
// Overview — 组装 + round2 + 透传作用域 + 错误上浮
// ============================================================================

func TestOverviewRoundsAndPassesScope(t *testing.T) {
	repo := &fakeRepo{overview: Overview{
		ActiveRemainingUSD: 12.3456,
		ExpiredUnusedUSD:   7.891,
		WalletUnusedUSD:    0.006,
		AnomalyCount:       3,
	}}
	svc := NewService(repo, &fakeSink{}, alert.Config{})
	scope := ptr(42)
	got, err := svc.Overview(bg(), scope, 1000)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.ActiveRemainingUSD != 12.35 || got.ExpiredUnusedUSD != 7.89 || got.WalletUnusedUSD != 0.01 {
		t.Errorf("round2 not applied: %+v", got)
	}
	if got.AnomalyCount != 3 {
		t.Errorf("AnomalyCount mutated: %d", got.AnomalyCount)
	}
	if repo.gotTenantID != scope || repo.gotNow != 1000 {
		t.Errorf("scope/now not passed through: tenant=%v now=%d", repo.gotTenantID, repo.gotNow)
	}
}

func TestOverviewErrorPropagates(t *testing.T) {
	sentinel := errors.New("boom")
	svc := NewService(&fakeRepo{overviewErr: sentinel}, &fakeSink{}, alert.Config{})
	_, err := svc.Overview(bg(), nil, 1)
	if !errors.Is(err, sentinel) {
		t.Errorf("err=%v, want sentinel", err)
	}
}

// ============================================================================
// Detail — 分页透传 + round2 + 区间/告警级校验 + 错误上浮
// ============================================================================

func TestDetailPassThroughAndRounds(t *testing.T) {
	repo := &fakeRepo{
		detail: []DetailRow{
			{UserID: 1, LimitUSD: 10.006, UsedUSD: 3.334, UnusedUSD: 6.671, UsagePct: 33.34},
		},
		total: 57,
	}
	svc := NewService(repo, &fakeSink{}, alert.Config{})
	f := Filter{TenantID: ptr(9), PlanCode: "pro", AlertLevel: AlertWarn, Page: 2, PageSize: 20}
	rows, total, err := svc.Detail(bg(), f, 500)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if total != 57 {
		t.Errorf("total=%d, want 57", total)
	}
	r := rows[0]
	if r.LimitUSD != 10.01 || r.UsedUSD != 3.33 || r.UnusedUSD != 6.67 {
		t.Errorf("round2 not applied: %+v", r)
	}
	if r.UsagePct != 33.34 {
		t.Errorf("UsagePct should be untouched: %v", r.UsagePct)
	}
	if repo.gotFilter.PlanCode != "pro" || repo.gotFilter.Page != 2 || repo.gotFilter.TenantID != f.TenantID {
		t.Errorf("filter not passed through: %+v", repo.gotFilter)
	}
	if repo.gotNow != 500 {
		t.Errorf("now not passed: %d", repo.gotNow)
	}
}

func TestDetailRejectsInvalidRange(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, &fakeSink{}, alert.Config{})
	_, _, err := svc.Detail(bg(), Filter{Start: 1000, End: 500}, 0)
	if !errors.Is(err, ErrRangeInvalid) {
		t.Errorf("err=%v, want ErrRangeInvalid", err)
	}
	if repo.gotNow != 0 && repo.gotFilter.Start != 0 {
		t.Error("repo.Detail should not be reached on invalid range")
	}
}

func TestDetailRejectsInvalidAlertLevel(t *testing.T) {
	svc := NewService(&fakeRepo{}, &fakeSink{}, alert.Config{})
	_, _, err := svc.Detail(bg(), Filter{AlertLevel: AlertLevel("nope")}, 0)
	if !errors.Is(err, ErrAlertLevelInvalid) {
		t.Errorf("err=%v, want ErrAlertLevelInvalid", err)
	}
}

func TestDetailErrorPropagates(t *testing.T) {
	sentinel := errors.New("db down")
	svc := NewService(&fakeRepo{detailErr: sentinel}, &fakeSink{}, alert.Config{})
	_, _, err := svc.Detail(bg(), Filter{}, 0)
	if !errors.Is(err, sentinel) {
		t.Errorf("err=%v, want sentinel", err)
	}
}

// ============================================================================
// Snapshots — 区间校验 + round2 + 透传 + 错误上浮
// ============================================================================

func TestSnapshotsRoundsAndPassesScope(t *testing.T) {
	repo := &fakeRepo{trend: []SnapshotPoint{
		{PeriodEnd: 100, ExpiredUnusedUSD: 1.111, ActiveRemainingUSD: 2.226, SubscriptionCount: 4},
	}}
	svc := NewService(repo, &fakeSink{}, alert.Config{})
	scope := ptr(7)
	pts, err := svc.Snapshots(bg(), scope, 10, 20)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if pts[0].ExpiredUnusedUSD != 1.11 || pts[0].ActiveRemainingUSD != 2.23 {
		t.Errorf("round2 not applied: %+v", pts[0])
	}
	if pts[0].SubscriptionCount != 4 {
		t.Errorf("count mutated: %d", pts[0].SubscriptionCount)
	}
	if repo.gotTenantID != scope || repo.gotStart != 10 || repo.gotEnd != 20 {
		t.Errorf("scope/range not passed: tenant=%v start=%d end=%d", repo.gotTenantID, repo.gotStart, repo.gotEnd)
	}
}

func TestSnapshotsRejectsInvalidRange(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, &fakeSink{}, alert.Config{})
	_, err := svc.Snapshots(bg(), nil, 1, 2+maxRangeSeconds)
	if !errors.Is(err, ErrRangeInvalid) {
		t.Errorf("err=%v, want ErrRangeInvalid", err)
	}
}

func TestSnapshotsErrorPropagates(t *testing.T) {
	sentinel := errors.New("trend fail")
	svc := NewService(&fakeRepo{trendErr: sentinel}, &fakeSink{}, alert.Config{})
	_, err := svc.Snapshots(bg(), nil, 0, 0)
	if !errors.Is(err, sentinel) {
		t.Errorf("err=%v, want sentinel", err)
	}
}

// ============================================================================
// RunSnapshot — collect→upsert→count，错误分支，空集短路
// ============================================================================

func TestRunSnapshotUpsertsAndCounts(t *testing.T) {
	repo := &fakeRepo{collect: []Snapshot{
		{SubID: 1, PeriodEnd: 100, UnusedUSD: 0}, // 未到期沉淀为 0（不触发告警）
		{SubID: 2, PeriodEnd: 200, UnusedUSD: 0},
	}}
	svc := NewService(repo, &fakeSink{}, alert.Config{}) // 告警关闭
	scope := ptr(5)
	n, err := svc.RunSnapshot(bg(), scope, 5000)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if n != 2 {
		t.Errorf("count=%d, want 2", n)
	}
	if repo.collectCalls != 1 || repo.upsertCalls != 1 {
		t.Errorf("collect=%d upsert=%d, want 1/1", repo.collectCalls, repo.upsertCalls)
	}
	if repo.gotTenantID != scope || repo.gotNow != 5000 {
		t.Errorf("scope/now not passed: tenant=%v now=%d", repo.gotTenantID, repo.gotNow)
	}
	if len(repo.upserted) != 2 {
		t.Errorf("upserted rows=%d, want 2", len(repo.upserted))
	}
}

func TestRunSnapshotEmptyShortCircuits(t *testing.T) {
	repo := &fakeRepo{collect: nil}
	svc := NewService(repo, &fakeSink{}, enabledCfg(0))
	n, err := svc.RunSnapshot(bg(), nil, 1)
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v, want 0/nil", n, err)
	}
	if repo.upsertCalls != 0 {
		t.Errorf("upsert should be skipped on empty collect, got %d calls", repo.upsertCalls)
	}
}

func TestRunSnapshotCollectError(t *testing.T) {
	sentinel := errors.New("collect fail")
	repo := &fakeRepo{collectErr: sentinel}
	svc := NewService(repo, &fakeSink{}, alert.Config{})
	_, err := svc.RunSnapshot(bg(), nil, 1)
	if !errors.Is(err, sentinel) {
		t.Errorf("err=%v, want sentinel", err)
	}
	if repo.upsertCalls != 0 {
		t.Error("upsert should not run when collect fails")
	}
}

func TestRunSnapshotUpsertError(t *testing.T) {
	sentinel := errors.New("upsert fail")
	repo := &fakeRepo{collect: []Snapshot{{SubID: 1, PeriodEnd: 1}}, upsertErr: sentinel}
	svc := NewService(repo, &fakeSink{}, alert.Config{})
	n, err := svc.RunSnapshot(bg(), nil, 1)
	if !errors.Is(err, sentinel) || n != 0 {
		t.Errorf("n=%d err=%v, want 0/sentinel", n, err)
	}
}

// ============================================================================
// 告警阈值判定 — RunSnapshot 后 maybeAlert 触发/跳过
// ============================================================================

func TestRunSnapshotAlertFiresWhenOverThreshold(t *testing.T) {
	// now=10*day；两笔已到期（period_end < now）未使用沉淀：unused=9, limit=10 → 90% ≥ 阈值 80
	now := 10 * day
	repo := &fakeRepo{collect: []Snapshot{
		{SubID: 1, PeriodEnd: 5 * day, LimitUSD: 6, UsedUSD: 0.5, UnusedUSD: 5.5},
		{SubID: 2, PeriodEnd: 6 * day, LimitUSD: 4, UsedUSD: 0.5, UnusedUSD: 3.5},
		{SubID: 3, PeriodEnd: 20 * day, LimitUSD: 100, UsedUSD: 0, UnusedUSD: 100}, // 未到期，不计
	}}
	sink := &fakeSink{}
	svc := NewService(repo, sink, enabledCfg(80))
	if _, err := svc.RunSnapshot(bg(), nil, now); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(sink.dispatched) != 1 {
		t.Fatalf("dispatched=%d, want 1", len(sink.dispatched))
	}
	a := sink.dispatched[0]
	if a.Level != alert.LevelWarning {
		t.Errorf("level=%v, want warning", a.Level)
	}
	if a.DedupKey == "" {
		t.Error("expected non-empty dedup key")
	}
	// 去重键按整天：now=10*day 已是整天，day 锚点应为 10*day
	wantKey := "breakage_expired_unused:" + itoa(now)
	if a.DedupKey != wantKey {
		t.Errorf("dedupKey=%q, want %q", a.DedupKey, wantKey)
	}
}

func TestRunSnapshotAlertSkipsBelowThreshold(t *testing.T) {
	now := 10 * day
	// 到期沉淀 unused=1, limit=10 → 10% < 阈值 80 → 不告警
	repo := &fakeRepo{collect: []Snapshot{
		{SubID: 1, PeriodEnd: 5 * day, LimitUSD: 10, UsedUSD: 9, UnusedUSD: 1},
	}}
	sink := &fakeSink{}
	svc := NewService(repo, sink, enabledCfg(80))
	if _, err := svc.RunSnapshot(bg(), nil, now); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(sink.dispatched) != 0 {
		t.Errorf("dispatched=%d, want 0 (below threshold)", len(sink.dispatched))
	}
}

func TestRunSnapshotAlertSkipsWhenDisabled(t *testing.T) {
	now := 10 * day
	repo := &fakeRepo{collect: []Snapshot{
		{SubID: 1, PeriodEnd: 5 * day, LimitUSD: 10, UsedUSD: 0, UnusedUSD: 10},
	}}
	sink := &fakeSink{}
	// Enabled=false → 即使沉淀 100% 也不告警
	svc := NewService(repo, sink, alert.Config{Enabled: false, ThresholdPct: 80})
	if _, err := svc.RunSnapshot(bg(), nil, now); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(sink.dispatched) != 0 {
		t.Errorf("dispatched=%d, want 0 (disabled)", len(sink.dispatched))
	}
}

func TestRunSnapshotAlertSkipsWhenNoSink(t *testing.T) {
	now := 10 * day
	repo := &fakeRepo{collect: []Snapshot{
		{SubID: 1, PeriodEnd: 5 * day, LimitUSD: 10, UsedUSD: 0, UnusedUSD: 10},
	}}
	// sink=nil + Enabled=true → 不 panic，静默跳过
	svc := NewService(repo, nil, enabledCfg(80))
	n, err := svc.RunSnapshot(bg(), nil, now)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v, want 1/nil", n, err)
	}
}

func TestRunSnapshotAlertDefaultThresholdWhenZero(t *testing.T) {
	now := 10 * day
	// ThresholdPct=0 → 回退 DefaultThresholdPct(95)。沉淀 96% ≥ 95 → 触发。
	repo := &fakeRepo{collect: []Snapshot{
		{SubID: 1, PeriodEnd: 5 * day, LimitUSD: 100, UsedUSD: 4, UnusedUSD: 96},
	}}
	sink := &fakeSink{}
	svc := NewService(repo, sink, enabledCfg(0))
	if _, err := svc.RunSnapshot(bg(), nil, now); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(sink.dispatched) != 1 {
		t.Errorf("dispatched=%d, want 1 (96%% >= default 95%%)", len(sink.dispatched))
	}
}

func TestRunSnapshotAlertErrorSwallowed(t *testing.T) {
	now := 10 * day
	repo := &fakeRepo{collect: []Snapshot{
		{SubID: 1, PeriodEnd: 5 * day, LimitUSD: 10, UsedUSD: 0, UnusedUSD: 10},
	}}
	sink := &fakeSink{err: errors.New("smtp down")}
	svc := NewService(repo, sink, enabledCfg(80))
	// 告警 Dispatch 失败不应影响 RunSnapshot 返回落库条数与 nil error。
	n, err := svc.RunSnapshot(bg(), nil, now)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v, want 1/nil (alert error must be swallowed)", n, err)
	}
	if len(sink.dispatched) != 1 {
		t.Error("alert should still be attempted")
	}
}

func TestRunSnapshotAlertNoExpiredNoDispatch(t *testing.T) {
	now := 10 * day
	// 全部未到期（period_end >= now）→ expiredCount==0 → 不告警
	repo := &fakeRepo{collect: []Snapshot{
		{SubID: 1, PeriodEnd: 20 * day, LimitUSD: 10, UsedUSD: 0, UnusedUSD: 10},
	}}
	sink := &fakeSink{}
	svc := NewService(repo, sink, enabledCfg(80))
	if _, err := svc.RunSnapshot(bg(), nil, now); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(sink.dispatched) != 0 {
		t.Errorf("dispatched=%d, want 0 (nothing expired)", len(sink.dispatched))
	}
}

// itoa 是测试内联的 int64→string（避免 import strconv 只为一处）。
func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
