package mtwire

import (
	"context"
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/agent"
	"github.com/QuantumNous/new-api/internal/tokenplan"
)

// TestConsumeCommissionCNY 校验消耗分润币种换算口径：收益¥ = (quotaUnits/QuotaPerUnit) × ratio × usdRate。
func TestConsumeCommissionCNY(t *testing.T) {
	const rate = 7.2
	// 消耗 = 2×QuotaPerUnit（=$2），ratio=0.2，rate=7.2 → 2 × 0.2 × 7.2 = 2.88¥。
	got := consumeCommissionCNY(int64(2*common.QuotaPerUnit), 0.2, rate)
	if math.Abs(got-2.88) > 1e-9 {
		t.Fatalf("commission = %v, want 2.88", got)
	}
	// 非正参数一律 0（旁路安全：不计佣、不入账）。
	for _, c := range []struct {
		q     int64
		ratio float64
		rate  float64
	}{
		{0, 0.2, rate},
		{int64(common.QuotaPerUnit), 0, rate},
		{int64(common.QuotaPerUnit), 0.2, 0},
		{-int64(common.QuotaPerUnit), 0.2, rate},
	} {
		if v := consumeCommissionCNY(c.q, c.ratio, c.rate); v != 0 {
			t.Fatalf("consumeCommissionCNY(%d,%v,%v) = %v, want 0", c.q, c.ratio, c.rate, v)
		}
	}
}

// TestTokenplanEarningAdapter_Maps 校验 tokenplan 差价收益经适配器原样转写为 agent 收益入账。
func TestTokenplanEarningAdapter_Maps(t *testing.T) {
	repo := agent.NewMemRepo()
	ad := newTokenplanEarningAdapter(agent.NewEarningSink(repo))

	if err := ad.AddEarning(context.Background(), tokenplan.EarningEntry{
		TenantID:   3,
		UserID:     8,
		SourceType: tokenplan.EarningTokenplanSpread,
		SourceID:   "SUBXYZ",
		Amount:     79,
		Reference:  "SUBXYZ",
	}); err != nil {
		t.Fatalf("adapter AddEarning: %v", err)
	}
	w, _ := repo.GetWallet(context.Background(), 3)
	if w.WithdrawableBalance != 79 || w.TotalEarned != 79 {
		t.Fatalf("wallet = (%v,%v), want 79/79", w.WithdrawableBalance, w.TotalEarned)
	}
	// 幂等：同 source_order_id 再次入账不重复加钱。
	if err := ad.AddEarning(context.Background(), tokenplan.EarningEntry{
		TenantID: 3, UserID: 8, SourceType: tokenplan.EarningTokenplanSpread, SourceID: "SUBXYZ", Amount: 79,
	}); err != nil {
		t.Fatalf("adapter dup AddEarning: %v", err)
	}
	w, _ = repo.GetWallet(context.Background(), 3)
	if w.WithdrawableBalance != 79 {
		t.Fatalf("idempotency broken: withdrawable = %v, want 79", w.WithdrawableBalance)
	}
}
