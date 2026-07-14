package mtwire

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/alert"
	"github.com/QuantumNous/new-api/internal/payment"
)

// captureSink 记录被 Dispatch 的告警（测试单 goroutine，无需锁）。
type captureSink struct{ alerts []alert.Alert }

func (c *captureSink) Dispatch(_ context.Context, a alert.Alert) error {
	c.alerts = append(c.alerts, a)
	return nil
}

func sinkHasSubject(alerts []alert.Alert, subject string) bool {
	for _, a := range alerts {
		if a.Subject == subject {
			return true
		}
	}
	return false
}

// seedOldHeartbeat 预置一行「很旧」的心跳（上一轮在 reconcileStaleAfter 之前），供陈旧告警测试。
func seedOldHeartbeat(t *testing.T, app *App) {
	t.Helper()
	old := time.Now().Add(-reconcileStaleAfter - time.Minute)
	if err := app.DB.Create(&reconcileHeartbeatRow{
		ID: 1, LastRunAt: old, LastTrigger: "cron", TodayDate: old.Format("2006-01-02"), TodayRuns: 1,
	}).Error; err != nil {
		t.Fatalf("seed heartbeat: %v", err)
	}
}

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

// TestRunReconcileAll_FailuresDispatchAlert 本轮有失败 → 分发 Critical「对账失败告警」。
func TestRunReconcileAll_FailuresDispatchAlert(t *testing.T) {
	app := newReconcileHistoryApp(t)
	sink := &captureSink{}
	app.AlertSink = sink
	empty := payment.ReconcileResult{Failed: map[string]string{}}
	// SUB 3 笔失败——对齐线上实证（每轮 3 笔 SUB 对账失败此前零通知）。
	sub := ReconcileSubResult{Scanned: 3, Failed: map[string]string{"s1": "e", "s2": "e", "s3": "e"}}
	stubReconcileSeams(t, empty, empty, sub)

	app.runReconcileAll(context.Background(), time.Now(), "cron")
	if !sinkHasSubject(sink.alerts, "对账失败告警") {
		t.Fatalf("失败轮应分发『对账失败告警』，实得 %+v", sink.alerts)
	}
}

// TestRunReconcileAll_HealthyFirstRunNoAlert 无失败 + 首轮（无上一轮心跳）→ 不发任何健康告警。
func TestRunReconcileAll_HealthyFirstRunNoAlert(t *testing.T) {
	app := newReconcileHistoryApp(t)
	sink := &captureSink{}
	app.AlertSink = sink
	empty := payment.ReconcileResult{Failed: map[string]string{}}
	stubReconcileSeams(t, empty, empty, ReconcileSubResult{Failed: map[string]string{}})

	app.runReconcileAll(context.Background(), time.Now(), "cron")
	if len(sink.alerts) != 0 {
		t.Fatalf("健康且首轮不应有任何告警，实得 %+v", sink.alerts)
	}
}

// TestRunReconcileAll_StaleGapDispatchesAlert 距上一轮 > reconcileStaleAfter（cron）→ 分发「对账循环曾停滞」。
func TestRunReconcileAll_StaleGapDispatchesAlert(t *testing.T) {
	app := newReconcileHistoryApp(t)
	sink := &captureSink{}
	app.AlertSink = sink
	seedOldHeartbeat(t, app)
	empty := payment.ReconcileResult{Failed: map[string]string{}}
	stubReconcileSeams(t, empty, empty, ReconcileSubResult{Failed: map[string]string{}})

	app.runReconcileAll(context.Background(), time.Now(), "cron")
	if !sinkHasSubject(sink.alerts, "对账循环曾停滞") {
		t.Fatalf("陈旧 cron 轮应分发『对账循环曾停滞』，实得 %+v", sink.alerts)
	}
}

// TestRunReconcileAll_ManualSkipsStaleAlert 手动触发不判陈旧（ad-hoc，间隔不代表停滞）。
func TestRunReconcileAll_ManualSkipsStaleAlert(t *testing.T) {
	app := newReconcileHistoryApp(t)
	sink := &captureSink{}
	app.AlertSink = sink
	seedOldHeartbeat(t, app)
	empty := payment.ReconcileResult{Failed: map[string]string{}}
	stubReconcileSeams(t, empty, empty, ReconcileSubResult{Failed: map[string]string{}})

	app.runReconcileAll(context.Background(), time.Now(), "manual")
	if sinkHasSubject(sink.alerts, "对账循环曾停滞") {
		t.Fatalf("手动触发不应分发陈旧告警，实得 %+v", sink.alerts)
	}
}
