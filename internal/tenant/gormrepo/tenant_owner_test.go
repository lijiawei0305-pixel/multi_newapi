package gormrepo

import (
	"context"
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/tenant"
)

// TestTenantByOwner_ExcludesDeletedAndSuspended 锁定安全审计 M2：owner→tenant 反查（代理自助鉴权
// AgentOwnerAuthByUser / callerOwnedTenant 的解析基础）必须排除 deleted/suspended 租户，使被停用/删除的
// 代理即时失去控制台访问。旧实现无 status 过滤 → 停用/删除后仍可反查通过鉴权（本用例复现并锁定修复）。
func TestTenantByOwner_ExcludesDeletedAndSuspended(t *testing.T) {
	ctx := context.Background()
	r := newGroupTestRepo(t)

	// active 租户 → 可反查到。
	act := &tenant.Tenant{Slug: "shop-a", Name: "A", OwnerUserID: 100}
	if err := r.CreateTenant(ctx, act); err != nil {
		t.Fatalf("create active: %v", err)
	}
	if got, err := r.TenantByOwner(ctx, 100); err != nil || got == nil || got.ID != act.ID {
		t.Fatalf("active owner must resolve, got %+v err=%v", got, err)
	}

	// 停用后 → 反查不到（撤权即时生效）。
	if err := r.SetTenantStatus(ctx, act.ID, tenant.StatusSuspended); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if _, err := r.TenantByOwner(ctx, 100); !errors.Is(err, tenant.ErrTenantNotFound) {
		t.Fatalf("suspended owner must NOT resolve, got err=%v", err)
	}

	// 软删（status=deleted，owner_user_id 仍在）→ 反查不到。
	del := &tenant.Tenant{Slug: "shop-b", Name: "B", OwnerUserID: 200}
	if err := r.CreateTenant(ctx, del); err != nil {
		t.Fatalf("create b: %v", err)
	}
	if err := r.SetTenantStatus(ctx, del.ID, tenant.StatusDeleted); err != nil {
		t.Fatalf("delete b: %v", err)
	}
	if _, err := r.TenantByOwner(ctx, 200); !errors.Is(err, tenant.ErrTenantNotFound) {
		t.Fatalf("deleted owner must NOT resolve, got err=%v", err)
	}
}
