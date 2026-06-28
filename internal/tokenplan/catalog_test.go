package tokenplan

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

func TestCatalogCreateAndGet(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	c := NewCatalog(repo)

	p, err := c.Create(ctx, basePlanInput())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if p.ID == 0 || p.CreatedAt.IsZero() {
		t.Fatalf("Create must backfill ID/CreatedAt, got %+v", p)
	}
	got, err := c.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Code != "solo" || got.MonthLimitUSD != 560 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestCatalogCreateRejectsInvalid(t *testing.T) {
	ctx := context.Background()
	c := NewCatalog(NewMemRepo())
	in := basePlanInput()
	in.MonthLimitUSD = 0 // 非法
	if _, err := c.Create(ctx, in); apperr.CodeOf(err) != CodePlanInputInvalid {
		t.Fatalf("want PLAN_INPUT_INVALID, got %v", err)
	}
}

func TestCatalogUpdate(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	c := NewCatalog(repo)
	p, _ := c.Create(ctx, basePlanInput())

	in := basePlanInput()
	in.Name = "Solo Plus"
	in.MonthLimitUSD = 600
	if err := c.Update(ctx, p.ID, in); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ := c.Get(ctx, p.ID)
	if got.Name != "Solo Plus" || got.MonthLimitUSD != 600 {
		t.Fatalf("update not applied: %+v", got)
	}
	if got.CreatedAt != p.CreatedAt {
		t.Fatal("Update must preserve CreatedAt")
	}
}

func TestCatalogUpdateNotFound(t *testing.T) {
	ctx := context.Background()
	c := NewCatalog(NewMemRepo())
	if err := c.Update(ctx, 999, basePlanInput()); apperr.CodeOf(err) != CodePlanNotFound {
		t.Fatalf("want PLAN_NOT_FOUND, got %v", err)
	}
}

func TestCatalogUpdateRejectsInvalid(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	c := NewCatalog(repo)
	p, _ := c.Create(ctx, basePlanInput())
	in := basePlanInput()
	in.Code = ""
	if err := c.Update(ctx, p.ID, in); apperr.CodeOf(err) != CodePlanInputInvalid {
		t.Fatalf("want PLAN_INPUT_INVALID, got %v", err)
	}
}

func TestCatalogGetNotFound(t *testing.T) {
	c := NewCatalog(NewMemRepo())
	if _, err := c.Get(context.Background(), 42); apperr.CodeOf(err) != CodePlanNotFound {
		t.Fatalf("want PLAN_NOT_FOUND, got %v", err)
	}
}

func TestCatalogListSorted(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	c := NewCatalog(repo)
	// 以乱序 Sort 插入，断言 List 按 Sort 升序。
	a := basePlanInput()
	a.Code, a.Sort = "max", 3
	b := basePlanInput()
	b.Code, b.Sort = "trial", 1
	d := basePlanInput()
	d.Code, d.Sort = "pro", 2
	_, _ = c.Create(ctx, a)
	_, _ = c.Create(ctx, b)
	_, _ = c.Create(ctx, d)

	list, err := c.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 3 || list[0].Code != "trial" || list[1].Code != "pro" || list[2].Code != "max" {
		t.Fatalf("List not sorted by Sort: %+v", list)
	}
}
