package pricing

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

func TestValidateGroupRatio(t *testing.T) {
	g := NewGuard()
	cases := []struct {
		name     string
		ratio    float64
		floor    float64
		wantCode string // "" 表示放行
	}{
		{"above floor", 1.2, 0.8, ""},
		{"equal floor passes", 0.8, 0.8, ""},
		{"just above floor", 0.80001, 0.8, ""},
		{"below floor blocked", 0.7, 0.8, CodeRatioBelowFloor},
		{"zero ratio below positive floor", 0, 0.5, CodeRatioBelowFloor},
		{"zero floor admits zero ratio", 0, 0, ""},
		{"negative ratio blocked", -0.1, 0, CodeRatioBelowFloor},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := g.ValidateGroupRatio(c.ratio, c.floor)
			assertErrCode(t, err, c.wantCode)
		})
	}
}

func TestValidateRetailPrice(t *testing.T) {
	g := NewGuard()
	cases := []struct {
		name      string
		retail    float64
		cost      float64
		minMargin float64
		wantCode  string // "" 表示放行
	}{
		// 使用二进制可精确表示的小数（0.25/0.5/0.125），避免浮点边界误差。
		{"above protection", 200, 100, 0.25, ""},
		{"equal protection passes", 125, 100, 0.25, ""},   // 100*1.25 == 125
		{"equal protection zero margin", 100, 100, 0, ""}, // floor == cost
		{"below protection blocked", 124, 100, 0.25, CodePriceBelowProtection},
		{"insufficient margin blocked", 105, 100, 0.25, CodePriceBelowProtection}, // 105 < 125
		{"negative profit blocked", 90, 100, 0, CodePriceBelowProtection},         // retail < cost
		{"negative profit with margin", 90, 100, 0.5, CodePriceBelowProtection},   // floor 150
		{"half margin boundary", 120, 80, 0.5, ""},                                // 80*1.5 == 120
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := g.ValidateRetailPrice(c.retail, c.cost, c.minMargin)
			assertErrCode(t, err, c.wantCode)
		})
	}
}

// assertErrCode 断言 err 的 apperr 错误码等于 wantCode；wantCode=="" 表示期望无错误。
func assertErrCode(t *testing.T, err error, wantCode string) {
	t.Helper()
	if wantCode == "" {
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("expected error code %q, got nil", wantCode)
	}
	if got := apperr.CodeOf(err); got != wantCode {
		t.Fatalf("error code = %q, want %q (err=%v)", got, wantCode, err)
	}
}
