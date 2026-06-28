package mtwire

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/promotion"
	promotionrepo "github.com/QuantumNous/new-api/internal/promotion/gormrepo"
	"github.com/QuantumNous/new-api/internal/tenant"
	tenantrepo "github.com/QuantumNous/new-api/internal/tenant/gormrepo"
)

// attribFixture 持有归属钩子测试所需的最小 App 装配 + 种子数据句柄。
type attribFixture struct {
	app   *App
	db    *gorm.DB
	pr    *promotionrepo.Repo
	tenA  int64  // 渠道所属租户（alpha.wedreamhub.com）
	tenB  int64  // 仅有域名、无渠道的租户（beta.wedreamhub.com）
	chAID int64  // tenA 下的推广渠道 ID
	chA   string // tenA 下的渠道码
}

// newAttribFixture 用纯 Go sqlite(:memory:) 装配 attributeRegistration 所需的 App
// （DB + TenantResolver + PromotionRepo），并种入两租户/域名 + 一个推广渠道。
// 单连接：:memory: 每连接独立库，限 1 连接保证同一库（对齐既有 gormrepo 测试约定）。
func newAttribFixture(t *testing.T) *attribFixture {
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

	// 模拟 new-api 原生 users 表的最小列集（含 mtwire 增列 tenant_id / promotion_channel_id）。
	if err := db.Exec(`CREATE TABLE users (
		id INTEGER PRIMARY KEY,
		tenant_id INTEGER NOT NULL DEFAULT 0,
		promotion_channel_id INTEGER NOT NULL DEFAULT 0
	)`).Error; err != nil {
		t.Fatalf("create users: %v", err)
	}
	if err := tenantrepo.AutoMigrate(db); err != nil {
		t.Fatalf("tenant migrate: %v", err)
	}
	if err := promotionrepo.AutoMigrate(db); err != nil {
		t.Fatalf("promotion migrate: %v", err)
	}

	ctx := context.Background()
	tr := tenantrepo.New(db)
	pr := promotionrepo.New(db)

	tenA := &tenant.Tenant{Slug: "alpha", Name: "Alpha", Status: tenant.StatusActive}
	if err := tr.CreateTenant(ctx, tenA); err != nil {
		t.Fatalf("create tenant A: %v", err)
	}
	if err := tr.CreateDomain(ctx, &tenant.TenantDomain{TenantID: tenA.ID, Domain: "alpha.wedreamhub.com", IsPrimary: true}); err != nil {
		t.Fatalf("create domain A: %v", err)
	}
	tenB := &tenant.Tenant{Slug: "beta", Name: "Beta", Status: tenant.StatusActive}
	if err := tr.CreateTenant(ctx, tenB); err != nil {
		t.Fatalf("create tenant B: %v", err)
	}
	if err := tr.CreateDomain(ctx, &tenant.TenantDomain{TenantID: tenB.ID, Domain: "beta.wedreamhub.com", IsPrimary: true}); err != nil {
		t.Fatalf("create domain B: %v", err)
	}

	chA := &promotion.Channel{TenantID: tenA.ID, Name: "微信公众号", ChannelCode: "wx_alpha1"}
	if err := pr.CreateChannel(ctx, chA); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	app := &App{DB: db, TenantRepo: tr, TenantResolver: tenant.NewResolver(tr, tenant.NewMemCache()), PromotionRepo: pr}
	return &attribFixture{app: app, db: db, pr: pr, tenA: tenA.ID, tenB: tenB.ID, chAID: chA.ID, chA: chA.ChannelCode}
}

// insertUser 落一行裸 user（仅 id；tenant_id/promotion_channel_id 取默认 0）。
func (f *attribFixture) insertUser(t *testing.T, id int64) {
	t.Helper()
	if err := f.db.Exec(`INSERT INTO users (id) VALUES (?)`, id).Error; err != nil {
		t.Fatalf("insert user %d: %v", id, err)
	}
}

// userAttrib 读回某用户的 tenant_id / promotion_channel_id。
func (f *attribFixture) userAttrib(t *testing.T, id int64) (tenantID, channelID int64) {
	t.Helper()
	var row struct {
		TenantID           int64
		PromotionChannelID int64
	}
	if err := f.db.Table("users").Select("tenant_id", "promotion_channel_id").Where("id = ?", id).Take(&row).Error; err != nil {
		t.Fatalf("read user %d: %v", id, err)
	}
	return row.TenantID, row.PromotionChannelID
}

// registeredCount 读回某渠道当前 registered_count。
func (f *attribFixture) registeredCount(t *testing.T, code string) int64 {
	t.Helper()
	ch, err := f.pr.GetChannelByCode(context.Background(), code)
	if err != nil {
		t.Fatalf("get channel %s: %v", code, err)
	}
	return ch.RegisteredCount
}

// attributionCount 数某用户的归属记录条数（幂等断言用）。
func (f *attribFixture) attributionCount(t *testing.T, userID int64) int64 {
	t.Helper()
	var n int64
	if err := f.db.Table("agent_promotion_attributions").Where("user_id = ?", userID).Count(&n).Error; err != nil {
		t.Fatalf("count attributions: %v", err)
	}
	return n
}

// TestAttributeRegistration_ChannelCode 有渠道码 → 归属到渠道所属租户 + 渠道，registered_count++，落归属记录。
func TestAttributeRegistration_ChannelCode(t *testing.T) {
	f := newAttribFixture(t)
	ctx := context.Background()
	const uid = 1001
	f.insertUser(t, uid)

	// Host 给一个未知根域，证明归属来自渠道码而非 Host。
	f.app.attributeRegistration(ctx, "wedreamhub.com", f.chA, uid)

	tid, cid := f.userAttrib(t, uid)
	if tid != f.tenA || cid != f.chAID {
		t.Fatalf("user attrib = (tenant=%d, channel=%d), want (%d, %d)", tid, cid, f.tenA, f.chAID)
	}
	if got := f.registeredCount(t, f.chA); got != 1 {
		t.Fatalf("registered_count = %d, want 1", got)
	}
	if got := f.attributionCount(t, uid); got != 1 {
		t.Fatalf("attribution rows = %d, want 1", got)
	}
}

// TestAttributeRegistration_HostFallback 无渠道码 → 按注册 Host 解析租户，promotion_channel_id 保持 0。
func TestAttributeRegistration_HostFallback(t *testing.T) {
	f := newAttribFixture(t)
	ctx := context.Background()
	const uid = 1002
	f.insertUser(t, uid)

	f.app.attributeRegistration(ctx, "beta.wedreamhub.com", "", uid)

	tid, cid := f.userAttrib(t, uid)
	if tid != f.tenB || cid != 0 {
		t.Fatalf("user attrib = (tenant=%d, channel=%d), want (%d, 0)", tid, cid, f.tenB)
	}
	// Host 归属不动渠道计数。
	if got := f.registeredCount(t, f.chA); got != 0 {
		t.Fatalf("registered_count = %d, want 0 (host path must not touch channel)", got)
	}
}

// TestAttributeRegistration_RootNoCode 无渠道码 + 主站根域/未知 Host → 不归属（tenant_id 保持 0）。
func TestAttributeRegistration_RootNoCode(t *testing.T) {
	f := newAttribFixture(t)
	ctx := context.Background()
	const uid = 1003
	f.insertUser(t, uid)

	f.app.attributeRegistration(ctx, "wedreamhub.com", "", uid)

	tid, cid := f.userAttrib(t, uid)
	if tid != 0 || cid != 0 {
		t.Fatalf("user attrib = (tenant=%d, channel=%d), want (0, 0)", tid, cid)
	}
}

// TestAttributeRegistration_ChannelBeatsHost 渠道码优先于 Host：在租户 B 的域名注册但带租户 A 的渠道码
// → 归属到租户 A（渠道码胜出）。
func TestAttributeRegistration_ChannelBeatsHost(t *testing.T) {
	f := newAttribFixture(t)
	ctx := context.Background()
	const uid = 1004
	f.insertUser(t, uid)

	f.app.attributeRegistration(ctx, "beta.wedreamhub.com", f.chA, uid)

	tid, cid := f.userAttrib(t, uid)
	if tid != f.tenA || cid != f.chAID {
		t.Fatalf("user attrib = (tenant=%d, channel=%d), want (%d, %d) — channel must win over host", tid, cid, f.tenA, f.chAID)
	}
}

// TestAttributeRegistration_UnknownCodeFallsBackToHost 未知渠道码 → 回落按 Host 归属（不静默丢归属）。
func TestAttributeRegistration_UnknownCodeFallsBackToHost(t *testing.T) {
	f := newAttribFixture(t)
	ctx := context.Background()
	const uid = 1005
	f.insertUser(t, uid)

	f.app.attributeRegistration(ctx, "beta.wedreamhub.com", "nope_unknown", uid)

	tid, cid := f.userAttrib(t, uid)
	if tid != f.tenB || cid != 0 {
		t.Fatalf("user attrib = (tenant=%d, channel=%d), want (%d, 0) — unknown code must fall back to host", tid, cid, f.tenB)
	}
	if got := f.registeredCount(t, f.chA); got != 0 {
		t.Fatalf("registered_count = %d, want 0 (unknown code must not bump a real channel)", got)
	}
}

// TestAttributeRegistration_NoUserNoop userID 非法时直接跳过（不写库、不 panic）。
func TestAttributeRegistration_NoUserNoop(t *testing.T) {
	f := newAttribFixture(t)
	// 不应 panic；渠道计数保持 0。
	f.app.attributeRegistration(context.Background(), "alpha.wedreamhub.com", f.chA, 0)
	if got := f.registeredCount(t, f.chA); got != 0 {
		t.Fatalf("registered_count = %d, want 0 (invalid userID must be a noop)", got)
	}
}
