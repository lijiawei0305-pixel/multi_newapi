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
