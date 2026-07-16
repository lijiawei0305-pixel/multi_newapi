package mtwire

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/platform/apperr"
	"github.com/QuantumNous/new-api/internal/risk"
)

// 后台风控运维错误码。
var (
	// errRiskEngineOff：风控引擎未装配（Redis 关）→ 无限购键存在，无从释放。
	errRiskEngineOff = apperr.New("RISK_ENGINE_UNAVAILABLE", "风控引擎未启用（Redis 未装配），无限购键可释放", http.StatusServiceUnavailable)
	// errRiskReleaseInput：释放限购入参非法（user_id 缺失/<=0）。
	errRiskReleaseInput = apperr.New("RISK_RELEASE_INPUT_INVALID", "释放限购入参非法：user_id 必填且需 >0", http.StatusBadRequest)
)

// HandleAdminReleaseTrialLimit POST /api/admin/risk/trial-limit/release —— 后台释放某用户被误占用的
// Trial 终身限购键（或某非 Trial 套餐的限购键）。需 AdminAuth。
//
// 背景（RETRO 2026-07-16 · Critical）：Purchase 在 CreateOrder 之前即调 CheckPurchaseLimit，以 SetNX 写
// 永久（PurchaseDedupTTL=0）三维去重键，而 KVCache 此前无 Del 原语 → 用户点开收银台犹豫关单即永久消耗、
// 后台零手段，客服只能直连**无密码/无卷/无审计**的 Redis 删键。此端点补上带鉴权 + 审计日志的释放通道：
// 删掉 user(∪realname∪device) 维度键，使其可重新购买 Trial。
//
// 入参（JSON）：{user_id(必填,>0), realname_id?, device_id?, plan_id?}。
//   - plan_id<=0 或缺省 → 释放 Trial 三维键（用户 + 传入的实名/设备维度，空维度跳过）；
//   - plan_id>0        → 释放该非 Trial 套餐的每用户限购计数键。
//
// Del 幂等：目标键本就不存在（如误传/已释放）也返回成功，不报错。
func (a *App) HandleAdminReleaseTrialLimit(c *gin.Context) {
	var body struct {
		UserID     int64  `json:"user_id"`
		RealNameID string `json:"realname_id"`
		DeviceID   string `json:"device_id"`
		PlanID     int64  `json:"plan_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.UserID <= 0 {
		respondErr(c, errRiskReleaseInput)
		return
	}
	// a.RiskEngine 是 risk.RiskEngine（CheckCall 契约），后台释放走独立的 PurchaseLimitAdmin 能力；
	// Redis 关时 riskEngine 为 nil（wire.go）→ 类型断言失败 → 明确报「引擎未启用」，不 panic。
	admin, ok := a.RiskEngine.(risk.PurchaseLimitAdmin)
	if !ok || admin == nil {
		respondErr(c, errRiskEngineOff)
		return
	}

	ctx := reqCtx(c)
	operator := c.GetInt("id") // AdminAuth 注入的操作者用户 id（审计留痕用）
	var (
		err   error
		scope string
	)
	if body.PlanID > 0 {
		scope = "purchase:plan=" + strconv.FormatInt(body.PlanID, 10)
		err = admin.ReleasePurchaseLimit(ctx, body.PlanID, body.UserID)
	} else {
		scope = "trial"
		err = admin.ReleaseTrialLimit(ctx, body.UserID, risk.PurchaseIdentity{
			RealNameID: body.RealNameID,
			DeviceID:   body.DeviceID,
		})
	}
	if err != nil {
		respondErr(c, err)
		return
	}

	// 审计：留痕「谁在何时释放了谁的限购、含哪些维度」——补上此前直连 Redis 删键的「无审计」缺口
	// （RETRO 2026-07-16）。走 common.SysLog 进系统日志表，可追责。
	common.SysLog(fmt.Sprintf(
		"admin release purchase-limit: operator=%d target_user=%d scope=%s realname=%q device=%q",
		operator, body.UserID, scope, body.RealNameID, body.DeviceID))

	respondOK(c, gin.H{"user_id": body.UserID, "scope": scope, "released": true})
}
