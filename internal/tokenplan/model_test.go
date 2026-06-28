package tokenplan

import (
	"math"
	"testing"

	"newapi-mt/internal/platform/apperr"
)

func TestPlanStatusValid(t *testing.T) {
	if !PlanEnabled.Valid() || !PlanDisabled.Valid() {
		t.Fatal("enabled/disabled must be valid")
	}
	if PlanStatus("weird").Valid() {
		t.Fatal("unknown status must be invalid")
	}
	if !PlanEnabled.IsEnabled() || PlanDisabled.IsEnabled() {
		t.Fatal("IsEnabled mismatch")
	}
}

func TestSubStatusStateMachine(t *testing.T) {
	// active 可迁往三个终态。
	for _, to := range []SubStatus{SubExhausted, SubExpired, SubRefunded} {
		if !SubActive.CanTransitionTo(to) {
			t.Fatalf("active -> %s must be allowed", to)
		}
	}
	// 终态不可迁出（含 same->same 与回 active）。
	for _, from := range []SubStatus{SubExhausted, SubExpired, SubRefunded} {
		for _, to := range []SubStatus{SubActive, SubExhausted, SubExpired, SubRefunded} {
			if from.CanTransitionTo(to) {
				t.Fatalf("terminal %s -> %s must be rejected", from, to)
			}
		}
	}
	if SubActive.CanTransitionTo(SubActive) {
		t.Fatal("active -> active must be rejected (no self-loop)")
	}
	if !SubActive.Valid() || SubStatus("x").Valid() {
		t.Fatal("Valid mismatch")
	}
}

func TestPlanInputValidate(t *testing.T) {
	ok := basePlanInput()
	if err := ok.Validate(); err != nil {
		t.Fatalf("baseline must be valid: %v", err)
	}

	bad := map[string]func(*PlanInput){
		"empty code":      func(p *PlanInput) { p.Code = "" },
		"empty name":      func(p *PlanInput) { p.Name = "" },
		"neg base":        func(p *PlanInput) { p.BasePrice = -1 },
		"nan anchor":      func(p *PlanInput) { p.AnchorPrice = math.NaN() },
		"zero monthlimit": func(p *PlanInput) { p.MonthLimitUSD = 0 },
		"zero multiplier": func(p *PlanInput) { p.Multiplier = 0 },
		"zero validdays":  func(p *PlanInput) { p.ValidDays = 0 },
		"neg agentcost":   func(p *PlanInput) { p.AgentCostPrice = -5 },
		"min below cost":  func(p *PlanInput) { p.MinPrice = p.AgentCostPrice - 1 },
		"bad status":      func(p *PlanInput) { p.Status = PlanStatus("nope") },
	}
	for name, mut := range bad {
		in := basePlanInput()
		mut(&in)
		if apperr.CodeOf(in.Validate()) != CodePlanInputInvalid {
			t.Fatalf("%s: want PLAN_INPUT_INVALID, got %v", name, in.Validate())
		}
	}
}

func TestTokenplanSpread(t *testing.T) {
	cases := []struct {
		retail, cost, want float64
	}{
		{279, 200, 79},
		{200, 200, 0}, // 零差价
		{150, 200, 0}, // 负差价归零
		{math.NaN(), 1, 0},
		{1, math.Inf(1), 0},
	}
	for _, c := range cases {
		if got := tokenplanSpread(c.retail, c.cost); got != c.want {
			t.Fatalf("tokenplanSpread(%v,%v)=%v want %v", c.retail, c.cost, got, c.want)
		}
	}
}

func TestSubscriptionRemainingUSD(t *testing.T) {
	s := &Subscription{MonthLimitUSD: 100, UsedUSD: 40}
	if s.RemainingUSD() != 60 {
		t.Fatalf("remaining=%v want 60", s.RemainingUSD())
	}
	s.UsedUSD = 130 // 超额钳到 0
	if s.RemainingUSD() != 0 {
		t.Fatalf("over-used remaining=%v want 0", s.RemainingUSD())
	}
}

func TestRemaining(t *testing.T) {
	if remaining(100, 30) != 70 {
		t.Fatal("100-30 should be 70")
	}
	if remaining(100, 100) != 0 {
		t.Fatal("full should be 0")
	}
	if remaining(100, 130) != 0 {
		t.Fatal("over should clamp to 0")
	}
}

func TestValidAmount(t *testing.T) {
	if validAmount(math.NaN()) || validAmount(math.Inf(1)) || validAmount(math.Inf(-1)) {
		t.Fatal("NaN/Inf must be invalid")
	}
	if !validAmount(0) || !validAmount(-3.2) || !validAmount(99.9) {
		t.Fatal("finite numbers must be valid")
	}
}

func TestSeedPlans(t *testing.T) {
	plans := SeedPlans()
	if len(plans) != 6 {
		t.Fatalf("want 6 seed plans, got %d", len(plans))
	}
	wantCodes := []string{"trial", "mini", "solo", "lite", "pro", "max"}
	wantLimits := []float64{80, 220, 560, 2200, 7200, 25000}
	wantBase := []float64{6.9, 119, 279, 899, 2699, 8999}
	for i, p := range plans {
		if p.Code != wantCodes[i] {
			t.Fatalf("plan[%d].Code=%q want %q", i, p.Code, wantCodes[i])
		}
		if p.MonthLimitUSD != wantLimits[i] {
			t.Fatalf("plan %s month_limit=%v want %v", p.Code, p.MonthLimitUSD, wantLimits[i])
		}
		if p.BasePrice != wantBase[i] {
			t.Fatalf("plan %s base=%v want %v", p.Code, p.BasePrice, wantBase[i])
		}
		if p.Multiplier != 1.0 {
			t.Fatalf("plan %s multiplier=%v want 1.0 (x1)", p.Code, p.Multiplier)
		}
		if p.ValidDays != 30 {
			t.Fatalf("plan %s valid_days=%v want 30", p.Code, p.ValidDays)
		}
		if p.Status != PlanEnabled {
			t.Fatalf("plan %s status=%v want enabled", p.Code, p.Status)
		}
		if p.Sort != i+1 {
			t.Fatalf("plan %s sort=%d want %d", p.Code, p.Sort, i+1)
		}
		// 派生保护线必须 >= 进货价 >= 0，且零售保护线不超过官方售价（否则代理无加价空间）。
		if !(p.AgentCostPrice >= 0 && p.MinPrice >= p.AgentCostPrice && p.MinPrice <= p.BasePrice) {
			t.Fatalf("plan %s price invariant broken: cost=%v min=%v base=%v",
				p.Code, p.AgentCostPrice, p.MinPrice, p.BasePrice)
		}
	}
}

func TestSeedInto(t *testing.T) {
	repo := NewMemRepo()
	if err := SeedInto(repo); err != nil {
		t.Fatalf("SeedInto: %v", err)
	}
	got, _ := repo.ListPlans(nil)
	if len(got) != 6 {
		t.Fatalf("want 6 plans persisted, got %d", len(got))
	}
	// 验证按 Sort 升序（trial 在前）。
	if got[0].Code != "trial" || got[5].Code != "max" {
		t.Fatalf("seed order wrong: first=%s last=%s", got[0].Code, got[5].Code)
	}
}
