package mtwire

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/tenant"
	tenantrepo "github.com/QuantumNous/new-api/internal/tenant/gormrepo"
)

// newOwnerAuthTestApp 造最小 App：sqlite + tenants/tenant_domains（owner→tenant 反查所需）。
func newOwnerAuthTestApp(t *testing.T) *App {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := tenantrepo.AutoMigrate(db); err != nil {
		t.Fatalf("tenant migrate: %v", err)
	}
	return &App{DB: db, TenantRepo: tenantrepo.New(db)}
}

// seedOwnedTenant 建租户并设 owner（owner→tenant 1:1），返回 tenant_id。
func seedOwnedTenant(t *testing.T, app *App, slug string, ownerUserID int64) int64 {
	t.Helper()
	ctx := context.Background()
	tn := &tenant.Tenant{Slug: slug, Name: slug, Status: tenant.StatusActive}
	if err := app.TenantRepo.CreateTenant(ctx, tn); err != nil {
		t.Fatalf("create tenant %s: %v", slug, err)
	}
	if err := app.TenantRepo.SetOwnerUserID(ctx, tn.ID, ownerUserID); err != nil {
		t.Fatalf("set owner for %s: %v", slug, err)
	}
	return tn.ID
}

// ownerAuthCtx 造一个「已登录(id=userID)、主站 Host」的 gin 上下文（不经 TenantMiddleware，
// 故 Host 解析不出任何租户 → 证明解析纯粹来自 owner，与 Host 无关）。
func ownerAuthCtx(t *testing.T, userID int64) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/api/tenant/earnings", nil)
	req.Host = "api.wedreamhub.com" // 主站 Host：无租户映射
	c.Request = req
	c.Set("id", int(userID)) // 模拟 new-api UserAuth 写入的 session 用户
	return c, w
}

// TestAgentOwnerAuthByUser_ResolvesOwnedTenantHostIndependent 覆盖 4 条安全断言：
// (a) owner 在主站 Host → 解析到自己的租户并放行；(b) 已登录非 owner → 403 中止；
// (c) 无归属用户 → 403 中止；(d) owner A 永不解析到 owner B 的租户（双向隔离）。
func TestAgentOwnerAuthByUser_ResolvesOwnedTenantHostIndependent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := newOwnerAuthTestApp(t)
	tidA := seedOwnedTenant(t, app, "alpha", 100) // 用户 100 拥有租户 A
	tidB := seedOwnedTenant(t, app, "beta", 200)  // 用户 200 拥有租户 B

	// (a) owner 100 在主站 Host → 放行 + agentTenantID == A。
	cA, wA := ownerAuthCtx(t, 100)
	app.AgentOwnerAuthByUser()(cA)
	if cA.IsAborted() || wA.Code != http.StatusOK {
		t.Fatalf("owner 100: aborted=%v code=%d, want not-aborted/200", cA.IsAborted(), wA.Code)
	}
	if got := agentTenantID(cA); got != tidA {
		t.Fatalf("owner 100 resolved to tenant %d, want %d (A)", got, tidA)
	}
	// (d) 双向隔离：A 的上下文绝不是 B 的租户。
	if agentTenantID(cA) == tidB {
		t.Fatalf("owner A must NEVER resolve to owner B's tenant")
	}
	cB, _ := ownerAuthCtx(t, 200)
	app.AgentOwnerAuthByUser()(cB)
	if got := agentTenantID(cB); got != tidB {
		t.Fatalf("owner 200 resolved to tenant %d, want %d (B)", got, tidB)
	}

	// (b)+(c) 已登录但不拥有任何租户 → 403 中止，且不写 agentTenantID。
	cNone, wNone := ownerAuthCtx(t, 999)
	app.AgentOwnerAuthByUser()(cNone)
	if !cNone.IsAborted() || wNone.Code != http.StatusForbidden {
		t.Fatalf("non-owner 999: aborted=%v code=%d, want abort/403", cNone.IsAborted(), wNone.Code)
	}
	if got := agentTenantID(cNone); got != 0 {
		t.Fatalf("non-owner must not have an agent tenant set, got %d", got)
	}
}
