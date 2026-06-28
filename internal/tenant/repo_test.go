package tenant

import (
	"context"
	"sync"
	"testing"

	"newapi-mt/internal/platform/apperr"
)

func TestMemRepo_CreateAndGet(t *testing.T) {
	ctx := context.Background()
	r := NewMemRepo()

	a := &Tenant{Slug: "a-shop"}
	b := &Tenant{Slug: "b-shop"}
	if err := r.CreateTenant(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := r.CreateTenant(ctx, b); err != nil {
		t.Fatal(err)
	}
	if a.ID == 0 || b.ID == a.ID {
		t.Fatalf("ids not assigned uniquely: a=%d b=%d", a.ID, b.ID)
	}
	if a.Status != StatusActive {
		t.Errorf("default status = %q, want active", a.Status)
	}

	// 重复 slug -> SLUG_DUPLICATE
	if err := r.CreateTenant(ctx, &Tenant{Slug: "a-shop"}); !apperr.Is(err, "SLUG_DUPLICATE") {
		t.Fatalf("dup slug err = %v", err)
	}

	got, err := r.GetTenantBySlug(ctx, "a-shop")
	if err != nil || got.ID != a.ID {
		t.Fatalf("GetTenantBySlug = %v, %v", got, err)
	}
	if _, err := r.GetTenantBySlug(ctx, "missing"); !apperr.Is(err, "TENANT_NOT_FOUND") {
		t.Fatalf("GetTenantBySlug(missing) err = %v", err)
	}
	if _, err := r.GetTenant(ctx, 9999); !apperr.Is(err, "TENANT_NOT_FOUND") {
		t.Fatalf("GetTenant(missing) err = %v", err)
	}
}

func TestMemRepo_Domains(t *testing.T) {
	ctx := context.Background()
	r := NewMemRepo()
	tn := &Tenant{Slug: "acme"}
	_ = r.CreateTenant(ctx, tn)

	d := &TenantDomain{TenantID: tn.ID, Domain: "acme.wedreamhub.com", IsPrimary: true}
	if err := r.CreateDomain(ctx, d); err != nil {
		t.Fatal(err)
	}
	if d.ID == 0 || d.CreatedAt.IsZero() {
		t.Errorf("domain id/createdAt not set: %+v", d)
	}
	// 重复域名 -> SLUG_DUPLICATE
	if err := r.CreateDomain(ctx, &TenantDomain{Domain: "acme.wedreamhub.com"}); !apperr.Is(err, "SLUG_DUPLICATE") {
		t.Fatalf("dup domain err = %v", err)
	}
	got, err := r.GetTenantByDomain(ctx, "acme.wedreamhub.com")
	if err != nil || got.ID != tn.ID {
		t.Fatalf("GetTenantByDomain = %v, %v", got, err)
	}
	if _, err := r.GetTenantByDomain(ctx, "missing.wedreamhub.com"); !apperr.Is(err, "TENANT_NOT_FOUND") {
		t.Fatalf("GetTenantByDomain(missing) err = %v", err)
	}
}

func TestMemRepo_SetTenantStatus(t *testing.T) {
	ctx := context.Background()
	r := NewMemRepo()
	tn := &Tenant{Slug: "acme"}
	_ = r.CreateTenant(ctx, tn)

	if err := r.SetTenantStatus(ctx, tn.ID, StatusSuspended); err != nil {
		t.Fatal(err)
	}
	got, _ := r.GetTenant(ctx, tn.ID)
	if got.Status != StatusSuspended {
		t.Errorf("status = %q", got.Status)
	}
	if err := r.SetTenantStatus(ctx, 9999, StatusActive); !apperr.Is(err, "TENANT_NOT_FOUND") {
		t.Fatalf("SetTenantStatus(missing) err = %v", err)
	}
}

func TestMemCache(t *testing.T) {
	ctx := context.Background()
	c := NewMemCache()

	if _, ok := c.Get(ctx, "x"); ok {
		t.Fatal("empty cache should miss")
	}
	c.Set(ctx, "x", nil) // nil 安全：不写入
	if _, ok := c.Get(ctx, "x"); ok {
		t.Fatal("nil Set should not populate")
	}
	c.Set(ctx, "x", &Tenant{ID: 1})
	got, ok := c.Get(ctx, "x")
	if !ok || got.ID != 1 {
		t.Fatalf("Get = %v, %v", got, ok)
	}
	// 返回值是拷贝，修改不影响缓存。
	got.ID = 999
	again, _ := c.Get(ctx, "x")
	if again.ID != 1 {
		t.Fatal("cache returned aliased pointer")
	}
	c.Invalidate(ctx, "x")
	if _, ok := c.Get(ctx, "x"); ok {
		t.Fatal("Invalidate failed")
	}
}

// 并发解析：缓存 + 回源在 -race 下无数据竞争。
func TestResolver_Concurrent(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	svc := NewService(repo, NewSlugValidator())
	if _, err := svc.Create(ctx, CreateTenantInput{Slug: "acme"}); err != nil {
		t.Fatal(err)
	}
	r := NewResolver(repo, NewMemCache())

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := r.ResolveByHost(ctx, "acme.wedreamhub.com"); err != nil {
				t.Errorf("resolve err = %v", err)
			}
		}()
	}
	wg.Wait()
}
