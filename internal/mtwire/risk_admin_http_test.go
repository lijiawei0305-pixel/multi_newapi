package mtwire

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/risk"
)

// decodeErrResp 解出统一响应的 success/code，供断言错误码。
func decodeRiskResp(t *testing.T, body []byte) (success bool, code string) {
	t.Helper()
	var r struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatalf("unmarshal resp: %v (body=%s)", err, string(body))
	}
	return r.Success, r.Code
}

// force=true 但 reason 空白（仅空格）→ 400 RISK_FORCE_REASON_REQUIRED，在触及引擎前即拒
// （audit F4：force 绕过归属校验须留可追溯理由；此处 App 无 RiskEngine 也应拒，证明校验前置）。
func TestHandleAdminReleaseTrialLimit_ForceRequiresReason(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := &App{} // 无需 RiskEngine：reason 校验早于引擎类型断言
	c, w := testCtx(http.MethodPost, "/api/admin/risk/trial-limit/release",
		`{"user_id":8,"device_id":"dev-D","force":true,"reason":"   "}`)
	setID(c, 1) // AdminAuth 注入的操作者 id（审计留痕用）

	app.HandleAdminReleaseTrialLimit(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if success, code := decodeRiskResp(t, w.Body.Bytes()); success || code != "RISK_FORCE_REASON_REQUIRED" {
		t.Fatalf("want failure RISK_FORCE_REASON_REQUIRED, got success=%v code=%q", success, code)
	}
}

// force=true 且带非空 reason → 通过 reason 闸门、抵达引擎并成功释放（用户维度恒释放，Del 幂等）。
// 证明新增闸门不误伤正常 force 流程。
func TestHandleAdminReleaseTrialLimit_ForceWithReasonSucceeds(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := &App{RiskEngine: risk.NewEngine(risk.NewMemKVCache(nil))}
	c, w := testCtx(http.MethodPost, "/api/admin/risk/trial-limit/release",
		`{"user_id":7,"force":true,"reason":"客服已人工核实，释放遗留键"}`)
	setID(c, 1)

	app.HandleAdminReleaseTrialLimit(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if success, code := decodeRiskResp(t, w.Body.Bytes()); !success || code != "" {
		t.Fatalf("want success, got success=%v code=%q", success, code)
	}
}

// 非 force 释放不强制 reason → 正常放行（reason 闸门只在 force=true 时生效）。
func TestHandleAdminReleaseTrialLimit_NonForceNoReasonOK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := &App{RiskEngine: risk.NewEngine(risk.NewMemKVCache(nil))}
	c, w := testCtx(http.MethodPost, "/api/admin/risk/trial-limit/release",
		`{"user_id":7,"device_id":"dev-X"}`)
	setID(c, 1)

	app.HandleAdminReleaseTrialLimit(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if success, _ := decodeRiskResp(t, w.Body.Bytes()); !success {
		t.Fatal("non-force release without reason must succeed")
	}
}
