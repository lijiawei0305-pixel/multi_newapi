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
	agt := ReconcileAgtResult{Failed: map[string]string{}}

	app.recordReconcileRun(ctx, "cron", paid, created, sub, agt)
	time.Sleep(2 * time.Millisecond) // 保证 ran_at 有先后
	app.recordReconcileRun(ctx, "manual", paid, created, sub, agt)

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
	emptyAgt := ReconcileAgtResult{Failed: map[string]string{}}
	if reconcileHasFacts(empty, empty, emptySub, emptyAgt) {
		t.Fatal("empty run should have no facts")
	}
	if !reconcileHasFacts(payment.ReconcileResult{Scanned: 1, Failed: map[string]string{}}, empty, emptySub, emptyAgt) {
		t.Fatal("scanned>0 should be a fact")
	}
	if !reconcileHasFacts(empty, payment.ReconcileResult{Failed: map[string]string{"_error": "db down"}}, emptySub, emptyAgt) {
		t.Fatal("failed>0 should be a fact")
	}
	if !reconcileHasFacts(empty, empty, emptySub, ReconcileAgtResult{Scanned: 1, Failed: map[string]string{}}) {
		t.Fatal("AGT scanned>0 should be a fact")
	}
}
