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
