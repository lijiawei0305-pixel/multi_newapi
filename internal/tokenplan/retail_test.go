package tokenplan

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

func TestSetListingSuccess(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	plan := seedPlanInto(repo, basePlanInput()) // MinPrice=220
	svc := NewRetailService(repo, &fakeGuard{})

	if err := svc.SetListing(ctx, 7, plan.ID, true, 250); err != nil {
		t.Fatalf("SetListing: %v", err)
	}
	l, _ := repo.GetListing(ctx, 7, plan.ID)
	if l == nil || !l.Enabled || l.RetailPrice != 250 {
		t.Fatalf("listing not persisted: %+v", l)
	}
}

func TestSetListingAtMinBoundary(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	plan := seedPlanInto(repo, basePlanInput()) // MinPrice=220
	svc := NewRetailService(repo, &fakeGuard{})

	// 恰好等于保护线放行（边界）。
	if err := svc.SetListing(ctx, 7, plan.ID, true, plan.MinPrice); err != nil {
		t.Fatalf("retail==min_price must pass, got %v", err)
	}
}

func TestSetListingBelowMin(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	plan := seedPlanInto(repo, basePlanInput()) // MinPrice=220
	g := &fakeGuard{}
	svc := NewRetailService(repo, g)

	if err := svc.SetListing(ctx, 7, plan.ID, true, 219.99); apperr.CodeOf(err) != CodeRetailBelowMin {
		t.Fatalf("want RETAIL_BELOW_MIN, got %v", err)
	}
	if g.calls != 1 {
		t.Fatalf("guard must be consulted exactly once, got %d", g.calls)
	}
	// 击穿时不落上架记录。
	if l, _ := repo.GetListing(ctx, 7, plan.ID); l != nil {
		t.Fatalf("must not persist listing when below min: %+v", l)
	}
}

func TestSetListingGuardForcedError(t *testing.T) {
	// 即便 guard 因任意原因返回错误，SetListing 也映射为本域 RETAIL_BELOW_MIN。
	ctx := context.Background()
	repo := NewMemRepo()
	plan := seedPlanInto(repo, basePlanInput())
	g := &fakeGuard{forceErr: apperr.New("PRICE_BELOW_PROTECTION", "x", 400)}
	svc := NewRetailService(repo, g)

	if err := svc.SetListing(ctx, 7, plan.ID, true, 9999); apperr.CodeOf(err) != CodeRetailBelowMin {
		t.Fatalf("want RETAIL_BELOW_MIN, got %v", err)
	}
}

func TestSetListingNegativeRetail(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	plan := seedPlanInto(repo, basePlanInput())
	svc := NewRetailService(repo, &fakeGuard{})
	if err := svc.SetListing(ctx, 7, plan.ID, true, -1); apperr.CodeOf(err) != CodeRetailBelowMin {
		t.Fatalf("negative retail want RETAIL_BELOW_MIN, got %v", err)
	}
}

func TestSetListingWithdraw(t *testing.T) {
	// 退出（enabled=false）不校验零售价，即便价格为 0。
	ctx := context.Background()
	repo := NewMemRepo()
	plan := seedPlanInto(repo, basePlanInput())
	g := &fakeGuard{}
	svc := NewRetailService(repo, g)

	if err := svc.SetListing(ctx, 7, plan.ID, false, 0); err != nil {
		t.Fatalf("withdraw must not validate price: %v", err)
	}
	if g.calls != 0 {
		t.Fatalf("guard must not be consulted on withdraw, got %d", g.calls)
	}
	l, _ := repo.GetListing(ctx, 7, plan.ID)
	if l == nil || l.Enabled {
		t.Fatalf("withdraw listing should exist and be disabled: %+v", l)
	}
}

func TestSetListingPlanDisabled(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	in := basePlanInput()
	in.Status = PlanDisabled
	plan := seedPlanInto(repo, in)
	svc := NewRetailService(repo, &fakeGuard{})

	if err := svc.SetListing(ctx, 7, plan.ID, true, 250); apperr.CodeOf(err) != CodePlanDisabled {
		t.Fatalf("want PLAN_DISABLED, got %v", err)
	}
}

func TestSetListingPlanNotFound(t *testing.T) {
	svc := NewRetailService(NewMemRepo(), &fakeGuard{})
	if err := svc.SetListing(context.Background(), 7, 123, true, 250); apperr.CodeOf(err) != CodePlanNotFound {
		t.Fatalf("want PLAN_NOT_FOUND, got %v", err)
	}
}

func TestSetListingReprice(t *testing.T) {
	// 二次 SetListing 应 upsert 同一条记录（不新增）。
	ctx := context.Background()
	repo := NewMemRepo()
	plan := seedPlanInto(repo, basePlanInput())
	svc := NewRetailService(repo, &fakeGuard{})
	_ = svc.SetListing(ctx, 7, plan.ID, true, 250)
	_ = svc.SetListing(ctx, 7, plan.ID, true, 300)
	ls, _ := repo.ListListings(ctx, 7)
	if len(ls) != 1 || ls[0].RetailPrice != 300 {
		t.Fatalf("reprice should upsert single record at 300: %+v", ls)
	}
}

func TestListForTenant(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	// 两个启用套餐 + 一个停用套餐（不应出现在代理视图）。
	a := basePlanInput()
	a.Code, a.Sort = "mini", 1
	pa := seedPlanInto(repo, a)
	b := basePlanInput()
	b.Code, b.Sort = "solo", 2
	pb := seedPlanInto(repo, b)
	d := basePlanInput()
	d.Code, d.Sort, d.Status = "old", 3, PlanDisabled
	seedPlanInto(repo, d)

	svc := NewRetailService(repo, &fakeGuard{})
	if err := svc.SetListing(ctx, 7, pa.ID, true, 230); err != nil { // 上架 mini@230 (>=min_price 220)
		t.Fatalf("SetListing: %v", err)
	}

	views, err := svc.ListForTenant(ctx, 7)
	if err != nil {
		t.Fatalf("ListForTenant: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("want 2 enabled plans in view, got %d", len(views))
	}
	// pa 上架：Listed/Enabled，零售价 130。
	var seenA, seenB bool
	for _, v := range views {
		switch v.Plan.ID {
		case pa.ID:
			seenA = true
			if !v.Listed || !v.Enabled || v.RetailPrice != 230 {
				t.Fatalf("mini view wrong: %+v", v)
			}
		case pb.ID:
			seenB = true
			// 未上架：回退 BasePrice，Listed=false。
			if v.Listed || v.RetailPrice != pb.BasePrice {
				t.Fatalf("solo (unlisted) view wrong: %+v", v)
			}
		}
	}
	if !seenA || !seenB {
		t.Fatal("both enabled plans must appear")
	}
}
