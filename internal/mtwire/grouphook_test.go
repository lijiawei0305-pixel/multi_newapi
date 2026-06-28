package mtwire

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	tenantrepo "github.com/QuantumNous/new-api/internal/tenant/gormrepo"
)

// newGroupHookTestApp 装配最小 App：sqlite(:memory:) + 原生 users 表(id,tenant_id) + tenant_groups 表。
func newGroupHookTestApp(t *testing.T) *App {
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
	// 原生 users 表（仅本测试所需列）：模拟 new-api users + 我们迁移加的 tenant_id 列。
	if err := db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL DEFAULT 0)`).Error; err != nil {
		t.Fatalf("create users table: %v", err)
	}
	if err := tenantrepo.AutoMigrate(db); err != nil { // 建 tenants/tenant_domains/tenant_groups
		t.Fatalf("tenant migrate: %v", err)
	}
	return &App{DB: db, TenantRepo: tenantrepo.New(db)}
}

func seedUser(t *testing.T, app *App, userID, tenantID int64) {
	t.Helper()
	if err := app.DB.Exec(`INSERT INTO users (id, tenant_id) VALUES (?, ?)`, userID, tenantID).Error; err != nil {
		t.Fatalf("seed user %d: %v", userID, err)
	}
}

// TestResolveTenantGroupRatio 覆盖：命中租户覆盖、同租户无覆盖回退、主站用户(tenant_id=0)回退、
// 未知用户回退、入参非法回退。
func TestResolveTenantGroupRatio(t *testing.T) {
	ctx := context.Background()
	app := newGroupHookTestApp(t)

	seedUser(t, app, 100, 5) // 用户 100 → 租户 5
	seedUser(t, app, 200, 0) // 主站用户 200 → 无租户
	if err := app.TenantRepo.UpsertGroup(ctx, 5, "vip", 1.8); err != nil {
		t.Fatalf("upsert tenant group: %v", err)
	}

	// 命中租户(enabled)覆盖 → 用租户倍率。
	if r, ok := app.resolveTenantGroupRatio(ctx, 100, "vip"); !ok || r != 1.8 {
		t.Fatalf("hit = (%v,%v), want (1.8,true)", r, ok)
	}
	// 同租户但该组无覆盖 → 回退全局（false）。
	if r, ok := app.resolveTenantGroupRatio(ctx, 100, "default"); ok || r != 0 {
		t.Fatalf("no-override = (%v,%v), want (0,false)", r, ok)
	}
	// 主站用户（tenant_id=0）→ 回退全局。
	if _, ok := app.resolveTenantGroupRatio(ctx, 200, "vip"); ok {
		t.Fatalf("main-site user (tenant_id=0) should fall back")
	}
	// 未知用户（users 无此 id → tenant_id=0）→ 回退全局。
	if _, ok := app.resolveTenantGroupRatio(ctx, 999, "vip"); ok {
		t.Fatalf("unknown user should fall back")
	}
	// 入参非法 → 回退全局。
	if _, ok := app.resolveTenantGroupRatio(ctx, 0, "vip"); ok {
		t.Fatalf("userID<=0 should fall back")
	}
	if _, ok := app.resolveTenantGroupRatio(ctx, 100, ""); ok {
		t.Fatalf("empty group should fall back")
	}
}

// TestResolveTenantGroupRatio_ErrorFallback 验证查询出错（缺 tenant_groups 表）一律回退、绝不 panic/阻断。
func TestResolveTenantGroupRatio_ErrorFallback(t *testing.T) {
	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// 只建 users，故意不建 tenant_groups → LookupEnabledGroupRatio 因缺表出错。
	if err := db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL DEFAULT 0)`).Error; err != nil {
		t.Fatalf("create users table: %v", err)
	}
	if err := db.Exec(`INSERT INTO users (id, tenant_id) VALUES (100, 5)`).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	app := &App{DB: db, TenantRepo: tenantrepo.New(db)}

	// 用户有租户(5) 但 tenant_groups 表缺失 → 查询出错 → 回退 (0,false)，不 panic、不阻断计费。
	if r, ok := app.resolveTenantGroupRatio(ctx, 100, "vip"); ok || r != 0 {
		t.Fatalf("error fallback = (%v,%v), want (0,false)", r, ok)
	}
}
