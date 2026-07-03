package gormrepo

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/tokenplan"
)

// newTestRepo 用纯 Go sqlite(:memory:) 建一个隔离的 tokenplan 仓储。
// 单连接：:memory: 每连接独立库，限 1 连接保证同一库（对齐 model 包测试约定）。
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

// orderSeq 为 seedSub 产出唯一 source_order_id（满足唯一约束）。
var orderSeq int

// seedSub 直接落一条订阅行（绕过购买流程，用于只读列表用例）。
func seedSub(t *testing.T, r *Repo, tenantID, userID int64, status tokenplan.SubStatus, used, limit float64, expireAt time.Time) {
	t.Helper()
	orderSeq++
	now := time.Now()
	row := subRow{
		TenantID:      tenantID,
		UserID:        userID,
		PlanID:        1,
		MonthLimitUSD: limit,
		UsedUSD:       used,
		Status:        string(status),
		StartAt:       expireAt.AddDate(0, 0, -30),
		ExpireAt:      expireAt,
		SourceOrderID: "ord-" + strconv.Itoa(orderSeq),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := r.db.WithContext(context.Background()).Create(&row).Error; err != nil {
		t.Fatalf("seed sub: %v", err)
	}
}

// TestListSubscriptionsByTenant 覆盖：租户隔离、id 降序、惰性过期落库、空租户。
func TestListSubscriptionsByTenant(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	now := time.Date(2026, 6, 28, 0, 0, 0, 0, time.UTC)

	// tenant 7：user11 active 未过期；user12 active 已过期（应被惰性翻 expired）。
	seedSub(t, r, 7, 11, tokenplan.SubActive, 5, 10, now.Add(24*time.Hour))
	seedSub(t, r, 7, 12, tokenplan.SubActive, 9, 10, now.Add(-1*time.Hour))
	// tenant 9：隔离断言——不得出现在 tenant 7 结果中。
	seedSub(t, r, 9, 13, tokenplan.SubActive, 1, 10, now.Add(24*time.Hour))

	got, err := r.ListSubscriptionsByTenant(ctx, 7, now)
	if err != nil {
		t.Fatalf("ListSubscriptionsByTenant: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 subs for tenant 7, got %d", len(got))
	}
	for _, s := range got {
		if s.TenantID != 7 {
			t.Fatalf("cross-tenant leak: got tenant %d", s.TenantID)
		}
	}
	// id DESC：后插入的 user12（id 更大）排前。
	if got[0].UserID != 12 || got[1].UserID != 11 {
		t.Fatalf("want id DESC [12,11], got [%d,%d]", got[0].UserID, got[1].UserID)
	}
	// 惰性过期落库：已过期 active → expired；未过期保持 active。
	if got[0].Status != tokenplan.SubExpired {
		t.Fatalf("want expired for lazily-expired sub, got %s", got[0].Status)
	}
	if got[1].Status != tokenplan.SubActive {
		t.Fatalf("want active for non-expired sub, got %s", got[1].Status)
	}

	// 落库验证：再查一次，user12 已是 expired（非读时临时翻牌）。
	var persisted subRow
	if err := r.db.WithContext(ctx).Take(&persisted, "tenant_id = ? AND user_id = ?", 7, 12).Error; err != nil {
		t.Fatalf("reload sub: %v", err)
	}
	if persisted.Status != statusExpired {
		t.Fatalf("lazy expiry not persisted: status=%s", persisted.Status)
	}

	// 空租户：返回空切片、无错误。
	empty, err := r.ListSubscriptionsByTenant(ctx, 999, now)
	if err != nil {
		t.Fatalf("empty tenant err: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("want 0 subs for empty tenant, got %d", len(empty))
	}
}

// TestListAllSubscriptions 覆盖：跨租户不过滤（主站订阅监控用）、id 降序、惰性过期落库。
// 与 TestListSubscriptionsByTenant 用同一批种子数据，唯一差异断言是"tenant 9 的订阅必须出现"。
func TestListAllSubscriptions(t *testing.T) {
	ctx := context.Background()
	r := newTestRepo(t)
	now := time.Date(2026, 6, 28, 0, 0, 0, 0, time.UTC)

	// tenant 7：user11 active 未过期；user12 active 已过期（应被惰性翻 expired）。
	seedSub(t, r, 7, 11, tokenplan.SubActive, 5, 10, now.Add(24*time.Hour))
	seedSub(t, r, 7, 12, tokenplan.SubActive, 9, 10, now.Add(-1*time.Hour))
	// tenant 9：与 ListSubscriptionsByTenant 相反的断言——必须出现在跨租户结果中。
	seedSub(t, r, 9, 13, tokenplan.SubActive, 1, 10, now.Add(24*time.Hour))

	got, err := r.ListAllSubscriptions(ctx, now)
	if err != nil {
		t.Fatalf("ListAllSubscriptions: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 subs across all tenants, got %d", len(got))
	}
	seenTenants := map[int64]bool{}
	for _, s := range got {
		seenTenants[s.TenantID] = true
	}
	if !seenTenants[7] || !seenTenants[9] {
		t.Fatalf("want both tenant 7 and tenant 9 represented, got tenants %v", seenTenants)
	}
	// id DESC：后插入的 user13（tenant 9）排最前。
	if got[0].UserID != 13 || got[0].TenantID != 9 {
		t.Fatalf("want id DESC head = tenant9/user13, got tenant%d/user%d", got[0].TenantID, got[0].UserID)
	}
	// 惰性过期落库：user12（tenant 7，已过期）→ expired。
	var persisted subRow
	if err := r.db.WithContext(ctx).Take(&persisted, "tenant_id = ? AND user_id = ?", 7, 12).Error; err != nil {
		t.Fatalf("reload sub: %v", err)
	}
	if persisted.Status != statusExpired {
		t.Fatalf("lazy expiry not persisted: status=%s", persisted.Status)
	}

	// 无任何订阅：返回空切片、无错误。
	empty := newTestRepo(t)
	emptyGot, err := empty.ListAllSubscriptions(ctx, now)
	if err != nil {
		t.Fatalf("empty repo err: %v", err)
	}
	if len(emptyGot) != 0 {
		t.Fatalf("want 0 subs for empty repo, got %d", len(emptyGot))
	}
}
