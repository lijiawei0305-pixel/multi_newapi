package mtwire

import (
	"time"

	"github.com/gin-gonic/gin"
)

// stuckOrderOut 是 admin「支付对账」页展示的一条卡单。
type stuckOrderOut struct {
	Kind      string  `json:"kind"`       // "RCG"（充值）| "SUB"（套餐）
	OrderNo   string  `json:"order_no"`
	TenantID  int64   `json:"tenant_id"`
	UserID    int64   `json:"user_id"`
	Amount    float64 `json:"amount"`     // 实付（¥）
	Status    string  `json:"status"`
	StuckSecs int64   `json:"stuck_secs"` // 卡了多久（秒）
}

// HandleAdminListStuck GET /api/admin/reconcile/stuck —— 列当前卡单（RCG paid + SUB pending、
// 早于对账阈值）。只读、不触发入账。AdminAuth。
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
	respondOK(c, gin.H{"stuck": out, "threshold_secs": int64(reconcileMinAge.Seconds())})
}

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
