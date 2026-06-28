package gormrepo

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newGroupTestRepo(t *testing.T) *Repo {
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

// TestUpsertAndListGroups_ScopedByTenant 验证用户组倍率 upsert（覆盖）+ 列表强制按租户隔离。
func TestUpsertAndListGroups_ScopedByTenant(t *testing.T) {
	ctx := context.Background()
	r := newGroupTestRepo(t)

	if err := r.UpsertGroup(ctx, 1, "vip", 1.5); err != nil {
		t.Fatalf("upsert t1 vip: %v", err)
	}
	if err := r.UpsertGroup(ctx, 2, "vip", 2.0); err != nil {
		t.Fatalf("upsert t2 vip: %v", err)
	}
	// 同 (tenant,group) 再 upsert：覆盖倍率（不新增行）。
	if err := r.UpsertGroup(ctx, 1, "vip", 1.8); err != nil {
		t.Fatalf("re-upsert t1 vip: %v", err)
	}

	g1, err := r.ListGroups(ctx, 1)
	if err != nil {
		t.Fatalf("list t1: %v", err)
	}
	if len(g1) != 1 || g1[0].GroupName != "vip" || g1[0].Ratio != 1.8 || !g1[0].Enabled {
		t.Fatalf("t1 groups = %+v, want single vip ratio 1.8 enabled", g1)
	}
	// 租户 2 隔离（看不到 t1，自己的倍率独立）。
	g2, _ := r.ListGroups(ctx, 2)
	if len(g2) != 1 || g2[0].Ratio != 2.0 {
		t.Fatalf("t2 groups = %+v, want single vip ratio 2.0", g2)
	}
	if l3, _ := r.ListGroups(ctx, 99); len(l3) != 0 {
		t.Fatalf("t99 groups = %+v, want empty", l3)
	}
}

// TestLookupEnabledGroupRatio 验证计费路径的「启用倍率覆盖」点查：命中 / 无行 / 跨租户隔离 / 已禁用。
func TestLookupEnabledGroupRatio(t *testing.T) {
	ctx := context.Background()
	r := newGroupTestRepo(t)

	// 命中：enabled 覆盖 → (ratio, true, nil)。
	if err := r.UpsertGroup(ctx, 1, "vip", 1.8); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if ratio, ok, err := r.LookupEnabledGroupRatio(ctx, 1, "vip"); err != nil || !ok || ratio != 1.8 {
		t.Fatalf("hit = (%v,%v,%v), want (1.8,true,nil)", ratio, ok, err)
	}

	// 无行：未配置组 → (0,false,nil)，调用方回退全局。
	if ratio, ok, err := r.LookupEnabledGroupRatio(ctx, 1, "default"); ok || ratio != 0 || err != nil {
		t.Fatalf("missing = (%v,%v,%v), want (0,false,nil)", ratio, ok, err)
	}

	// 跨租户隔离：他租户覆盖不可见。
	if _, ok, _ := r.LookupEnabledGroupRatio(ctx, 2, "vip"); ok {
		t.Fatalf("cross-tenant lookup should miss")
	}

	// 已禁用：enabled=false 行 → (0,false,nil)，回退全局（不可用禁用的倍率计费）。
	// 用原生 SQL 显式写 enabled=0：GORM 的 Create 会省略 bool 零值并套用列 default:true，无法落库为 false。
	if err := r.db.WithContext(ctx).Exec(
		`INSERT INTO tenant_groups (tenant_id, group_name, ratio, enabled) VALUES (?, ?, ?, ?)`,
		1, "svip", 2.5, false,
	).Error; err != nil {
		t.Fatalf("insert disabled row: %v", err)
	}
	if ratio, ok, err := r.LookupEnabledGroupRatio(ctx, 1, "svip"); ok || ratio != 0 || err != nil {
		t.Fatalf("disabled = (%v,%v,%v), want (0,false,nil)", ratio, ok, err)
	}
}
