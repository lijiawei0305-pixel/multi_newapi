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
		AmountUSD: 10, ActualPaid: 73, ActualPaidFen: 7300,
		ProviderTransactionID: "txn-" + no,
		Status:                OrderPaid, UpdatedAt: paidAt,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

// paidQueryResult builds a strict QueryResult for tests.
func paidQueryResult(orderNo string, fen int64) *QueryResult {
	return &QueryResult{
		Provider: ProviderWxpay, OrderNo: orderNo, Paid: true,
		TransactionID: "txn-" + orderNo, PaidAmountFen: fen, TradeState: "SUCCESS",
		NormalizedState: TradeStateSuccess, Currency: "CNY",
		MchID: "m", AppID: "a", ExpectedMchID: "m", ExpectedAppID: "a",
	}
}

func unpaidQueryResult(orderNo string) *QueryResult {
	return &QueryResult{
		Provider: ProviderWxpay, OrderNo: orderNo, Paid: false, TradeState: "NOTPAY",
		NormalizedState: TradeStateNotPay, MchID: "m", AppID: "a", ExpectedMchID: "m", ExpectedAppID: "a",
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

	query := func(_ context.Context, orderNo, _ string) (*QueryResult, error) {
		if orderNo == "RCG-paid" {
			return paidQueryResult(orderNo, 7300), nil
		}
		return unpaidQueryResult(orderNo), nil
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

// TestReconcileStuckCreatedExpire Phase E：本地过期不得在 NOTPAY 时直接 failed（微信默认最长 7 天）。
// 已付仍补入账；未付无 QR → close_pending。
func TestReconcileStuckCreatedExpire(t *testing.T) {
	g, repo, _, _ := newGateway()
	seedCreatedOrder(t, repo, "RCG-paid", OrderTypeRecharge)
	seedCreatedOrder(t, repo, "RCG-stale", OrderTypeRecharge)

	query := func(_ context.Context, orderNo, _ string) (*QueryResult, error) {
		if orderNo == "RCG-paid" {
			return paidQueryResult(orderNo, 7300), nil
		}
		return unpaidQueryResult(orderNo), nil
	}
	res, err := g.ReconcileStuckCreated(context.Background(), time.Unix(1<<40, 0), time.Hour, 0, query)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(res.Reconciled) != 1 || res.Reconciled[0] != "RCG-paid" {
		t.Fatalf("reconciled=%v, want [RCG-paid]", res.Reconciled)
	}
	if len(res.Expired) != 0 {
		t.Fatalf("expired=%v, want [] (Phase E: no local-expire→failed on NOTPAY)", res.Expired)
	}
	if got, _ := repo.GetByOrderNo(context.Background(), "RCG-paid"); got.Status != OrderCredited {
		t.Fatalf("RCG-paid status=%q, want credited", got.Status)
	}
	if got, _ := repo.GetByOrderNo(context.Background(), "RCG-stale"); got.Status != OrderCreated {
		t.Fatalf("RCG-stale status=%q, want created (close_pending path)", got.Status)
	}
	if got, _ := repo.GetByOrderNo(context.Background(), "RCG-stale"); got.CreateState != CreateStateClosePending {
		t.Fatalf("RCG-stale create_state=%q, want close_pending", got.CreateState)
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
		AmountUSD: 10, ActualPaid: 73, ActualPaidFen: 7300, Status: OrderCreated, CreatedAt: createdAt,
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

	query := func(_ context.Context, orderNo, _ string) (*QueryResult, error) {
		return paidQueryResult(orderNo, 7300), nil
	}
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

	query := func(context.Context, string, string) (*QueryResult, error) {
		return nil, errors.New("context deadline exceeded")
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

// Phase F：ORDER_NOT_EXIST → local_created（允许同 out_trade_no 再 Prepay），不因本地 2h 直接 failed。
func TestReconcileStuckCreatedNotExistExpiresAfterWindow(t *testing.T) {
	now := time.Unix(200_000_000, 0)
	g, repo, _, _ := newGateway(WithClock(func() time.Time { return now }))
	seedCreatedOrderAt(t, repo, "RCG-gone-old", now.Add(-3*time.Hour))
	seedCreatedOrderAt(t, repo, "RCG-gone-recent", now.Add(-30*time.Minute))

	query := func(_ context.Context, orderNo, _ string) (*QueryResult, error) {
		return &QueryResult{Provider: ProviderWxpay, NotExist: true, NormalizedState: TradeStateOrderNotExist},
			fmt.Errorf("wxpay query: %w", ErrOrderNotExist)
	}
	res, err := g.ReconcileStuckCreated(context.Background(), now, 2*time.Hour, 0, query)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(res.Expired) != 0 {
		t.Fatalf("expired=%v, want [] (ORDER_NOT_EXIST → re-prepay allowed, not failed)", res.Expired)
	}
	for _, no := range []string{"RCG-gone-old", "RCG-gone-recent"} {
		got, _ := repo.GetByOrderNo(context.Background(), no)
		if got.Status != OrderCreated {
			t.Fatalf("%s status=%q, want created", no, got.Status)
		}
		if got.CreateState != CreateStateLocalCreated {
			t.Fatalf("%s create_state=%q, want local_created (re-prepay after NOT_EXIST)", no, got.CreateState)
		}
	}
}
