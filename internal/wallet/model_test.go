package wallet

import (
	"math"
	"testing"
	"time"

	"newapi-mt/internal/platform/apperr"
)

func TestRechargeSpread(t *testing.T) {
	cases := []struct {
		name  string
		paid  float64
		ratio float64
		want  float64
	}{
		// 用二进制可精确表示的小数避免浮点误差。
		{"premium ratio yields positive spread", 120, 1.5, 40}, // 120 - 120/1.5(=80) = 40
		{"normal ratio zero spread", 100, 1.0, 0},
		{"discount ratio negative spread", 90, 0.9, -10}, // 90 - 100 = -10
		{"double ratio", 100, 2.0, 50},                   // 100 - 50
		{"zero ratio treated as no spread", 100, 0, 0},
		{"negative ratio treated as no spread", 100, -1, 0},
		{"zero paid zero spread", 0, 1.5, 0},
		{"NaN paid no spread", math.NaN(), 1.5, 0},
		{"Inf ratio no spread", 100, math.Inf(1), 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := rechargeSpread(c.paid, c.ratio)
			if got != c.want {
				t.Fatalf("rechargeSpread(%v,%v) = %v, want %v", c.paid, c.ratio, got, c.want)
			}
		})
	}
}

func TestRedemptionStatusValid(t *testing.T) {
	for _, s := range []RedemptionStatus{RedemptionEnabled, RedemptionUsed, RedemptionDisabled} {
		if !s.Valid() {
			t.Fatalf("%q should be valid", s)
		}
	}
	if RedemptionStatus("bogus").Valid() {
		t.Fatal("bogus status should be invalid")
	}
}

func TestRedeemableError(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	cases := []struct {
		name     string
		code     RedemptionCode
		wantCode string // "" 表示可兑换
	}{
		{"enabled no expiry redeemable", RedemptionCode{Status: RedemptionEnabled}, ""},
		{"enabled future expiry redeemable", RedemptionCode{Status: RedemptionEnabled, ExpireAt: future}, ""},
		{"enabled but expired invalid", RedemptionCode{Status: RedemptionEnabled, ExpireAt: past}, CodeRedeemCodeInvalid},
		{"enabled expiring exactly now invalid", RedemptionCode{Status: RedemptionEnabled, ExpireAt: now}, CodeRedeemCodeInvalid},
		{"used", RedemptionCode{Status: RedemptionUsed}, CodeRedeemCodeUsed},
		{"disabled invalid", RedemptionCode{Status: RedemptionDisabled}, CodeRedeemCodeInvalid},
		{"unknown status invalid", RedemptionCode{Status: "bogus"}, CodeRedeemCodeInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.code.redeemableError(now)
			if c.wantCode == "" {
				if err != nil {
					t.Fatalf("expected redeemable, got %v", err)
				}
				return
			}
			if got := apperr.CodeOf(err); got != c.wantCode {
				t.Fatalf("error code = %q, want %q", got, c.wantCode)
			}
		})
	}
}
