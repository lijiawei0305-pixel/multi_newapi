package mtwire

// Fix 2（严重）回归测试：HandleAgentGetSiteConfig / HandleAgentUpdateSiteConfig 此前用 tenantFrom(c)
// （Host 解析出的租户）读取/返回装修配置，而鉴权 + DB 写入却用 agentTenantID(c)（owner 解析出的租户）
// ——读写鉴权三处口径不一致：(a) 调用者 Host 若解析到别的租户，GET 会读到别人的装修配置（越权读）；
// (b) PUT 在无 Host→租户映射（如主站）时 tenantFrom(c) 为 nil，用于响应会 nil 解引用 panic
//（且 DB 写入已经先发生）。修复：GET/PUT 一律用 agentTenantID(c) 取 *tenant.Tenant（经
// TenantService.Get），使读 scope == 写 scope == 鉴权 scope。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/agent"
	"github.com/QuantumNous/new-api/internal/siteconfig"
	"github.com/QuantumNous/new-api/internal/tenant"
	tenantrepo "github.com/QuantumNous/new-api/internal/tenant/gormrepo"
)

// newSiteConfigTestApp 造最小 App：sqlite tenants（TenantService.Get 供 handler 取 *Tenant）+
// agent MemRepo（ensureAgentLevel(c,1) 门禁）+ siteconfig MemRepo（装修配置，天然按 tenant_id 隔离）。
func newSiteConfigTestApp(t *testing.T) (*App, *tenantrepo.Repo) {
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
	tr := tenantrepo.New(db)
	scRepo := siteconfig.NewMemRepo()
	app := &App{
		DB:             db,
		TenantRepo:     tr,
		TenantService:  tenant.NewService(tr, tenant.NewSlugValidator()),
		SiteConfig:     siteconfig.NewService(scRepo),
		siteConfigRepo: scRepo,
		AgentService:   agent.NewService(agent.NewMemRepo(), nil),
	}
	return app, tr
}

func createTestTenant(t *testing.T, tr *tenantrepo.Repo, slug string) int64 {
	t.Helper()
	tn := &tenant.Tenant{Slug: slug, Name: slug, Status: tenant.StatusActive}
	if err := tr.CreateTenant(context.Background(), tn); err != nil {
		t.Fatalf("create tenant %s: %v", slug, err)
	}
	return tn.ID
}

// siteConfigCtx 造一个已通过 AgentOwnerAuthByUser（ginKeyAgentTenant=ownedTenantID）的请求上下文；
// hostTenant 非 nil 时额外模拟 TenantMiddleware 命中了另一个（Host 解析出的）租户，用来证明
// handler 绝不能读/写到它。
func siteConfigCtx(method, path string, ownedTenantID int64, hostTenant *tenant.Tenant, body string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	c.Request = req
	c.Set(ginKeyAgentTenant, ownedTenantID)
	if hostTenant != nil {
		c.Set(ginKeyTenant, hostTenant)
	}
	return c, w
}

type siteConfigEnvelope struct {
	Success bool `json:"success"`
	Data    struct {
		SiteName string `json:"site_name"`
	} `json:"data"`
}

func decodeSiteConfig(t *testing.T, w *httptest.ResponseRecorder) siteConfigEnvelope {
	t.Helper()
	var env siteConfigEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, w.Body.String())
	}
	return env
}

// TestHandleAgentGetSiteConfig_ScopesToAgentTenantNotHost 是 Fix 2(a) 的回归测试：调用者被
// AgentOwnerAuthByUser 授权的租户是 A，但请求 Host 解析出的租户是 B（不同租户）——GET 必须返回
// A 的装修配置，绝不能读到 B 的（跨租户越权读）。
func TestHandleAgentGetSiteConfig_ScopesToAgentTenantNotHost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, tr := newSiteConfigTestApp(t)
	ctx := context.Background()
	ownedID := createTestTenant(t, tr, "owned")
	hostID := createTestTenant(t, tr, "otherhost")
	if err := app.AgentService.SetAgentType(ctx, ownedID, agent.AgentParams{Level: 1}); err != nil {
		t.Fatalf("seed level: %v", err)
	}
	ownedName, hostName := "owned-brand", "other-brand"
	if err := app.SiteConfig.Patch(ctx, ownedID, siteconfig.SiteConfigPatch{SiteName: &ownedName}); err != nil {
		t.Fatalf("seed owned config: %v", err)
	}
	if err := app.SiteConfig.Patch(ctx, hostID, siteconfig.SiteConfigPatch{SiteName: &hostName}); err != nil {
		t.Fatalf("seed host config: %v", err)
	}
	hostTenant, err := tr.GetTenant(ctx, hostID)
	if err != nil {
		t.Fatalf("get host tenant: %v", err)
	}

	c, w := siteConfigCtx(http.MethodGet, "/api/tenant/site-config", ownedID, hostTenant, "")
	app.HandleAgentGetSiteConfig(c)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	env := decodeSiteConfig(t, w)
	if env.Data.SiteName != ownedName {
		t.Fatalf("site_name = %q, want owned tenant's %q, not Host tenant's %q (Fix 2 cross-tenant read regression)",
			env.Data.SiteName, ownedName, hostName)
	}
}

// TestHandleAgentGetSiteConfig_NoHostTenantStillWorks 覆盖 GET 在无 Host→租户映射（如主站 Host）
// 下仍必须正常工作——回归防止重新引入对 tenantFrom(c) 的依赖。
func TestHandleAgentGetSiteConfig_NoHostTenantStillWorks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, tr := newSiteConfigTestApp(t)
	ctx := context.Background()
	ownedID := createTestTenant(t, tr, "ownedmain")
	if err := app.AgentService.SetAgentType(ctx, ownedID, agent.AgentParams{Level: 1}); err != nil {
		t.Fatalf("seed level: %v", err)
	}

	c, w := siteConfigCtx(http.MethodGet, "/api/tenant/site-config", ownedID, nil /* 无 Host 租户 */, "")
	app.HandleAgentGetSiteConfig(c)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200 even with no Host-resolved tenant; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleAgentUpdateSiteConfig_NoHostTenantNoPanic 是 Fix 2(b) 的回归测试：PUT 在无 Host→租户
// 映射时（tenantFrom(c)==nil，如主站 Host）此前会在 DB 写入之后 nil 解引用 panic。修复后必须
// (1) 不 panic、(2) 200 返回更新后的配置、(3) 写 scope 落在 agentTenantID(c) 的租户上。
func TestHandleAgentUpdateSiteConfig_NoHostTenantNoPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, tr := newSiteConfigTestApp(t)
	ctx := context.Background()
	ownedID := createTestTenant(t, tr, "ownedput")
	if err := app.AgentService.SetAgentType(ctx, ownedID, agent.AgentParams{Level: 1}); err != nil {
		t.Fatalf("seed level: %v", err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("HandleAgentUpdateSiteConfig panicked with no Host-resolved tenant: %v", r)
		}
	}()

	c, w := siteConfigCtx(http.MethodPut, "/api/tenant/site-config", ownedID, nil /* 无 Host 租户 */, `{"site_name":"new-name"}`)
	app.HandleAgentUpdateSiteConfig(c)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	env := decodeSiteConfig(t, w)
	if env.Data.SiteName != "new-name" {
		t.Fatalf("site_name = %q, want %q", env.Data.SiteName, "new-name")
	}
	// 写 scope 必须落在 owner 的租户（agentTenantID），不是别处。
	cfg, found, err := app.siteConfigRepo.GetConfig(ctx, ownedID)
	if err != nil || !found || cfg.SiteName != "new-name" {
		t.Fatalf("owned tenant config not updated as expected: found=%v cfg=%+v err=%v", found, cfg, err)
	}
}

// TestHandleAgentUpdateSiteConfig_ScopesToAgentTenantNotHost 覆盖 PUT 的读写 scope 与鉴权 scope
// 一致：即使 Host 解析出另一个租户 B，写入与响应都必须作用于被授权的租户 A，绝不触碰 B。
func TestHandleAgentUpdateSiteConfig_ScopesToAgentTenantNotHost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, tr := newSiteConfigTestApp(t)
	ctx := context.Background()
	ownedID := createTestTenant(t, tr, "ownedb")
	hostID := createTestTenant(t, tr, "hostb")
	if err := app.AgentService.SetAgentType(ctx, ownedID, agent.AgentParams{Level: 1}); err != nil {
		t.Fatalf("seed level: %v", err)
	}
	hostName := "must-not-change"
	if err := app.SiteConfig.Patch(ctx, hostID, siteconfig.SiteConfigPatch{SiteName: &hostName}); err != nil {
		t.Fatalf("seed host config: %v", err)
	}
	hostTenant, err := tr.GetTenant(ctx, hostID)
	if err != nil {
		t.Fatalf("get host tenant: %v", err)
	}

	c, w := siteConfigCtx(http.MethodPut, "/api/tenant/site-config", ownedID, hostTenant, `{"site_name":"owned-updated"}`)
	app.HandleAgentUpdateSiteConfig(c)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	// 响应必须回显被授权租户（A）的新值，不是 Host 租户（B）的陈旧值——即便写入 scope 本就正确，
	// 响应仍可能（修复前确实会）回显错的那个租户，见 Fix 2(a)。
	env := decodeSiteConfig(t, w)
	if env.Data.SiteName != "owned-updated" {
		t.Fatalf("response site_name = %q, want owned tenant's %q, not Host tenant's stale value (Fix 2 PUT response cross-tenant leak)",
			env.Data.SiteName, "owned-updated")
	}
	// 被授权租户已更新。
	ownedCfg, found, err := app.siteConfigRepo.GetConfig(ctx, ownedID)
	if err != nil || !found || ownedCfg.SiteName != "owned-updated" {
		t.Fatalf("owned tenant config not updated: found=%v cfg=%+v err=%v", found, ownedCfg, err)
	}
	// Host 租户必须原封不动（未被越权写）。
	hostCfg, found, err := app.siteConfigRepo.GetConfig(ctx, hostID)
	if err != nil || !found || hostCfg.SiteName != hostName {
		t.Fatalf("host tenant config must be untouched: found=%v cfg=%+v err=%v", found, hostCfg, err)
	}
}
