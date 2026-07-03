package tenant

import (
	"context"
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

func newService() (TenantService, *MemRepo) {
	repo := NewMemRepo()
	return NewService(repo, NewSlugValidator()), repo
}

func TestService_Create_Success(t *testing.T) {
	ctx := context.Background()
	svc, repo := newService()

	tn, err := svc.Create(ctx, CreateTenantInput{Slug: "acme", Name: "Acme Inc", TokenplanEnabled: true})
	if err != nil {
		t.Fatalf("Create err = %v", err)
	}
	if tn.ID == 0 {
		t.Fatal("expected assigned ID")
	}
	if tn.Status != StatusActive {
		t.Errorf("status = %q, want active", tn.Status)
	}
	if !tn.TokenplanEnabled {
		t.Error("TokenplanEnabled not persisted")
	}
	if tn.CreatedAt.IsZero() || tn.UpdatedAt.IsZero() {
		t.Error("timestamps not set")
	}

	// 自动写入二级域名记录，可被域名解析命中。
	got, err := repo.GetTenantByDomain(ctx, "acme.wedreamhub.com")
	if err != nil {
		t.Fatalf("domain record missing: %v", err)
	}
	if got.ID != tn.ID {
		t.Errorf("domain maps to tenant %d, want %d", got.ID, tn.ID)
	}
}

func TestService_Create_Errors(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		slug string
		code string
	}{
		{"reserved", "admin", "SLUG_RESERVED"},
		{"invalid", "Bad_Slug", "SLUG_INVALID"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc, _ := newService()
			_, err := svc.Create(ctx, CreateTenantInput{Slug: c.slug})
			if !apperr.Is(err, c.code) {
				t.Fatalf("err = %v, want code %s", err, c.code)
			}
		})
	}
}

func TestService_Create_Duplicate(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService()
	if _, err := svc.Create(ctx, CreateTenantInput{Slug: "acme"}); err != nil {
		t.Fatalf("first Create err = %v", err)
	}
	_, err := svc.Create(ctx, CreateTenantInput{Slug: "acme"})
	if !apperr.Is(err, "SLUG_DUPLICATE") {
		t.Fatalf("second Create err = %v, want SLUG_DUPLICATE", err)
	}
}

func TestService_Get(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService()
	created, _ := svc.Create(ctx, CreateTenantInput{Slug: "acme"})

	got, err := svc.Get(ctx, created.ID)
	if err != nil || got.Slug != "acme" {
		t.Fatalf("Get = %v, %v", got, err)
	}

	if _, err := svc.Get(ctx, 99999); !apperr.Is(err, "TENANT_NOT_FOUND") {
		t.Fatalf("Get(missing) err = %v, want TENANT_NOT_FOUND", err)
	}
}

func TestService_SetStatus_Transitions(t *testing.T) {
	ctx := context.Background()

	// 合法路径：active -> suspended -> active -> deleted。
	t.Run("legal-path", func(t *testing.T) {
		svc, _ := newService()
		tn, _ := svc.Create(ctx, CreateTenantInput{Slug: "acme"})
		steps := []TenantStatus{StatusSuspended, StatusActive, StatusDeleted}
		for _, s := range steps {
			if err := svc.SetStatus(ctx, tn.ID, s); err != nil {
				t.Fatalf("SetStatus(%q) err = %v", s, err)
			}
			got, _ := svc.Get(ctx, tn.ID)
			if got.Status != s {
				t.Fatalf("status = %q, want %q", got.Status, s)
			}
		}
	})

	// suspended -> deleted 合法。
	t.Run("suspended-to-deleted", func(t *testing.T) {
		svc, _ := newService()
		tn, _ := svc.Create(ctx, CreateTenantInput{Slug: "beta"})
		if err := svc.SetStatus(ctx, tn.ID, StatusSuspended); err != nil {
			t.Fatal(err)
		}
		if err := svc.SetStatus(ctx, tn.ID, StatusDeleted); err != nil {
			t.Fatalf("suspended->deleted err = %v", err)
		}
	})

	// 非法迁移与非法目标。
	t.Run("illegal", func(t *testing.T) {
		svc, _ := newService()
		tn, _ := svc.Create(ctx, CreateTenantInput{Slug: "gamma"})
		// active -> active 非法
		if err := svc.SetStatus(ctx, tn.ID, StatusActive); !apperr.Is(err, "TENANT_STATUS_INVALID") {
			t.Fatalf("active->active err = %v", err)
		}
		// 未知目标状态
		if err := svc.SetStatus(ctx, tn.ID, TenantStatus("frozen")); !apperr.Is(err, "TENANT_STATUS_INVALID") {
			t.Fatalf("unknown status err = %v", err)
		}
		// 进入终态后不可再迁出
		_ = svc.SetStatus(ctx, tn.ID, StatusDeleted)
		if err := svc.SetStatus(ctx, tn.ID, StatusActive); !apperr.Is(err, "TENANT_STATUS_INVALID") {
			t.Fatalf("deleted->active err = %v", err)
		}
	})

	// 租户不存在。
	t.Run("not-found", func(t *testing.T) {
		svc, _ := newService()
		if err := svc.SetStatus(ctx, 12345, StatusSuspended); !apperr.Is(err, "TENANT_NOT_FOUND") {
			t.Fatalf("err = %v, want TENANT_NOT_FOUND", err)
		}
	})
}

// faultyRepo 让 CreateDomain 失败，覆盖 Create 的域名写入错误分支。
type faultyRepo struct {
	*MemRepo
	domainErr error
}

func (f *faultyRepo) CreateDomain(ctx context.Context, d *TenantDomain) error {
	return f.domainErr
}

func TestService_Create_DomainError(t *testing.T) {
	sentinel := errors.New("domain write failed")
	repo := &faultyRepo{MemRepo: NewMemRepo(), domainErr: sentinel}
	svc := NewService(repo, NewSlugValidator())
	_, err := svc.Create(context.Background(), CreateTenantInput{Slug: "acme"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want sentinel propagated", err)
	}
}

func TestService_Create_SkipSubdomain(t *testing.T) {
	ctx := context.Background()
	svc, repo := newService()

	tn, err := svc.Create(ctx, CreateTenantInput{Slug: "basic", SkipSubdomain: true})
	if err != nil {
		t.Fatalf("Create err = %v", err)
	}
	// 无子域名：解析该 Host 返回 NotFound（不炸），但租户本身仍可按 id 读到。
	if _, err := repo.GetTenantByDomain(ctx, "basic.wedreamhub.com"); !errors.Is(err, ErrTenantNotFound) {
		t.Fatalf("subdomain must NOT be provisioned for L0, got err=%v", err)
	}
	if _, err := repo.GetTenant(ctx, tn.ID); err != nil {
		t.Fatalf("tenant must still resolve by id: %v", err)
	}
}

func TestService_EnsureSubdomain_Idempotent(t *testing.T) {
	ctx := context.Background()
	svc, repo := newService()
	tn, _ := svc.Create(ctx, CreateTenantInput{Slug: "grow", SkipSubdomain: true})

	if err := svc.EnsureSubdomain(ctx, tn.ID, "grow"); err != nil {
		t.Fatalf("EnsureSubdomain: %v", err)
	}
	got, err := repo.GetTenantByDomain(ctx, "grow.wedreamhub.com")
	if err != nil || got.ID != tn.ID {
		t.Fatalf("after ensure, domain must map to tenant: got=%v err=%v", got, err)
	}
	// 幂等：再次调用不报错。
	if err := svc.EnsureSubdomain(ctx, tn.ID, "grow"); err != nil {
		t.Fatalf("EnsureSubdomain must be idempotent, got %v", err)
	}
}
