package agent

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

func TestAgentType_Valid(t *testing.T) {
	cases := []struct {
		t    AgentType
		want bool
	}{
		{AgentTypeNormal, true},
		{AgentTypeOEM, true},
		{AgentTypeAPI, true},
		{AgentType(""), false},
		{AgentType("reseller"), false},
		{AgentType("NORMAL"), false}, // 大小写敏感
	}
	for _, c := range cases {
		if got := c.t.Valid(); got != c.want {
			t.Errorf("%q.Valid() = %v, want %v", c.t, got, c.want)
		}
	}
}

func TestAgentParams_Validate(t *testing.T) {
	cases := []struct {
		name     string
		p        AgentParams
		wantCode string // "" 表示放行
	}{
		{"all zero ok", AgentParams{}, ""},
		{"typical ok", AgentParams{CostPrice: 10, PackageDiscount: 0.9, CommissionRatio: 0.2, Level: 3}, ""},
		{"boundary ratios ok", AgentParams{CommissionRatio: 1, PackageDiscount: 1}, ""},
		{"negative cost", AgentParams{CostPrice: -0.01}, CodeAgentTypeInvalid},
		{"commission below 0", AgentParams{CommissionRatio: -0.1}, CodeAgentTypeInvalid},
		{"commission above 1", AgentParams{CommissionRatio: 1.01}, CodeAgentTypeInvalid},
		{"discount below 0", AgentParams{PackageDiscount: -0.1}, CodeAgentTypeInvalid},
		{"discount above 1", AgentParams{PackageDiscount: 1.5}, CodeAgentTypeInvalid},
		{"negative level", AgentParams{Level: -1}, CodeAgentTypeInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertCode(t, c.p.Validate(), c.wantCode)
		})
	}
}

func TestEarningSource_Valid(t *testing.T) {
	cases := []struct {
		s    EarningSource
		want bool
	}{
		{SourceRechargeSpread, true},
		{SourceConsumeCommission, true},
		{SourceTokenplanSpread, true},
		{SourceTokenplanCommission, true},
		{SourceManualAdjustment, true},
		{EarningSource(""), false},
		{EarningSource("bonus"), false},
	}
	for _, c := range cases {
		if got := c.s.Valid(); got != c.want {
			t.Errorf("%q.Valid() = %v, want %v", c.s, got, c.want)
		}
	}
}

func TestEarningEntry_Validate(t *testing.T) {
	cases := []struct {
		name     string
		e        EarningEntry
		wantCode string
	}{
		{"ok", EarningEntry{TenantID: 1, SourceType: SourceRechargeSpread, SourceID: "ord-1", Amount: 5}, ""},
		{"bad source type", EarningEntry{TenantID: 1, SourceType: "x", SourceID: "ord-1"}, CodeEarningInvalid},
		{"empty source id", EarningEntry{TenantID: 1, SourceType: SourceRechargeSpread, SourceID: ""}, CodeEarningInvalid},
		{"non-positive tenant", EarningEntry{TenantID: 0, SourceType: SourceRechargeSpread, SourceID: "ord-1"}, CodeEarningInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertCode(t, c.e.Validate(), c.wantCode)
		})
	}
}

func TestEarningEntry_IdempotencyKey(t *testing.T) {
	a := EarningEntry{SourceType: SourceRechargeSpread, SourceID: "1"}
	b := EarningEntry{SourceType: SourceRechargeSpread, SourceID: "1"}
	c := EarningEntry{SourceType: SourceConsumeCommission, SourceID: "1"}
	d := EarningEntry{SourceType: SourceRechargeSpread, SourceID: "2"}
	if a.IdempotencyKey() != b.IdempotencyKey() {
		t.Fatal("same (type,id) must yield equal keys")
	}
	if a.IdempotencyKey() == c.IdempotencyKey() {
		t.Fatal("different source type must yield different keys")
	}
	if a.IdempotencyKey() == d.IdempotencyKey() {
		t.Fatal("different source id must yield different keys")
	}
}

func TestWithdrawStatus_Valid(t *testing.T) {
	cases := []struct {
		s    WithdrawStatus
		want bool
	}{
		{WithdrawPending, true},
		{WithdrawApproved, true},
		{WithdrawRejected, true},
		{WithdrawStatus(""), false},
		{WithdrawStatus("paid"), false},
	}
	for _, c := range cases {
		if got := c.s.Valid(); got != c.want {
			t.Errorf("%q.Valid() = %v, want %v", c.s, got, c.want)
		}
	}
}

func TestWithdrawStatus_CanTransitionTo(t *testing.T) {
	cases := []struct {
		from WithdrawStatus
		to   WithdrawStatus
		want bool
	}{
		// 合法迁移
		{WithdrawPending, WithdrawApproved, true},
		{WithdrawPending, WithdrawRejected, true},
		// same->same 非法
		{WithdrawPending, WithdrawPending, false},
		{WithdrawApproved, WithdrawApproved, false},
		{WithdrawRejected, WithdrawRejected, false},
		// 终态不可迁出
		{WithdrawApproved, WithdrawRejected, false},
		{WithdrawApproved, WithdrawPending, false},
		{WithdrawRejected, WithdrawApproved, false},
		{WithdrawRejected, WithdrawPending, false},
		// 未知目标
		{WithdrawPending, WithdrawStatus("paid"), false},
	}
	for _, c := range cases {
		if got := c.from.CanTransitionTo(c.to); got != c.want {
			t.Errorf("%q -> %q = %v, want %v", c.from, c.to, got, c.want)
		}
	}
}

// TestErrorCodes 锁定错误码字符串契约：稳定码值不得随意变更。
func TestErrorCodes(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{
		{ErrAgentTypeInvalid, "AGENT_TYPE_INVALID"},
		{ErrWithdrawInsufficient, "WITHDRAW_INSUFFICIENT"},
		{ErrWithdrawNotPending, "WITHDRAW_NOT_PENDING"},
		{ErrWithdrawNotFound, "WITHDRAW_NOT_FOUND"},
		{ErrEarningInvalid, "EARNING_INVALID"},
	}
	for _, c := range cases {
		if got := apperr.CodeOf(c.err); got != c.code {
			t.Errorf("CodeOf = %q, want %q", got, c.code)
		}
	}
}

// assertCode 断言 err 的 apperr 错误码等于 wantCode；wantCode=="" 表示期望无错误。
func assertCode(t *testing.T, err error, wantCode string) {
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
