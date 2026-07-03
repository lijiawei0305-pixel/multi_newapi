package mtwire

import (
	"context"
	"reflect"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/agent"
	agentrepo "github.com/QuantumNous/new-api/internal/agent/gormrepo"
	"github.com/QuantumNous/new-api/internal/modelgroup"
	tenantrepo "github.com/QuantumNous/new-api/internal/tenant/gormrepo"
)

// newModelGroup2DApp 装配最小 App：sqlite(:memory:) + model_groups 表 + ModelGroupRepo。
func newModelGroup2DApp(t *testing.T) *App {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := modelgroup.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &App{DB: db, ModelGroupRepo: modelgroup.New(db)}
}

// newModelGroup2DTenantApp 在 newModelGroup2DApp 基础上加 users(id,tenant_id) + tenant_groups +
// TenantRepo + agent_profiles + AgentRepo/AgentService，供「代理 per-tenant 覆盖」(模型分组轴，既有)
// 与「代理自设 vip 力度」(层级轴，Change 1/spec §9.6.1)两类用例共用——resolveModelGroup2D 两条轴都要
// 解析 userID→租户→level/tenant_groups 覆盖。
func newModelGroup2DTenantApp(t *testing.T) *App {
	t.Helper()
	app := newModelGroup2DApp(t)
	if err := app.DB.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL DEFAULT 0)`).Error; err != nil {
		t.Fatalf("create users table: %v", err)
	}
	if err := tenantrepo.AutoMigrate(app.DB); err != nil { // tenants/tenant_domains/tenant_groups
		t.Fatalf("tenant migrate: %v", err)
	}
	app.TenantRepo = tenantrepo.New(app.DB)
	if err := agentrepo.AutoMigrate(app.DB); err != nil { // agent_profiles + agent_earning_logs + wallet
		t.Fatalf("agent migrate: %v", err)
	}
	ar := agentrepo.New(app.DB)
	app.AgentRepo = ar
	app.AgentService = agent.NewService(ar, nil)
	return app
}

func almostEqual(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-9
}

// stub2DRatios 注入 default=1、vip=0.8、svip=0.6、claude-kiro=0.3，其余 1（避免污染全局 ratio_setting）。
func stub2DRatios(name string) float64 {
	switch name {
	case "default":
		return 1
	case "vip":
		return 0.8
	case "svip":
		return 0.6
	case "claude-kiro":
		return 0.3
	default:
		return 1
	}
}

// TestResolveModelGroup2D 覆盖二维相乘（无租户覆盖，platform 基准）：
//
//	default×1、default×claude-kiro=0.3、vip×claude-kiro=0.24、vip 仅层级、层级名作 usingGroup 不重复算、未登记系数 1。
func TestResolveModelGroup2D(t *testing.T) {
	ctx := context.Background()
	app := newModelGroup2DApp(t)
	// 仅登记 claude-kiro 为模型分组（enabled）。
	if _, err := app.ModelGroupRepo.Create(ctx, modelgroup.ModelGroup{Name: "claude-kiro", Enabled: true}); err != nil {
		t.Fatalf("register claude-kiro: %v", err)
	}

	restore := groupRatioOf
	defer func() { groupRatioOf = restore }()
	groupRatioOf = stub2DRatios

	cases := []struct {
		user, using string
		want        float64
		note        string
	}{
		{"default", "", 1, "default × 1（无 usingGroup）"},
		{"default", "claude-kiro", 0.3, "default(1) × kiro(0.3)"},
		{"vip", "claude-kiro", 0.24, "vip(0.8) × kiro(0.3) = 0.24"},
		{"vip", "", 0.8, "vip 仅层级"},
		{"vip", "vip", 0.8, "层级名作 usingGroup（vip 非模型分组）→ 系数 1，不重复算"},
		{"vip", "unregistered", 0.8, "未登记模型分组 → 系数 1，仅层级"},
		{"default", "default", 1, "default×default：default 非模型分组 → 系数 1"},
	}
	for _, c := range cases {
		// userID=0：主站维度，无租户覆盖，走 platform 基准。
		r, ok := app.resolveModelGroup2D(0, c.user, c.using)
		if !ok || !almostEqual(r, c.want) {
			t.Fatalf("resolve(0,%q,%q) = (%v,%v), want (%v,true) — %s", c.user, c.using, r, ok, c.want, c.note)
		}
	}
}

// TestResolveModelGroup2D_TenantOverride 覆盖「代理 per-tenant 覆盖」组合：
// 租户对模型分组的覆盖叠入 modelFactor（命中=覆盖值、否则平台基准）；覆盖仅对模型分组生效，
// 非模型分组（层级名 / 未登记）一律系数 1（不再受租户对 default 的遗留 markup 影响）。
func TestResolveModelGroup2D_TenantOverride(t *testing.T) {
	ctx := context.Background()
	app := newModelGroup2DTenantApp(t)
	if _, err := app.ModelGroupRepo.Create(ctx, modelgroup.ModelGroup{Name: "claude-kiro", Enabled: true}); err != nil {
		t.Fatalf("register claude-kiro: %v", err)
	}
	restore := groupRatioOf
	defer func() { groupRatioOf = restore }()
	groupRatioOf = stub2DRatios

	// 用户 50 → 租户 7（有覆盖）；用户 60 → 主站（tenant 0，无覆盖）。
	seedUser(t, app, 50, 7)
	seedUser(t, app, 60, 0)
	// 租户 7：claude-kiro 覆盖为 0.4（≥ 基准 0.3，加价）；default 设遗留 markup 1.5（应被忽略：非模型分组）。
	if err := app.TenantRepo.UpsertGroup(ctx, 7, "claude-kiro", 0.4); err != nil {
		t.Fatalf("upsert kiro override: %v", err)
	}
	if err := app.TenantRepo.UpsertGroup(ctx, 7, "default", 1.5); err != nil {
		t.Fatalf("upsert default markup: %v", err)
	}
	// Level 门禁 + Change 1(vip 力度覆盖，spec §9.6.1):租户 7 设为 L1，并显式设 vip 覆盖 = 0.8
	// (与平台全局 vip 基准数值相同，仅为让本测试原有断言在"Default=1"新语义下继续成立——
	// "未设覆盖时不再回退平台全局"这一新行为由 TestResolveModelGroup2D_AgentVipOverride_DefaultsToOne
	// 单独覆盖)。
	if err := app.AgentService.SetAgentType(ctx, 7, agent.AgentParams{Level: 1}); err != nil {
		t.Fatalf("set tenant 7 to L1: %v", err)
	}
	if err := app.TenantRepo.UpsertGroup(ctx, 7, tierGroupKey("vip"), 0.8); err != nil {
		t.Fatalf("upsert vip tier override: %v", err)
	}

	cases := []struct {
		userID      int64
		user, using string
		want        float64
		note        string
	}{
		{50, "default", "claude-kiro", 0.4, "default(1) × 覆盖(0.4) = 0.4"},
		{50, "vip", "claude-kiro", 0.32, "vip(0.8) × 覆盖(0.4) = 0.32"},
		{60, "default", "claude-kiro", 0.3, "主站用户无覆盖 → 基准 0.3"},
		{60, "vip", "claude-kiro", 0.24, "主站用户无覆盖 → vip×基准 = 0.24"},
		{50, "vip", "", 0.8, "无 usingGroup → 仅层级（覆盖不参与）"},
		{50, "vip", "default", 0.8, "default 非模型分组 → 遗留 markup(1.5) 被忽略，仅层级 vip=0.8"},
		{50, "default", "openai-plus", 1, "openai-plus 未登记 → 系数 1（无覆盖路径）"},
	}
	for _, c := range cases {
		r, ok := app.resolveModelGroup2D(c.userID, c.user, c.using)
		if !ok || !almostEqual(r, c.want) {
			t.Fatalf("resolve(%d,%q,%q) = (%v,%v), want (%v,true) — %s", c.userID, c.user, c.using, r, ok, c.want, c.note)
		}
	}
}

// TestResolveModelGroup2D_AgentVipOverride_DefaultsToOne 覆盖 Change 1 的核心新行为(spec agent-tiering
// §9.6.1):"No overlap" —— L1(独立档)代理下级用户的层级折扣只能来自该代理自己的覆盖，从不回退平台
// 全局 GroupRatio['vip']；未配置覆盖 → tier=1(无折扣)。与此相对：① 主站直客(tenant_id=0)不受影响，
// 继续吃平台全局；② L0(普通档)代理下级用户也不受影响，继续吃平台全局(§9.6.1 标注为一处需确认的范围
// 判断——L0 没有 HandleAgentSetTierRatio 的调用权限，本实现选择让 L0 保持 v2 行为完全不变)。
func TestResolveModelGroup2D_AgentVipOverride_DefaultsToOne(t *testing.T) {
	ctx := context.Background()
	app := newModelGroup2DTenantApp(t)
	if _, err := app.ModelGroupRepo.Create(ctx, modelgroup.ModelGroup{Name: "claude-kiro", Enabled: true}); err != nil {
		t.Fatalf("register claude-kiro: %v", err)
	}
	restore := groupRatioOf
	defer func() { groupRatioOf = restore }()
	groupRatioOf = stub2DRatios // 平台全局 vip=0.8

	if err := app.AgentService.SetAgentType(ctx, 70, agent.AgentParams{Level: 1}); err != nil { // L1，未设 vip 覆盖
		t.Fatalf("set tenant 70 to L1: %v", err)
	}
	if err := app.AgentService.SetAgentType(ctx, 71, agent.AgentParams{Level: 0}); err != nil { // L0
		t.Fatalf("set tenant 71 to L0: %v", err)
	}
	seedUser(t, app, 500, 70) // L1 代理下级，未配置 vip 覆盖
	seedUser(t, app, 510, 71) // L0 代理下级
	seedUser(t, app, 600, 0)  // 主站直客

	cases := []struct {
		userID      int64
		user, using string
		want        float64
		note        string
	}{
		{500, "vip", "", 1, "L1 代理下级 vip 用户，代理未设覆盖 → Default=1(不回退平台全局 0.8)"},
		{510, "vip", "", 0.8, "L0 代理下级：无自设覆盖能力，行为不变，继续吃平台全局 0.8"},
		{600, "vip", "", 0.8, "主站直客：不受影响，继续吃平台全局 0.8"},
		{500, "default", "", 1, "default 层级本就不可代理覆盖，行为不变(=1)"},
		{500, "vip", "claude-kiro", 1 * stub2DRatios("claude-kiro"), "tier=1(未覆盖) × 模型分组基准(claude-kiro 未覆盖)=0.3"},
	}
	for _, c := range cases {
		r, ok := app.resolveModelGroup2D(c.userID, c.user, c.using)
		if !ok || !almostEqual(r, c.want) {
			t.Fatalf("resolve(%d,%q,%q) = (%v,%v), want (%v,true) — %s", c.userID, c.user, c.using, r, ok, c.want, c.note)
		}
	}

	// 租户 70 随后自设 vip 力度 0.5(比平台全局 0.8 折扣更深)→ 立即生效，且只影响自己的下级。
	if err := app.TenantRepo.UpsertGroup(ctx, 70, tierGroupKey("vip"), 0.5); err != nil {
		t.Fatalf("upsert vip tier override: %v", err)
	}
	if r, ok := app.resolveModelGroup2D(500, "vip", ""); !ok || !almostEqual(r, 0.5) {
		t.Fatalf("after override: resolve(500,vip,\"\") = (%v,%v), want (0.5,true)", r, ok)
	}
	if r, ok := app.resolveModelGroup2D(510, "vip", ""); !ok || !almostEqual(r, 0.8) {
		t.Fatalf("L0 tenant 71 must stay unaffected by tenant 70's override: got %v", r)
	}
	if r, ok := app.resolveModelGroup2D(600, "vip", ""); !ok || !almostEqual(r, 0.8) {
		t.Fatalf("main-site user must stay unaffected by tenant 70's override: got %v", r)
	}
}

// TestResolveModelGroup2D_NilRepoFallback 验证未装配（ModelGroupRepo=nil）→ (0,false)，回退原生倍率。
func TestResolveModelGroup2D_NilRepoFallback(t *testing.T) {
	app := &App{}
	if r, ok := app.resolveModelGroup2D(0, "vip", "claude-kiro"); ok || r != 0 {
		t.Fatalf("nil repo = (%v,%v), want (0,false)", r, ok)
	}
}

// TestResolveModelGroup2D_PanicRecover 验证内部 panic 被兜底 → (0,false)，绝不阻断/破坏计费。
func TestResolveModelGroup2D_PanicRecover(t *testing.T) {
	app := newModelGroup2DApp(t)
	restore := groupRatioOf
	defer func() { groupRatioOf = restore }()
	groupRatioOf = func(string) float64 { panic("boom") }
	if r, ok := app.resolveModelGroup2D(0, "vip", "claude-kiro"); ok || r != 0 {
		t.Fatalf("panic recover = (%v,%v), want (0,false)", r, ok)
	}
}

// seedChannel 在 sqlite 插一条 channels 行（id,name,group）。group 为保留字，按 sqlite 双引号引用。
func seedChannel(t *testing.T, app *App, id int64, name, group string) {
	t.Helper()
	if err := app.DB.Exec(`INSERT INTO channels (id, name, "group") VALUES (?, ?, ?)`, id, name, group).Error; err != nil {
		t.Fatalf("seed channel %d: %v", id, err)
	}
}

// TestServingChannelsByGroup 验证「服务渠道」反推：渠道↔模型分组多对一，按 channels.group（逗号分隔、含空白）归集。
// 同时验证 channelGroupColumn 的 sqlite 引号正确（否则查询报错 → 空 map → 断言失败）。
func TestServingChannelsByGroup(t *testing.T) {
	ctx := context.Background()
	app := newModelGroup2DApp(t)
	if err := app.DB.Exec(`CREATE TABLE channels (id INTEGER PRIMARY KEY, name TEXT, "group" TEXT)`).Error; err != nil {
		t.Fatalf("create channels: %v", err)
	}
	seedChannel(t, app, 1, "codex", "openai-plus")
	seedChannel(t, app, 2, "codex2", "openai-plus,default") // 一个渠道服务多个分组
	seedChannel(t, app, 3, "kiro", "claude-kiro")
	seedChannel(t, app, 4, "misc", "default")                    // 不服务任何被查分组
	seedChannel(t, app, 5, "spaced", "openai-plus, claude-kiro") // 逗号后带空格，应被 TrimSpace 命中

	got := app.servingChannelsByGroup(ctx, []string{"openai-plus", "claude-kiro"})

	wantOpenai := []servingChannel{{ID: 1, Name: "codex"}, {ID: 2, Name: "codex2"}, {ID: 5, Name: "spaced"}}
	wantKiro := []servingChannel{{ID: 3, Name: "kiro"}, {ID: 5, Name: "spaced"}}
	if !reflect.DeepEqual(got["openai-plus"], wantOpenai) {
		t.Fatalf("openai-plus serving = %+v, want %+v", got["openai-plus"], wantOpenai)
	}
	if !reflect.DeepEqual(got["claude-kiro"], wantKiro) {
		t.Fatalf("claude-kiro serving = %+v, want %+v", got["claude-kiro"], wantKiro)
	}
	// 未被查询的 default 不出现在结果里。
	if _, ok := got["default"]; ok {
		t.Fatalf("unqueried group 'default' must not appear: %+v", got)
	}
}

// TestServingChannelsByGroup_Empty 验证无分组名 / channels 表缺失时安全给空（不 panic）。
func TestServingChannelsByGroup_Empty(t *testing.T) {
	ctx := context.Background()
	app := newModelGroup2DApp(t)
	if got := app.servingChannelsByGroup(ctx, nil); len(got) != 0 {
		t.Fatalf("nil names = %v, want empty", got)
	}
	// channels 表不存在 → 查询出错 → 给空（展示字段非关键路径）。
	if got := app.servingChannelsByGroup(ctx, []string{"openai-plus"}); len(got) != 0 {
		t.Fatalf("missing channels table = %v, want empty", got)
	}
}
