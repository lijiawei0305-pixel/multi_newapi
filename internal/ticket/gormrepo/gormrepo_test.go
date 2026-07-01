package gormrepo

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
	"github.com/QuantumNous/new-api/internal/ticket"
)

var ctx = context.Background()

func newTestRepo(t *testing.T) *Repo {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// sqlite :memory: 每连接独立库；repo 用事务，限单连接确保迁移与后续读写同库（同 recharge_test 约定）。
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return New(db)
}

func seed(t *testing.T, r *Repo, tenantID, userID int64, title string) *ticket.SupportTicket {
	t.Helper()
	now := time.Now()
	tk := &ticket.SupportTicket{
		TenantID: tenantID, UserID: userID, Title: title,
		Status: ticket.StatusOpen, Priority: ticket.PriorityNormal,
		MessageCount: 1, LastReplyAt: now, LastReplyRole: ticket.AuthorUser,
		CreatedAt: now, UpdatedAt: now,
	}
	opening := &ticket.TicketMessage{
		TenantID: tenantID, UserID: userID, AuthorRole: ticket.AuthorUser,
		Content: "body of " + title, CreatedAt: now,
	}
	if err := r.Create(ctx, tk, opening); err != nil {
		t.Fatalf("create %q: %v", title, err)
	}
	if tk.ID == 0 || opening.ID == 0 {
		t.Fatalf("ids not backfilled: ticket=%d msg=%d", tk.ID, opening.ID)
	}
	return tk
}

func TestUserScopeIsolation(t *testing.T) {
	r := newTestRepo(t)
	a := seed(t, r, 1, 100, "A")
	b := seed(t, r, 1, 200, "B")

	// GetForUser 精确到 user_id：本人可读，跨用户 → NOT_FOUND。
	if _, err := r.GetForUser(ctx, 100, a.ID); err != nil {
		t.Fatalf("owner get: %v", err)
	}
	if _, err := r.GetForUser(ctx, 200, a.ID); !apperr.Is(err, "TICKET_NOT_FOUND") {
		t.Fatalf("cross-user get err = %v", err)
	}

	// ListForUser 只见本人。
	items, total, err := r.ListForUser(ctx, 100, ticket.TicketFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != a.ID {
		t.Fatalf("user 100 list = %+v (total %d), want only A", items, total)
	}
	_ = b
}

func TestTenantScopeIsolation(t *testing.T) {
	r := newTestRepo(t)
	t1 := seed(t, r, 1, 100, "t1")
	t2 := seed(t, r, 2, 300, "t2")

	if _, err := r.GetForTenant(ctx, 1, t1.ID); err != nil {
		t.Fatalf("tenant1 get own: %v", err)
	}
	if _, err := r.GetForTenant(ctx, 2, t1.ID); !apperr.Is(err, "TICKET_NOT_FOUND") {
		t.Fatalf("cross-tenant get err = %v", err)
	}
	items, total, _ := r.ListForTenant(ctx, 2, ticket.TicketFilter{})
	if total != 1 || items[0].ID != t2.ID {
		t.Fatalf("tenant 2 list leaked: %+v (total %d)", items, total)
	}
}

func TestAdminScopeAndTenantFilter(t *testing.T) {
	r := newTestRepo(t)
	seed(t, r, 1, 100, "t1")
	seed(t, r, 2, 200, "t2")
	seed(t, r, 0, 300, "platform")

	_, total, _ := r.ListForAdmin(ctx, ticket.TicketFilter{})
	if total != 3 {
		t.Fatalf("admin total = %d, want 3", total)
	}
	zero := int64(0)
	items, total, _ := r.ListForAdmin(ctx, ticket.TicketFilter{TenantID: &zero})
	if total != 1 || items[0].TenantID != 0 {
		t.Fatalf("admin tenant=0 filter = %+v (total %d)", items, total)
	}
}

func TestAddReplyAtomicUpdate(t *testing.T) {
	r := newTestRepo(t)
	tk := seed(t, r, 1, 100, "help")

	m := &ticket.TicketMessage{
		TicketID: tk.ID, TenantID: 1, UserID: 500, AuthorRole: ticket.AuthorAgent,
		Content: "on it", CreatedAt: time.Now(),
	}
	if err := r.AddReply(ctx, m, ticket.StatusPending, ticket.Scope{}); err != nil {
		t.Fatalf("add reply: %v", err)
	}
	if m.ID == 0 {
		t.Fatal("message ID not backfilled")
	}
	got, err := r.GetByID(ctx, tk.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.MessageCount != 2 {
		t.Fatalf("message_count = %d, want 2", got.MessageCount)
	}
	if got.Status != ticket.StatusPending || got.LastReplyRole != ticket.AuthorAgent {
		t.Fatalf("after reply status=%s role=%s, want pending/agent", got.Status, got.LastReplyRole)
	}
	msgs, _ := r.ListMessages(ctx, tk.ID)
	if len(msgs) != 2 || msgs[0].AuthorRole != ticket.AuthorUser || msgs[1].AuthorRole != ticket.AuthorAgent {
		t.Fatalf("messages order/roles = %+v", msgs)
	}
}

func TestSetStatusAndNotFound(t *testing.T) {
	r := newTestRepo(t)
	tk := seed(t, r, 1, 100, "help")
	if err := r.SetStatus(ctx, tk.ID, ticket.StatusResolved, ticket.Scope{}); err != nil {
		t.Fatalf("set status: %v", err)
	}
	got, _ := r.GetByID(ctx, tk.ID)
	if got.Status != ticket.StatusResolved {
		t.Fatalf("status = %s, want resolved", got.Status)
	}
	if err := r.SetStatus(ctx, 999999, ticket.StatusClosed, ticket.Scope{}); !apperr.Is(err, "TICKET_NOT_FOUND") {
		t.Fatalf("set status missing err = %v", err)
	}
}

func TestListPaginationAndFilters(t *testing.T) {
	r := newTestRepo(t)
	// 5 张同一用户工单，last_reply_at 递增以固定倒序。
	base := time.Now()
	for i := 0; i < 5; i++ {
		tk := &ticket.SupportTicket{
			TenantID: 1, UserID: 100, Title: "t", Status: ticket.StatusOpen, Priority: ticket.PriorityNormal,
			MessageCount: 1, LastReplyAt: base.Add(time.Duration(i) * time.Minute), LastReplyRole: ticket.AuthorUser,
			CreatedAt: base, UpdatedAt: base,
		}
		if err := r.Create(ctx, tk, &ticket.TicketMessage{TenantID: 1, UserID: 100, AuthorRole: ticket.AuthorUser, Content: "x", CreatedAt: base}); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	items, total, _ := r.ListForUser(ctx, 100, ticket.TicketFilter{Page: 1, PageSize: 2})
	if total != 5 || len(items) != 2 {
		t.Fatalf("page1 = %d items, total %d, want 2/5", len(items), total)
	}
	// 倒序：最新 last_reply_at（i=4）在最前。
	if !items[0].LastReplyAt.After(items[1].LastReplyAt) {
		t.Fatalf("not sorted desc by last_reply_at: %v vs %v", items[0].LastReplyAt, items[1].LastReplyAt)
	}
	// status 过滤：无 resolved → 0。
	_, total, _ = r.ListForUser(ctx, 100, ticket.TicketFilter{Status: ticket.StatusResolved})
	if total != 0 {
		t.Fatalf("resolved filter total = %d, want 0", total)
	}
}

// TestMutationScopeBinding：AddReply/SetStatus 的作用域约束（E1 加固）——
// 跨作用域一律 ErrNotFound 且失败回复回滚不留消息；同作用域放行。写操作自带隔离，不依赖 service 前置 Get。
func TestMutationScopeBinding(t *testing.T) {
	r := newTestRepo(t)
	tk := seed(t, r, 1, 100, "scoped") // tenant=1, user=100

	// SetStatus：跨用户 → NOT_FOUND；本人 → 放行。
	if err := r.SetStatus(ctx, tk.ID, ticket.StatusResolved, ticket.Scope{UserID: 999}); !apperr.Is(err, "TICKET_NOT_FOUND") {
		t.Fatalf("cross-user SetStatus err = %v, want TICKET_NOT_FOUND", err)
	}
	if err := r.SetStatus(ctx, tk.ID, ticket.StatusResolved, ticket.Scope{UserID: 100}); err != nil {
		t.Fatalf("owner SetStatus: %v", err)
	}

	// AddReply：跨租户 → NOT_FOUND 且消息回滚；本租户 → 放行。
	mBad := &ticket.TicketMessage{TicketID: tk.ID, TenantID: 2, UserID: 500, AuthorRole: ticket.AuthorAgent, Content: "x", CreatedAt: time.Now()}
	if err := r.AddReply(ctx, mBad, ticket.StatusPending, ticket.Scope{TenantID: 2}); !apperr.Is(err, "TICKET_NOT_FOUND") {
		t.Fatalf("cross-tenant AddReply err = %v, want TICKET_NOT_FOUND", err)
	}
	mOK := &ticket.TicketMessage{TicketID: tk.ID, TenantID: 1, UserID: 500, AuthorRole: ticket.AuthorAgent, Content: "y", CreatedAt: time.Now()}
	if err := r.AddReply(ctx, mOK, ticket.StatusPending, ticket.Scope{TenantID: 1}); err != nil {
		t.Fatalf("in-tenant AddReply: %v", err)
	}
	// 开帖(1) + 成功回复(1) = 2；跨租户失败的回复必须已回滚。
	got, _ := r.GetByID(ctx, tk.ID)
	if got.MessageCount != 2 {
		t.Fatalf("message_count = %d, want 2 (failed cross-tenant reply must roll back)", got.MessageCount)
	}
	if msgs, _ := r.ListMessages(ctx, tk.ID); len(msgs) != 2 {
		t.Fatalf("messages = %d, want 2 (rolled-back reply must not persist)", len(msgs))
	}
}
