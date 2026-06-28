package identity

import (
	"context"
	"errors"
	"testing"

	"newapi-mt/internal/platform/appctx"
	"newapi-mt/internal/platform/apperr"
)

func TestHashTokenDeterministicAndDistinct(t *testing.T) {
	h1 := HashToken("secret-abc")
	h2 := HashToken("secret-abc")
	h3 := HashToken("secret-xyz")
	if h1 != h2 {
		t.Fatalf("hash not deterministic: %s vs %s", h1, h2)
	}
	if h1 == h3 {
		t.Fatal("distinct tokens produced same hash")
	}
	if len(h1) != 64 { // hex(sha256) = 32 bytes -> 64 hex chars
		t.Fatalf("unexpected hash length %d", len(h1))
	}
	if h1 == "secret-abc" {
		t.Fatal("hash must not equal plaintext")
	}
}

func TestAuthenticateTokenValid(t *testing.T) {
	store := NewMemTokenStore()
	store.Put("tok-A", TokenRecord{UserID: 7, TenantID: 1001, Role: appctx.RoleAgentOwner})
	auth := NewAuthenticator(store)

	p, err := auth.AuthenticateToken(context.Background(), "tok-A")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if p == nil || p.UserID != 7 || p.TenantID != 1001 || p.Role != appctx.RoleAgentOwner {
		t.Fatalf("principal mismatch: %+v", p)
	}
}

func TestAuthenticateTokenBearerAndWhitespace(t *testing.T) {
	store := NewMemTokenStore()
	store.Put("tok-A", TokenRecord{UserID: 1, TenantID: 2, Role: appctx.RoleUser})
	auth := NewAuthenticator(store)
	for _, raw := range []string{"tok-A", "  tok-A  ", "Bearer tok-A", "bearer tok-A", "  Bearer tok-A"} {
		p, err := auth.AuthenticateToken(context.Background(), raw)
		if err != nil {
			t.Fatalf("raw %q: unexpected err %v", raw, err)
		}
		if p.UserID != 1 {
			t.Fatalf("raw %q: wrong principal %+v", raw, p)
		}
	}
}

func TestAuthenticateTokenEmptyUnauthorized(t *testing.T) {
	auth := NewAuthenticator(NewMemTokenStore())
	for _, raw := range []string{"", "   ", "Bearer ", "  Bearer  "} {
		_, err := auth.AuthenticateToken(context.Background(), raw)
		if !apperr.Is(err, CodeUnauthorized) {
			t.Fatalf("raw %q: want UNAUTHORIZED got %v", raw, err)
		}
	}
}

func TestAuthenticateTokenNotFound(t *testing.T) {
	auth := NewAuthenticator(NewMemTokenStore())
	_, err := auth.AuthenticateToken(context.Background(), "nope")
	if !apperr.Is(err, CodeTokenInvalid) {
		t.Fatalf("want TOKEN_INVALID got %v", err)
	}
}

// 冻结租户/用户的 Token 不能调用：冻结令牌与冻结用户均返回 TOKEN_INVALID。
func TestAuthenticateTokenFrozen(t *testing.T) {
	store := NewMemTokenStore()
	store.Put("frozen-token", TokenRecord{UserID: 1, TenantID: 2, Role: appctx.RoleUser, Disabled: true})
	store.Put("frozen-user", TokenRecord{UserID: 3, TenantID: 2, Role: appctx.RoleUser, UserDisabled: true})
	auth := NewAuthenticator(store)
	for _, raw := range []string{"frozen-token", "frozen-user"} {
		_, err := auth.AuthenticateToken(context.Background(), raw)
		if !apperr.Is(err, CodeTokenInvalid) {
			t.Fatalf("raw %q: want TOKEN_INVALID got %v", raw, err)
		}
	}
}

func TestAuthenticateTokenStoreError(t *testing.T) {
	sentinel := errors.New("db down")
	auth := NewAuthenticator(errTokenStore{err: sentinel})
	_, err := auth.AuthenticateToken(context.Background(), "x")
	if !errors.Is(err, sentinel) {
		t.Fatalf("store error not propagated: %v", err)
	}
}

// 跨租户 Token 必拒：B 租户 token 鉴权得到 TenantID=B 的 Principal，
// 访问 A 租户资源时被 RequireTenantOwner 拦截为 FORBIDDEN_TENANT。
func TestCrossTenantTokenRejected(t *testing.T) {
	const tenantA, tenantB = int64(1001), int64(2002)
	store := NewMemTokenStore()
	store.Put("token-of-B", TokenRecord{UserID: 9, TenantID: tenantB, Role: appctx.RoleAgentOwner})
	auth := NewAuthenticator(store)
	guard := NewAccessGuard(NewMemTenantStatusChecker())

	p, err := auth.AuthenticateToken(context.Background(), "token-of-B")
	if err != nil {
		t.Fatalf("auth failed: %v", err)
	}
	// 访问自己租户 → 放行。
	if err := guard.RequireTenantOwner(p, tenantB); err != nil {
		t.Fatalf("owner of B should access B: %v", err)
	}
	// 访问 A 租户 → 拒绝。
	if err := guard.RequireTenantOwner(p, tenantA); !apperr.Is(err, CodeForbiddenTenant) {
		t.Fatalf("cross-tenant must be FORBIDDEN_TENANT, got %v", err)
	}
}

type errTokenStore struct{ err error }

func (e errTokenStore) FindByHash(ctx context.Context, h string) (*TokenRecord, bool, error) {
	return nil, false, e.err
}
