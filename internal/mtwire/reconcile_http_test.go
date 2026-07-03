package mtwire

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
