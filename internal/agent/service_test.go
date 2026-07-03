package agent

import (
	"context"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

// stubGuard 是 PricingGuard 的测试假实现：ratio < floor 时以 blockCode 拒绝（模拟击穿保护线）。
type stubGuard struct{ blockCode string }

func (g stubGuard) ValidateGroupRatio(ratio, floor float64) error {
	if ratio < floor {
		return apperr.New(g.blockCode, "ratio below floor", http.StatusBadRequest)
	}
	return nil
}

func TestSetAgentType_PersistsValid(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	svc := NewService(repo, nil)

	params := AgentParams{CostPrice: 10, PackageDiscount: 0.9, CommissionRatio: 0.2, Level: 2}
	if err := svc.SetAgentType(ctx, 7, AgentTypeOEM, params); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	gotType, gotParams, found, err := repo.GetAgentType(ctx, 7)
	if err != nil || !found {
		t.Fatalf("profile not persisted: found=%v err=%v", found, err)
	}
	if gotType != AgentTypeOEM || gotParams != params {
		t.Fatalf("persisted = (%v, %+v), want (%v, %+v)", gotType, gotParams, AgentTypeOEM, params)
	}
}

func TestSetAgentType_InvalidTypeRejected(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemRepo(), nil)
	err := svc.SetAgentType(ctx, 1, AgentType("reseller"), AgentParams{})
	assertCode(t, err, CodeAgentTypeInvalid)
}

func TestSetAgentType_InvalidParamsRejected(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemRepo(), nil)
	cases := []struct {
		name string
		p    AgentParams
	}{
		{"negative cost", AgentParams{CostPrice: -1}},
		{"commission >1", AgentParams{CommissionRatio: 2}},
		{"discount <0", AgentParams{PackageDiscount: -0.5}},
		{"negative level", AgentParams{Level: -3}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := svc.SetAgentType(ctx, 1, AgentTypeNormal, c.p)
			assertCode(t, err, CodeAgentTypeInvalid)
		})
	}
}

func TestSetAgentType_GuardBlocksBelowProtection(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	svc := NewService(repo, stubGuard{blockCode: "RATIO_BELOW_FLOOR"})

	// 折扣 0.5 < 保护下限 0.8 → 击穿保护线，原样上浮守卫错误码。
	err := svc.SetAgentType(ctx, 1, AgentTypeNormal, AgentParams{PackageDiscount: 0.5, DiscountFloor: 0.8})
	assertCode(t, err, "RATIO_BELOW_FLOOR")

	// 未落库（被守卫拦截）。
	if _, _, found, _ := repo.GetAgentType(ctx, 1); found {
		t.Fatal("profile must not be persisted when guard blocks")
	}
}

func TestSetAgentType_GuardAdmitsAtOrAboveFloor(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemRepo(), stubGuard{blockCode: "RATIO_BELOW_FLOOR"})
	// 折扣 0.9 ≥ 保护下限 0.8 → 放行。
	if err := svc.SetAgentType(ctx, 1, AgentTypeAPI, AgentParams{PackageDiscount: 0.9, DiscountFloor: 0.8}); err != nil {
		t.Fatalf("expected admit, got %v", err)
	}
}

func TestGetWallet_UnknownTenantIsZero(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemRepo(), nil)
	w, err := svc.GetWallet(ctx, 99)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.TenantID != 99 || w.WithdrawableBalance != 0 || w.TotalEarned != 0 || w.FrozenWithdrawAmount != 0 {
		t.Fatalf("expected zero wallet for tenant 99, got %+v", w)
	}
}

func TestAgentLevel_ReturnsProfileLevel(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	svc := NewService(repo, nil)
	if err := svc.SetAgentType(ctx, 7, AgentTypeNormal, AgentParams{Level: 1, CanAPI: true}); err != nil {
		t.Fatalf("set: %v", err)
	}
	lvl, err := svc.AgentLevel(ctx, 7)
	if err != nil || lvl != 1 {
		t.Fatalf("AgentLevel(7) = (%d,%v), want (1,nil)", lvl, err)
	}
	// 未设代理的租户 → level 0（非错误）。
	if lvl, err := svc.AgentLevel(ctx, 99); err != nil || lvl != 0 {
		t.Fatalf("AgentLevel(99) = (%d,%v), want (0,nil)", lvl, err)
	}
}
