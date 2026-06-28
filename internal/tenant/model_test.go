package tenant

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

func TestTenantStatus_Valid(t *testing.T) {
	cases := []struct {
		s    TenantStatus
		want bool
	}{
		{StatusActive, true},
		{StatusSuspended, true},
		{StatusDeleted, true},
		{TenantStatus(""), false},
		{TenantStatus("frozen"), false},
	}
	for _, c := range cases {
		if got := c.s.Valid(); got != c.want {
			t.Errorf("%q.Valid() = %v, want %v", c.s, got, c.want)
		}
	}
}

func TestTenantStatus_CanTransitionTo(t *testing.T) {
	cases := []struct {
		from TenantStatus
		to   TenantStatus
		want bool
	}{
		// 合法迁移
		{StatusActive, StatusSuspended, true},
		{StatusActive, StatusDeleted, true},
		{StatusSuspended, StatusActive, true},
		{StatusSuspended, StatusDeleted, true},
		// same->same 非法
		{StatusActive, StatusActive, false},
		{StatusSuspended, StatusSuspended, false},
		{StatusDeleted, StatusDeleted, false},
		// 终态 deleted 不可迁出
		{StatusDeleted, StatusActive, false},
		{StatusDeleted, StatusSuspended, false},
		// 未知目标
		{StatusActive, TenantStatus("frozen"), false},
	}
	for _, c := range cases {
		if got := c.from.CanTransitionTo(c.to); got != c.want {
			t.Errorf("%q -> %q = %v, want %v", c.from, c.to, got, c.want)
		}
	}
}

func TestDomainForSlug(t *testing.T) {
	if got := DomainForSlug("acme"); got != "acme.wedreamhub.com" {
		t.Fatalf("DomainForSlug = %q", got)
	}
}

// 错误码字符串契约：稳定码值不得随意变更。
func TestErrorCodes(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{
		{ErrTenantNotFound, "TENANT_NOT_FOUND"},
		{ErrTenantSuspended, "TENANT_SUSPENDED"},
		{ErrSlugReserved, "SLUG_RESERVED"},
		{ErrSlugInvalid, "SLUG_INVALID"},
		{ErrSlugDuplicate, "SLUG_DUPLICATE"},
		{ErrStatusTransition, "TENANT_STATUS_INVALID"},
	}
	for _, c := range cases {
		if got := apperr.CodeOf(c.err); got != c.code {
			t.Errorf("CodeOf = %q, want %q", got, c.code)
		}
	}
}
