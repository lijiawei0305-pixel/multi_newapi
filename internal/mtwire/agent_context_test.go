package mtwire

// Fix 1（严重）回归测试：HandleAgentContext / isAgentOwner 此前从 tenantFrom(c)（Host 解析）判定
// 代理 owner 身份。L0 代理没有子域名，任何 Host 都解析不到它们的租户，导致 is_agent_owner 永远
// false —— 整个前端代理自助 UI（侧栏 + 10 处路由守卫，均以 /api/tenant/agent-context 的
// is_agent_owner 为门控）对 L0 不可达，即便后端本会放行其请求（AgentOwnerAuthByUser 已是
// owner-based、Host 无关）。修复：改用 a.TenantRepo.TenantByOwner 解析「当前用户拥有的租户」，
// 与 Host 无关，镜像 AgentOwnerAuthByUser 的口径。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/agent"
	agentrepo "github.com/QuantumNous/new-api/internal/agent/gormrepo"
	tenantrepo "github.com/QuantumNous/new-api/internal/tenant/gormrepo"
)

// newAgentContextTestApp 造最小 App：sqlite tenants（owner→tenant 反查）+ agent_profiles（level/can_api）。
func newAgentContextTestApp(t *testing.T) *App {
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
	if err := agentrepo.AutoMigrate(db); err != nil {
		t.Fatalf("agent migrate: %v", err)
	}
	return &App{DB: db, TenantRepo: tenantrepo.New(db), AgentRepo: agentrepo.New(db)}
}

// agentContextCtx 造一个「已登录(id=userID)、主站 Host、未经 TenantMiddleware」的 gin 上下文
// （不设 ginKeyTenant，模拟 Host 解析不到任何租户——覆盖 L0 无子域名 / 主站 Host 的场景）。
func agentContextCtx(userID int64) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/api/tenant/agent-context", nil)
	req.Host = "api.wedreamhub.com" // 主站 Host：无租户映射（ginKeyTenant 故意不设）
	c.Request = req
	if userID > 0 {
		c.Set("id", int(userID))
	}
	return c, w
}

type agentContextEnvelope struct {
	Success bool `json:"success"`
	Data    struct {
		IsAgentOwner bool `json:"is_agent_owner"`
		Level        int  `json:"level"`
		CanAPI       bool `json:"can_api"`
	} `json:"data"`
}

func decodeAgentContext(t *testing.T, w *httptest.ResponseRecorder) agentContextEnvelope {
	t.Helper()
	var env agentContextEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, w.Body.String())
	}
	return env
}

// TestHandleAgentContext_L0OwnerReachableWithoutHost 是 Fix 1 的核心回归测试：L0 代理没有子域名，
// 门控信号端点必须仅凭「登录用户拥有的租户」（owner-based，TenantByOwner）就判定 is_agent_owner，
// 与 Host 无关——即便命中主站 Host（无 Host→租户映射），前端代理自助 UI 才对 L0 可达。
func TestHandleAgentContext_L0OwnerReachableWithoutHost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := newAgentContextTestApp(t)
	ctx := context.Background()
	tid := seedOwnedTenant(t, app, "l0shop", 100) // 用户 100 拥有一个 L0（普通档）租户
	if err := app.AgentRepo.SetAgentType(ctx, tid, agent.AgentParams{Level: 0, CanAPI: false}); err != nil {
		t.Fatalf("seed agent profile: %v", err)
	}

	c, w := agentContextCtx(100)
	app.HandleAgentContext(c)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200 (always-200 contract); body=%s", w.Code, w.Body.String())
	}
	env := decodeAgentContext(t, w)
	if !env.Success {
		t.Fatalf("success = false; body=%s", w.Body.String())
	}
	if !env.Data.IsAgentOwner {
		t.Fatalf("is_agent_owner = false for L0 owner on main-site Host, want true (Fix 1 regression: L0 console unreachable)")
	}
	if env.Data.Level != 0 {
		t.Fatalf("level = %d, want 0", env.Data.Level)
	}
	if env.Data.CanAPI {
		t.Fatalf("can_api = true, want false")
	}
}

// TestHandleAgentContext_L1OwnerLevelAndCanAPI 覆盖 L1 owner：level/can_api 必须取自其拥有的租户
// （owner-based 解析），验证修复没有把 Level/CanAPI 读丢。
func TestHandleAgentContext_L1OwnerLevelAndCanAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := newAgentContextTestApp(t)
	ctx := context.Background()
	tid := seedOwnedTenant(t, app, "l1shop", 200)
	if err := app.AgentRepo.SetAgentType(ctx, tid, agent.AgentParams{Level: 1, CanAPI: true}); err != nil {
		t.Fatalf("seed agent profile: %v", err)
	}

	c, w := agentContextCtx(200)
	app.HandleAgentContext(c)

	env := decodeAgentContext(t, w)
	if !env.Data.IsAgentOwner || env.Data.Level != 1 || !env.Data.CanAPI {
		t.Fatalf("got %+v, want is_agent_owner=true level=1 can_api=true", env.Data)
	}
}

// TestHandleAgentContext_NonOwnerFalse 覆盖非 owner（已登录但未拥有任何租户）：is_agent_owner=false，
// 且永远 200（不 abort）——与 spec 的 fail-safe 契约一致，前端门控信号端点绝不能中止请求。
func TestHandleAgentContext_NonOwnerFalse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := newAgentContextTestApp(t)
	_ = seedOwnedTenant(t, app, "someoneelse", 100) // 占位：仓储非空不影响断言

	c, w := agentContextCtx(999) // 已登录但不拥有任何租户
	app.HandleAgentContext(c)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200 (always-200 contract, never abort); body=%s", w.Code, w.Body.String())
	}
	env := decodeAgentContext(t, w)
	if env.Data.IsAgentOwner {
		t.Fatalf("is_agent_owner = true for non-owner, want false")
	}
	if env.Data.Level != 0 || env.Data.CanAPI {
		t.Fatalf("got %+v, want zero-value level/can_api for non-owner", env.Data)
	}
}

// TestHandleAgentContext_NotLoggedInFalse 覆盖未登录（id 缺失/<=0）：is_agent_owner=false，仍 200。
func TestHandleAgentContext_NotLoggedInFalse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := newAgentContextTestApp(t)

	c, w := agentContextCtx(0) // 未登录：不设 "id"
	app.HandleAgentContext(c)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if env := decodeAgentContext(t, w); env.Data.IsAgentOwner {
		t.Fatalf("is_agent_owner = true for not-logged-in caller, want false")
	}
}
