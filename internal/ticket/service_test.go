package ticket

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

var bg = context.Background()

func newSvc() (TicketService, *MemRepo) {
	repo := NewMemRepo()
	return NewService(repo), repo
}

func mustCreate(t *testing.T, s TicketService, tenantID, userID int64, title string) *SupportTicket {
	t.Helper()
	tk, err := s.Create(bg, CreateInput{TenantID: tenantID, UserID: userID, Title: title, Content: "body"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return tk
}

func TestCreate_ValidationAndDefaults(t *testing.T) {
	s, _ := newSvc()

	if _, err := s.Create(bg, CreateInput{UserID: 1, Title: "  ", Content: "x"}); !apperr.Is(err, "TICKET_INPUT_INVALID") {
		t.Fatalf("blank title err = %v", err)
	}
	if _, err := s.Create(bg, CreateInput{UserID: 1, Title: "t", Content: "   "}); !apperr.Is(err, "TICKET_INPUT_INVALID") {
		t.Fatalf("blank content err = %v", err)
	}
	if _, err := s.Create(bg, CreateInput{UserID: 1, Title: "t", Content: "x", Priority: "bogus"}); !apperr.Is(err, "TICKET_PRIORITY_INVALID") {
		t.Fatalf("bad priority err = %v", err)
	}

	tk, err := s.Create(bg, CreateInput{TenantID: 5, UserID: 9, Title: "help", Content: "please"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if tk.Status != StatusOpen {
		t.Fatalf("status = %s, want open", tk.Status)
	}
	if tk.Priority != PriorityNormal {
		t.Fatalf("priority = %s, want normal (default)", tk.Priority)
	}
	if tk.MessageCount != 1 || tk.LastReplyRole != AuthorUser {
		t.Fatalf("opening message not counted: count=%d role=%s", tk.MessageCount, tk.LastReplyRole)
	}
	// 开帖作为第一条 user 消息落库。
	det, err := s.GetForUser(bg, 9, tk.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(det.Messages) != 1 || det.Messages[0].AuthorRole != AuthorUser || det.Messages[0].Content != "please" {
		t.Fatalf("opening message = %+v", det.Messages)
	}
}

// TestUserIsolation：用户 A 看不到用户 B 的工单（列表 + 详情 + 回复 + 关闭全维度）。
func TestUserIsolation(t *testing.T) {
	s, _ := newSvc()
	a := mustCreate(t, s, 1, 100, "A ticket")
	_ = mustCreate(t, s, 1, 200, "B ticket")

	// 列表：用户 200 只见自己的 1 条。
	pageB, _ := s.ListForUser(bg, 200, TicketFilter{})
	if pageB.Total != 1 || pageB.Items[0].UserID != 200 {
		t.Fatalf("user 200 list leaked: %+v", pageB)
	}
	// 详情：用户 200 读用户 100 的工单 → NOT_FOUND（IDOR 坍缩）。
	if _, err := s.GetForUser(bg, 200, a.ID); !apperr.Is(err, "TICKET_NOT_FOUND") {
		t.Fatalf("cross-user get err = %v", err)
	}
	// 回复：用户 200 回复用户 100 的工单 → NOT_FOUND。
	if _, err := s.ReplyAsUser(bg, 200, a.ID, "hi"); !apperr.Is(err, "TICKET_NOT_FOUND") {
		t.Fatalf("cross-user reply err = %v", err)
	}
	// 关闭：用户 200 关闭用户 100 的工单 → NOT_FOUND。
	if err := s.CloseByUser(bg, 200, a.ID); !apperr.Is(err, "TICKET_NOT_FOUND") {
		t.Fatalf("cross-user close err = %v", err)
	}
}

// TestTenantIsolation：代理 A（租户 1）看不到代理 B（租户 2）的工单，传入的作用域是权威 tenantID。
func TestTenantIsolation(t *testing.T) {
	s, _ := newSvc()
	t1 := mustCreate(t, s, 1, 100, "tenant1 ticket")
	_ = mustCreate(t, s, 2, 300, "tenant2 ticket")

	// 租户 2 列表只见自己的。
	p2, _ := s.ListForTenant(bg, 2, TicketFilter{})
	if p2.Total != 1 || p2.Items[0].TenantID != 2 {
		t.Fatalf("tenant 2 list leaked: %+v", p2)
	}
	// 租户 2 读租户 1 的工单 → NOT_FOUND。
	if _, err := s.GetForTenant(bg, 2, t1.ID); !apperr.Is(err, "TICKET_NOT_FOUND") {
		t.Fatalf("cross-tenant get err = %v", err)
	}
	// 租户 2 回复/改状态租户 1 的工单 → NOT_FOUND。
	if _, err := s.ReplyAsAgent(bg, 2, 999, t1.ID, "hi"); !apperr.Is(err, "TICKET_NOT_FOUND") {
		t.Fatalf("cross-tenant reply err = %v", err)
	}
	if err := s.SetStatusAsAgent(bg, 2, t1.ID, StatusResolved); !apperr.Is(err, "TICKET_NOT_FOUND") {
		t.Fatalf("cross-tenant status err = %v", err)
	}
}

// TestAdminCrossTenant：管理员可跨租户读取任意租户工单。
func TestAdminCrossTenant(t *testing.T) {
	s, _ := newSvc()
	t1 := mustCreate(t, s, 1, 100, "t1")
	t2 := mustCreate(t, s, 2, 200, "t2")
	p0 := mustCreate(t, s, 0, 300, "platform") // 主站平台工单（tenant_id=0）

	all, _ := s.ListForAdmin(bg, TicketFilter{})
	if all.Total != 3 {
		t.Fatalf("admin list total = %d, want 3", all.Total)
	}
	for _, id := range []int64{t1.ID, t2.ID, p0.ID} {
		if _, err := s.GetForAdmin(bg, id); err != nil {
			t.Fatalf("admin get %d: %v", id, err)
		}
	}
	// 管理端按租户精确筛选（含平台 tenant_id=0）。
	zero := int64(0)
	pf, _ := s.ListForAdmin(bg, TicketFilter{TenantID: &zero})
	if pf.Total != 1 || pf.Items[0].TenantID != 0 {
		t.Fatalf("admin tenant=0 filter = %+v", pf)
	}
}

// TestReplyReopenAndClosedRules：用户回复重开、客服回复置 pending、已关闭不可回复、空回复拒绝。
func TestReplyReopenAndClosedRules(t *testing.T) {
	s, _ := newSvc()
	tk := mustCreate(t, s, 1, 100, "help")

	// 客服回复 → pending。
	if _, err := s.ReplyAsAgent(bg, 1, 500, tk.ID, "on it"); err != nil {
		t.Fatalf("agent reply: %v", err)
	}
	if d, _ := s.GetForTenant(bg, 1, tk.ID); d.Ticket.Status != StatusPending || d.Ticket.LastReplyRole != AuthorAgent {
		t.Fatalf("after agent reply status=%s role=%s, want pending/agent", d.Ticket.Status, d.Ticket.LastReplyRole)
	}
	// 用户回复 → 重开为 open。
	if _, err := s.ReplyAsUser(bg, 100, tk.ID, "thanks, more info"); err != nil {
		t.Fatalf("user reply: %v", err)
	}
	if d, _ := s.GetForUser(bg, 100, tk.ID); d.Ticket.Status != StatusOpen {
		t.Fatalf("user reply should reopen to open, got %s", d.Ticket.Status)
	}
	// 空回复被拒。
	if _, err := s.ReplyAsUser(bg, 100, tk.ID, "   "); !apperr.Is(err, "TICKET_REPLY_EMPTY") {
		t.Fatalf("empty reply err = %v", err)
	}
	// 用户关闭 → closed；已关闭再回复 → TICKET_CLOSED。
	if err := s.CloseByUser(bg, 100, tk.ID); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := s.ReplyAsUser(bg, 100, tk.ID, "reopen?"); !apperr.Is(err, "TICKET_CLOSED") {
		t.Fatalf("reply to closed err = %v", err)
	}
	if _, err := s.ReplyAsAgent(bg, 1, 500, tk.ID, "closed"); !apperr.Is(err, "TICKET_CLOSED") {
		t.Fatalf("agent reply to closed err = %v", err)
	}
}

// TestStatusTransitions：closed 仅可重开为 open；非法枚举/流转→TICKET_STATUS_INVALID。
func TestStatusTransitions(t *testing.T) {
	s, _ := newSvc()
	tk := mustCreate(t, s, 1, 100, "help")

	// open → resolved（合法）。
	if err := s.SetStatusAsAgent(bg, 1, tk.ID, StatusResolved); err != nil {
		t.Fatalf("open->resolved: %v", err)
	}
	// resolved → closed（合法）。
	if err := s.SetStatusAsAgent(bg, 1, tk.ID, StatusClosed); err != nil {
		t.Fatalf("resolved->closed: %v", err)
	}
	// closed → resolved（非法：终态只能重开 open）。
	if err := s.SetStatusAsAgent(bg, 1, tk.ID, StatusResolved); !apperr.Is(err, "TICKET_STATUS_INVALID") {
		t.Fatalf("closed->resolved err = %v", err)
	}
	// closed → open（合法重开）。
	if err := s.SetStatusAsAgent(bg, 1, tk.ID, StatusOpen); err != nil {
		t.Fatalf("closed->open reopen: %v", err)
	}
	// 非法枚举值。
	if err := s.SetStatusAsAgent(bg, 1, tk.ID, TicketStatus("bogus")); !apperr.Is(err, "TICKET_STATUS_INVALID") {
		t.Fatalf("bogus status err = %v", err)
	}
}

// TestCloseAlreadyClosed：关闭已关闭工单 → TICKET_STATUS_INVALID。
func TestCloseAlreadyClosed(t *testing.T) {
	s, _ := newSvc()
	tk := mustCreate(t, s, 1, 100, "help")
	if err := s.CloseByUser(bg, 100, tk.ID); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := s.CloseByUser(bg, 100, tk.ID); !apperr.Is(err, "TICKET_STATUS_INVALID") {
		t.Fatalf("re-close err = %v", err)
	}
}
