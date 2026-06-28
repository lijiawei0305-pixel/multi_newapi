package identity

import (
	"context"
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/platform/appctx"
	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

func principal(role appctx.Role, tenantID int64) *appctx.Principal {
	return &appctx.Principal{UserID: 1, TenantID: tenantID, Role: role}
}

func TestRequireAdmin(t *testing.T) {
	g := NewAccessGuard(NewMemTenantStatusChecker())
	cases := []struct {
		name    string
		p       *appctx.Principal
		wantErr bool
	}{
		{"admin", principal(appctx.RoleAdmin, 0), false},
		{"agent_owner", principal(appctx.RoleAgentOwner, 1), true},
		{"user", principal(appctx.RoleUser, 1), true},
		{"nil", nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := g.RequireAdmin(c.p)
			if c.wantErr {
				if !apperr.Is(err, CodeForbiddenAdmin) {
					t.Fatalf("want FORBIDDEN_ADMIN got %v", err)
				}
			} else if err != nil {
				t.Fatalf("want nil got %v", err)
			}
		})
	}
}

func TestRequireTenantOwner(t *testing.T) {
	g := NewAccessGuard(NewMemTenantStatusChecker())
	const tA, tB = int64(1), int64(2)
	cases := []struct {
		name    string
		p       *appctx.Principal
		tenant  int64
		wantErr bool
	}{
		{"admin cross-tenant allowed", principal(appctx.RoleAdmin, 99), tA, false},
		{"owner same tenant", principal(appctx.RoleAgentOwner, tA), tA, false},
		{"owner other tenant", principal(appctx.RoleAgentOwner, tB), tA, true},
		{"user same tenant", principal(appctx.RoleUser, tA), tA, true},
		{"user other tenant", principal(appctx.RoleUser, tB), tA, true},
		{"nil", nil, tA, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := g.RequireTenantOwner(c.p, c.tenant)
			if c.wantErr {
				if !apperr.Is(err, CodeForbiddenTenant) {
					t.Fatalf("want FORBIDDEN_TENANT got %v", err)
				}
			} else if err != nil {
				t.Fatalf("want nil got %v", err)
			}
		})
	}
}

func TestRequireTenantActive(t *testing.T) {
	checker := NewMemTenantStatusChecker()
	checker.Set(1, TenantStatusActive)
	checker.Set(2, TenantStatusSuspended)
	checker.Set(3, TenantStatusDeleted)
	g := NewAccessGuard(checker)
	ctx := context.Background()

	if err := g.RequireTenantActive(ctx, 1); err != nil {
		t.Fatalf("active tenant should pass: %v", err)
	}
	// suspended / deleted / 不存在 → TENANT_INACTIVE。
	for _, id := range []int64{2, 3, 999} {
		if err := g.RequireTenantActive(ctx, id); !apperr.Is(err, CodeTenantInactive) {
			t.Fatalf("tenant %d want TENANT_INACTIVE got %v", id, err)
		}
	}
}

func TestRequireTenantActiveCheckerError(t *testing.T) {
	sentinel := errors.New("checker boom")
	g := NewAccessGuard(errChecker{err: sentinel})
	if err := g.RequireTenantActive(context.Background(), 1); !errors.Is(err, sentinel) {
		t.Fatalf("checker error not propagated: %v", err)
	}
}

type errChecker struct{ err error }

func (e errChecker) StatusOf(ctx context.Context, id int64) (TenantStatus, bool, error) {
	return "", false, e.err
}
