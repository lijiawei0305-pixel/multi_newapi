package mtwire

// AGT 对账终态判定必须只接受网关**确定性答复**（audit 2026-07-17 发现#7）：
// 旧 maxAge=26h「极旧+任何查单错误→兜底过期」是循环论证——其辩护「若曾支付,前面数百轮查单必已
// 捕获」恰恰在该分支的触发条件（查单正在失败：凭据轮换配错/渠道临时停用→每次查单都报错）下不成立。
// 后果：已付 ¥9990 订单被静默写成终态 expired、从卡单页消失（listStuck 只选 pending）、Failed 为空
// → alertReconcileHealth 不触发、永不重试。修复后：查单失败一律留 Failed（可见+告警+下轮重扫），
// 只有「查无此单」（超二维码窗口）或「确认未付且超时」这两种确定性答复才允许过期。
// 代价（刻意）：真死单在网关持续不可查期间一直占卡单页+重复告警——可见噪音优于无声钱损。

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/payment"
)

// seedAncientAgtOrder 落一笔下单于 30h 前的 pending AGT 单（超 2h 过期窗、也超旧 26h maxAge）。
func seedAncientAgtOrder(t *testing.T, app *App, orderNo string, ownerID int64) {
	t.Helper()
	old := time.Now().Add(-30 * time.Hour)
	if err := app.DB.Create(&agentPlanOrderRow{
		OrderNo: orderNo, OwnerUserID: ownerID, PlanID: 1, PlanCode: "starter",
		AmountCNY: 6.90, Status: agtOrderPending, GrantLevel: 0, ValidDays: 365,
		CreatedAt: old, UpdatedAt: old,
	}).Error; err != nil {
		t.Fatalf("seed ancient order %s: %v", orderNo, err)
	}
}

// stubAgtQueryErr 把查单桩成持续瞬时错误（非 ErrOrderNotExist——超时/限流/凭据不完整同形状）。
func stubAgtQueryErr(t *testing.T) {
	t.Helper()
	orig := agtOrderPaidQuery
	agtOrderPaidQuery = func(*App, context.Context, string, string) (bool, error) {
		return false, errors.New("context deadline exceeded (Client.Timeout exceeded)")
	}
	t.Cleanup(func() { agtOrderPaidQuery = orig })
}

// 核心回归：超龄单 + 瞬时查单错误 → 绝不 expired；进 Failed（触发告警）、仍被卡单页列出、下轮仍被扫。
// 今日行为＝ancient 短路任何错误 → 静默终态、Failed 空、卡单页消失、永不重试。
func TestReconcileAgt_AncientTransientErrorStaysVisible(t *testing.T) {
	app, ownerID := newAgentPlanActivateTestApp(t)
	ctx := context.Background()
	stubAgtQueryErr(t)
	seedAncientAgtOrder(t, app, "AGTANCIENT", ownerID)

	before := time.Now().Add(-time.Minute)
	res, err := app.ReconcileStuckAgentPlans(ctx, before)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(res.Expired) != 0 {
		t.Fatalf("Expired=%v, want []（查单失败没有确定性答复，不得过期）", res.Expired)
	}
	if _, ok := res.Failed["AGTANCIENT"]; !ok || len(res.Failed) != 1 {
		t.Fatalf("Failed=%v, want {AGTANCIENT: query …}（留 Failed → failed>0 → Critical 告警）", res.Failed)
	}
	var row agentPlanOrderRow
	if err := app.DB.Take(&row, "order_no = ?", "AGTANCIENT").Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if row.Status != agtOrderPending {
		t.Fatalf("status=%q, want pending（保持可见可重试）", row.Status)
	}
	// 仍在管理员卡单页可见。
	stuck, err := app.listStuckAgentPlans(ctx, before)
	if err != nil || len(stuck) != 1 || stuck[0].OrderNo != "AGTANCIENT" {
		t.Fatalf("listStuck=(%v, %v), want [AGTANCIENT]（不得从卡单页消失）", stuck, err)
	}
	// 下一轮仍被扫描（未被写成终态）。
	res2, err := app.ReconcileStuckAgentPlans(ctx, before)
	if err != nil || res2.Scanned != 1 {
		t.Fatalf("second round Scanned=%d (err=%v), want 1（下轮必须继续重试）", res2.Scanned, err)
	}
}

// 头号场景端到端：已付 ¥ 单在查单故障期（如凭据轮换配错）绝不被杀，故障恢复后下一轮真实激活成功。
// 今日行为＝第一轮即静默 expired，恢复后第二轮 Scanned=0、钱永久蒸发。
func TestReconcileAgt_AncientPaidRecoversAfterOutage(t *testing.T) {
	app, ownerID := newAgentPlanActivateTestApp(t)
	ctx := context.Background()
	seedAncientAgtOrder(t, app, "AGTPAIDOLD", ownerID)
	before := time.Now().Add(-time.Minute)

	// 故障期：查单持续报错 → 只允许 Failed。
	origQ := agtOrderPaidQuery
	t.Cleanup(func() { agtOrderPaidQuery = origQ })
	agtOrderPaidQuery = func(*App, context.Context, string, string) (bool, error) {
		return false, errors.New("wxpay: SYSTEM_ERROR (backoff)")
	}
	if res, err := app.ReconcileStuckAgentPlans(ctx, before); err != nil || len(res.Expired) != 0 {
		t.Fatalf("故障期 = (Expired=%v, %v), want 不过期", res.Expired, err)
	}

	// 故障恢复：网关确认已付 → 真实激活链补激活（非桩）。
	agtOrderPaidQuery = func(*App, context.Context, string, string) (bool, error) { return true, nil }
	res, err := app.ReconcileStuckAgentPlans(ctx, before)
	if err != nil {
		t.Fatalf("reconcile after recovery: %v", err)
	}
	if len(res.Activated) != 1 || res.Activated[0] != "AGTPAIDOLD" {
		t.Fatalf("Activated=%v, want [AGTPAIDOLD]", res.Activated)
	}
	var row agentPlanOrderRow
	if err := app.DB.Take(&row, "order_no = ?", "AGTPAIDOLD").Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if row.Status != agtOrderActivated {
		t.Fatalf("status=%q, want activated（故障恢复后已付单必须救回）", row.Status)
	}
	_ = ownerID
}

// 守卫：确定性答复仍照常清废单——「查无此单」且超 2h 二维码窗口 → 过期（清理能力不因修复而丢失）。
func TestReconcileAgt_AncientOrderNotExistStillExpires(t *testing.T) {
	app, ownerID := newAgentPlanActivateTestApp(t)
	ctx := context.Background()
	seedAncientAgtOrder(t, app, "AGTGONE", ownerID)

	origQ := agtOrderPaidQuery
	t.Cleanup(func() { agtOrderPaidQuery = origQ })
	agtOrderPaidQuery = func(*App, context.Context, string, string) (bool, error) {
		return false, fmt.Errorf("wxpay query: %w", payment.ErrOrderNotExist)
	}
	res, err := app.ReconcileStuckAgentPlans(ctx, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(res.Expired) != 1 || res.Expired[0] != "AGTGONE" {
		t.Fatalf("Expired=%v, want [AGTGONE]（确定性『查无此单』仍须清理）", res.Expired)
	}
	var row agentPlanOrderRow
	if err := app.DB.Take(&row, "order_no = ?", "AGTGONE").Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if row.Status != agtOrderExpired {
		t.Fatalf("status=%q, want expired", row.Status)
	}
}
