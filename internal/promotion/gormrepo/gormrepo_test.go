package gormrepo

import (
	"context"
	"sync"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/promotion"
)

// newTestRepo 用纯 Go sqlite(:memory:) 建一个隔离的推广仓储。
// 单连接：:memory: 每连接独立库，限 1 连接保证同一库（对齐 agent/tokenplan 仓储测试约定）。
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

// TestCreateAndListScopedByTenant 验证渠道入库 + 列表强制按租户隔离（越权防线）。
func TestCreateAndListScopedByTenant(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)

	a := &promotion.Channel{TenantID: 1, Name: "微信公众号", ChannelCode: "wx_aaa111"}
	b := &promotion.Channel{TenantID: 2, Name: "抖音", ChannelCode: "dy_bbb222"}
	if err := r.CreateChannel(ctx, a); err != nil || a.ID == 0 {
		t.Fatalf("create a: id=%d err=%v", a.ID, err)
	}
	if err := r.CreateChannel(ctx, b); err != nil {
		t.Fatalf("create b: %v", err)
	}

	// 租户 1 只看到自己的渠道，看不到租户 2 的（scopeByTenant）。
	list1, err := r.ListChannelsByTenant(ctx, 1)
	if err != nil {
		t.Fatalf("list t1: %v", err)
	}
	if len(list1) != 1 || list1[0].ChannelCode != "wx_aaa111" || list1[0].Name != "微信公众号" {
		t.Fatalf("t1 list = %+v, want only wx_aaa111", list1)
	}
	if l2, _ := r.ListChannelsByTenant(ctx, 2); len(l2) != 1 || l2[0].ChannelCode != "dy_bbb222" {
		t.Fatalf("t2 list = %+v, want only dy_bbb222", l2)
	}
	// 无渠道的租户得空列表（非 nil）。
	if l3, _ := r.ListChannelsByTenant(ctx, 99); len(l3) != 0 {
		t.Fatalf("t99 list = %+v, want empty", l3)
	}
}

// TestCreateChannel_DuplicateCode 验证 code 唯一冲突翻译为 ErrChannelPrefixDup。
func TestCreateChannel_DuplicateCode(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	if err := r.CreateChannel(ctx, &promotion.Channel{TenantID: 1, ChannelCode: "dup_x"}); err != nil {
		t.Fatalf("first: %v", err)
	}
	// 即便不同租户，code 全局唯一仍冲突。
	if err := r.CreateChannel(ctx, &promotion.Channel{TenantID: 2, ChannelCode: "dup_x"}); err != promotion.ErrChannelPrefixDup {
		t.Fatalf("dup = %v, want ErrChannelPrefixDup", err)
	}
}

// TestGetChannelByCode_NotFound 验证未知码的错误码。
func TestGetChannelByCode_NotFound(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	if _, err := r.GetChannelByCode(ctx, "nope_0"); err != promotion.ErrChannelNotFound {
		t.Fatalf("get missing = %v, want ErrChannelNotFound", err)
	}
}

// TestIncrRegisteredCount_AtomicUnderConcurrency 验证原子自增：并发 +1 不丢更新。
func TestIncrRegisteredCount_AtomicUnderConcurrency(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	ch := &promotion.Channel{TenantID: 1, ChannelCode: "c_atomic"}
	if err := r.CreateChannel(ctx, ch); err != nil {
		t.Fatalf("create: %v", err)
	}

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_ = r.IncrRegisteredCount(ctx, ch.ID)
		}()
	}
	wg.Wait()

	got, err := r.GetChannelByCode(ctx, "c_atomic")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.RegisteredCount != n {
		t.Fatalf("registered_count = %d, want %d (lost updates)", got.RegisteredCount, n)
	}
	// 不存在的渠道自增报错。
	if err := r.IncrRegisteredCount(ctx, 999999); err != promotion.ErrChannelNotFound {
		t.Fatalf("incr missing = %v, want ErrChannelNotFound", err)
	}
}

// TestAttribution_IdempotentByUser 验证用户归属按 user_id 幂等（重复注册不重复落库）。
func TestAttribution_IdempotentByUser(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	a := &promotion.Attribution{UserID: 7, TenantID: 1, ChannelID: 3, ChannelCode: "c_x"}
	if err := r.CreateAttribution(ctx, a); err != nil {
		t.Fatalf("first attribution: %v", err)
	}
	// 同用户再次归属：幂等（不报错）。
	if err := r.CreateAttribution(ctx, a); err != nil {
		t.Fatalf("dup attribution should be idempotent, got %v", err)
	}
}
