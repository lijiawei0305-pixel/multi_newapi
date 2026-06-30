package payment

import (
	"context"
	"errors"
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
	// before 取极大值确保两单都被扫到（seed 的 UpdatedAt 为零值）；maxAge=0 关闭年龄过滤。
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
