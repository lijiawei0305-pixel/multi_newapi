package mtwire

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	agentrepo "github.com/QuantumNous/new-api/internal/agent/gormrepo"
	"github.com/QuantumNous/new-api/internal/platform/apperr"
	"github.com/QuantumNous/new-api/internal/pricing"
	"github.com/QuantumNous/new-api/internal/tenant"
	tenantrepo "github.com/QuantumNous/new-api/internal/tenant/gormrepo"
	"github.com/QuantumNous/new-api/internal/tokenplan"
	tprepo "github.com/QuantumNous/new-api/internal/tokenplan/gormrepo"
)

// newPlatformSeedTestApp 装配一个仅含平台租户 seed 所需依赖的 App：sqlite(:memory:) + tenant/
// tokenplan/agent 三套 gorm 表 + 原生 users 表（仅 id/role 两列，firstAdminUserID 只读这两列）。
func newPlatformSeedTestApp(t *testing.T) *App {
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
	if err := tenantrepo.AutoMigrate(db); err != nil {
		t.Fatalf("tenant migrate: %v", err)
	}
	if err := tprepo.AutoMigrate(db); err != nil {
		t.Fatalf("tokenplan migrate: %v", err)
	}
	if err := agentrepo.AutoMigrate(db); err != nil {
		t.Fatalf("agent migrate: %v", err)
	}
	if err := db.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY, role INTEGER NOT NULL DEFAULT 1)").Error; err != nil {
		t.Fatalf("create users: %v", err)
	}

	tr := tenantrepo.New(db)
	ar := agentrepo.New(db)
	tp := tprepo.New(db)
	guard := pricing.NewGuard()
	return &App{
		DB:            db,
		TenantRepo:    tr,
		TenantService: tenant.NewService(tr, tenant.NewSlugValidator()),
		TokenPlanRepo: tp,
		Catalog:       tokenplan.NewCatalog(tp),
		Retail:        tokenplan.NewRetailService(tp, guard),
		AgentRepo:     ar,
	}
}

// seedUserWithRole 插入一条最小 users 行（仅 id/role），供 firstAdminUserID 相关用例摆数据。
// 命名区分 grouphook_test.go 的 seedUser(userID,tenantID)——两者 users 表 schema 不同，互不复用。
func seedUserWithRole(t *testing.T, app *App, id int64, role int) {
	t.Helper()
	if err := app.DB.Exec("INSERT INTO users (id, role) VALUES (?,?)", id, role).Error; err != nil {
		t.Fatalf("seed user %d: %v", id, err)
	}
}

// TestPlatformTenant_NotSeededYet_ReturnsTenantNotFound 校验 seed 前直接查平台租户会得到
// TENANT_NOT_FOUND（http.go resolveBuyerTenant 据此判定"主站但尚未 seed"这种边缘情形）。
func TestPlatformTenant_NotSeededYet_ReturnsTenantNotFound(t *testing.T) {
	app := newPlatformSeedTestApp(t)
	if _, err := app.platformTenant(context.Background()); !apperr.Is(err, "TENANT_NOT_FOUND") {
		t.Fatalf("platformTenant before seed: err = %v, want TENANT_NOT_FOUND", err)
	}
}

// TestSeedPlatformTenant_CreatesTenantWithListings 校验平台租户被创建、启用 tokenplan，
// 且主站基准 6 档套餐已全部上架启用（零售价回退主站官方售价，同 seedDemoAgent 的 demo 租户上架口径）。
func TestSeedPlatformTenant_CreatesTenantWithListings(t *testing.T) {
	app := newPlatformSeedTestApp(t)
	ctx := context.Background()

	if err := app.seedPlatformTenant(ctx); err != nil {
		t.Fatalf("seedPlatformTenant: %v", err)
	}

	pt, err := app.platformTenant(ctx)
	if err != nil {
		t.Fatalf("platformTenant after seed: %v", err)
	}
	if pt.Slug != platformSlug {
		t.Errorf("slug = %q, want %q", pt.Slug, platformSlug)
	}
	if !pt.TokenplanEnabled {
		t.Errorf("TokenplanEnabled = false, want true (platform sells its own plans)")
	}

	listings, err := app.TokenPlanRepo.ListListings(ctx, pt.ID)
	if err != nil {
		t.Fatalf("ListListings: %v", err)
	}
	if len(listings) != len(tokenplan.SeedPlans()) {
		t.Fatalf("listings = %d, want %d (all baseline plans listed)", len(listings), len(tokenplan.SeedPlans()))
	}
	for _, l := range listings {
		if !l.Enabled {
			t.Errorf("listing plan_id=%d not enabled", l.PlanID)
		}
	}
}

// TestSeedPlatformTenant_NotListedAsAgent 是本任务的硬约束回归测试：平台租户绝不能出现在
// HandleAdminListAgents 依赖的 AgentRepo.ListProfiles 里（没有 agent_profiles 行 = 不是可管理的代理）。
func TestSeedPlatformTenant_NotListedAsAgent(t *testing.T) {
	app := newPlatformSeedTestApp(t)
	ctx := context.Background()
	if err := app.seedPlatformTenant(ctx); err != nil {
		t.Fatalf("seedPlatformTenant: %v", err)
	}
	pt, err := app.platformTenant(ctx)
	if err != nil {
		t.Fatalf("platformTenant: %v", err)
	}

	profiles, err := app.AgentRepo.ListProfiles(ctx)
	if err != nil {
		t.Fatalf("ListProfiles: %v", err)
	}
	for _, p := range profiles {
		if p.TenantID == pt.ID {
			t.Fatalf("platform tenant %d must not have an agent_profiles row, found %+v", pt.ID, p)
		}
	}
}

// TestSeedPlatformTenant_Idempotent 校验二次调用不重复建租户/不重复插入上架记录。
func TestSeedPlatformTenant_Idempotent(t *testing.T) {
	app := newPlatformSeedTestApp(t)
	ctx := context.Background()

	if err := app.seedPlatformTenant(ctx); err != nil {
		t.Fatalf("seed #1: %v", err)
	}
	first, err := app.platformTenant(ctx)
	if err != nil {
		t.Fatalf("platformTenant #1: %v", err)
	}

	if err := app.seedPlatformTenant(ctx); err != nil {
		t.Fatalf("seed #2: %v", err)
	}
	second, err := app.platformTenant(ctx)
	if err != nil {
		t.Fatalf("platformTenant #2: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("tenant id changed across seed runs: %d -> %d", first.ID, second.ID)
	}

	listings, err := app.TokenPlanRepo.ListListings(ctx, second.ID)
	if err != nil {
		t.Fatalf("ListListings: %v", err)
	}
	if len(listings) != len(tokenplan.SeedPlans()) {
		t.Fatalf("listings after 2nd seed = %d, want %d (no duplicates)", len(listings), len(tokenplan.SeedPlans()))
	}
}

// TestSeedPlatformTenant_NoAdminYet_OwnerStaysZero 覆盖新库场景（setup 向导尚未创建任何管理员）：
// 不报错，OwnerUserID 保持 0，等下次启动（管理员已建）重试补齐。
func TestSeedPlatformTenant_NoAdminYet_OwnerStaysZero(t *testing.T) {
	app := newPlatformSeedTestApp(t)
	ctx := context.Background()
	seedUserWithRole(t, app, 1, common.RoleCommonUser) // 只有普通用户，还没有管理员

	if err := app.seedPlatformTenant(ctx); err != nil {
		t.Fatalf("seedPlatformTenant: %v", err)
	}
	pt, err := app.platformTenant(ctx)
	if err != nil {
		t.Fatalf("platformTenant: %v", err)
	}
	if pt.OwnerUserID != 0 {
		t.Fatalf("OwnerUserID = %d, want 0 (no admin exists yet)", pt.OwnerUserID)
	}
}

// TestSeedPlatformTenant_AssignsFirstAdminAsOwner 校验平台租户挂靠"root/首个管理员"：
// role >= RoleAdminUser 里 id 最小的一个（不是任意管理员、也不是普通用户）。
func TestSeedPlatformTenant_AssignsFirstAdminAsOwner(t *testing.T) {
	app := newPlatformSeedTestApp(t)
	ctx := context.Background()
	seedUserWithRole(t, app, 1, common.RoleCommonUser) // 普通用户 id 更小，但不能被选中
	seedUserWithRole(t, app, 2, common.RoleAdminUser)  // 应被选中：最小的管理员/root id
	seedUserWithRole(t, app, 3, common.RoleRootUser)

	if err := app.seedPlatformTenant(ctx); err != nil {
		t.Fatalf("seedPlatformTenant: %v", err)
	}
	pt, err := app.platformTenant(ctx)
	if err != nil {
		t.Fatalf("platformTenant: %v", err)
	}
	if pt.OwnerUserID != 2 {
		t.Fatalf("OwnerUserID = %d, want 2 (first admin-or-above by id)", pt.OwnerUserID)
	}
}

// TestSeedPlatformTenant_OwnerNotReassignedOnRerun 校验 owner 一旦设定，后续 seed 不会因为
// 出现更小 id 的管理员而被改写（对齐 seedDemoAgent 的"仅当尚未设代理才设置"语义，尊重后续人工改派）。
func TestSeedPlatformTenant_OwnerNotReassignedOnRerun(t *testing.T) {
	app := newPlatformSeedTestApp(t)
	ctx := context.Background()
	seedUserWithRole(t, app, 5, common.RoleAdminUser)

	if err := app.seedPlatformTenant(ctx); err != nil {
		t.Fatalf("seed #1: %v", err)
	}
	pt, err := app.platformTenant(ctx)
	if err != nil {
		t.Fatalf("platformTenant #1: %v", err)
	}
	if pt.OwnerUserID != 5 {
		t.Fatalf("OwnerUserID after #1 = %d, want 5", pt.OwnerUserID)
	}

	seedUserWithRole(t, app, 1, common.RoleAdminUser) // 更小 id 的管理员事后出现
	if err := app.seedPlatformTenant(ctx); err != nil {
		t.Fatalf("seed #2: %v", err)
	}
	pt, err = app.platformTenant(ctx)
	if err != nil {
		t.Fatalf("platformTenant #2: %v", err)
	}
	if pt.OwnerUserID != 5 {
		t.Fatalf("OwnerUserID after #2 = %d, want unchanged 5 (must not reassign)", pt.OwnerUserID)
	}
}

// TestFirstAdminUserID 直接覆盖辅助函数本身：找不到管理员返回 (0,false,nil)；
// 找到时返回 role>=RoleAdminUser 中 id 最小者。
func TestFirstAdminUserID(t *testing.T) {
	app := newPlatformSeedTestApp(t)
	ctx := context.Background()

	if _, found, err := app.firstAdminUserID(ctx); err != nil || found {
		t.Fatalf("empty users: (found=%v, err=%v), want (false, nil)", found, err)
	}

	seedUserWithRole(t, app, 10, common.RoleCommonUser)
	if _, found, err := app.firstAdminUserID(ctx); err != nil || found {
		t.Fatalf("only common users: (found=%v, err=%v), want (false, nil)", found, err)
	}

	seedUserWithRole(t, app, 20, common.RoleAdminUser)
	seedUserWithRole(t, app, 15, common.RoleRootUser)
	id, found, err := app.firstAdminUserID(ctx)
	if err != nil || !found {
		t.Fatalf("with admins: (found=%v, err=%v), want (true, nil)", found, err)
	}
	if id != 15 {
		t.Fatalf("id = %d, want 15 (smallest id among role>=RoleAdminUser)", id)
	}
}

// TestIsPlatformTenant 覆盖排除判定本身：nil / 非平台 slug / 平台 slug 三种输入。
func TestIsPlatformTenant(t *testing.T) {
	if isPlatformTenant(nil) {
		t.Fatalf("isPlatformTenant(nil) = true, want false")
	}
	if isPlatformTenant(&tenant.Tenant{ID: 1, Slug: "acme"}) {
		t.Fatalf("isPlatformTenant(acme) = true, want false")
	}
	if !isPlatformTenant(&tenant.Tenant{ID: 2, Slug: platformSlug}) {
		t.Fatalf("isPlatformTenant(platform) = false, want true")
	}
}
