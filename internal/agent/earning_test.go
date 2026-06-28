package agent

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
)

func TestAddEarning_AccruesBalanceAndTotal(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	sink := NewEarningSink(repo)

	must := func(e EarningEntry) {
		if err := sink.AddEarning(ctx, e); err != nil {
			t.Fatalf("AddEarning(%s) error: %v", e.SourceID, err)
		}
	}
	must(EarningEntry{TenantID: 1, UserID: 5, SourceType: SourceRechargeSpread, SourceID: "r1", Amount: 30})
	must(EarningEntry{TenantID: 1, UserID: 5, SourceType: SourceConsumeCommission, SourceID: "c1", Amount: 20})

	w, _ := repo.GetWallet(ctx, 1)
	if w.WithdrawableBalance != 50 || w.TotalEarned != 50 {
		t.Fatalf("withdrawable=%v total=%v, want 50/50", w.WithdrawableBalance, w.TotalEarned)
	}
	if w.UserID != 5 {
		t.Fatalf("wallet UserID = %d, want 5", w.UserID)
	}
}

func TestAddEarning_IdempotentSameSource(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	sink := NewEarningSink(repo)

	e := EarningEntry{TenantID: 1, UserID: 5, SourceType: SourceTokenplanSpread, SourceID: "ord-99", Amount: 40}
	for i := 0; i < 5; i++ {
		if err := sink.AddEarning(ctx, e); err != nil {
			t.Fatalf("AddEarning attempt %d error: %v", i, err)
		}
	}
	w, _ := repo.GetWallet(ctx, 1)
	if w.WithdrawableBalance != 40 || w.TotalEarned != 40 {
		t.Fatalf("duplicate source_id double-counted: withdrawable=%v total=%v, want 40/40",
			w.WithdrawableBalance, w.TotalEarned)
	}

	// 不同 source_id 正常再入账。
	if err := sink.AddEarning(ctx, EarningEntry{TenantID: 1, SourceType: SourceTokenplanSpread, SourceID: "ord-100", Amount: 10}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	w, _ = repo.GetWallet(ctx, 1)
	if w.WithdrawableBalance != 50 {
		t.Fatalf("withdrawable = %v, want 50", w.WithdrawableBalance)
	}
}

func TestAppendEarning_AppliedFlag(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	e := EarningEntry{TenantID: 1, SourceType: SourceRechargeSpread, SourceID: "x", Amount: 1}

	applied, err := repo.AppendEarning(ctx, e)
	if err != nil || !applied {
		t.Fatalf("first AppendEarning: applied=%v err=%v, want true/nil", applied, err)
	}
	applied, err = repo.AppendEarning(ctx, e)
	if err != nil || applied {
		t.Fatalf("duplicate AppendEarning: applied=%v err=%v, want false/nil", applied, err)
	}
}

func TestAddEarning_ManualAdjustmentNegative(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	sink := NewEarningSink(repo)

	_ = sink.AddEarning(ctx, EarningEntry{TenantID: 1, SourceType: SourceRechargeSpread, SourceID: "r1", Amount: 100})
	if err := sink.AddEarning(ctx, EarningEntry{TenantID: 1, SourceType: SourceManualAdjustment, SourceID: "adj-1", Amount: -30}); err != nil {
		t.Fatalf("manual adjustment error: %v", err)
	}
	w, _ := repo.GetWallet(ctx, 1)
	if w.WithdrawableBalance != 70 || w.TotalEarned != 70 {
		t.Fatalf("withdrawable=%v total=%v, want 70/70", w.WithdrawableBalance, w.TotalEarned)
	}
}

func TestAddEarning_InvalidRejected(t *testing.T) {
	ctx := context.Background()
	sink := NewEarningSink(NewMemRepo())
	cases := []EarningEntry{
		{TenantID: 1, SourceType: "weird", SourceID: "1", Amount: 1},
		{TenantID: 1, SourceType: SourceRechargeSpread, SourceID: "", Amount: 1},
		{TenantID: 0, SourceType: SourceRechargeSpread, SourceID: "1", Amount: 1},
	}
	for _, e := range cases {
		assertCode(t, sink.AddEarning(ctx, e), CodeEarningInvalid)
	}
}

func TestAddEarning_TenantIsolation(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	sink := NewEarningSink(repo)

	_ = sink.AddEarning(ctx, EarningEntry{TenantID: 1, SourceType: SourceRechargeSpread, SourceID: "r1", Amount: 50})
	// 跨租户重用同一 source_id 不应被对方的幂等键吞掉，且只落到各自钱包。
	_ = sink.AddEarning(ctx, EarningEntry{TenantID: 2, SourceType: SourceRechargeSpread, SourceID: "r1", Amount: 70})

	w1, _ := repo.GetWallet(ctx, 1)
	w2, _ := repo.GetWallet(ctx, 2)
	if w1.WithdrawableBalance != 50 {
		t.Fatalf("tenant1 withdrawable = %v, want 50", w1.WithdrawableBalance)
	}
	if w2.WithdrawableBalance != 70 {
		t.Fatalf("tenant2 withdrawable = %v, want 70", w2.WithdrawableBalance)
	}
}

// TestAddEarning_ConcurrentSameSource 验证 -race 下同 source_id 并发只入账一次。
func TestAddEarning_ConcurrentSameSource(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	sink := NewEarningSink(repo)
	e := EarningEntry{TenantID: 1, SourceType: SourceConsumeCommission, SourceID: "race-1", Amount: 25}

	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = sink.AddEarning(ctx, e)
		}()
	}
	wg.Wait()

	w, _ := repo.GetWallet(ctx, 1)
	if w.WithdrawableBalance != 25 || w.TotalEarned != 25 {
		t.Fatalf("concurrent same source: withdrawable=%v total=%v, want 25/25",
			w.WithdrawableBalance, w.TotalEarned)
	}
}

// TestAddEarning_ConcurrentDistinctSources 验证 -race 下不同 source_id 并发累加正确。
func TestAddEarning_ConcurrentDistinctSources(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	sink := NewEarningSink(repo)

	const n = 100
	var wg sync.WaitGroup
	var ok int64
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			e := EarningEntry{
				TenantID:   1,
				SourceType: SourceConsumeCommission,
				SourceID:   "s-" + strconv.Itoa(i),
				Amount:     1,
			}
			if err := sink.AddEarning(ctx, e); err == nil {
				atomic.AddInt64(&ok, 1)
			}
		}(i)
	}
	wg.Wait()

	if ok != n {
		t.Fatalf("successful AddEarning = %d, want %d", ok, n)
	}
	w, _ := repo.GetWallet(ctx, 1)
	if w.WithdrawableBalance != n {
		t.Fatalf("withdrawable = %v, want %d", w.WithdrawableBalance, n)
	}
}
