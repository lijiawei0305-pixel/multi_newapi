package mtwire

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/platform/apperr"
	"github.com/QuantumNous/new-api/internal/risk"
)

// 后台风控运维错误码。
var (
	// errRiskEngineOff：风控引擎未装配（测试/异常装配）→ 无从释放。生产始终注入带 DB 台账的 Engine。
	errRiskEngineOff = apperr.New("RISK_ENGINE_UNAVAILABLE", "风控引擎未启用，无限购台账可释放", http.StatusServiceUnavailable)
	// errRiskReleaseInput：释放限购入参非法（user_id 缺失/<=0）。
	errRiskReleaseInput = apperr.New("RISK_RELEASE_INPUT_INVALID", "释放限购入参非法：user_id 必填且需 >0", http.StatusBadRequest)
	// errRiskForceReasonRequired：force 绕过归属校验属敏感操作（防线与绕过开关同握一手），
	// 必须带非空 reason 留可追溯理由，否则拒绝。
	errRiskForceReasonRequired = apperr.New("RISK_FORCE_REASON_REQUIRED", "force 释放必须提供 reason（归属校验绕过须留可追溯理由）", http.StatusBadRequest)
)

// HandleAdminReleaseTrialLimit POST /api/admin/risk/trial-limit/release —— 后台释放某用户被误占用的
// Trial 终身限购键（或某非 Trial 套餐的限购键）。需 AdminAuth。
//
// 背景（RETRO 2026-07-16 · Critical）：Purchase 在 CreateOrder 之前即调 CheckPurchaseLimit，以 SetNX 写
// 永久（PurchaseDedupTTL=0）三维去重键，而 KVCache 此前无 Del 原语 → 用户点开收银台犹豫关单即永久消耗、
// 后台零手段，客服只能直连**无密码/无卷/无审计**的 Redis 删键。此端点补上带鉴权 + 审计日志的释放通道：
// 删掉 user(∪realname∪device) 维度键，使其可重新购买 Trial。
//
// 入参（JSON）：{user_id(必填,>0), realname_id?, device_id?, plan_id?, force?, reason?(force=true 时必填)}。
//   - plan_id<=0 或缺省 → 释放 Trial 三维键（用户 + 传入的实名/设备维度，空维度跳过）；
//   - plan_id>0        → 释放该非 Trial 套餐的每用户限购计数键。
//
// 归属校验：realname/device 维度跨用户共享，请求体里的值系客服转述用户**自报**、与 user_id
// 零绑定——引擎侧按键值（占用者 userID）校验归属，不符默认拒删并在响应 skipped 中回报，
// 防止「给 B 释放」误删 A 合法占用的反刷键（新账号可借已消耗设备再领 Trial）。
// force=true **仅**豁免归属不可考的遗留/脏值键（**绝不**豁免另一真实用户的有效占用键，引擎侧
// forceReleasable 兜底），且**必须带非空 reason**（可追溯理由，否则 400 RISK_FORCE_REASON_REQUIRED）——
// 防线（归属校验）与绕过开关（force）握在同一只客服手里，reason + 审计留痕是唯一可追责抓手（audit F4）。
//
// Del 幂等：目标键本就不存在（如误传/已释放）也返回成功，不报错。
func (a *App) HandleAdminReleaseTrialLimit(c *gin.Context) {
	var body struct {
		UserID     int64  `json:"user_id"`
		RealNameID string `json:"realname_id"`
		DeviceID   string `json:"device_id"`
		PlanID     int64  `json:"plan_id"`
		Force      bool   `json:"force"`
		Reason     string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.UserID <= 0 {
		respondErr(c, errRiskReleaseInput)
		return
	}
	// force 绕过归属校验属敏感操作——必须带非空 reason 留可追溯理由（仅空格视为未提供），否则拒绝。
	if body.Force && strings.TrimSpace(body.Reason) == "" {
		respondErr(c, errRiskForceReasonRequired)
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
		res   risk.TrialReleaseResult
	)
	if body.PlanID > 0 {
		scope = "purchase:plan=" + strconv.FormatInt(body.PlanID, 10)
		err = admin.ReleasePurchaseLimit(ctx, body.PlanID, body.UserID)
		res.Released = []string{"purchase"}
	} else {
		scope = "trial"
		res, err = admin.ReleaseTrialLimit(ctx, body.UserID, risk.PurchaseIdentity{
			RealNameID: body.RealNameID,
			DeviceID:   body.DeviceID,
		}, body.Force)
	}
	if err != nil {
		respondErr(c, err)
		return
	}

	// 审计：留痕「谁在何时释放了谁的限购、含哪些维度、是否 force 绕过归属校验、绕过理由、
	// 哪些维度被归属校验拒删」——补上此前直连 Redis 删键的「无审计」缺口（RETRO 2026-07-16）。
	// reason 是 force 绕过的可追溯理由（audit F4，force=true 时必填）。走 common.SysLog 进系统日志表，可追责。
	common.SysLog(fmt.Sprintf(
		"admin release purchase-limit: operator=%d target_user=%d scope=%s realname=%q device=%q force=%t reason=%q released=%v skipped=%v",
		operator, body.UserID, scope, body.RealNameID, body.DeviceID, body.Force, body.Reason, res.Released, res.Skipped))

	respondOK(c, gin.H{
		"user_id":  body.UserID,
		"scope":    scope,
		"released": res.Released,
		"skipped":  res.Skipped,
	})
}
