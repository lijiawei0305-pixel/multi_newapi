package wallet

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/platform/appctx"
	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

func newSvc() (*walletService, *MemRepo, *fakePricing, *fakeEarnings) {
	repo := NewMemRepo()
	pricing := newFakePricing(1.0)
	earn := newFakeEarnings()
	return NewService(repo, pricing, earn).(*walletService), repo, pricing, earn
}

func TestCreditRechargeComputesSpread(t *testing.T) {
	ctx := context.Background()
	svc, repo, pricing, earn := newSvc()
	pricing.set(7, 3, 1.5) // 溢价组

	in := CreditInput{
		TenantID: 7, UserID: 42, GroupID: 3,
		CreditedUSD: 100, ActualPaid: 120,
		Source: SourceRecharge, Reference: "order-1",
	}
	if err := svc.Credit(ctx, in); err != nil {
		t.Fatalf("Credit: %v", err)
	}

	// 余额按 CreditedUSD 入账。
	if bal, _ := repo.Balance(ctx, 7, 42); bal != 100 {
		t.Fatalf("balance = %v, want 100", bal)
	}
	// 差价 = 实付120 − 成本(120/1.5=80) = 40。
	e, ok := earn.last()
	if !ok {
		t.Fatal("expected one earning entry")
	}
	if e.SourceType != EarningRechargeSpread || e.AmountUSD != 40 {
		t.Fatalf("earning = %+v, want recharge_spread 40", e)
	}
	// 收益绑定 tenant_id+user_id。
	if e.TenantID != 7 || e.UserID != 42 || e.Reference != "order-1" {
		t.Fatalf("earning binding = %+v, want tenant 7 user 42 ref order-1", e)
	}
}

func TestCreditNormalRatioNoSpread(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, earn := newSvc() // defaultRatio 1.0

	in := CreditInput{TenantID: 1, UserID: 2, CreditedUSD: 50, ActualPaid: 50, Source: SourceRecharge}
	if err := svc.Credit(ctx, in); err != nil {
		t.Fatalf("Credit: %v", err)
	}
	if bal, _ := repo.Balance(ctx, 1, 2); bal != 50 {
		t.Fatalf("balance = %v, want 50", bal)
	}
	if earn.count() != 0 {
		t.Fatalf("expected no earning for ratio 1.0, got %d", earn.count())
	}
}

func TestCreditManualNoSpread(t *testing.T) {
	ctx := context.Background()
	svc, repo, pricing, earn := newSvc()
	pricing.set(7, 0, 1.5) // 即便有溢价倍率，人工入账也不计差价

	in := CreditInput{TenantID: 7, UserID: 9, CreditedUSD: 30, Source: SourceManual}
	if err := svc.Credit(ctx, in); err != nil {
		t.Fatalf("Credit: %v", err)
	}
	if bal, _ := repo.Balance(ctx, 7, 9); bal != 30 {
		t.Fatalf("balance = %v, want 30", bal)
	}
	if earn.count() != 0 {
		t.Fatalf("manual credit must not earn, got %d", earn.count())
	}
	if pricing.calls != 0 {
		t.Fatalf("manual credit must not query pricing, calls=%d", pricing.calls)
	}
}

func TestCreditRequiresTenantAndUser(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _ := newSvc()
	cases := []CreditInput{
		{TenantID: 0, UserID: 1, CreditedUSD: 10, Source: SourceManual},
		{TenantID: 1, UserID: 0, CreditedUSD: 10, Source: SourceManual},
	}
	for _, in := range cases {
		if got := apperr.CodeOf(svc.Credit(ctx, in)); got != CodeRechargeOrderInvalid {
			t.Fatalf("Credit(%+v) code = %q, want %q", in, got, CodeRechargeOrderInvalid)
		}
	}
}

func TestCreditRejectsBadAmounts(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _ := newSvc()
	cases := []CreditInput{
		{TenantID: 1, UserID: 1, CreditedUSD: -1, Source: SourceManual},
		{TenantID: 1, UserID: 1, CreditedUSD: 10, ActualPaid: -5, Source: SourceRecharge},
	}
	for _, in := range cases {
		if got := apperr.CodeOf(svc.Credit(ctx, in)); got != CodeAmountInvalid {
			t.Fatalf("Credit(%+v) code = %q, want %q", in, got, CodeAmountInvalid)
		}
	}
}

func TestCreditPropagatesPricingError(t *testing.T) {
	ctx := context.Background()
	svc, _, pricing, _ := newSvc()
	pricing.err = errors.New("pricing down")

	in := CreditInput{TenantID: 1, UserID: 1, CreditedUSD: 10, ActualPaid: 10, Source: SourceRecharge}
	if err := svc.Credit(ctx, in); !errors.Is(err, pricing.err) {
		t.Fatalf("expected pricing error to propagate, got %v", err)
	}
}

func TestCreditPropagatesEarningError(t *testing.T) {
	ctx := context.Background()
	svc, _, pricing, earn := newSvc()
	pricing.set(1, 0, 2.0) // 产生正差价
	earn.err = errors.New("earning down")

	in := CreditInput{TenantID: 1, UserID: 1, CreditedUSD: 10, ActualPaid: 10, Source: SourceRecharge}
	if err := svc.Credit(ctx, in); !errors.Is(err, earn.err) {
		t.Fatalf("expected earning error to propagate, got %v", err)
	}
}

func TestRedeemSuccess(t *testing.T) {
	ctx := appctx.WithPrincipal(context.Background(), appctx.Principal{TenantID: 5, UserID: 8, Role: appctx.RoleUser})
	svc, repo, _, _ := newSvc()
	repo.AddRedemption(&RedemptionCode{TenantID: 5, Code: "GIFT10", AmountUSD: 10})

	if err := svc.Redeem(ctx, 8, "GIFT10"); err != nil {
		t.Fatalf("Redeem: %v", err)
	}
	if bal, _ := repo.Balance(ctx, 5, 8); bal != 10 {
		t.Fatalf("balance = %v, want 10", bal)
	}
	// 状态翻为 used，二次兑换被拒。
	if got := apperr.CodeOf(svc.Redeem(ctx, 8, "GIFT10")); got != CodeRedeemCodeUsed {
		t.Fatalf("second redeem code = %q, want %q", got, CodeRedeemCodeUsed)
	}
}

func TestRedeemRejectsInvalidCodes(t *testing.T) {
	now := time.Now()
	svc, repo, _, _ := newSvc()
	repo.AddRedemption(&RedemptionCode{TenantID: 5, Code: "DISABLED", AmountUSD: 10, Status: RedemptionDisabled})
	repo.AddRedemption(&RedemptionCode{TenantID: 5, Code: "EXPIRED", AmountUSD: 10, Status: RedemptionEnabled, ExpireAt: now.Add(-time.Hour)})

	ctx := appctx.WithPrincipal(context.Background(), appctx.Principal{TenantID: 5, UserID: 8})
	cases := []struct {
		code     string
		wantCode string
	}{
		{"UNKNOWN", CodeRedeemCodeInvalid},  // 不存在
		{"DISABLED", CodeRedeemCodeInvalid}, // 禁用
		{"EXPIRED", CodeRedeemCodeInvalid},  // 过期
	}
	for _, c := range cases {
		if got := apperr.CodeOf(svc.Redeem(ctx, 8, c.code)); got != c.wantCode {
			t.Fatalf("Redeem(%q) code = %q, want %q", c.code, got, c.wantCode)
		}
	}
}

func TestRedeemTenantScoped(t *testing.T) {
	svc, repo, _, _ := newSvc()
	repo.AddRedemption(&RedemptionCode{TenantID: 5, Code: "GIFT10", AmountUSD: 10})

	// 另一租户（或无租户上下文）看不到该码 → REDEEM_CODE_INVALID。
	otherCtx := appctx.WithPrincipal(context.Background(), appctx.Principal{TenantID: 6, UserID: 8})
	if got := apperr.CodeOf(svc.Redeem(otherCtx, 8, "GIFT10")); got != CodeRedeemCodeInvalid {
		t.Fatalf("cross-tenant redeem code = %q, want %q", got, CodeRedeemCodeInvalid)
	}
	if got := apperr.CodeOf(svc.Redeem(context.Background(), 8, "GIFT10")); got != CodeRedeemCodeInvalid {
		t.Fatalf("no-principal redeem code = %q, want %q", got, CodeRedeemCodeInvalid)
	}
}
