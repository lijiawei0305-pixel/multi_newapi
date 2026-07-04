package agent

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

// seedWithdrawable 通过收益入账为 tenant 注入可提现余额 + 一个默认收款账户（申请提现的前置条件，
// 提现闭环补强 #1），返回组装好的 repo。
func seedWithdrawable(t *testing.T, tenantID int64, amount float64) *MemRepo {
	t.Helper()
	repo := NewMemRepo()
	if _, err := repo.AppendEarning(context.Background(), EarningEntry{
		TenantID: tenantID, SourceType: SourceRechargeSpread, SourceID: "seed", Amount: amount,
	}); err != nil {
		t.Fatalf("seed earning failed: %v", err)
	}
	if err := repo.SetPayoutAccount(context.Background(), tenantID, PayoutAccount{
		Method: PayoutAlipay, Account: "alice@example.com", Name: "Alice",
	}); err != nil {
		t.Fatalf("seed payout account failed: %v", err)
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

// TestReview_ApproveMovesNoMoney 验证提现闭环补强 #2 的钱流调整：通过(approve)只翻状态记决策，
// 绝不动钱——钱仍留在 frozen，等 mark-paid 才真正出账。
func TestReview_ApproveMovesNoMoney(t *testing.T) {
	ctx := context.Background()
	repo := seedWithdrawable(t, 1, 100)
	svc := NewWithdrawalService(repo)

	wd, _ := svc.Request(ctx, WithdrawInput{TenantID: 1, Amount: 40})
	if err := svc.Review(ctx, wd.ID, true, "looks good"); err != nil {
		t.Fatalf("approve error: %v", err)
	}

	got, _ := repo.GetWithdrawal(ctx, wd.ID)
	if got.Status != WithdrawApproved || got.Remark != "looks good" || got.ReviewedAt.IsZero() {
		t.Fatalf("withdrawal after approve = %+v", got)
	}
	// approve 不动钱：冻结仍是 40（未扣），可提现仍是 60（未退回）。
	w, _ := repo.GetWallet(ctx, 1)
	if w.FrozenWithdrawAmount != 40 || w.WithdrawableBalance != 60 {
		t.Fatalf("withdrawable=%v frozen=%v, want 60/40 (approve must not move money)", w.WithdrawableBalance, w.FrozenWithdrawAmount)
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
	// 再次审核已 approved 的单（approve/reject 均非法——approved 只能迁去 paid）→ WITHDRAW_NOT_PENDING。
	assertCode(t, svc.Review(ctx, wd.ID, false, "again"), CodeWithdrawNotPending)
	assertCode(t, svc.Review(ctx, wd.ID, true, "again"), CodeWithdrawNotPending)

	// 资金不因二次审核而改变；approve 本身也不动钱（frozen 仍是 40，见 TestReview_ApproveMovesNoMoney）。
	w, _ := repo.GetWallet(ctx, 1)
	if w.WithdrawableBalance != 60 || w.FrozenWithdrawAmount != 40 {
		t.Fatalf("withdrawable=%v frozen=%v, want 60/40", w.WithdrawableBalance, w.FrozenWithdrawAmount)
	}
}

func TestReview_NotFound(t *testing.T) {
	ctx := context.Background()
	svc := NewWithdrawalService(NewMemRepo())
	assertCode(t, svc.Review(ctx, 404, true, ""), CodeWithdrawNotFound)
}

// ---- 收款账户前置校验 + 快照（提现闭环补强 #1）----

// TestRequest_RejectedWithoutPayoutAccount 验证未设收款账户时申请提现被拒（不冻结、不建单）。
func TestRequest_RejectedWithoutPayoutAccount(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	// 只注入余额，不设收款账户。
	if _, err := repo.AppendEarning(ctx, EarningEntry{
		TenantID: 1, SourceType: SourceRechargeSpread, SourceID: "seed", Amount: 100,
	}); err != nil {
		t.Fatalf("seed earning: %v", err)
	}
	svc := NewWithdrawalService(repo)

	_, err := svc.Request(ctx, WithdrawInput{TenantID: 1, Amount: 30})
	assertCode(t, err, CodePayoutAccountRequired)

	// 拦截必须发生在冻结之前：余额分毫不动。
	w, _ := repo.GetWallet(ctx, 1)
	if w.WithdrawableBalance != 100 || w.FrozenWithdrawAmount != 0 {
		t.Fatalf("withdrawable=%v frozen=%v, want 100/0 (must not freeze without payout account)",
			w.WithdrawableBalance, w.FrozenWithdrawAmount)
	}
}

// TestRequest_SnapshotsPayoutAccount 验证申请提现时把当前收款账户整份快照进提现单，
// 且日后修改收款账户不影响已提交的历史快照（记录不可变）。
func TestRequest_SnapshotsPayoutAccount(t *testing.T) {
	ctx := context.Background()
	repo := seedWithdrawable(t, 1, 100) // alipay / alice@example.com / Alice
	svc := NewWithdrawalService(repo)

	wd, err := svc.Request(ctx, WithdrawInput{TenantID: 1, Amount: 30})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wd.PayoutMethod != PayoutAlipay || wd.PayoutAccount != "alice@example.com" || wd.PayoutName != "Alice" {
		t.Fatalf("withdrawal payout snapshot = %+v, want alipay/alice@example.com/Alice", wd)
	}

	// 代理之后改收款账户……
	if err := repo.SetPayoutAccount(ctx, 1, PayoutAccount{
		Method: PayoutBank, Account: "6222000000", Name: "Alice", Bank: "ICBC",
	}); err != nil {
		t.Fatalf("update payout account: %v", err)
	}
	// ……已提交的历史快照不受影响。
	got, err := repo.GetWithdrawal(ctx, wd.ID)
	if err != nil {
		t.Fatalf("get withdrawal: %v", err)
	}
	if got.PayoutMethod != PayoutAlipay || got.PayoutAccount != "alice@example.com" || got.PayoutBank != "" {
		t.Fatalf("withdrawal snapshot mutated after profile update: %+v", got)
	}
}

// ---- mark-paid（提现闭环补强 #2：approve 不动钱，mark-paid 才真正出账）----

// TestMarkPaid_DebitsFrozenAndRecordsRef 验证标记已打款：扣减冻结（资金真正出账）+ 记录打款单号/时间。
func TestMarkPaid_DebitsFrozenAndRecordsRef(t *testing.T) {
	ctx := context.Background()
	repo := seedWithdrawable(t, 1, 100)
	svc := NewWithdrawalService(repo)

	wd, _ := svc.Request(ctx, WithdrawInput{TenantID: 1, Amount: 40})
	if err := svc.Review(ctx, wd.ID, true, ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := svc.MarkPaid(ctx, wd.ID, "WX20260704001"); err != nil {
		t.Fatalf("mark paid: %v", err)
	}

	got, _ := repo.GetWithdrawal(ctx, wd.ID)
	if got.Status != WithdrawPaid || got.PayoutRef != "WX20260704001" || got.PaidAt.IsZero() {
		t.Fatalf("withdrawal after mark-paid = %+v", got)
	}
	// mark-paid 才真正出账：冻结清零；可提现维持申请后的 60（资金早在申请时已冻结，approve 未动它）。
	w, _ := repo.GetWallet(ctx, 1)
	if w.FrozenWithdrawAmount != 0 || w.WithdrawableBalance != 60 {
		t.Fatalf("withdrawable=%v frozen=%v, want 60/0", w.WithdrawableBalance, w.FrozenWithdrawAmount)
	}
}

// TestMarkPaid_OnlyFromApproved 验证 CAS：仅 approved→paid；pending 直接标记 / 重复标记均被拒，
// 且被拒的调用绝不重复扣款。
func TestMarkPaid_OnlyFromApproved(t *testing.T) {
	ctx := context.Background()
	repo := seedWithdrawable(t, 1, 100)
	svc := NewWithdrawalService(repo)

	wd, _ := svc.Request(ctx, WithdrawInput{TenantID: 1, Amount: 40})
	// 仍是 pending，未 approve：mark-paid 必须拒绝。
	assertCode(t, svc.MarkPaid(ctx, wd.ID, "ref-1"), CodeWithdrawNotApproved)

	if err := svc.Review(ctx, wd.ID, true, ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := svc.MarkPaid(ctx, wd.ID, "ref-1"); err != nil {
		t.Fatalf("first mark-paid: %v", err)
	}
	// 已是 paid（终态）：重复标记必须拒绝，不可重复打款。
	assertCode(t, svc.MarkPaid(ctx, wd.ID, "ref-2"), CodeWithdrawNotApproved)

	w, _ := repo.GetWallet(ctx, 1)
	if w.FrozenWithdrawAmount != 0 || w.WithdrawableBalance != 60 {
		t.Fatalf("double mark-paid moved money again: withdrawable=%v frozen=%v",
			w.WithdrawableBalance, w.FrozenWithdrawAmount)
	}
	got, _ := repo.GetWithdrawal(ctx, wd.ID)
	if got.PayoutRef != "ref-1" {
		t.Fatalf("payout_ref = %q, want unchanged %q after rejected second mark-paid", got.PayoutRef, "ref-1")
	}
}

// TestMarkPaid_RequiresPayoutRef 验证打款单号/凭证必填（空白字符串同样拒绝）。
func TestMarkPaid_RequiresPayoutRef(t *testing.T) {
	ctx := context.Background()
	repo := seedWithdrawable(t, 1, 100)
	svc := NewWithdrawalService(repo)
	wd, _ := svc.Request(ctx, WithdrawInput{TenantID: 1, Amount: 40})
	if err := svc.Review(ctx, wd.ID, true, ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	assertCode(t, svc.MarkPaid(ctx, wd.ID, ""), CodePayoutRefRequired)
	assertCode(t, svc.MarkPaid(ctx, wd.ID, "   "), CodePayoutRefRequired)
}

// TestMarkPaid_NotFound 验证提现单不存在时的错误码。
func TestMarkPaid_NotFound(t *testing.T) {
	ctx := context.Background()
	svc := NewWithdrawalService(NewMemRepo())
	assertCode(t, svc.MarkPaid(ctx, 404, "ref"), CodeWithdrawNotFound)
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
