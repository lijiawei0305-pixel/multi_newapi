package mtwire

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

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

// newModelGroup2DTenantApp 在 newModelGroup2DApp 基础上加 users(id,tenant_id) + tenant_groups + TenantRepo，
// 供「代理 per-tenant 覆盖」用例（resolveModelGroup2D 解析 userID→租户→tenant_groups 覆盖）。
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
