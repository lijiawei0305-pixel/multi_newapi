package mtwire

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// stuckOrderOut 是 admin「支付对账」页展示的一条卡单。
type stuckOrderOut struct {
	Kind      string  `json:"kind"` // "RCG"（充值）| "SUB"（套餐）| "AGT"（代理套餐）
	OrderNo   string  `json:"order_no"`
	TenantID  int64   `json:"tenant_id"`
	UserID    int64   `json:"user_id"`
	Amount    float64 `json:"amount"` // 实付（¥）
	Status    string  `json:"status"`
	StuckSecs int64   `json:"stuck_secs"` // 卡了多久（秒）
}

// HandleAdminListStuck GET /api/admin/reconcile/stuck —— 列当前卡单（RCG paid + SUB 平台建单中/失败/pending +
// AGT pending、早于对账阈值）。只读、不触发入账。AdminAuth。
func (a *App) HandleAdminListStuck(c *gin.Context) {
	ctx := reqCtx(c)
	now := time.Now()
	before := now.Add(-reconcileMinAge)
	out := []stuckOrderOut{}

	if a.RechargeGateway != nil {
		paid, err := a.RechargeGateway.ListStuckPaid(ctx, before)
		if err != nil {
			respondErr(c, err)
			return
		}
		for _, o := range paid {
			out = append(out, stuckOrderOut{
				Kind: "RCG", OrderNo: o.OrderNo, TenantID: o.TenantID, UserID: o.UserID,
				Amount: o.ActualPaid, Status: string(o.Status), StuckSecs: int64(now.Sub(o.UpdatedAt).Seconds()),
			})
		}
	}
	subs, err := a.listStuckSubscriptions(ctx, before)
	if err != nil {
		respondErr(c, err)
		return
	}
	for _, s := range subs {
		out = append(out, stuckOrderOut{
			Kind: "SUB", OrderNo: s.OrderNo, TenantID: s.TenantID, UserID: s.UserID,
			Amount: s.AmountCNY, Status: s.Status, StuckSecs: int64(now.Sub(s.UpdatedAt).Seconds()),
		})
	}
	agts, err := a.listStuckAgentPlans(ctx, before)
	if err != nil {
		respondErr(c, err)
		return
	}
	for _, o := range agts {
		// pending AGT 单尚未激活，agent_tenant_id=0（激活时才回填）；owner_user_id 为买家。
		out = append(out, stuckOrderOut{
			Kind: "AGT", OrderNo: o.OrderNo, TenantID: o.AgentTenantID, UserID: o.OwnerUserID,
			Amount: o.AmountCNY, Status: o.Status, StuckSecs: int64(now.Sub(o.UpdatedAt).Seconds()),
		})
	}
	hbOut := reconcileHeartbeatOut{}
	if hb, ok := a.getReconcileHeartbeat(ctx); ok {
		hbOut = reconcileHeartbeatOut{
			LastRunAt: hb.LastRunAt.Unix(), LastTrigger: hb.LastTrigger, TodayRuns: hb.TodayRuns,
			LastStuckCount: hb.LastStuckCount, LastFailedCount: hb.LastFailedCount,
		}
	}
	respondOK(c, gin.H{"stuck": out, "threshold_secs": int64(reconcileMinAge.Seconds()), "heartbeat": hbOut})
}

// HandleAdminRunReconcile POST /api/admin/reconcile/run —— 手动立即对账，走与 5min 定时同一入口
// runReconcileAll("manual")：跑全 4 条（RCG-paid ① + RCG-created ② + SUB ③ + AGT ④，修早前只跑 ①③ 的
// drift）、更新心跳、落一条历史。返回四路径结果（best-effort：单路径错误折进各自 failed，仍返回 200）。AdminAuth。
func (a *App) HandleAdminRunReconcile(c *gin.Context) {
	ctx := reqCtx(c)
	before := time.Now().Add(-reconcileMinAge)
	paid, created, sub, agt := a.runReconcileAll(ctx, before, "manual")
	respondOK(c, gin.H{
		"rcg":         gin.H{"scanned": paid.Scanned, "credited": paid.Reconciled, "failed": paid.Failed},
		"rcg_created": gin.H{"scanned": created.Scanned, "credited": created.Reconciled, "expired": created.Expired, "failed": created.Failed},
		"sub":         gin.H{"scanned": sub.Scanned, "activated": sub.Activated, "unpaid": sub.Unpaid, "expired": sub.Expired, "failed": sub.Failed},
		"agt":         gin.H{"scanned": agt.Scanned, "activated": agt.Activated, "unpaid": agt.Unpaid, "expired": agt.Expired, "failed": agt.Failed},
	})
}

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

// reconcileHeartbeatOut 是 /stuck 响应内嵌的心跳（前端顶部心跳条用）；从未跑过则 last_run_at=0。
type reconcileHeartbeatOut struct {
	LastRunAt       int64  `json:"last_run_at"` // unix 秒，0=从未
	LastTrigger     string `json:"last_trigger"`
	TodayRuns       int    `json:"today_runs"`
	LastStuckCount  int    `json:"last_stuck_count"`
	LastFailedCount int    `json:"last_failed_count"`
}
