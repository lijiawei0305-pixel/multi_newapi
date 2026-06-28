package mtwire

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/modelgroup"
	tenantrepo "github.com/QuantumNous/new-api/internal/tenant/gormrepo"
)

// apiResp 解码 mtwire 统一响应 {success,message,code,data}（断言 success/code 用）。
type apiResp struct {
	Success bool            `json:"success"`
	Code    string          `json:"code"`
	Data    json.RawMessage `json:"data"`
}

// newAgentCtx 构造已通过 AgentOwnerAuth 的 gin 上下文：注入权威租户 + JSON body + 路由参数。
func newAgentCtx(tenantID int64, method, body string, params gin.Params) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(method, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	c.Params = params
	c.Set(ginKeyAgentTenant, tenantID)
	return c, rec
}

func decodeResp(t *testing.T, rec *httptest.ResponseRecorder) apiResp {
	t.Helper()
	var r apiResp
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatalf("decode resp %q: %v", rec.Body.String(), err)
	}
	return r
}

// ---- 代理设层级（PUT /api/tenant/users/:id/tier）----

// newTierApp 装配 App：sqlite + users(id,tenant_id,`group`)。tier 端点只需直读/改 users。
func newTierApp(t *testing.T) *App {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL DEFAULT 0, `group` TEXT NOT NULL DEFAULT 'default')").Error; err != nil {
		t.Fatalf("create users: %v", err)
	}
	return &App{DB: db}
}

func seedTierUser(t *testing.T, app *App, id, tenantID int64, group string) {
	t.Helper()
	if err := app.DB.Exec("INSERT INTO users (id, tenant_id, `group`) VALUES (?,?,?)", id, tenantID, group).Error; err != nil {
		t.Fatalf("seed user %d: %v", id, err)
	}
}

func userGroup(t *testing.T, app *App, id int64) string {
	t.Helper()
	var row struct{ Group string }
	if err := app.DB.Table("users").Select("`group` as `group`").Where("id = ?", id).Take(&row).Error; err != nil {
		t.Fatalf("read group of %d: %v", id, err)
	}
	return row.Group
}

func TestHandleAgentSetUserTier(t *testing.T) {
	// 单测无 Redis：RedisEnabled 默认 true 但 RDB=nil（生产里 true 必伴随已连客户端），
	// 关掉它让 InvalidateUserCache 走空操作分支（与生产「未配 Redis」路径一致）。
	redisRestore := common.RedisEnabled
	common.RedisEnabled = false
	defer func() { common.RedisEnabled = redisRestore }()

	app := newTierApp(t)
	const tenantID = int64(7)
	seedTierUser(t, app, 50, tenantID, "default") // 本租户下级
	seedTierUser(t, app, 60, 0, "default")        // 主站用户（非本租户）
	seedTierUser(t, app, 70, 9, "default")        // 别的租户

	// 1) 合法：本租户用户 50 → vip。
	c, rec := newAgentCtx(tenantID, "PUT", `{"tier":"vip"}`, gin.Params{{Key: "id", Value: "50"}})
	app.HandleAgentSetUserTier(c)
	if r := decodeResp(t, rec); !r.Success {
		t.Fatalf("set tier vip should succeed, got %+v", r)
	}
	if g := userGroup(t, app, 50); g != "vip" {
		t.Fatalf("user 50 group = %q, want vip", g)
	}

	// 2) 越权：用户 60 不属于本租户 → 403。
	c, rec = newAgentCtx(tenantID, "PUT", `{"tier":"vip"}`, gin.Params{{Key: "id", Value: "60"}})
	app.HandleAgentSetUserTier(c)
	if r := decodeResp(t, rec); r.Success || r.Code != "AGENT_FORBIDDEN" {
		t.Fatalf("cross-tenant (main-site) user must be forbidden, got %+v", r)
	}
	if g := userGroup(t, app, 60); g != "default" {
		t.Fatalf("user 60 group must stay default, got %q", g)
	}

	// 3) 越权：别租户用户 70 → 403，且不改组。
	c, rec = newAgentCtx(tenantID, "PUT", `{"tier":"vip"}`, gin.Params{{Key: "id", Value: "70"}})
	app.HandleAgentSetUserTier(c)
	if r := decodeResp(t, rec); r.Success || r.Code != "AGENT_FORBIDDEN" {
		t.Fatalf("other-tenant user must be forbidden, got %+v", r)
	}

	// 4) 非允许层级：svip → 400 AGENT_TIER_INVALID，不改组。
	c, rec = newAgentCtx(tenantID, "PUT", `{"tier":"svip"}`, gin.Params{{Key: "id", Value: "50"}})
	app.HandleAgentSetUserTier(c)
	if r := decodeResp(t, rec); r.Success || r.Code != "AGENT_TIER_INVALID" {
		t.Fatalf("disallowed tier must reject, got %+v", r)
	}
	if g := userGroup(t, app, 50); g != "vip" {
		t.Fatalf("user 50 group must stay vip after rejected svip, got %q", g)
	}

	// 5) 不存在用户 → tenant 不匹配 → 403。
	c, rec = newAgentCtx(tenantID, "PUT", `{"tier":"vip"}`, gin.Params{{Key: "id", Value: "999"}})
	app.HandleAgentSetUserTier(c)
	if r := decodeResp(t, rec); r.Success || r.Code != "AGENT_FORBIDDEN" {
		t.Fatalf("unknown user must be forbidden, got %+v", r)
	}
}

// ---- 代理调模型分组倍率（PUT /api/tenant/groups/:group, GET /api/tenant/groups）----

// newGroupRatioApp 装配 App：sqlite + model_groups + users + tenant_groups + Repos。
func newGroupRatioApp(t *testing.T) *App {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := modelgroup.AutoMigrate(db); err != nil {
		t.Fatalf("model_groups migrate: %v", err)
	}
	if err := db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL DEFAULT 0)`).Error; err != nil {
		t.Fatalf("create users: %v", err)
	}
	if err := tenantrepo.AutoMigrate(db); err != nil {
		t.Fatalf("tenant migrate: %v", err)
	}
	return &App{DB: db, ModelGroupRepo: modelgroup.New(db), TenantRepo: tenantrepo.New(db)}
}

func TestHandleAgentSetGroupRatio(t *testing.T) {
	app := newGroupRatioApp(t)
	ctx := context.Background()
	const tenantID = int64(7)
	if _, err := app.ModelGroupRepo.Create(ctx, modelgroup.ModelGroup{Name: "claude-kiro", Enabled: true}); err != nil {
		t.Fatalf("register claude-kiro: %v", err)
	}
	restore := groupRatioOf
	defer func() { groupRatioOf = restore }()
	groupRatioOf = stub2DRatios // claude-kiro 基准=0.3

	// 1) 合法加价：claude-kiro=0.4（≥0.3）→ 200，落 tenant_groups。
	c, rec := newAgentCtx(tenantID, "PUT", `{"ratio":0.4}`, gin.Params{{Key: "group", Value: "claude-kiro"}})
	app.HandleAgentSetGroupRatio(c)
	if r := decodeResp(t, rec); !r.Success {
		t.Fatalf("markup 0.4 should succeed, got %+v", r)
	}
	if got, found, _ := app.TenantRepo.LookupEnabledGroupRatio(ctx, tenantID, "claude-kiro"); !found || got != 0.4 {
		t.Fatalf("tenant override = (%v,%v), want (0.4,true)", got, found)
	}

	// 2) 击穿下限：claude-kiro=0.2（<0.3）→ 400 RATIO_BELOW_FLOOR。
	c, rec = newAgentCtx(tenantID, "PUT", `{"ratio":0.2}`, gin.Params{{Key: "group", Value: "claude-kiro"}})
	app.HandleAgentSetGroupRatio(c)
	if r := decodeResp(t, rec); r.Success || r.Code != "RATIO_BELOW_FLOOR" {
		t.Fatalf("below floor must reject, got %+v", r)
	}

	// 3) 非模型分组：vip（层级名）→ 400 AGENT_GROUP_NOT_MODEL。
	c, rec = newAgentCtx(tenantID, "PUT", `{"ratio":2.0}`, gin.Params{{Key: "group", Value: "vip"}})
	app.HandleAgentSetGroupRatio(c)
	if r := decodeResp(t, rec); r.Success || r.Code != "AGENT_GROUP_NOT_MODEL" {
		t.Fatalf("non-model group must reject, got %+v", r)
	}

	// 4) 非模型分组：default → 400 AGENT_GROUP_NOT_MODEL（即便 ratio 合法）。
	c, rec = newAgentCtx(tenantID, "PUT", `{"ratio":1.5}`, gin.Params{{Key: "group", Value: "default"}})
	app.HandleAgentSetGroupRatio(c)
	if r := decodeResp(t, rec); r.Success || r.Code != "AGENT_GROUP_NOT_MODEL" {
		t.Fatalf("default (non-model) must reject, got %+v", r)
	}
}

func TestHandleAgentListGroups(t *testing.T) {
	app := newGroupRatioApp(t)
	ctx := context.Background()
	const tenantID = int64(7)
	for _, n := range []string{"claude-kiro", "openai-plus"} {
		if _, err := app.ModelGroupRepo.Create(ctx, modelgroup.ModelGroup{Name: n, Enabled: true}); err != nil {
			t.Fatalf("register %s: %v", n, err)
		}
	}
	restore := groupRatioOf
	defer func() { groupRatioOf = restore }()
	groupRatioOf = func(name string) float64 {
		switch name {
		case "claude-kiro":
			return 0.3
		case "openai-plus":
			return 0.5
		default:
			return 1
		}
	}
	// 租户 7 覆盖 claude-kiro=0.4；openai-plus 无覆盖。
	if err := app.TenantRepo.UpsertGroup(ctx, tenantID, "claude-kiro", 0.4); err != nil {
		t.Fatalf("upsert override: %v", err)
	}

	c, rec := newAgentCtx(tenantID, "GET", "", nil)
	app.HandleAgentListGroups(c)
	r := decodeResp(t, rec)
	if !r.Success {
		t.Fatalf("list groups should succeed, got %+v", r)
	}
	var rows []modelGroupRatioOut
	if err := json.Unmarshal(r.Data, &rows); err != nil {
		t.Fatalf("decode rows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 model groups, got %d (%+v)", len(rows), rows)
	}
	// 升序：claude-kiro 在前（有覆盖），openai-plus 在后（无覆盖）。
	kiro, plus := rows[0], rows[1]
	if kiro.GroupName != "claude-kiro" || !kiro.HasOverride || kiro.Ratio != 0.4 || kiro.PlatformRatio != 0.3 || kiro.Floor != 0.3 {
		t.Fatalf("claude-kiro row wrong: %+v", kiro)
	}
	if plus.GroupName != "openai-plus" || plus.HasOverride || plus.Ratio != 0.5 || plus.PlatformRatio != 0.5 || plus.Floor != 0.5 {
		t.Fatalf("openai-plus row wrong: %+v", plus)
	}
}
