package agent

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

func TestAgentParams_Validate(t *testing.T) {
	cases := []struct {
		name     string
		p        AgentParams
		wantCode string // "" 表示放行
	}{
		{"all zero ok", AgentParams{}, ""},
		{"typical ok", AgentParams{CostPrice: 10, PackageDiscount: 0.9, CommissionRatio: 0.2, Level: 3}, ""},
		{"boundary ratios ok", AgentParams{CommissionRatio: 1, PackageDiscount: 1}, ""},
		{"bottom price ratio ok", AgentParams{BottomPriceRatio: 0.7}, ""},
		{"negative cost", AgentParams{CostPrice: -0.01}, CodeAgentTypeInvalid},
		{"commission below 0", AgentParams{CommissionRatio: -0.1}, CodeAgentTypeInvalid},
		{"commission above 1", AgentParams{CommissionRatio: 1.01}, CodeAgentTypeInvalid},
		{"discount below 0", AgentParams{PackageDiscount: -0.1}, CodeAgentTypeInvalid},
		{"discount above 1", AgentParams{PackageDiscount: 1.5}, CodeAgentTypeInvalid},
		{"negative level", AgentParams{Level: -1}, CodeAgentTypeInvalid},
		{"negative bottom price ratio", AgentParams{BottomPriceRatio: -0.01}, CodeAgentTypeInvalid},
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
		{SourceRatioMarkup, true},
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
		{WithdrawPaid, true},
		{WithdrawRejected, true},
		{WithdrawStatus(""), false},
		{WithdrawStatus("unknown"), false},
	}
	for _, c := range cases {
		if got := c.s.Valid(); got != c.want {
			t.Errorf("%q.Valid() = %v, want %v", c.s, got, c.want)
		}
	}
}

// TestWithdrawStatus_CanTransitionTo 锁定提现闭环补强 #2 的状态机：
//
//	pending -> approved | rejected
//	approved -> paid   （approved 不再是终态）
//	paid / rejected 为终态（不可迁出）。
func TestWithdrawStatus_CanTransitionTo(t *testing.T) {
	cases := []struct {
		from WithdrawStatus
		to   WithdrawStatus
		want bool
	}{
		// 合法迁移
		{WithdrawPending, WithdrawApproved, true},
		{WithdrawPending, WithdrawRejected, true},
		{WithdrawApproved, WithdrawPaid, true},
		// same->same 非法
		{WithdrawPending, WithdrawPending, false},
		{WithdrawApproved, WithdrawApproved, false},
		{WithdrawRejected, WithdrawRejected, false},
		{WithdrawPaid, WithdrawPaid, false},
		// approved 不再是终态，但只能迁去 paid，不能迁去 pending/rejected
		{WithdrawApproved, WithdrawRejected, false},
		{WithdrawApproved, WithdrawPending, false},
		// pending 不能跳级直接到 paid（须先 approved）
		{WithdrawPending, WithdrawPaid, false},
		// rejected / paid 终态不可迁出
		{WithdrawRejected, WithdrawApproved, false},
		{WithdrawRejected, WithdrawPending, false},
		{WithdrawRejected, WithdrawPaid, false},
		{WithdrawPaid, WithdrawApproved, false},
		{WithdrawPaid, WithdrawPending, false},
		{WithdrawPaid, WithdrawRejected, false},
		// 未知目标
		{WithdrawPending, WithdrawStatus("unknown"), false},
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
		{ErrAgentParamsInvalid, "AGENT_TYPE_INVALID"},
		{ErrWithdrawInsufficient, "WITHDRAW_INSUFFICIENT"},
		{ErrWithdrawNotPending, "WITHDRAW_NOT_PENDING"},
		{ErrWithdrawNotFound, "WITHDRAW_NOT_FOUND"},
		{ErrEarningInvalid, "EARNING_INVALID"},
		{ErrWithdrawNotApproved, "WITHDRAW_NOT_APPROVED"},
		{ErrPayoutAccountRequired, "PAYOUT_ACCOUNT_REQUIRED"},
		{ErrPayoutAccountInvalid, "PAYOUT_ACCOUNT_INVALID"},
		{ErrPayoutRefRequired, "PAYOUT_REF_REQUIRED"},
	}
	for _, c := range cases {
		if got := apperr.CodeOf(c.err); got != c.code {
			t.Errorf("CodeOf = %q, want %q", got, c.code)
		}
	}
}

// ---- 收款账户（提现闭环补强 #1）----

func TestPayoutMethod_Valid(t *testing.T) {
	cases := []struct {
		m    PayoutMethod
		want bool
	}{
		{PayoutAlipay, true},
		{PayoutBank, true},
		{PayoutMethod(""), false},
		{PayoutMethod("wechat"), false},
	}
	for _, c := range cases {
		if got := c.m.Valid(); got != c.want {
			t.Errorf("%q.Valid() = %v, want %v", c.m, got, c.want)
		}
	}
}

func TestPayoutAccount_Validate(t *testing.T) {
	cases := []struct {
		name     string
		p        PayoutAccount
		wantCode string // "" 表示放行
	}{
		{"valid alipay", PayoutAccount{Method: PayoutAlipay, Account: "alice@example.com", Name: "Alice"}, ""},
		{"valid bank", PayoutAccount{Method: PayoutBank, Account: "6222000000", Name: "Alice", Bank: "ICBC"}, ""},
		{"unknown method", PayoutAccount{Method: "wechat", Account: "a", Name: "b"}, CodePayoutAccountInvalid},
		{"empty method", PayoutAccount{Account: "a", Name: "b"}, CodePayoutAccountInvalid},
		{"empty account", PayoutAccount{Method: PayoutAlipay, Name: "Alice"}, CodePayoutAccountInvalid},
		{"empty name", PayoutAccount{Method: PayoutAlipay, Account: "a"}, CodePayoutAccountInvalid},
		{"bank missing bank name", PayoutAccount{Method: PayoutBank, Account: "6222000000", Name: "Alice"}, CodePayoutAccountInvalid},
		{"alipay does not require bank", PayoutAccount{Method: PayoutAlipay, Account: "a", Name: "b", Bank: ""}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertCode(t, c.p.Validate(), c.wantCode)
		})
	}
}

func TestPayoutAccount_IsZero(t *testing.T) {
	if !(PayoutAccount{}).IsZero() {
		t.Fatal("zero-value PayoutAccount must report IsZero() true")
	}
	if (PayoutAccount{Method: PayoutAlipay, Account: "a", Name: "b"}).IsZero() {
		t.Fatal("configured PayoutAccount must report IsZero() false")
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
