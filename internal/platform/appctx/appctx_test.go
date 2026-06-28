package appctx

import (
	"context"
	"testing"
)

func TestWithPrincipalRoundTrip(t *testing.T) {
	ctx := WithPrincipal(context.Background(), Principal{UserID: 7, TenantID: 1001, Role: RoleAgentOwner})
	p, ok := PrincipalFrom(ctx)
	if !ok {
		t.Fatal("principal not found")
	}
	if p.UserID != 7 || p.TenantID != 1001 || p.Role != RoleAgentOwner {
		t.Fatalf("round-trip mismatch: %+v", p)
	}
	if !p.IsAgentOwner() || p.IsAdmin() {
		t.Fatal("role predicates wrong")
	}
	if TenantID(ctx) != 1001 {
		t.Fatalf("TenantID = %d", TenantID(ctx))
	}
}

func TestPrincipalFromEmpty(t *testing.T) {
	if _, ok := PrincipalFrom(context.Background()); ok {
		t.Fatal("should be absent on empty ctx")
	}
	if TenantID(context.Background()) != 0 {
		t.Fatal("TenantID should be 0 when absent")
	}
}
