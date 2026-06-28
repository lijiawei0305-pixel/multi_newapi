package agent

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

// seedWithdrawable 通过收益入账为 tenant 注入可提现余额，返回组装好的 repo。
func seedWithdrawable(t *testing.T, tenantID int64, amount float64) *MemRepo {
	t.Helper()
	repo := NewMemRepo()
	if _, err := repo.AppendEarning(context.Background(), EarningEntry{
		TenantID: tenantID, SourceType: SourceRechargeSpread, SourceID: "seed", Amount: amount,
	}); err != nil {
		t.Fatalf("seed earning failed: %v", err)
	}
	return repo
}

func TestRequest_FreezesAndConserves(t *testing.T) {
	ctx := context.Background()
	repo := seedWithdrawable(t, 1, 100)
	svc := NewWithdrawalService(repo)

	wd, err := svc.Request(ctx, WithdrawInput{TenantID: 1, UserID: 5, Amount: 30, Remark: "cashout"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wd.ID == 0 || wd.Status != WithdrawPending {
		t.Fatalf("withdrawal = %+v, want non-zero id and pending", wd)
	}

	w, _ := repo.GetWallet(ctx, 1)
	if w.WithdrawableBalance != 70 || w.FrozenWithdrawAmount != 30 {
		t.Fatalf("withdrawable=%v frozen=%v, want 70/30", w.WithdrawableBalance, w.FrozenWithdrawAmount)
	}
	// 金额守恒：冻结 + 可用 不变。
	if w.WithdrawableBalance+w.FrozenWithdrawAmount != 100 {
		t.Fatalf("conservation broken: available+frozen = %v, want 100",
			w.WithdrawableBalance+w.FrozenWithdrawAmount)
	}
}

func TestRequest_InsufficientRejected(t *testing.T) {
	ctx := context.Background()
	repo := seedWithdrawable(t, 1, 20)
	svc := NewWithdrawalService(repo)

	_, err := svc.Request(ctx, WithdrawInput{TenantID: 1, Amount: 50})
	assertCode(t, err, CodeWithdrawInsufficient)

	// 失败不冻结、余额不动。
	w, _ := repo.GetWallet(ctx, 1)
	if w.WithdrawableBalance != 20 || w.FrozenWithdrawAmount != 0 {
		t.Fatalf("withdrawable=%v frozen=%v, want 20/0", w.WithdrawableBalance, w.FrozenWithdrawAmount)
	}
}

func TestRequest_NonPositiveRejected(t *testing.T) {
	ctx := context.Background()
	svc := NewWithdrawalService(seedWithdrawable(t, 1, 100))
	for _, amt := range []float64{0, -10} {
		_, err := svc.Request(ctx, WithdrawInput{TenantID: 1, Amount: amt})
		assertCode(t, err, CodeWithdrawInsufficient)
	}
}

func TestReview_ApprovePaysOut(t *testing.T) {
	ctx := context.Background()
	repo := seedWithdrawable(t, 1, 100)
	svc := NewWithdrawalService(repo)

	wd, _ := svc.Request(ctx, WithdrawInput{TenantID: 1, Amount: 40})
	if err := svc.Review(ctx, wd.ID, true, "paid offline"); err != nil {
		t.Fatalf("approve error: %v", err)
	}

	got, _ := repo.GetWithdrawal(ctx, wd.ID)
	if got.Status != WithdrawApproved || got.Remark != "paid offline" || got.ReviewedAt.IsZero() {
		t.Fatalf("withdrawal after approve = %+v", got)
	}
	// 线下打款：冻结清零，可提现不退回（资金离开系统）。
	w, _ := repo.GetWallet(ctx, 1)
	if w.FrozenWithdrawAmount != 0 || w.WithdrawableBalance != 60 {
		t.Fatalf("withdrawable=%v frozen=%v, want 60/0", w.WithdrawableBalance, w.FrozenWithdrawAmount)
	}
}

func TestReview_RejectUnfreezesAndConserves(t *testing.T) {
	ctx := context.Background()
	repo := seedWithdrawable(t, 1, 100)
	svc := NewWithdrawalService(repo)

	wd, _ := svc.Request(ctx, WithdrawInput{TenantID: 1, Amount: 40})
	if err := svc.Review(ctx, wd.ID, false, "rejected"); err != nil {
		t.Fatalf("reject error: %v", err)
	}

	got, _ := repo.GetWithdrawal(ctx, wd.ID)
	if got.Status != WithdrawRejected {
		t.Fatalf("status = %q, want rejected", got.Status)
	}
	// 解冻退回：可提现复原，冻结清零，金额守恒。
	w, _ := repo.GetWallet(ctx, 1)
	if w.WithdrawableBalance != 100 || w.FrozenWithdrawAmount != 0 {
		t.Fatalf("withdrawable=%v frozen=%v, want 100/0", w.WithdrawableBalance, w.FrozenWithdrawAmount)
	}
}

func TestReview_NonPendingRejected(t *testing.T) {
	ctx := context.Background()
	repo := seedWithdrawable(t, 1, 100)
	svc := NewWithdrawalService(repo)

	wd, _ := svc.Request(ctx, WithdrawInput{TenantID: 1, Amount: 40})
	if err := svc.Review(ctx, wd.ID, true, "first"); err != nil {
		t.Fatalf("first approve error: %v", err)
	}
	// 再次审核已 approved 的单 → WITHDRAW_NOT_PENDING。
	assertCode(t, svc.Review(ctx, wd.ID, false, "again"), CodeWithdrawNotPending)
	assertCode(t, svc.Review(ctx, wd.ID, true, "again"), CodeWithdrawNotPending)

	// 资金不因二次审核而改变。
	w, _ := repo.GetWallet(ctx, 1)
	if w.WithdrawableBalance != 60 || w.FrozenWithdrawAmount != 0 {
		t.Fatalf("withdrawable=%v frozen=%v, want 60/0", w.WithdrawableBalance, w.FrozenWithdrawAmount)
	}
}

func TestReview_NotFound(t *testing.T) {
	ctx := context.Background()
	svc := NewWithdrawalService(NewMemRepo())
	assertCode(t, svc.Review(ctx, 404, true, ""), CodeWithdrawNotFound)
}

// TestRequest_ConcurrentNoOverdraw 验证 -race 下并发申请不击穿可提现余额，且金额守恒。
func TestRequest_ConcurrentNoOverdraw(t *testing.T) {
	ctx := context.Background()
	const balance = 100
	repo := seedWithdrawable(t, 1, balance)
	svc := NewWithdrawalService(repo)

	var wg sync.WaitGroup
	var ok int64
	// 200 个并发申请，每个 1 元，余额仅 100 → 恰好 100 个成功。
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.Request(ctx, WithdrawInput{TenantID: 1, Amount: 1}); err == nil {
				atomic.AddInt64(&ok, 1)
			}
		}()
	}
	wg.Wait()

	if ok != balance {
		t.Fatalf("successful requests = %d, want %d (overdraw / lost update)", ok, balance)
	}
	w, _ := repo.GetWallet(ctx, 1)
	if w.WithdrawableBalance != 0 || w.FrozenWithdrawAmount != balance {
		t.Fatalf("withdrawable=%v frozen=%v, want 0/%d", w.WithdrawableBalance, w.FrozenWithdrawAmount, balance)
	}
	if w.WithdrawableBalance+w.FrozenWithdrawAmount != balance {
		t.Fatalf("conservation broken: %v, want %d", w.WithdrawableBalance+w.FrozenWithdrawAmount, balance)
	}
}
