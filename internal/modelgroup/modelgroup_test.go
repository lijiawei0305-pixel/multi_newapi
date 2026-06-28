package modelgroup

import (
	"context"
	"errors"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// newTestRepo 装配最小仓储：sqlite(:memory:) + model_groups 表（AutoMigrate）。
func newTestRepo(t *testing.T) *Repo {
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
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return New(db)
}

// TestCreateGetListAndCache 覆盖：建登记 → 缓存命中(IsModelGroup/ListEnabled) → Get/List 读回。
func TestCreateGetListAndCache(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)

	chID := int64(7)
	mg, err := r.Create(ctx, ModelGroup{Name: "claude-kiro", ChannelID: &chID, Description: "kiro", Enabled: true, Sort: 1})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if mg.ID == 0 || mg.Name != "claude-kiro" || mg.ChannelID == nil || *mg.ChannelID != 7 || !mg.Enabled {
		t.Fatalf("create out = %+v", mg)
	}

	// 缓存热路径：已登记 → true；未登记 → false。
	if !r.IsModelGroup("claude-kiro") {
		t.Fatalf("IsModelGroup(claude-kiro) should be true after create")
	}
	if r.IsModelGroup("nope") {
		t.Fatalf("unregistered group should be false")
	}
	// ListEnabled（缓存）。
	if got := r.ListEnabled(); len(got) != 1 || got[0] != "claude-kiro" {
		t.Fatalf("ListEnabled = %v, want [claude-kiro]", got)
	}
	// Get / List。
	g, err := r.Get(ctx, "claude-kiro")
	if err != nil || g.Description != "kiro" {
		t.Fatalf("get = %+v err = %v", g, err)
	}
	all, err := r.List(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("list = %v err = %v", all, err)
	}
}

// TestDuplicateName 验证 name 唯一：重复建返回 ErrNameTaken。
func TestDuplicateName(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	if _, err := r.Create(ctx, ModelGroup{Name: "openai-plus", Enabled: true}); err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err := r.Create(ctx, ModelGroup{Name: "openai-plus", Enabled: true})
	if !errors.Is(err, ErrNameTaken) {
		t.Fatalf("dup err = %v, want ErrNameTaken", err)
	}
}

// TestUpdateTogglesCache 验证：禁用 → 退出缓存（IsModelGroup=false）；改描述落库。
func TestUpdateTogglesCache(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	if _, err := r.Create(ctx, ModelGroup{Name: "claude-kiro", Enabled: true}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if !r.IsModelGroup("claude-kiro") {
		t.Fatalf("want true before disable")
	}
	disabled := false
	if err := r.Update(ctx, "claude-kiro", ModelGroupUpdate{Enabled: &disabled}); err != nil {
		t.Fatalf("update enabled=false: %v", err)
	}
	if r.IsModelGroup("claude-kiro") {
		t.Fatalf("disabled group must not be a model group (cache stale)")
	}
	desc := "updated"
	if err := r.Update(ctx, "claude-kiro", ModelGroupUpdate{Description: &desc}); err != nil {
		t.Fatalf("update desc: %v", err)
	}
	g, _ := r.Get(ctx, "claude-kiro")
	if g.Description != "updated" {
		t.Fatalf("desc = %q, want updated", g.Description)
	}
	// 不存在的 name 更新 → ErrNotFound。
	if err := r.Update(ctx, "ghost", ModelGroupUpdate{Description: &desc}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update missing = %v, want ErrNotFound", err)
	}
}

// TestDelete 验证：删登记 → 退出缓存 + Get 返回 ErrNotFound。
func TestDelete(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	if _, err := r.Create(ctx, ModelGroup{Name: "claude-kiro", Enabled: true}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := r.Delete(ctx, "claude-kiro"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if r.IsModelGroup("claude-kiro") {
		t.Fatalf("deleted group should be false in cache")
	}
	if _, err := r.Get(ctx, "claude-kiro"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete = %v, want ErrNotFound", err)
	}
}

// TestReloadCacheFromDB 验证缓存只收「enabled」行（模拟他节点直写后重载）。
func TestReloadCacheFromDB(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	if err := r.db.Exec(`INSERT INTO model_groups (name, enabled) VALUES (?, ?)`, "a", true).Error; err != nil {
		t.Fatalf("insert a: %v", err)
	}
	if err := r.db.Exec(`INSERT INTO model_groups (name, enabled) VALUES (?, ?)`, "b", false).Error; err != nil {
		t.Fatalf("insert b: %v", err)
	}
	if err := r.ReloadCache(ctx); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !r.IsModelGroup("a") {
		t.Fatalf("enabled 'a' should be cached")
	}
	if r.IsModelGroup("b") {
		t.Fatalf("disabled 'b' should not be cached")
	}
}
