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
	"github.com/QuantumNous/new-api/internal/agent"
	agentrepo "github.com/QuantumNous/new-api/internal/agent/gormrepo"
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

// newGroupRatioApp 装配 App：sqlite + model_groups + users + tenant_groups + agent 四表 + Repos。
// AgentRepo/AgentService 供 ensureAgentLevel（level 门禁）+ consumeFloorRatio（该代理底价，Task 12/
// spec §9.7）用；同一份底层 sqlite 存储，SetAgentType 写入的档位/底价对两者立即可见（无缓存分歧）。
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
	if err := agentrepo.AutoMigrate(db); err != nil {
		t.Fatalf("agent migrate: %v", err)
	}
	ar := agentrepo.New(db)
	return &App{
		DB: db, ModelGroupRepo: modelgroup.New(db), TenantRepo: tenantrepo.New(db),
		AgentRepo: ar, AgentService: agent.NewService(ar, nil),
	}
}

func TestHandleAgentSetGroupRatio(t *testing.T) {
	app := newGroupRatioApp(t)
	ctx := context.Background()
	const tenantID = int64(7)
	if err := app.AgentService.SetAgentType(ctx, tenantID, agent.AgentParams{Level: 1}); err != nil { // Task 12 门禁：需 L1
		t.Fatalf("set L1: %v", err)
	}
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

// TestHandleAgentSetGroupRatio_RequiresLevel1 覆盖分层门禁（spec §9.5）：L0（普通档）设卖价 →
// 403 AGENT_LEVEL_LOCKED，且不落 tenant_groups；L1（独立档）→ 200 放行（既有校验——下限/仅模型
// 分组——照常生效）。
func TestHandleAgentSetGroupRatio_RequiresLevel1(t *testing.T) {
	app := newGroupRatioApp(t)
	ctx := context.Background()
	if _, err := app.ModelGroupRepo.Create(ctx, modelgroup.ModelGroup{Name: "claude-kiro", Enabled: true}); err != nil {
		t.Fatalf("register claude-kiro: %v", err)
	}
	restore := groupRatioOf
	defer func() { groupRatioOf = restore }()
	groupRatioOf = stub2DRatios

	if err := app.AgentService.SetAgentType(ctx, 70, agent.AgentParams{Level: 0}); err != nil {
		t.Fatalf("set L0: %v", err)
	}
	if err := app.AgentService.SetAgentType(ctx, 71, agent.AgentParams{Level: 1}); err != nil {
		t.Fatalf("set L1: %v", err)
	}

	c, rec := newAgentCtx(70, "PUT", `{"ratio":0.4}`, gin.Params{{Key: "group", Value: "claude-kiro"}})
	app.HandleAgentSetGroupRatio(c)
	if r := decodeResp(t, rec); r.Success || r.Code != "AGENT_LEVEL_LOCKED" {
		t.Fatalf("L0 must be locked, got %+v", r)
	}
	if _, found, _ := app.TenantRepo.LookupEnabledGroupRatio(ctx, 70, "claude-kiro"); found {
		t.Fatal("L0 must not have persisted a group override")
	}

	c, rec = newAgentCtx(71, "PUT", `{"ratio":0.4}`, gin.Params{{Key: "group", Value: "claude-kiro"}})
	app.HandleAgentSetGroupRatio(c)
	if r := decodeResp(t, rec); !r.Success {
		t.Fatalf("L1 should be allowed, got %+v", r)
	}
}

// TestHandleAgentSetGroupRatio_FloorUsesAgentBottomPriceRatio 是本任务的核心用例（spec §9.7）：
// 一旦该代理配置了 BottomPriceRatio，卖价下限改用它而非全局平台基准——即便该值高于平台基准，
// 曾经合法的加价现在也可能被挡（下限收紧，不是放宽）。
func TestHandleAgentSetGroupRatio_FloorUsesAgentBottomPriceRatio(t *testing.T) {
	app := newGroupRatioApp(t)
	ctx := context.Background()
	const tenantID = int64(72)
	if _, err := app.ModelGroupRepo.Create(ctx, modelgroup.ModelGroup{Name: "claude-kiro", Enabled: true}); err != nil {
		t.Fatalf("register claude-kiro: %v", err)
	}
	restore := groupRatioOf
	defer func() { groupRatioOf = restore }()
	groupRatioOf = stub2DRatios // claude-kiro 平台基准 = 0.3

	// 该代理底价 0.5（高于平台基准 0.3）。
	if err := app.AgentService.SetAgentType(ctx, tenantID, agent.AgentParams{Level: 1, BottomPriceRatio: 0.5}); err != nil {
		t.Fatalf("set L1 with bottom price ratio: %v", err)
	}

	// 0.4：曾经（对平台基准=0.3 而言）合法，但现在 < 该代理底价 0.5 → 拒。
	c, rec := newAgentCtx(tenantID, "PUT", `{"ratio":0.4}`, gin.Params{{Key: "group", Value: "claude-kiro"}})
	app.HandleAgentSetGroupRatio(c)
	if r := decodeResp(t, rec); r.Success || r.Code != "RATIO_BELOW_FLOOR" {
		t.Fatalf("0.4 must be rejected by the agent's own 0.5 floor, got %+v", r)
	}

	// 0.5：等于该代理底价 → 放行（边界=允许，同 pricing.Guard.ValidateGroupRatio 的既有语义）。
	c, rec = newAgentCtx(tenantID, "PUT", `{"ratio":0.5}`, gin.Params{{Key: "group", Value: "claude-kiro"}})
	app.HandleAgentSetGroupRatio(c)
	if r := decodeResp(t, rec); !r.Success {
		t.Fatalf("0.5 (== agent floor) should be admitted, got %+v", r)
	}
}

// TestHandleAgentListGroups_FloorReflectsAgentBottomPriceRatio 覆盖 spec §9.7 的展示口径：
// Floor 字段跟随该代理的 BottomPriceRatio；PlatformRatio 保持平台基准不变（两者语义分离）。
func TestHandleAgentListGroups_FloorReflectsAgentBottomPriceRatio(t *testing.T) {
	app := newGroupRatioApp(t)
	ctx := context.Background()
	const tenantID = int64(73)
	if _, err := app.ModelGroupRepo.Create(ctx, modelgroup.ModelGroup{Name: "claude-kiro", Enabled: true}); err != nil {
		t.Fatalf("register claude-kiro: %v", err)
	}
	restore := groupRatioOf
	defer func() { groupRatioOf = restore }()
	groupRatioOf = stub2DRatios // claude-kiro 平台基准 = 0.3
	if err := app.AgentService.SetAgentType(ctx, tenantID, agent.AgentParams{Level: 1, BottomPriceRatio: 0.6}); err != nil {
		t.Fatalf("set L1 with bottom price ratio: %v", err)
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
	if len(rows) != 1 {
		t.Fatalf("want 1 model group, got %d (%+v)", len(rows), rows)
	}
	row := rows[0]
	if row.PlatformRatio != 0.3 {
		t.Fatalf("PlatformRatio must stay at platform baseline, got %v", row.PlatformRatio)
	}
	if row.Floor != 0.6 {
		t.Fatalf("Floor must reflect the agent's own BottomPriceRatio, got %v, want 0.6", row.Floor)
	}
	if row.Ratio != 0.3 {
		t.Fatalf("Ratio (effective price, no override set) must still be platform baseline, got %v", row.Ratio)
	}
}

// TestHandleAgentSetTierRatio_RequiresLevel1AndValidTier 覆盖 Change 1 的门禁 + 校验（spec agent-tiering
// §9.6.1）：L0 → 403 AGENT_LEVEL_LOCKED；"default"/模型分组名 → 400 AGENT_TIER_INVALID；
// "vip" + L1 → 200，落 tenant_groups[tenant, tierGroupKey("vip")]（与卖价覆盖同表，前缀隔离命名空间）。
func TestHandleAgentSetTierRatio_RequiresLevel1AndValidTier(t *testing.T) {
	app := newGroupRatioApp(t)
	ctx := context.Background()
	if _, err := app.ModelGroupRepo.Create(ctx, modelgroup.ModelGroup{Name: "claude-kiro", Enabled: true}); err != nil {
		t.Fatalf("register claude-kiro: %v", err)
	}
	if err := app.AgentService.SetAgentType(ctx, 80, agent.AgentParams{Level: 0}); err != nil {
		t.Fatalf("set L0: %v", err)
	}
	if err := app.AgentService.SetAgentType(ctx, 81, agent.AgentParams{Level: 1}); err != nil {
		t.Fatalf("set L1: %v", err)
	}

	// L0：锁。
	c, rec := newAgentCtx(80, "PUT", `{"ratio":0.7}`, gin.Params{{Key: "tier", Value: "vip"}})
	app.HandleAgentSetTierRatio(c)
	if r := decodeResp(t, rec); r.Success || r.Code != "AGENT_LEVEL_LOCKED" {
		t.Fatalf("L0 must be locked, got %+v", r)
	}

	// L1 + "default"：拒（default 恒 1，不可覆盖）。
	c, rec = newAgentCtx(81, "PUT", `{"ratio":0.7}`, gin.Params{{Key: "tier", Value: "default"}})
	app.HandleAgentSetTierRatio(c)
	if r := decodeResp(t, rec); r.Success || r.Code != "AGENT_TIER_INVALID" {
		t.Fatalf("default tier must be rejected, got %+v", r)
	}

	// L1 + 模型分组名（"claude-kiro"）：拒（两轴互斥，防串号）。
	c, rec = newAgentCtx(81, "PUT", `{"ratio":0.7}`, gin.Params{{Key: "tier", Value: "claude-kiro"}})
	app.HandleAgentSetTierRatio(c)
	if r := decodeResp(t, rec); r.Success || r.Code != "AGENT_TIER_INVALID" {
		t.Fatalf("model-group name must be rejected on the tier axis, got %+v", r)
	}

	// L1 + "vip"：放行，落库到 tierGroupKey 隔离的命名空间。
	c, rec = newAgentCtx(81, "PUT", `{"ratio":0.7}`, gin.Params{{Key: "tier", Value: "vip"}})
	app.HandleAgentSetTierRatio(c)
	if r := decodeResp(t, rec); !r.Success {
		t.Fatalf("L1 vip should be allowed, got %+v", r)
	}
	got, found, err := app.TenantRepo.LookupEnabledGroupRatio(ctx, 81, tierGroupKey("vip"))
	if err != nil || !found || got != 0.7 {
		t.Fatalf("tenant_groups[81,%q] = (%v,%v,%v), want (0.7,true,nil)", tierGroupKey("vip"), got, found, err)
	}
	// 隔离验证：模型分组轴的裸 "vip" 键必须不存在（两轴不串号）。
	if _, found, _ := app.TenantRepo.LookupEnabledGroupRatio(ctx, 81, "vip"); found {
		t.Fatal("bare \"vip\" key must not be written — tier overrides must use the tierGroupKey-prefixed namespace")
	}
}

// TestHandleAgentListTierRatios_ReflectsOverrideOrDefaultOne 覆盖列表展示：未覆盖展示 1（不是平台参考
// 值，避免 UI 暗示"没设=用平台的"）；已覆盖展示覆盖值 + has_override=true。
func TestHandleAgentListTierRatios_ReflectsOverrideOrDefaultOne(t *testing.T) {
	app := newGroupRatioApp(t)
	ctx := context.Background()
	restore := groupRatioOf
	defer func() { groupRatioOf = restore }()
	groupRatioOf = stub2DRatios // 平台参考 vip=0.8
	if err := app.AgentService.SetAgentType(ctx, 82, agent.AgentParams{Level: 1}); err != nil {
		t.Fatalf("set L1: %v", err)
	}

	c, rec := newAgentCtx(82, "GET", "", nil)
	app.HandleAgentListTierRatios(c)
	r := decodeResp(t, rec)
	var rows []tierRatioOut
	if err := json.Unmarshal(r.Data, &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 1 || rows[0].Tier != "vip" || rows[0].Ratio != 1 || rows[0].HasOverride {
		t.Fatalf("no override yet: want [{vip,1,false}], got %+v", rows)
	}

	if err := app.TenantRepo.UpsertGroup(ctx, 82, tierGroupKey("vip"), 0.6); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	c, rec = newAgentCtx(82, "GET", "", nil)
	app.HandleAgentListTierRatios(c)
	r = decodeResp(t, rec)
	if err := json.Unmarshal(r.Data, &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 1 || rows[0].Ratio != 0.6 || !rows[0].HasOverride || rows[0].PlatformRatio != 0.8 {
		t.Fatalf("after override: want [{vip,0.6,true,platform=0.8}], got %+v", rows)
	}
}
