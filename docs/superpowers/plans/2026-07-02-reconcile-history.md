# 对账记录 (reconcile-history) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Persist every reconcile run + a live heartbeat so admins can see reconcile is running and what it did, and make the manual "Reconcile Now" button run all 3 paths (fixing the drift where it skipped RCG-created).

**Architecture:** A single orchestrator `runReconcileAll(ctx, before, trigger)` runs all 3 reconcile paths (RCG-paid ①, RCG-created ②, SUB ③), aggregates results, upserts a single-row heartbeat every run, and appends a `reconcile_runs` history row when there is real work (any path scanned>0 or failed) OR the trigger is manual. Both the 5-min cron (`runReconcileOnce`) and the admin button (`HandleAdminRunReconcile`) call this one entry point. A new `GET /history` endpoint plus heartbeat fields on `GET /stuck` feed the existing admin React page (heartbeat banner + expandable history table).

**Tech Stack:** Go 1.21+ (gin+GORM), React 19 (web/default)

## Global Constraints
- Go module `github.com/QuantumNous/new-api`; multi-tenant increments live under `internal/`.
- Build/migrate/deploy/E2E happen ON THE SERVER (test stack `newapi_test`, port 3100) — local you can run Go unit tests only.
- .ts/.tsx files carry a GNU AGPL header (copy from an existing sibling file, e.g. `web/default/src/features/payment-reconcile/api.ts`).
- i18n: English key = identity; Chinese in `web/default/src/i18n/locales/zh.json` (`{"translation":{...}}`).
- TDD: write failing test → run (fail) → minimal impl → run (pass) → commit. Frequent commits.
- Reconcile is best-effort: recording failures (history/heartbeat) must NEVER break reconcile itself — all writes swallow errors.
- **Shared integration files (serialize with the human / other tracks): `internal/mtwire/wire.go` (Task 1) and `router/mt-router.go` (Task 5).** Flag before editing.
---

## File Structure

| File | Create/Modify | Responsibility |
| --- | --- | --- |
| `internal/mtwire/reconcile_history.go` | **Create** | Data layer: `reconcile_runs` + `reconcile_heartbeat` row structs, migrations, `recordReconcileRun`, `listReconcileRuns`, `reconcileHasFacts`, `updateReconcileHeartbeat`, `getReconcileHeartbeat`. |
| `internal/mtwire/reconcile_history_test.go` | **Create** | Tests for the data layer (migrate+roundtrip, heartbeat upsert + cross-day reset, hasFacts truth table) + shared test helper `newReconcileHistoryApp`. |
| `internal/mtwire/reconcile_orchestrate.go` | **Create** | 3-path seams (`reconcilePaidFn`/`reconcileCreatedFn`/`reconcileSubFn`) + unified `runReconcileAll` (aggregate → heartbeat → record) + preserved per-path logging. |
| `internal/mtwire/reconcile_orchestrate_test.go` | **Create** | Tests: 3-path aggregation, record policy (empty/facts/manual), heartbeat side-effect + shared stub `stubReconcileSeams`. |
| `internal/mtwire/reconcile_loop.go` | **Modify** | `runReconcileOnce` → calls `runReconcileAll(ctx, before, "cron")`; drop now-unused `fmt`/`payment` imports. |
| `internal/mtwire/reconcile_http.go` | **Modify** | `HandleAdminRunReconcile` → `runReconcileAll("manual")` (+`rcg_created` in response); new `HandleAdminListHistory` + `reconcileRunOut`; heartbeat on `/stuck` (`reconcileHeartbeatOut`). |
| `internal/mtwire/reconcile_http_test.go` | **Create** | httptest for `/run` drift fix, `/history` desc+limit, `/stuck` heartbeat + shared helper `newReconcileCtx`. |
| `router/mt-router.go` | **Modify** (SHARED) | Register `GET /api/admin/reconcile/history`. |
| `web/default/src/features/payment-reconcile/api.ts` | **Modify** | Add `ReconcileHeartbeat`, `heartbeat?` on `StuckList`, `rcg_created` on `ReconcileRunResult`, `HistoryRun` + `listHistory`. |
| `web/default/src/features/payment-reconcile/index.tsx` | **Modify** | Heartbeat banner (top) + expandable "Reconcile History" table (bottom). |
| `web/default/src/i18n/locales/zh.json` | **Modify** | Chinese for new English keys. |

---

### Task 1: Data layer — `reconcile_runs` + `reconcile_heartbeat` (structs, migrations, record/list, heartbeat, hasFacts)

**Files**
- Create: `internal/mtwire/reconcile_history.go`
- Create: `internal/mtwire/reconcile_history_test.go`
- Modify: `internal/mtwire/wire.go` (SHARED — the `Migrate()` method, currently ends line 308 `return migrateSubscriptionBridge(a.DB)`)

**Interfaces**
- Produces (Go):
  - `type reconcileRunRow struct { ID int64; RanAt time.Time; Trigger string; Summary string; Detail string }` (TableName `reconcile_runs`)
  - `type reconcileHeartbeatRow struct { ID int64; LastRunAt time.Time; LastTrigger string; TodayDate string; TodayRuns int; LastStuckCount int; LastFailedCount int }` (TableName `reconcile_heartbeat`, fixed id=1)
  - `func migrateReconcileRuns(db *gorm.DB) error`
  - `func migrateReconcileHeartbeat(db *gorm.DB) error`
  - `func reconcileHasFacts(paid, created payment.ReconcileResult, sub ReconcileSubResult) bool`
  - `func (a *App) recordReconcileRun(ctx context.Context, trigger string, paid, created payment.ReconcileResult, sub ReconcileSubResult)`
  - `func (a *App) listReconcileRuns(ctx context.Context, limit int) ([]reconcileRunRow, error)`
  - `func (a *App) updateReconcileHeartbeat(ctx context.Context, trigger string, stuckCount, failedCount int)`
  - `func (a *App) getReconcileHeartbeat(ctx context.Context) (reconcileHeartbeatRow, bool)`
  - Test helper `func newReconcileHistoryApp(t *testing.T) *App`
- Consumes (Go): `payment.ReconcileResult{Scanned int; Reconciled []string; Failed map[string]string}`, `mtwire.ReconcileSubResult{Scanned int; Activated []string; Unpaid []string; Failed map[string]string}`.

- [ ] **Step 1: Write the failing test file `internal/mtwire/reconcile_history_test.go`.**
```go
package mtwire

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/payment"
)

// newReconcileHistoryApp 装配一个仅带对账记录/心跳两表的 in-memory App（供本 track 各测复用）。
func newReconcileHistoryApp(t *testing.T) *App {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := migrateReconcileRuns(db); err != nil {
		t.Fatalf("migrate runs: %v", err)
	}
	if err := migrateReconcileHeartbeat(db); err != nil {
		t.Fatalf("migrate heartbeat: %v", err)
	}
	return &App{DB: db}
}

// TestRecordAndListReconcileRuns 落两条记录 → listReconcileRuns 按 ran_at 倒序、limit 生效。
func TestRecordAndListReconcileRuns(t *testing.T) {
	app := newReconcileHistoryApp(t)
	ctx := context.Background()
	paid := payment.ReconcileResult{Scanned: 1, Reconciled: []string{"RCG-1"}, Failed: map[string]string{}}
	created := payment.ReconcileResult{Failed: map[string]string{}}
	sub := ReconcileSubResult{Scanned: 2, Activated: []string{"SUB-1"}, Failed: map[string]string{}}

	app.recordReconcileRun(ctx, "cron", paid, created, sub)
	time.Sleep(2 * time.Millisecond) // 保证 ran_at 有先后
	app.recordReconcileRun(ctx, "manual", paid, created, sub)

	rows, err := app.listReconcileRuns(ctx, 50)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows=%d, want 2", len(rows))
	}
	if rows[0].Trigger != "manual" {
		t.Fatalf("first row trigger=%q, want manual (desc by ran_at)", rows[0].Trigger)
	}
	if limited, _ := app.listReconcileRuns(ctx, 1); len(limited) != 1 {
		t.Fatalf("limit=1 returned %d rows", len(limited))
	}
}

// TestReconcileHeartbeatUpsertAndDailyReset 首轮插入 today_runs=1；同日再轮 +1；跨天重置回 1。
func TestReconcileHeartbeatUpsertAndDailyReset(t *testing.T) {
	app := newReconcileHistoryApp(t)
	ctx := context.Background()

	app.updateReconcileHeartbeat(ctx, "cron", 3, 1)
	hb, ok := app.getReconcileHeartbeat(ctx)
	if !ok || hb.TodayRuns != 1 || hb.LastTrigger != "cron" || hb.LastStuckCount != 3 || hb.LastFailedCount != 1 {
		t.Fatalf("after first: ok=%v hb=%+v, want runs=1 trigger=cron stuck=3 failed=1", ok, hb)
	}

	app.updateReconcileHeartbeat(ctx, "manual", 0, 0)
	if hb, _ = app.getReconcileHeartbeat(ctx); hb.TodayRuns != 2 {
		t.Fatalf("same-day second run today_runs=%d, want 2", hb.TodayRuns)
	}

	// 造跨天：把 today_date 改成一个过去日期，再跑一轮应重置为 1。
	app.DB.Model(&reconcileHeartbeatRow{}).Where("id = ?", 1).Update("today_date", "2000-01-01")
	app.updateReconcileHeartbeat(ctx, "cron", 0, 0)
	hb, _ = app.getReconcileHeartbeat(ctx)
	if hb.TodayRuns != 1 {
		t.Fatalf("cross-day today_runs=%d, want reset to 1", hb.TodayRuns)
	}
	if hb.TodayDate != time.Now().Format("2006-01-02") {
		t.Fatalf("today_date=%q, want %q", hb.TodayDate, time.Now().Format("2006-01-02"))
	}
}

// TestReconcileHasFacts 空跑=false；扫到单=true；仅失败=true。
func TestReconcileHasFacts(t *testing.T) {
	empty := payment.ReconcileResult{Failed: map[string]string{}}
	emptySub := ReconcileSubResult{Failed: map[string]string{}}
	if reconcileHasFacts(empty, empty, emptySub) {
		t.Fatal("empty run should have no facts")
	}
	if !reconcileHasFacts(payment.ReconcileResult{Scanned: 1, Failed: map[string]string{}}, empty, emptySub) {
		t.Fatal("scanned>0 should be a fact")
	}
	if !reconcileHasFacts(empty, payment.ReconcileResult{Failed: map[string]string{"_error": "db down"}}, emptySub) {
		t.Fatal("failed>0 should be a fact")
	}
}
```

- [ ] **Step 2: Run the test — expect FAIL (compile error: undefined symbols).**
  `go test ./internal/mtwire/ -run 'TestRecordAndListReconcileRuns|TestReconcileHeartbeat|TestReconcileHasFacts'`
  Expected: build fails — `undefined: migrateReconcileRuns`, `undefined: reconcileHasFacts`, etc.

- [ ] **Step 3: Create `internal/mtwire/reconcile_history.go` with the full data layer.**
```go
package mtwire

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/payment"
)

// reconcileRunRow 一条对账运行记录（reconcile_runs 表）：手动全记 + 定时有实事才记，供 admin 历史查询。
type reconcileRunRow struct {
	ID      int64     `gorm:"column:id;primaryKey;autoIncrement"`
	RanAt   time.Time `gorm:"column:ran_at;index"`
	Trigger string    `gorm:"column:trigger;type:varchar(8);not null"` // cron | manual
	Summary string    `gorm:"column:summary;type:varchar(255);not null"`
	Detail  string    `gorm:"column:detail;type:text"` // JSON: paid/created/sub 三路 order_no 明细
}

// TableName 固定表名。
func (reconcileRunRow) TableName() string { return "reconcile_runs" }

// reconcileHeartbeatRow 单行心跳（reconcile_heartbeat 表，固定 id=1）：每轮对账 upsert，供 admin
// 顶部心跳条常显「最近对账时间 / 今日轮次 / 上轮卡单·失败数」，解决健康系统历史为空看不出在跑的困惑。
type reconcileHeartbeatRow struct {
	ID              int64     `gorm:"column:id;primaryKey;autoIncrement:false"` // 固定 1
	LastRunAt       time.Time `gorm:"column:last_run_at"`
	LastTrigger     string    `gorm:"column:last_trigger;type:varchar(8)"`
	TodayDate       string    `gorm:"column:today_date;type:varchar(10)"` // YYYY-MM-DD，跨天重置 today_runs
	TodayRuns       int       `gorm:"column:today_runs;not null;default:0"`
	LastStuckCount  int       `gorm:"column:last_stuck_count;not null;default:0"`
	LastFailedCount int       `gorm:"column:last_failed_count;not null;default:0"`
}

// TableName 固定表名。
func (reconcileHeartbeatRow) TableName() string { return "reconcile_heartbeat" }

func migrateReconcileRuns(db *gorm.DB) error      { return db.AutoMigrate(&reconcileRunRow{}) }
func migrateReconcileHeartbeat(db *gorm.DB) error { return db.AutoMigrate(&reconcileHeartbeatRow{}) }

// reconcileHasFacts 报告一轮对账是否「有实事」（任一路径扫到单，或任一路径有失败）——决定定时轮是否落史。
// （手动触发另行总记，不看此函数。）避免每 5min 空跑刷屏。
func reconcileHasFacts(paid, created payment.ReconcileResult, sub ReconcileSubResult) bool {
	return paid.Scanned > 0 || created.Scanned > 0 || sub.Scanned > 0 ||
		len(paid.Failed) > 0 || len(created.Failed) > 0 || len(sub.Failed) > 0
}

// recordReconcileRun 落一条对账历史（best-effort：写库失败仅放弃记录，绝不影响对账本身）。
func (a *App) recordReconcileRun(ctx context.Context, trigger string, paid, created payment.ReconcileResult, sub ReconcileSubResult) {
	summary := fmt.Sprintf("RCG-paid 扫%d/补%d/败%d · RCG-created 扫%d/补%d/败%d · SUB 扫%d/激活%d/未付%d/败%d",
		paid.Scanned, len(paid.Reconciled), len(paid.Failed),
		created.Scanned, len(created.Reconciled), len(created.Failed),
		sub.Scanned, len(sub.Activated), len(sub.Unpaid), len(sub.Failed))
	detail, _ := json.Marshal(map[string]any{"paid": paid, "created": created, "sub": sub})
	row := &reconcileRunRow{RanAt: time.Now(), Trigger: trigger, Summary: summary, Detail: string(detail)}
	_ = a.DB.WithContext(ctx).Create(row).Error
}

// listReconcileRuns 取最近 limit 条对账记录（按 ran_at 倒序）。
func (a *App) listReconcileRuns(ctx context.Context, limit int) ([]reconcileRunRow, error) {
	var rows []reconcileRunRow
	err := a.DB.WithContext(ctx).Order("ran_at DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

// updateReconcileHeartbeat 每轮对账 upsert 单行心跳（best-effort）；today_runs 跨天自动重置。
func (a *App) updateReconcileHeartbeat(ctx context.Context, trigger string, stuckCount, failedCount int) {
	now := time.Now()
	today := now.Format("2006-01-02")
	var hb reconcileHeartbeatRow
	err := a.DB.WithContext(ctx).Take(&hb, "id = ?", 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		hb = reconcileHeartbeatRow{
			ID: 1, LastRunAt: now, LastTrigger: trigger, TodayDate: today, TodayRuns: 1,
			LastStuckCount: stuckCount, LastFailedCount: failedCount,
		}
		_ = a.DB.WithContext(ctx).Create(&hb).Error
		return
	}
	if err != nil {
		return // best-effort：读失败即放弃本轮心跳，不影响对账
	}
	if hb.TodayDate != today {
		hb.TodayDate = today
		hb.TodayRuns = 0
	}
	hb.TodayRuns++
	hb.LastRunAt = now
	hb.LastTrigger = trigger
	hb.LastStuckCount = stuckCount
	hb.LastFailedCount = failedCount
	_ = a.DB.WithContext(ctx).Save(&hb).Error
}

// getReconcileHeartbeat 读单行心跳；无记录（从未跑过）返回 (zero, false)。
func (a *App) getReconcileHeartbeat(ctx context.Context) (reconcileHeartbeatRow, bool) {
	var hb reconcileHeartbeatRow
	if err := a.DB.WithContext(ctx).Take(&hb, "id = ?", 1).Error; err != nil {
		return reconcileHeartbeatRow{}, false
	}
	return hb, true
}
```

- [ ] **Step 4: Run the test — expect PASS.**
  `go test ./internal/mtwire/ -run 'TestRecordAndListReconcileRuns|TestReconcileHeartbeat|TestReconcileHasFacts'`
  Expected: `ok  github.com/QuantumNous/new-api/internal/mtwire`.

- [ ] **Step 5: Wire the two migrations into `wire.go` (SHARED FILE — coordinate first).** In `internal/mtwire/wire.go`, the `Migrate()` method currently ends with:
```go
	// 目标③桥接表：mt_subscription_orders（SUB 套餐订单状态机）+ mt_native_subscription_plans
	// （tokenplan→原生 SubscriptionPlan 映射）。均为 mt_ 前缀，不与原生订阅表冲突。
	return migrateSubscriptionBridge(a.DB)
```
Replace that `return migrateSubscriptionBridge(a.DB)` line with:
```go
	// 目标③桥接表：mt_subscription_orders（SUB 套餐订单状态机）+ mt_native_subscription_plans
	// （tokenplan→原生 SubscriptionPlan 映射）。均为 mt_ 前缀，不与原生订阅表冲突。
	if err := migrateSubscriptionBridge(a.DB); err != nil {
		return err
	}
	// 对账记录 + 心跳（reconcile-history）：历史列表 reconcile_runs + 单行心跳 reconcile_heartbeat。
	if err := migrateReconcileRuns(a.DB); err != nil {
		return err
	}
	return migrateReconcileHeartbeat(a.DB)
```

- [ ] **Step 6: Build to confirm wire.go compiles.**
  `go build ./internal/mtwire/`
  Expected: no output (success).

- [ ] **Step 7: Commit.**
  `git add internal/mtwire/reconcile_history.go internal/mtwire/reconcile_history_test.go internal/mtwire/wire.go`
  `git commit -m "feat(reconcile): reconcile_runs + reconcile_heartbeat data layer + migrations"`

---

### Task 2: Unified `runReconcileAll` orchestrator (3 paths) + record policy + heartbeat

**Files**
- Create: `internal/mtwire/reconcile_orchestrate.go`
- Create: `internal/mtwire/reconcile_orchestrate_test.go`

**Interfaces**
- Produces (Go):
  - `var reconcilePaidFn = func(a *App, ctx context.Context, before time.Time) (payment.ReconcileResult, error)`
  - `var reconcileCreatedFn = func(a *App, ctx context.Context, before time.Time) (payment.ReconcileResult, error)`
  - `var reconcileSubFn = func(a *App, ctx context.Context, before time.Time) (ReconcileSubResult, error)`
  - `func (a *App) runReconcileAll(ctx context.Context, before time.Time, trigger string) (paid, created payment.ReconcileResult, sub ReconcileSubResult)`
  - Test helper `func stubReconcileSeams(t *testing.T, paid, created payment.ReconcileResult, sub ReconcileSubResult)`
- Consumes (Go): `newReconcileHistoryApp` (Task 1), `updateReconcileHeartbeat`/`recordReconcileRun`/`reconcileHasFacts` (Task 1), `payment.Gateway.ReconcileStuckPaid/ReconcileStuckCreated`, `App.ReconcileStuckSubscriptions`, `providerMgr.QueryOrder(ctx, payment.Provider, orderNo) (bool, error)`, constants `reconcileCreatedMaxAge`/`reconcileCreatedLimit` (in `reconcile_loop.go`).
- **Design note (record policy):** logging is placed INSIDE `runReconcileAll` (identical per-path lines to the old cron loop) so cron behavior is preserved exactly and manual triggers get the same logs; seam errors are folded into `Failed["_error"]` so they count toward heartbeat `failedCount` and get recorded.

- [ ] **Step 1: Write the failing test file `internal/mtwire/reconcile_orchestrate_test.go`.**
```go
package mtwire

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/payment"
)

// stubReconcileSeams 用桩替换三路径 seam，返回预设结果；t.Cleanup 自动还原（供本 track 各测复用）。
func stubReconcileSeams(t *testing.T, paid, created payment.ReconcileResult, sub ReconcileSubResult) {
	t.Helper()
	op, oc, osub := reconcilePaidFn, reconcileCreatedFn, reconcileSubFn
	t.Cleanup(func() { reconcilePaidFn, reconcileCreatedFn, reconcileSubFn = op, oc, osub })
	reconcilePaidFn = func(_ *App, _ context.Context, _ time.Time) (payment.ReconcileResult, error) { return paid, nil }
	reconcileCreatedFn = func(_ *App, _ context.Context, _ time.Time) (payment.ReconcileResult, error) { return created, nil }
	reconcileSubFn = func(_ *App, _ context.Context, _ time.Time) (ReconcileSubResult, error) { return sub, nil }
}

// TestRunReconcileAllAggregatesThreePaths 三路径结果原样聚合返回（含 RCG-created ②，防 drift）。
func TestRunReconcileAllAggregatesThreePaths(t *testing.T) {
	app := newReconcileHistoryApp(t)
	paid := payment.ReconcileResult{Scanned: 1, Reconciled: []string{"RCG-p"}, Failed: map[string]string{}}
	created := payment.ReconcileResult{Scanned: 2, Reconciled: []string{"RCG-c"}, Failed: map[string]string{}}
	sub := ReconcileSubResult{Scanned: 3, Activated: []string{"SUB-a"}, Failed: map[string]string{}}
	stubReconcileSeams(t, paid, created, sub)

	gotP, gotC, gotS := app.runReconcileAll(context.Background(), time.Now(), "cron")
	if gotP.Scanned != 1 || gotC.Scanned != 2 || gotS.Scanned != 3 {
		t.Fatalf("aggregate paid=%d created=%d sub=%d, want 1/2/3", gotP.Scanned, gotC.Scanned, gotS.Scanned)
	}
	if len(gotC.Reconciled) != 1 || gotC.Reconciled[0] != "RCG-c" {
		t.Fatalf("created path missing: %v (drift: cron/manual must run RCG-created)", gotC.Reconciled)
	}
}

// TestRunReconcileAllRecordPolicy cron 空跑不记；manual 空跑也记（总记）；cron 有实事记 1 条。
func TestRunReconcileAllRecordPolicy(t *testing.T) {
	ctx := context.Background()
	empty := payment.ReconcileResult{Failed: map[string]string{}}
	emptySub := ReconcileSubResult{Failed: map[string]string{}}

	app1 := newReconcileHistoryApp(t)
	stubReconcileSeams(t, empty, empty, emptySub)
	app1.runReconcileAll(ctx, time.Now(), "cron")
	if rows, _ := app1.listReconcileRuns(ctx, 50); len(rows) != 0 {
		t.Fatalf("cron empty recorded %d rows, want 0", len(rows))
	}

	app2 := newReconcileHistoryApp(t)
	stubReconcileSeams(t, empty, empty, emptySub)
	app2.runReconcileAll(ctx, time.Now(), "manual")
	if rows, _ := app2.listReconcileRuns(ctx, 50); len(rows) != 1 {
		t.Fatalf("manual empty recorded %d rows, want 1 (always record manual)", len(rows))
	}

	app3 := newReconcileHistoryApp(t)
	stubReconcileSeams(t, payment.ReconcileResult{Scanned: 1, Failed: map[string]string{}}, empty, emptySub)
	app3.runReconcileAll(ctx, time.Now(), "cron")
	if rows, _ := app3.listReconcileRuns(ctx, 50); len(rows) != 1 {
		t.Fatalf("cron with facts recorded %d rows, want 1", len(rows))
	}
}

// TestRunReconcileAllUpdatesHeartbeat 每轮都更新心跳（即使空跑）。
func TestRunReconcileAllUpdatesHeartbeat(t *testing.T) {
	app := newReconcileHistoryApp(t)
	empty := payment.ReconcileResult{Failed: map[string]string{}}
	stubReconcileSeams(t, empty, empty, ReconcileSubResult{Failed: map[string]string{}})
	app.runReconcileAll(context.Background(), time.Now(), "cron")
	hb, ok := app.getReconcileHeartbeat(context.Background())
	if !ok || hb.TodayRuns != 1 || hb.LastTrigger != "cron" {
		t.Fatalf("heartbeat after empty cron: ok=%v hb=%+v, want runs=1 trigger=cron", ok, hb)
	}
}
```

- [ ] **Step 2: Run the test — expect FAIL (compile error).**
  `go test ./internal/mtwire/ -run TestRunReconcileAll`
  Expected: build fails — `undefined: reconcilePaidFn`, `undefined: runReconcileAll`, etc.

- [ ] **Step 3: Create `internal/mtwire/reconcile_orchestrate.go`.**
```go
package mtwire

import (
	"context"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/internal/payment"
	"github.com/QuantumNous/new-api/logger"
)

// 三路径 seam（仿 subOrderPaidQuery/activatePaidSubHook 的包级钩子）：默认接真实网关，单测替换为桩。
var reconcilePaidFn = func(a *App, ctx context.Context, before time.Time) (payment.ReconcileResult, error) {
	if a.RechargeGateway == nil {
		return payment.ReconcileResult{}, nil
	}
	return a.RechargeGateway.ReconcileStuckPaid(ctx, before)
}

var reconcileCreatedFn = func(a *App, ctx context.Context, before time.Time) (payment.ReconcileResult, error) {
	if a.RechargeGateway == nil || a.providerMgr == nil {
		return payment.ReconcileResult{}, nil
	}
	query := func(ctx context.Context, orderNo, provider string) (bool, error) {
		return a.providerMgr.QueryOrder(ctx, payment.Provider(provider), orderNo)
	}
	return a.RechargeGateway.ReconcileStuckCreated(ctx, before, reconcileCreatedMaxAge, reconcileCreatedLimit, query)
}

var reconcileSubFn = func(a *App, ctx context.Context, before time.Time) (ReconcileSubResult, error) {
	return a.ReconcileStuckSubscriptions(ctx, before)
}

// runReconcileAll 是定时(cron)与手动(manual)统一入口：依次跑 RCG-paid ① / RCG-created ② / SUB ③
// 三条路径，聚合结果 → 每轮 upsert 心跳 → 手动总记 or 有实事时落一条历史（均 best-effort，绝不影响对账）。
// 手动经此入口自动补上过去漏跑的 ②（修 drift）。日志保留原 cron 逐路径格式，错误折进 Failed["_error"]。
func (a *App) runReconcileAll(ctx context.Context, before time.Time, trigger string) (paid, created payment.ReconcileResult, sub ReconcileSubResult) {
	paid, perr := reconcilePaidFn(a, ctx, before)
	if perr != nil {
		logger.LogWarn(ctx, "reconcile RCG(paid) failed: "+perr.Error())
		paid.Failed = map[string]string{"_error": perr.Error()}
	} else if len(paid.Reconciled) > 0 || len(paid.Failed) > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("reconcile RCG(paid): scanned=%d credited=%d failed=%d",
			paid.Scanned, len(paid.Reconciled), len(paid.Failed)))
	}

	created, cerr := reconcileCreatedFn(a, ctx, before)
	if cerr != nil {
		logger.LogWarn(ctx, "reconcile RCG(created) failed: "+cerr.Error())
		created.Failed = map[string]string{"_error": cerr.Error()}
	} else if len(created.Reconciled) > 0 || len(created.Failed) > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("reconcile RCG(created): scanned=%d credited=%d failed=%d",
			created.Scanned, len(created.Reconciled), len(created.Failed)))
	}

	sub, serr := reconcileSubFn(a, ctx, before)
	if serr != nil {
		logger.LogWarn(ctx, "reconcile SUB failed: "+serr.Error())
		sub.Failed = map[string]string{"_error": serr.Error()}
	} else if len(sub.Activated) > 0 || len(sub.Failed) > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("reconcile SUB: scanned=%d activated=%d unpaid=%d failed=%d",
			sub.Scanned, len(sub.Activated), len(sub.Unpaid), len(sub.Failed)))
	}

	stuck := paid.Scanned + created.Scanned + sub.Scanned
	failed := len(paid.Failed) + len(created.Failed) + len(sub.Failed)
	a.updateReconcileHeartbeat(ctx, trigger, stuck, failed)

	if trigger == "manual" || reconcileHasFacts(paid, created, sub) {
		a.recordReconcileRun(ctx, trigger, paid, created, sub)
	}
	return paid, created, sub
}
```

- [ ] **Step 4: Run the test — expect PASS.**
  `go test ./internal/mtwire/ -run TestRunReconcileAll`
  Expected: `ok  github.com/QuantumNous/new-api/internal/mtwire`.

- [ ] **Step 5: Commit.**
  `git add internal/mtwire/reconcile_orchestrate.go internal/mtwire/reconcile_orchestrate_test.go`
  `git commit -m "feat(reconcile): unified runReconcileAll orchestrator (3 paths) + record/heartbeat policy"`

---

### Task 3: Wire cron `runReconcileOnce` → `runReconcileAll("cron")`

**Files**
- Modify: `internal/mtwire/reconcile_loop.go` (replace body of `runReconcileOnce`, lines 50–90; drop unused imports `fmt` and `internal/payment`)

**Interfaces**
- Consumes (Go): `runReconcileAll` (Task 2), `reconcileMinAge`, `reconcileRunning` (existing in this file).
- Produces: no new symbols; `runReconcileOnce` now delegates all 3 paths + logging + heartbeat + record to `runReconcileAll`.
- **Note:** this is a pure wiring change; cron path is a goroutine/ticker not unit-tested directly — validated by compile + the existing mtwire test suite staying green. `logger` stays (used by `StartReconcileLoop`); `common`/`gopool`/`sync`/`sync/atomic`/`time`/`context` stay.

- [ ] **Step 1: Replace the `runReconcileOnce` function (lines 50–90) in `internal/mtwire/reconcile_loop.go`.** Old (delete):
```go
// runReconcileOnce 跑一轮 RCG + SUB 对账（atomic 防与上一轮重叠：上一轮还没跑完则跳过本次）。
func (a *App) runReconcileOnce() {
	if !reconcileRunning.CompareAndSwap(false, true) {
		return
	}
	defer reconcileRunning.Store(false)

	ctx := context.Background()
	before := time.Now().Add(-reconcileMinAge)

	// RCG 充值卡单：扫 paid 未 credited → 重跑入账（幂等）。
	if a.RechargeGateway != nil {
		if res, err := a.RechargeGateway.ReconcileStuckPaid(ctx, before); err != nil {
			logger.LogWarn(ctx, "reconcile RCG(paid) failed: "+err.Error())
		} else if len(res.Reconciled) > 0 || len(res.Failed) > 0 {
			logger.LogInfo(ctx, fmt.Sprintf("reconcile RCG(paid): scanned=%d credited=%d failed=%d",
				res.Scanned, len(res.Reconciled), len(res.Failed)))
		}
	}

	// RCG 充值卡单：扫 created（回调始终未送达）→ 向平台主动查单（进程内）→ 已付补入账。
	if a.RechargeGateway != nil && a.providerMgr != nil {
		query := func(ctx context.Context, orderNo, provider string) (bool, error) {
			return a.providerMgr.QueryOrder(ctx, payment.Provider(provider), orderNo)
		}
		if res, err := a.RechargeGateway.ReconcileStuckCreated(ctx, before, reconcileCreatedMaxAge, reconcileCreatedLimit, query); err != nil {
			logger.LogWarn(ctx, "reconcile RCG(created) failed: "+err.Error())
		} else if len(res.Reconciled) > 0 || len(res.Failed) > 0 {
			logger.LogInfo(ctx, fmt.Sprintf("reconcile RCG(created): scanned=%d credited=%d failed=%d",
				res.Scanned, len(res.Reconciled), len(res.Failed)))
		}
	}

	// SUB 套餐卡单：扫 pending → 查单 → 已付则补激活。
	if res, err := a.ReconcileStuckSubscriptions(ctx, before); err != nil {
		logger.LogWarn(ctx, "reconcile SUB failed: "+err.Error())
	} else if len(res.Activated) > 0 || len(res.Failed) > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("reconcile SUB: scanned=%d activated=%d unpaid=%d failed=%d",
			res.Scanned, len(res.Activated), len(res.Unpaid), len(res.Failed)))
	}
}
```
New (replace with):
```go
// runReconcileOnce 跑一轮全 3 路径对账（cron 触发），atomic 防与上一轮重叠：上一轮还没跑完则跳过本次。
// 编排/日志/心跳/历史统一在 runReconcileAll；手动触发（HandleAdminRunReconcile）共用同一入口。
func (a *App) runReconcileOnce() {
	if !reconcileRunning.CompareAndSwap(false, true) {
		return
	}
	defer reconcileRunning.Store(false)

	ctx := context.Background()
	before := time.Now().Add(-reconcileMinAge)
	a.runReconcileAll(ctx, before, "cron")
}
```

- [ ] **Step 2: Remove now-unused imports `fmt` and `internal/payment` from `internal/mtwire/reconcile_loop.go`.** The import block becomes:
```go
import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"github.com/bytedance/gopkg/util/gopool"
)
```

- [ ] **Step 3: Build + run the mtwire suite — expect PASS (no regression, no unused-import error).**
  `go build ./internal/mtwire/ && go test ./internal/mtwire/ -run 'Reconcile'`
  Expected: build succeeds; reconcile tests `ok`.

- [ ] **Step 4: Commit.**
  `git add internal/mtwire/reconcile_loop.go`
  `git commit -m "refactor(reconcile): cron runReconcileOnce delegates to runReconcileAll(cron)"`

---

### Task 4: Refactor `HandleAdminRunReconcile` → `runReconcileAll("manual")` (fixes missing RCG-created path)

**Files**
- Modify: `internal/mtwire/reconcile_http.go` (replace `HandleAdminRunReconcile`, lines 55–77)
- Create: `internal/mtwire/reconcile_http_test.go`

**Interfaces**
- Produces (Go): revised `HandleAdminRunReconcile`; test helper `func newReconcileCtx(method, target, body string) (*gin.Context, *httptest.ResponseRecorder)`.
- Consumes (Go): `runReconcileAll` (Task 2), `reqCtx`/`respondOK` (http.go), `reconcileMinAge`; `apiResp`/`decodeResp` (defined package-level in `distribution_test.go`), `stubReconcileSeams`/`newReconcileHistoryApp` (Tasks 1–2).
- Produces (JSON response of `POST /run`): `{ rcg:{scanned,credited,failed}, rcg_created:{scanned,credited,failed}, sub:{scanned,activated,unpaid,failed} }` — note the NEW `rcg_created` key (the drift fix) and that it now always returns 200 (errors fold into each path's `failed`).

- [ ] **Step 1: Write the failing test file `internal/mtwire/reconcile_http_test.go`.**
```go
package mtwire

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/payment"
)

// newReconcileCtx 构造一个裸 gin 上下文（未过鉴权即可，handler 只用 reqCtx 组 ctx，无 principal 依赖）。
func newReconcileCtx(method, target, body string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	return c, rec
}

// TestHandleAdminRunReconcileIncludesCreatedPath 手动对账响应含 rcg_created（修 drift：早前只跑 ①③）。
func TestHandleAdminRunReconcileIncludesCreatedPath(t *testing.T) {
	app := newReconcileHistoryApp(t)
	stubReconcileSeams(t,
		payment.ReconcileResult{Scanned: 1, Failed: map[string]string{}},
		payment.ReconcileResult{Scanned: 2, Reconciled: []string{"RCG-c"}, Failed: map[string]string{}},
		ReconcileSubResult{Failed: map[string]string{}},
	)
	c, rec := newReconcileCtx("POST", "/api/admin/reconcile/run", "")
	app.HandleAdminRunReconcile(c)

	resp := decodeResp(t, rec)
	if !resp.Success {
		t.Fatalf("resp not success: %s", rec.Body.String())
	}
	var data struct {
		RcgCreated struct {
			Scanned  int      `json:"scanned"`
			Credited []string `json:"credited"`
		} `json:"rcg_created"`
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if data.RcgCreated.Scanned != 2 || len(data.RcgCreated.Credited) != 1 {
		t.Fatalf("rcg_created=%+v, want scanned=2 credited=[RCG-c] (drift fixed)", data.RcgCreated)
	}
	// 手动总记一条历史。
	if rows, _ := app.listReconcileRuns(context.Background(), 50); len(rows) != 1 {
		t.Fatalf("manual run recorded %d history rows, want 1", len(rows))
	}
}
```

- [ ] **Step 2: Run the test — expect FAIL.**
  `go test ./internal/mtwire/ -run TestHandleAdminRunReconcileIncludesCreatedPath`
  Expected: FAIL — the current handler has no `rcg_created` key, so `data.RcgCreated.Scanned` is 0 (want 2). (Compiles: `newReconcileCtx` is new here; `HandleAdminRunReconcile` already exists.)

- [ ] **Step 3: Replace `HandleAdminRunReconcile` (lines 55–77) in `internal/mtwire/reconcile_http.go`.** Old (delete):
```go
// HandleAdminRunReconcile POST /api/admin/reconcile/run —— 手动立即对账（RCG+SUB），返回结果。
// 与 5min 定时扫同一逻辑、同样幂等，仅免去等待。AdminAuth。
func (a *App) HandleAdminRunReconcile(c *gin.Context) {
	ctx := reqCtx(c)
	before := time.Now().Add(-reconcileMinAge)
	res := gin.H{}

	if a.RechargeGateway != nil {
		rcg, err := a.RechargeGateway.ReconcileStuckPaid(ctx, before)
		if err != nil {
			respondErr(c, err)
			return
		}
		res["rcg"] = gin.H{"scanned": rcg.Scanned, "credited": rcg.Reconciled, "failed": rcg.Failed}
	}
	sub, err := a.ReconcileStuckSubscriptions(ctx, before)
	if err != nil {
		respondErr(c, err)
		return
	}
	res["sub"] = gin.H{"scanned": sub.Scanned, "activated": sub.Activated, "unpaid": sub.Unpaid, "failed": sub.Failed}
	respondOK(c, res)
}
```
New (replace with):
```go
// HandleAdminRunReconcile POST /api/admin/reconcile/run —— 手动立即对账，走与 5min 定时同一入口
// runReconcileAll("manual")：跑全 3 条（RCG-paid ① + RCG-created ② + SUB ③，修早前只跑 ①③ 的 drift）、
// 更新心跳、落一条历史。返回三路径结果（best-effort：单路径错误折进各自 failed，仍返回 200）。AdminAuth。
func (a *App) HandleAdminRunReconcile(c *gin.Context) {
	ctx := reqCtx(c)
	before := time.Now().Add(-reconcileMinAge)
	paid, created, sub := a.runReconcileAll(ctx, before, "manual")
	respondOK(c, gin.H{
		"rcg":         gin.H{"scanned": paid.Scanned, "credited": paid.Reconciled, "failed": paid.Failed},
		"rcg_created": gin.H{"scanned": created.Scanned, "credited": created.Reconciled, "failed": created.Failed},
		"sub":         gin.H{"scanned": sub.Scanned, "activated": sub.Activated, "unpaid": sub.Unpaid, "failed": sub.Failed},
	})
}
```

- [ ] **Step 4: Run the test — expect PASS.**
  `go test ./internal/mtwire/ -run TestHandleAdminRunReconcileIncludesCreatedPath`
  Expected: `ok`.

- [ ] **Step 5: Commit.**
  `git add internal/mtwire/reconcile_http.go internal/mtwire/reconcile_http_test.go`
  `git commit -m "fix(reconcile): manual run goes through runReconcileAll(manual), adds rcg_created (drift fix)"`

---

### Task 5: `GET /history` handler + route registration

**Files**
- Modify: `internal/mtwire/reconcile_http.go` (add `reconcileRunOut` + `HandleAdminListHistory`; add imports `encoding/json`, `strconv`)
- Modify: `router/mt-router.go` (SHARED — register `GET /history` in `adminReconcileGroup`, after line 153)
- Modify: `internal/mtwire/reconcile_http_test.go` (add `/history` test)

**Interfaces**
- Produces (Go):
  - `type reconcileRunOut struct { ID int64 json:"id"; RanAt int64 json:"ran_at"; Trigger string json:"trigger"; Summary string json:"summary"; Detail json.RawMessage json:"detail" }`
  - `func (a *App) HandleAdminListHistory(c *gin.Context)` → `GET /api/admin/reconcile/history?limit=50`, response `{ runs: reconcileRunOut[] }` (desc by ran_at; `limit` default 50, clamped 1..500; `detail` passed through as raw JSON object).
- Consumes (Go): `listReconcileRuns` (Task 1), `reqCtx`/`respondOK`/`respondErr` (http.go).

- [ ] **Step 1: Add the `/history` test to `internal/mtwire/reconcile_http_test.go`** (append this function; `time` import is needed — add `"time"` to that file's import block):
```go
// TestHandleAdminListHistoryDescAndLimit /history 倒序 + limit 生效 + detail 为 JSON 对象。
func TestHandleAdminListHistoryDescAndLimit(t *testing.T) {
	app := newReconcileHistoryApp(t)
	ctx := context.Background()
	r := payment.ReconcileResult{Failed: map[string]string{}}
	s := ReconcileSubResult{Failed: map[string]string{}}
	app.recordReconcileRun(ctx, "cron", payment.ReconcileResult{Scanned: 1, Failed: map[string]string{}}, r, s)
	time.Sleep(2 * time.Millisecond)
	app.recordReconcileRun(ctx, "manual", r, r, s)

	c, rec := newReconcileCtx("GET", "/api/admin/reconcile/history?limit=1", "")
	app.HandleAdminListHistory(c)
	resp := decodeResp(t, rec)
	if !resp.Success {
		t.Fatalf("not success: %s", rec.Body.String())
	}
	var data struct {
		Runs []struct {
			Trigger string          `json:"trigger"`
			RanAt   int64           `json:"ran_at"`
			Detail  json.RawMessage `json:"detail"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(data.Runs) != 1 {
		t.Fatalf("limit=1 returned %d runs", len(data.Runs))
	}
	if data.Runs[0].Trigger != "manual" {
		t.Fatalf("first run trigger=%q, want manual (desc)", data.Runs[0].Trigger)
	}
	if data.Runs[0].RanAt == 0 {
		t.Fatalf("ran_at should be unix seconds > 0")
	}
	if len(data.Runs[0].Detail) == 0 || string(data.Runs[0].Detail) == "null" {
		t.Fatalf("detail should be a JSON object, got %q", string(data.Runs[0].Detail))
	}
}
```

- [ ] **Step 2: Run the test — expect FAIL (compile error: `undefined: app.HandleAdminListHistory`).**
  `go test ./internal/mtwire/ -run TestHandleAdminListHistoryDescAndLimit`
  Expected: build fails.

- [ ] **Step 3: Add the handler + DTO to `internal/mtwire/reconcile_http.go`.** First update its import block from:
```go
import (
	"time"

	"github.com/gin-gonic/gin"
)
```
to:
```go
import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)
```
Then append at end of file:
```go
// reconcileRunOut 是 admin「对账记录」表的一行（detail 原样透传 JSON，前端展开看三路明细）。
type reconcileRunOut struct {
	ID      int64           `json:"id"`
	RanAt   int64           `json:"ran_at"` // unix 秒
	Trigger string          `json:"trigger"`
	Summary string          `json:"summary"`
	Detail  json.RawMessage `json:"detail"`
}

// HandleAdminListHistory GET /api/admin/reconcile/history?limit=50 —— 倒序返回对账运行记录
// （手动全记 + 定时有实事才记）。limit 默认 50、夹到 1..500。AdminAuth。
func (a *App) HandleAdminListHistory(c *gin.Context) {
	ctx := reqCtx(c)
	limit := 50
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	rows, err := a.listReconcileRuns(ctx, limit)
	if err != nil {
		respondErr(c, err)
		return
	}
	out := make([]reconcileRunOut, 0, len(rows))
	for _, r := range rows {
		detail := r.Detail
		if detail == "" {
			detail = "null" // 空 detail 也返回合法 JSON，避免前端 JSON.parse 崩
		}
		out = append(out, reconcileRunOut{
			ID: r.ID, RanAt: r.RanAt.Unix(), Trigger: r.Trigger, Summary: r.Summary,
			Detail: json.RawMessage(detail),
		})
	}
	respondOK(c, gin.H{"runs": out})
}
```

- [ ] **Step 4: Run the test — expect PASS.**
  `go test ./internal/mtwire/ -run TestHandleAdminListHistoryDescAndLimit`
  Expected: `ok`.

- [ ] **Step 5: Register the route in `router/mt-router.go` (SHARED FILE — coordinate first).** The block at lines 149–154 currently reads:
```go
	adminReconcileGroup := router.Group("/api/admin/reconcile")
	adminReconcileGroup.Use(middleware.AdminAuth())
	{
		adminReconcileGroup.GET("/stuck", app.HandleAdminListStuck)
		adminReconcileGroup.POST("/run", app.HandleAdminRunReconcile)
	}
```
Add one line so the block becomes:
```go
	adminReconcileGroup := router.Group("/api/admin/reconcile")
	adminReconcileGroup.Use(middleware.AdminAuth())
	{
		adminReconcileGroup.GET("/stuck", app.HandleAdminListStuck)
		adminReconcileGroup.POST("/run", app.HandleAdminRunReconcile)
		adminReconcileGroup.GET("/history", app.HandleAdminListHistory)
	}
```

- [ ] **Step 6: Build to confirm the router compiles.**
  `go build ./router/ ./internal/mtwire/`
  Expected: no output (success).

- [ ] **Step 7: Commit.**
  `git add internal/mtwire/reconcile_http.go internal/mtwire/reconcile_http_test.go router/mt-router.go`
  `git commit -m "feat(reconcile): GET /api/admin/reconcile/history + route"`

---

### Task 6: Add heartbeat to `GET /stuck` response

**Files**
- Modify: `internal/mtwire/reconcile_http.go` (add `reconcileHeartbeatOut`; extend `HandleAdminListStuck`'s final `respondOK`)
- Modify: `internal/mtwire/reconcile_http_test.go` (add `/stuck` heartbeat test)

**Interfaces**
- Produces (Go):
  - `type reconcileHeartbeatOut struct { LastRunAt int64 json:"last_run_at"; LastTrigger string json:"last_trigger"; TodayRuns int json:"today_runs"; LastStuckCount int json:"last_stuck_count"; LastFailedCount int json:"last_failed_count" }`
  - `HandleAdminListStuck` response now `{ stuck, threshold_secs, heartbeat }` where `heartbeat` is `reconcileHeartbeatOut` (`last_run_at=0` when never run).
- Consumes (Go): `getReconcileHeartbeat` (Task 1).

- [ ] **Step 1: Add the `/stuck` heartbeat test to `internal/mtwire/reconcile_http_test.go`** (append):
```go
// TestHandleAdminListStuckIncludesHeartbeat /stuck 响应内嵌心跳（前端顶部心跳条用）。
func TestHandleAdminListStuckIncludesHeartbeat(t *testing.T) {
	app := newReconcileHistoryApp(t)
	// listStuckSubscriptions 查 mt_subscription_orders：需建表（无 RechargeGateway → 只查 SUB）。
	if err := migrateSubscriptionBridge(app.DB); err != nil {
		t.Fatalf("migrate sub bridge: %v", err)
	}
	app.updateReconcileHeartbeat(context.Background(), "cron", 2, 1)

	c, rec := newReconcileCtx("GET", "/api/admin/reconcile/stuck", "")
	app.HandleAdminListStuck(c)
	resp := decodeResp(t, rec)
	if !resp.Success {
		t.Fatalf("not success: %s", rec.Body.String())
	}
	var data struct {
		Heartbeat struct {
			TodayRuns       int    `json:"today_runs"`
			LastTrigger     string `json:"last_trigger"`
			LastFailedCount int    `json:"last_failed_count"`
			LastRunAt       int64  `json:"last_run_at"`
		} `json:"heartbeat"`
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if data.Heartbeat.TodayRuns != 1 || data.Heartbeat.LastTrigger != "cron" ||
		data.Heartbeat.LastFailedCount != 1 || data.Heartbeat.LastRunAt == 0 {
		t.Fatalf("heartbeat=%+v, want runs=1 trigger=cron failed=1 lastRunAt>0", data.Heartbeat)
	}
}
```

- [ ] **Step 2: Run the test — expect FAIL.**
  `go test ./internal/mtwire/ -run TestHandleAdminListStuckIncludesHeartbeat`
  Expected: FAIL — current `/stuck` response has no `heartbeat` key (all fields decode to 0/"").

- [ ] **Step 3: Extend `HandleAdminListStuck` in `internal/mtwire/reconcile_http.go`.** The function currently ends (line 52) with:
```go
	respondOK(c, gin.H{"stuck": out, "threshold_secs": int64(reconcileMinAge.Seconds())})
}
```
Replace that single `respondOK` line with:
```go
	hbOut := reconcileHeartbeatOut{}
	if hb, ok := a.getReconcileHeartbeat(ctx); ok {
		hbOut = reconcileHeartbeatOut{
			LastRunAt: hb.LastRunAt.Unix(), LastTrigger: hb.LastTrigger, TodayRuns: hb.TodayRuns,
			LastStuckCount: hb.LastStuckCount, LastFailedCount: hb.LastFailedCount,
		}
	}
	respondOK(c, gin.H{"stuck": out, "threshold_secs": int64(reconcileMinAge.Seconds()), "heartbeat": hbOut})
}
```
Then add the DTO at end of file:
```go
// reconcileHeartbeatOut 是 /stuck 响应内嵌的心跳（前端顶部心跳条用）；从未跑过则 last_run_at=0。
type reconcileHeartbeatOut struct {
	LastRunAt       int64  `json:"last_run_at"` // unix 秒，0=从未
	LastTrigger     string `json:"last_trigger"`
	TodayRuns       int    `json:"today_runs"`
	LastStuckCount  int    `json:"last_stuck_count"`
	LastFailedCount int    `json:"last_failed_count"`
}
```

- [ ] **Step 4: Run the test — expect PASS.**
  `go test ./internal/mtwire/ -run TestHandleAdminListStuckIncludesHeartbeat`
  Expected: `ok`.

- [ ] **Step 5: Run the whole reconcile suite as a regression gate.**
  `go test ./internal/mtwire/ -run 'Reconcile|RunReconcile|HandleAdmin' && go test ./internal/payment/`
  Expected: both `ok`.

- [ ] **Step 6: Commit.**
  `git add internal/mtwire/reconcile_http.go internal/mtwire/reconcile_http_test.go`
  `git commit -m "feat(reconcile): embed heartbeat in GET /stuck response"`

---

### Task 7: Frontend — heartbeat banner + api.ts heartbeat types + i18n

> **Server note:** `bun run build` + deploy + Playwright E2E happen ON THE SERVER (test stack `newapi_test`, port 3100). Locally run at most `cd web/default && bun run typecheck` and `bun run lint`. There is no component unit-test harness for this page, so this task verifies via typecheck/lint locally and Playwright on the server (spec §6) — not go-style red/green.

**Files**
- Modify: `web/default/src/features/payment-reconcile/api.ts` (add `ReconcileHeartbeat`, `heartbeat?` on `StuckList`, `rcg_created` on `ReconcileRunResult`)
- Modify: `web/default/src/features/payment-reconcile/index.tsx` (heartbeat banner + include `rcg_created` in last-run text)
- Modify: `web/default/src/i18n/locales/zh.json` (banner keys)

**Interfaces**
- Produces (TS):
  - `export interface ReconcileHeartbeat { last_run_at: number; last_trigger: string; today_runs: number; last_stuck_count: number; last_failed_count: number }`
  - `StuckList` gains `heartbeat?: ReconcileHeartbeat`
  - `ReconcileRunResult` gains `rcg_created?: { scanned: number; credited: string[] | null; failed: Record<string, string> }`
- Consumes (TS): existing `listStuckOrders()` (already returns the extended `StuckList` from Task 6 backend).

- [ ] **Step 1: Extend `web/default/src/features/payment-reconcile/api.ts`.** Replace the `StuckList` and `ReconcileRunResult` interfaces (current lines 31–44) with:
```ts
export interface ReconcileHeartbeat {
  last_run_at: number // unix seconds; 0 = never
  last_trigger: string // 'cron' | 'manual' | ''
  today_runs: number
  last_stuck_count: number
  last_failed_count: number
}

export interface StuckList {
  stuck: StuckOrder[]
  threshold_secs: number
  heartbeat?: ReconcileHeartbeat
}

export interface ReconcileRunResult {
  rcg?: { scanned: number; credited: string[] | null; failed: Record<string, string> }
  rcg_created?: { scanned: number; credited: string[] | null; failed: Record<string, string> }
  sub?: {
    scanned: number
    activated: string[] | null
    unpaid: string[] | null
    failed: Record<string, string>
  }
}
```

- [ ] **Step 2: Add the heartbeat banner + relative-time helper to `index.tsx`.** Insert this pure helper just above `export function PaymentReconcile() {` (after the imports):
```tsx
/** relTime returns a short language-neutral "5m" / "2h" / "3d" string from a unix-seconds timestamp. */
function relTime(unixSecs: number): string {
  const diff = Math.max(0, Math.floor(Date.now() / 1000 - unixSecs))
  if (diff < 60) return `${diff}s`
  if (diff < 3600) return `${Math.floor(diff / 60)}m`
  if (diff < 86400) return `${Math.floor(diff / 3600)}h`
  return `${Math.floor(diff / 86400)}d`
}
```
Then, inside the component (right after `const stuck = data?.stuck || []`, before `const [lastResult, setLastResult] = useState('')`), add:
```tsx
  const hb = data?.heartbeat
  const failedCount = hb?.last_failed_count ?? 0
  const statusKind = failedCount > 0 ? 'fail' : stuck.length > 0 ? 'stuck' : 'ok'
  const statusLabel =
    statusKind === 'fail'
      ? t('Has failures')
      : statusKind === 'stuck'
        ? t('Has stuck orders')
        : t('Normal')
  const statusClass =
    statusKind === 'fail'
      ? 'text-red-600'
      : statusKind === 'stuck'
        ? 'text-yellow-600'
        : 'text-green-600'
```
Then render the banner as the FIRST child inside `<SectionPageLayout.Content>` (immediately after the opening `<SectionPageLayout.Content>` tag, before the `<p className='text-muted-foreground mb-3 text-sm'>` description):
```tsx
        <div
          className='bg-muted/40 mb-3 flex flex-wrap items-center gap-x-4 gap-y-1 rounded-md border p-3 text-sm'
          data-testid='reconcile-heartbeat'
        >
          <span>
            {t('Last reconcile')}:{' '}
            {hb?.last_run_at ? `${relTime(hb.last_run_at)} ${t('ago')}` : t('Never')}
          </span>
          <span>
            {t('Runs today')}: <span className='tabular-nums'>{hb?.today_runs ?? 0}</span>
          </span>
          <span>
            {t('Status')}: <span className={statusClass}>{statusLabel}</span>
          </span>
        </div>
```

- [ ] **Step 3: Include `rcg_created` in the last-run summary text.** In `runMut`'s `onSuccess`, replace the `setLastResult(...)` call with:
```tsx
      const c = res.rcg_created
      setLastResult(
        `RCG ${t('scanned')}${r?.scanned ?? 0}/${t('credited')}${r?.credited?.length ?? 0}/${t('failed')}${Object.keys(r?.failed ?? {}).length} · ` +
          `RCG-created ${t('scanned')}${c?.scanned ?? 0}/${t('credited')}${c?.credited?.length ?? 0}/${t('failed')}${Object.keys(c?.failed ?? {}).length} · ` +
          `SUB ${t('scanned')}${s?.scanned ?? 0}/${t('activated')}${s?.activated?.length ?? 0}/${t('unpaid')}${s?.unpaid?.length ?? 0}/${t('failed')}${Object.keys(s?.failed ?? {}).length}`
      )
```

- [ ] **Step 4: Add Chinese i18n for the new banner keys.** In `web/default/src/i18n/locales/zh.json`, insert these keys right after the existing line `"failed": "已失败",` (line 153):
```json
    "Last reconcile": "最近对账",
    "Never": "从未",
    "ago": "前",
    "Runs today": "今日轮次",
    "Normal": "正常",
    "Has stuck orders": "有卡单",
    "Has failures": "有失败",
```
(`Status` already exists in zh.json — do not duplicate.)

- [ ] **Step 5: Local typecheck + lint (build/deploy/E2E are on the server).**
  `cd web/default && bun run typecheck && bun run lint`
  Expected: both pass with no errors touching `payment-reconcile`.

- [ ] **Step 6: Commit.**
  `git add web/default/src/features/payment-reconcile/api.ts web/default/src/features/payment-reconcile/index.tsx web/default/src/i18n/locales/zh.json`
  `git commit -m "feat(reconcile-ui): heartbeat banner + rcg_created in last-run summary"`

---

### Task 8: Frontend — expandable "Reconcile History" table + api.ts `listHistory` + i18n

> **Server note:** same as Task 7 — `bun run build` + deploy + Playwright E2E on the server; locally run `bun run typecheck` and `bun run lint` only.

**Files**
- Modify: `web/default/src/features/payment-reconcile/api.ts` (add `HistoryRun` + `listHistory`)
- Modify: `web/default/src/features/payment-reconcile/index.tsx` (history query + expandable table; invalidate history on manual run)
- Modify: `web/default/src/i18n/locales/zh.json` (table keys)

**Interfaces**
- Produces (TS):
  - `export interface HistoryRun { id: number; ran_at: number; trigger: string; summary: string; detail: unknown }`
  - `export async function listHistory(limit?: number): Promise<HistoryRun[]>` → `GET /api/admin/reconcile/history` returning `res.data.data.runs`.
- Consumes (TS): backend `GET /history` (Task 5). `detail` arrives already-parsed (axios JSON) as `{ paid, created, sub }`.

- [ ] **Step 1: Add `HistoryRun` + `listHistory` to `api.ts`** (append at end of file, after `runReconcile`):
```ts
export interface HistoryRun {
  id: number
  ran_at: number // unix seconds
  trigger: string // 'cron' | 'manual'
  summary: string
  detail: unknown // parsed JSON: { paid, created, sub }
}

/** 列最近对账记录（倒序）。手动全记 + 定时有实事才记。 */
export async function listHistory(limit = 50): Promise<HistoryRun[]> {
  const res = await api.get('/api/admin/reconcile/history', { params: { limit } })
  return res.data.data.runs
}
```

- [ ] **Step 2: Update `index.tsx` imports.** Change the React import line `import { useState } from 'react'` to `import { Fragment, useState } from 'react'`, add `import { ChevronDown, ChevronRight } from 'lucide-react'` below the react-i18next import, and extend the api import to:
```tsx
import { listHistory, listStuckOrders, runReconcile } from './api'
```

- [ ] **Step 3: Add the history query + expand state inside the component.** Right after the existing `const { data, isLoading } = useQuery({ ... })` block (before `const stuck = ...` is fine; place after `const stuck = data?.stuck || []`), add:
```tsx
  const { data: history = [] } = useQuery({
    queryKey: ['admin-reconcile-history'],
    queryFn: () => listHistory(50),
    placeholderData: (prev) => prev,
  })
  const [expanded, setExpanded] = useState<Record<number, boolean>>({})
```

- [ ] **Step 4: Invalidate history after a manual run.** In `runMut`'s `onSuccess`, right after the existing `qc.invalidateQueries({ queryKey: ['admin-reconcile-stuck'] })`, add:
```tsx
      qc.invalidateQueries({ queryKey: ['admin-reconcile-history'] })
```

- [ ] **Step 5: Render the expandable history table.** Insert this block immediately after the closing `</div>` of the existing stuck-table `<div className='overflow-hidden rounded-lg border' data-testid='stuck-table'>` (still inside `<SectionPageLayout.Content>`):
```tsx
        <h3 className='mt-6 mb-2 text-sm font-medium'>{t('Reconcile History')}</h3>
        <div className='overflow-hidden rounded-lg border' data-testid='history-table'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className='w-8'></TableHead>
                <TableHead>{t('Time')}</TableHead>
                <TableHead>{t('Trigger')}</TableHead>
                <TableHead>{t('Summary')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {history.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={4} className='text-muted-foreground text-center'>
                    {t('No history yet')}
                  </TableCell>
                </TableRow>
              ) : (
                history.map((run) => (
                  <Fragment key={run.id}>
                    <TableRow
                      className='cursor-pointer'
                      onClick={() => setExpanded((m) => ({ ...m, [run.id]: !m[run.id] }))}
                      data-testid={`history-row-${run.id}`}
                    >
                      <TableCell>
                        {expanded[run.id] ? (
                          <ChevronDown className='size-4' />
                        ) : (
                          <ChevronRight className='size-4' />
                        )}
                      </TableCell>
                      <TableCell className='text-sm'>
                        {new Date(run.ran_at * 1000).toLocaleString()}
                      </TableCell>
                      <TableCell>
                        <Badge variant={run.trigger === 'manual' ? 'default' : 'secondary'}>
                          {run.trigger === 'manual' ? t('Manual') : t('Scheduled')}
                        </Badge>
                      </TableCell>
                      <TableCell className='text-sm'>{run.summary}</TableCell>
                    </TableRow>
                    {expanded[run.id] && (
                      <TableRow data-testid={`history-detail-${run.id}`}>
                        <TableCell colSpan={4} className='bg-muted/30'>
                          <pre className='overflow-x-auto text-xs'>
                            {JSON.stringify(run.detail, null, 2)}
                          </pre>
                        </TableCell>
                      </TableRow>
                    )}
                  </Fragment>
                ))
              )}
            </TableBody>
          </Table>
        </div>
```

- [ ] **Step 6: Add Chinese i18n for the new table keys.** In `web/default/src/i18n/locales/zh.json`, insert right after the `"Has failures": "有失败",` line added in Task 7:
```json
    "Reconcile History": "对账记录",
    "Time": "时间",
    "Trigger": "触发",
    "Summary": "摘要",
    "Manual": "手动",
    "Scheduled": "定时",
    "No history yet": "暂无记录",
```
(`Type`, `User`, `Tenant`, `Amount`, `Order No`, `Stuck`, `Status` already exist — do not duplicate.)

- [ ] **Step 7: Local typecheck + lint.**
  `cd web/default && bun run typecheck && bun run lint`
  Expected: both pass.

- [ ] **Step 8: Commit.**
  `git add web/default/src/features/payment-reconcile/api.ts web/default/src/features/payment-reconcile/index.tsx web/default/src/i18n/locales/zh.json`
  `git commit -m "feat(reconcile-ui): expandable Reconcile History table + listHistory api"`

---

## Server Verification (after all tasks — run ON THE SERVER)
Per spec §6 (do NOT run locally):
1. On the server, build + deploy the test stack: project `newapi_test`, port 3100, `--env-file /root/newapi-test/.env`. Migrations create `reconcile_runs` + `reconcile_heartbeat` on boot (via `Migrate()`).
2. Playwright E2E: login admin → open "支付对账" page → confirm heartbeat banner shows (`最近对账 … · 今日 N 轮 · 状态：正常`) → click "立即对账" → a new row appears in the "对账记录" table (trigger 手动) → expand it and confirm `detail` JSON shows `paid/created/sub`.
3. (Optional) Seed a `created`/`paid` stuck order → manual run or wait for cron → verify the补单 lands in history + heartbeat `last_stuck_count`/`last_failed_count` update.
4. If Cloudflare/Turnstile blocks admin login: temporarily disable → test → re-enable (confirm the disable/enable location with the owner BEFORE touching it).

## Spec → Task Traceability (self-review)
- Spec §4.1 data model (`reconcile_runs` + `reconcile_heartbeat` + migrations in `wire.go`) → Task 1.
- Spec §4.2 orchestrator (`runReconcileAll`, 3 paths, heartbeat every run, record policy) → Task 2; cron wiring → Task 3; manual drift fix → Task 4.
- Spec §4.3 Admin API (`GET /history` + route) → Task 5; heartbeat on `/stuck` → Task 6; `POST /run` all-3 → Task 4.
- Spec §4.4 frontend (heartbeat banner, history table expandable, `api.ts`, i18n) → Tasks 7–8.
- Spec §5 tests: 3-path aggregation + record policy + heartbeat reset → Tasks 1–2; `/history` desc+limit → Task 5; `POST /run` includes ② → Task 4.
- Spec §4.5 parked WIP adapted: old 2-path `reconcileBoth`/`recordReconcileRun`/`listReconcileRuns`/`migrateReconcileRuns` extended to 3 paths + heartbeat; branch `wip/reconcile-history` NOT merged.
- Spec §7 status semantics (`有失败`=last Failed>0; `有卡单`=live `/stuck` non-empty; else `正常`) → Task 7 banner logic.
