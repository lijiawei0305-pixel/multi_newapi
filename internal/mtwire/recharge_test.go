package mtwire

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
)

func TestRechargeQuota(t *testing.T) {
	per := int(common.QuotaPerUnit)
	cases := []struct {
		usd  float64
		want int
	}{
		{1, per},       // $1 = QuotaPerUnit
		{10, 10 * per}, // $10
		{0, 0},         // 零额
		{-5, 0},        // 负额防御
		{2.5, 5 * per / 2},
	}
	for _, c := range cases {
		if got := rechargeQuota(c.usd); got != c.want {
			t.Errorf("rechargeQuota(%g) = %d, want %d", c.usd, got, c.want)
		}
	}
}

func TestActualPaidCNY(t *testing.T) {
	if got := actualPaidCNY(10, 7.3); got != 73 {
		t.Fatalf("actualPaidCNY(10, 7.3) = %g, want 73", got)
	}
	if got := actualPaidCNY(1, 7.3); got != 7.3 {
		t.Fatalf("actualPaidCNY(1, 7.3) = %g, want 7.3", got)
	}
}
