package payment

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// seedPaidOrder 直接落一条卡在 paid 的订单（绕过下单/回调），UpdatedAt 可控以测 before 过滤。
func seedPaidOrder(t *testing.T, repo *MemRepo, no string, typ OrderType, paidAt time.Time) {
	t.Helper()
	if err := repo.Create(context.Background(), &PayOrder{
		OrderNo: no, Type: typ, TenantID: 1, UserID: 42, Provider: ProviderWxpay,
		AmountUSD: 10, ActualPaid: 73, Status: OrderPaid, UpdatedAt: paidAt,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

// TestReconcileStuckPaidCreditsAndFinalizes 卡在 paid 的订单 → 重跑 OnPaid 一次 → credited。
func TestReconcileStuckPaidCreditsAndFinalizes(t *testing.T) {
	g, repo, recharge, _ := newGateway()
	seedPaidOrder(t, repo, "RCG-stuck", OrderTypeRecharge, time.Unix(1000, 0))

	res, err := g.ReconcileStuckPaid(context.Background(), time.Unix(2000, 0))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.Scanned != 1 || len(res.Reconciled) != 1 || res.Reconciled[0] != "RCG-stuck" {
		t.Fatalf("scanned=%d reconciled=%v, want 1 / [RCG-stuck]", res.Scanned, res.Reconciled)
	}
	if recharge.count() != 1 {
		t.Fatalf("OnPaid called %d times, want exactly 1 (idempotent re-credit)", recharge.count())
	}
	got, _ := repo.GetByOrderNo(context.Background(), "RCG-stuck")
	if got.Status != OrderCredited {
		t.Fatalf("status=%q, want credited", got.Status)
	}
}

// TestReconcileSkipsFreshAndNonPaid before 过滤掉刚占位的在途单；created/credited 不动。
func TestReconcileSkipsFreshAndNonPaid(t *testing.T) {
	g, repo, recharge, _ := newGateway()
	seedPaidOrder(t, repo, "RCG-fresh", OrderTypeRecharge, time.Unix(3000, 0)) // 晚于 before → 在途
	seedCreatedOrder(t, repo, "RCG-created", OrderTypeRecharge)                // created → 不扫

	res, err := g.ReconcileStuckPaid(context.Background(), time.Unix(2000, 0))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.Scanned != 0 || len(res.Reconciled) != 0 {
		t.Fatalf("scanned=%d reconciled=%v, want 0 (fresh-paid + created both skipped)", res.Scanned, res.Reconciled)
	}
	if recharge.count() != 0 {
		t.Fatalf("OnPaid called %d, want 0", recharge.count())
	}
}

// TestReconcileStuckCreated 卡在 created（回调从未送达）的订单 → 主动查单：已付补入账、未付不动。
func TestReconcileStuckCreated(t *testing.T) {
	g, repo, recharge, _ := newGateway()
	seedCreatedOrder(t, repo, "RCG-paid", OrderTypeRecharge)
	seedCreatedOrder(t, repo, "RCG-unpaid", OrderTypeRecharge)

	query := func(_ context.Context, orderNo, _ string) (bool, error) {
		return orderNo == "RCG-paid", nil // 仅 RCG-paid 平台已收款
	}
	// before 取极大值确保两单都被扫到（seed 的 UpdatedAt 为零值）。
	res, err := g.ReconcileStuckCreated(context.Background(), time.Unix(1<<40, 0), 0, 0, query)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.Scanned != 2 {
		t.Fatalf("scanned=%d, want 2", res.Scanned)
	}
	if len(res.Reconciled) != 1 || res.Reconciled[0] != "RCG-paid" {
		t.Fatalf("reconciled=%v, want [RCG-paid]", res.Reconciled)
	}
	if recharge.count() != 1 {
		t.Fatalf("OnPaid called %d, want exactly 1", recharge.count())
	}
	if got, _ := repo.GetByOrderNo(context.Background(), "RCG-paid"); got.Status != OrderCredited {
		t.Fatalf("RCG-paid status=%q, want credited", got.Status)
	}
	if got, _ := repo.GetByOrderNo(context.Background(), "RCG-unpaid"); got.Status != OrderCreated {
		t.Fatalf("RCG-unpaid status=%q, want created (untouched)", got.Status)
	}
}

// TestReconcileStuckCreatedExpire 未付且已超过 expireAge 的 created 单 → 自动置 failed（未付超时/过期）；
// 已付的仍正常补入账 credited（过期逻辑不误伤真付款）。
func TestReconcileStuckCreatedExpire(t *testing.T) {
	g, repo, _, _ := newGateway()
	seedCreatedOrder(t, repo, "RCG-paid", OrderTypeRecharge)
	seedCreatedOrder(t, repo, "RCG-stale", OrderTypeRecharge)

	query := func(_ context.Context, orderNo, _ string) (bool, error) {
		return orderNo == "RCG-paid", nil // 仅 RCG-paid 平台已收款
	}
	// expireAge=1h：seed 的 CreatedAt 为零值（远古），未付者均早于 now-1h → 过期置 failed。before 取极大值扫全部。
	res, err := g.ReconcileStuckCreated(context.Background(), time.Unix(1<<40, 0), time.Hour, 0, query)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(res.Reconciled) != 1 || res.Reconciled[0] != "RCG-paid" {
		t.Fatalf("reconciled=%v, want [RCG-paid]", res.Reconciled)
	}
	if len(res.Expired) != 1 || res.Expired[0] != "RCG-stale" {
		t.Fatalf("expired=%v, want [RCG-stale]", res.Expired)
	}
	if got, _ := repo.GetByOrderNo(context.Background(), "RCG-paid"); got.Status != OrderCredited {
		t.Fatalf("RCG-paid status=%q, want credited", got.Status)
	}
	if got, _ := repo.GetByOrderNo(context.Background(), "RCG-stale"); got.Status != OrderFailed {
		t.Fatalf("RCG-stale status=%q, want failed (expired)", got.Status)
	}
}

// TestReconcileStuckCreatedNilQuery query 未注入 → 安全空跑（不扫不入账）。
func TestReconcileStuckCreatedNilQuery(t *testing.T) {
	g, repo, _, _ := newGateway()
	seedCreatedOrder(t, repo, "RCG-x", OrderTypeRecharge)
	res, err := g.ReconcileStuckCreated(context.Background(), time.Unix(1<<40, 0), 0, 0, nil)
	if err != nil || res.Scanned != 0 {
		t.Fatalf("nil query should no-op, got scanned=%d err=%v", res.Scanned, err)
	}
}

// TestReconcileSinkStillFailingStaysPaid 入账仍失败 → 留在 paid、计入 Failed，下次可再试。
func TestReconcileSinkStillFailingStaysPaid(t *testing.T) {
	repo := NewMemRepo()
	recharge := newFakeSink()
	recharge.err = errors.New("quota down")
	g := NewGateway(repo, NewStubPaySDK("s"), map[OrderType]OrderSink{OrderTypeRecharge: recharge})
	seedPaidOrder(t, repo, "RCG-fail", OrderTypeRecharge, time.Unix(1000, 0))

	res, err := g.ReconcileStuckPaid(context.Background(), time.Unix(2000, 0))
	if err != nil {
		t.Fatalf("reconcile returned err: %v", err)
	}
	if len(res.Reconciled) != 0 || len(res.Failed) != 1 {
		t.Fatalf("reconciled=%v failed=%v, want 0 reconciled / 1 failed", res.Reconciled, res.Failed)
	}
	got, _ := repo.GetByOrderNo(context.Background(), "RCG-fail")
	if got.Status != OrderPaid {
		t.Fatalf("status=%q, want stays paid (retryable next sweep)", got.Status)
	}
}

// seedCreatedOrderAt 落一条指定下单时刻的 created 单（测年龄相关分支；UpdatedAt 零值恒早于 before）。
func seedCreatedOrderAt(t *testing.T, repo *MemRepo, no string, createdAt time.Time) {
	t.Helper()
	if err := repo.Create(context.Background(), &PayOrder{
		OrderNo: no, Type: OrderTypeRecharge, TenantID: 1, UserID: 42, Provider: ProviderWxpay,
		AmountUSD: 10, ActualPaid: 73, Status: OrderCreated, CreatedAt: createdAt,
	}); err != nil {
		t.Fatalf("seed order %s: %v", no, err)
	}
}

// 超龄已付单必须照常查单并补入账（audit 2026-07-17 #7 同族）：旧「超 maxAge 不查单直接置 failed」
// 把「没拿到答复」当「证实未付」——回调持续未达+查单持续故障超 26h 后，已付款单被静默注销。
func TestReconcileStuckCreatedAncientPaidStillCredited(t *testing.T) {
	now := time.Unix(200_000_000, 0)
	g, repo, recharge, _ := newGateway(WithClock(func() time.Time { return now }))
	seedCreatedOrderAt(t, repo, "RCG-ancient-paid", now.Add(-30*time.Hour))

	query := func(context.Context, string, string) (bool, error) { return true, nil }
	res, err := g.ReconcileStuckCreated(context.Background(), now, 2*time.Hour, 0, query)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(res.Reconciled) != 1 || res.Reconciled[0] != "RCG-ancient-paid" {
		t.Fatalf("reconciled=%v, want [RCG-ancient-paid]（超龄已付单必须查单救回，不得跳过查单直接注销）", res.Reconciled)
	}
	if len(res.Expired) != 0 {
		t.Fatalf("expired=%v, want []", res.Expired)
	}
	if recharge.count() != 1 {
		t.Fatalf("OnPaid called %d, want 1", recharge.count())
	}
	got, _ := repo.GetByOrderNo(context.Background(), "RCG-ancient-paid")
	if got.Status != OrderCredited {
		t.Fatalf("status=%q, want credited", got.Status)
	}
}

// 超龄单 + 查单失败 → 留 Failed（可见+告警+下轮重扫），保持 created 绝不静默终态。
func TestReconcileStuckCreatedAncientQueryErrorStaysScannable(t *testing.T) {
	now := time.Unix(200_000_000, 0)
	g, repo, _, _ := newGateway(WithClock(func() time.Time { return now }))
	seedCreatedOrderAt(t, repo, "RCG-ancient-err", now.Add(-30*time.Hour))

	query := func(context.Context, string, string) (bool, error) {
		return false, errors.New("context deadline exceeded")
	}
	res, err := g.ReconcileStuckCreated(context.Background(), now, 2*time.Hour, 0, query)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if _, ok := res.Failed["RCG-ancient-err"]; !ok || len(res.Expired) != 0 {
		t.Fatalf("Failed=%v Expired=%v, want Failed 含该单且不过期（查单失败没有确定性答复）", res.Failed, res.Expired)
	}
	got, _ := repo.GetByOrderNo(context.Background(), "RCG-ancient-err")
	if got.Status != OrderCreated {
		t.Fatalf("status=%q, want created（保持可见可重扫）", got.Status)
	}
}

// 网关明确「查无此单」＝确定性答复：超二维码窗口 → 过期清理（对齐 AGT/SUB）；窗口内 → 仍 Failed 不误杀。
func TestReconcileStuckCreatedNotExistExpiresAfterWindow(t *testing.T) {
	now := time.Unix(200_000_000, 0)
	g, repo, _, _ := newGateway(WithClock(func() time.Time { return now }))
	seedCreatedOrderAt(t, repo, "RCG-gone-old", now.Add(-3*time.Hour))       // 超 2h 窗口 → 过期
	seedCreatedOrderAt(t, repo, "RCG-gone-recent", now.Add(-30*time.Minute)) // 窗口内 → Failed

	query := func(_ context.Context, orderNo, _ string) (bool, error) {
		return false, fmt.Errorf("wxpay query: %w", ErrOrderNotExist)
	}
	res, err := g.ReconcileStuckCreated(context.Background(), now, 2*time.Hour, 0, query)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(res.Expired) != 1 || res.Expired[0] != "RCG-gone-old" {
		t.Fatalf("expired=%v, want [RCG-gone-old]（确定性『查无此单』超窗即清）", res.Expired)
	}
	if _, ok := res.Failed["RCG-gone-recent"]; !ok {
		t.Fatalf("Failed=%v, want 含 RCG-gone-recent（窗口内不误杀）", res.Failed)
	}
	gone, _ := repo.GetByOrderNo(context.Background(), "RCG-gone-old")
	if gone.Status != OrderFailed {
		t.Fatalf("RCG-gone-old status=%q, want failed", gone.Status)
	}
}
