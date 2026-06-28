package tenant

import (
	"context"
	"testing"

	"newapi-mt/internal/platform/appctx"
	"newapi-mt/internal/platform/apperr"
)

// spyRepo 仅覆盖 resolver 用到的 GetTenantByDomain，并记录调用次数；
// 其余方法继承 nil 内嵌接口（resolver 不会调用，故安全）。
type spyRepo struct {
	TenantRepo
	calls int
	ret   *Tenant
	err   error
}

func (s *spyRepo) GetTenantByDomain(_ context.Context, _ string) (*Tenant, error) {
	s.calls++
	return s.ret, s.err
}

func TestResolver_CacheHit(t *testing.T) {
	ctx := context.Background()
	cache := NewMemCache()
	cached := &Tenant{ID: 7, Slug: "acme", Status: StatusActive}
	cache.Set(ctx, "acme.wedreamhub.com", cached)

	repo := &spyRepo{err: ErrTenantNotFound} // 若被回源会失败
	r := NewResolver(repo, cache)

	got, err := r.ResolveByHost(ctx, "acme.wedreamhub.com")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got.ID != 7 {
		t.Errorf("tenant = %+v", got)
	}
	if repo.calls != 0 {
		t.Errorf("repo consulted on cache hit (calls=%d)", repo.calls)
	}
}

func TestResolver_MissThenSource(t *testing.T) {
	ctx := context.Background()
	cache := NewMemCache()
	repo := &spyRepo{ret: &Tenant{ID: 9, Slug: "beta", Status: StatusActive}}
	r := NewResolver(repo, cache)

	got, err := r.ResolveByHost(ctx, "beta.wedreamhub.com")
	if err != nil || got.ID != 9 {
		t.Fatalf("got %+v, err %v", got, err)
	}
	if repo.calls != 1 {
		t.Errorf("repo calls = %d, want 1", repo.calls)
	}
	// 回源后应写缓存，再次解析不再回源。
	if _, ok := cache.Get(ctx, "beta.wedreamhub.com"); !ok {
		t.Fatal("cache not populated after source")
	}
	if _, err := r.ResolveByHost(ctx, "beta.wedreamhub.com"); err != nil {
		t.Fatal(err)
	}
	if repo.calls != 1 {
		t.Errorf("repo calls = %d after warm cache, want 1", repo.calls)
	}
}

func TestResolver_NotFound(t *testing.T) {
	ctx := context.Background()
	cache := NewMemCache()
	repo := &spyRepo{err: ErrTenantNotFound}
	r := NewResolver(repo, cache)

	_, err := r.ResolveByHost(ctx, "ghost.wedreamhub.com")
	if !apperr.Is(err, "TENANT_NOT_FOUND") {
		t.Fatalf("err = %v, want TENANT_NOT_FOUND", err)
	}
	// 负结果不缓存。
	if _, ok := cache.Get(ctx, "ghost.wedreamhub.com"); ok {
		t.Fatal("negative result should not be cached")
	}
}

func TestResolver_EmptyHost(t *testing.T) {
	r := NewResolver(&spyRepo{}, NewMemCache())
	if _, err := r.ResolveByHost(context.Background(), "   "); !apperr.Is(err, "TENANT_NOT_FOUND") {
		t.Fatalf("err = %v, want TENANT_NOT_FOUND", err)
	}
}

// 端到端：Service.Create 后，归一化 Host（含大小写/端口/末尾点）能解析到租户。
func TestResolver_HostNormalization_EndToEnd(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	svc := NewService(repo, NewSlugValidator())
	created, err := svc.Create(ctx, CreateTenantInput{Slug: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	r := NewResolver(repo, NewMemCache())

	for _, host := range []string{
		"acme.wedreamhub.com",
		"ACME.WEDREAMHUB.COM",
		"acme.wedreamhub.com:8443",
		" acme.wedreamhub.com. ",
	} {
		got, err := r.ResolveByHost(ctx, host)
		if err != nil {
			t.Fatalf("ResolveByHost(%q) err = %v", host, err)
		}
		if got.ID != created.ID {
			t.Errorf("ResolveByHost(%q) -> %d, want %d", host, got.ID, created.ID)
		}
	}
}

func TestContextWithTenant(t *testing.T) {
	// 空 ctx：注入 TenantID。
	ctx := ContextWithTenant(context.Background(), &Tenant{ID: 42})
	if got := appctx.TenantID(ctx); got != 42 {
		t.Fatalf("TenantID = %d, want 42", got)
	}
	// 已有 Principal：保留 UserID/Role，仅覆盖 TenantID。
	base := appctx.WithPrincipal(context.Background(), appctx.Principal{UserID: 5, Role: appctx.RoleUser})
	ctx = ContextWithTenant(base, &Tenant{ID: 7})
	p, ok := appctx.PrincipalFrom(ctx)
	if !ok || p.UserID != 5 || p.Role != appctx.RoleUser || p.TenantID != 7 {
		t.Fatalf("principal = %+v, ok=%v", p, ok)
	}
}
