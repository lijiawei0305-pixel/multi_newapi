package ticket

import (
	"context"
	"strings"
	"time"
)

// maxTitleLen 标题最大字符数（rune 计），与 support_tickets.title varchar(255) 对齐留余量。
const maxTitleLen = 200

// service 是 TicketService 的实现。依赖以接口注入，便于单测（MemRepo）。
type service struct {
	repo TicketRepo
	now  func() time.Time
}

var _ TicketService = (*service)(nil)

// NewService 构造工单服务。
func NewService(repo TicketRepo) TicketService {
	return &service{repo: repo, now: time.Now}
}

// Create 新建工单：校验标题/正文/优先级 → 落工单(status=open) + 开帖(第一条 user 消息)。
func (s *service) Create(ctx context.Context, in CreateInput) (*SupportTicket, error) {
	title := strings.TrimSpace(in.Title)
	content := strings.TrimSpace(in.Content)
	if in.UserID <= 0 || title == "" || content == "" {
		return nil, ErrInputInvalid
	}
	if len([]rune(title)) > maxTitleLen {
		return nil, ErrInputInvalid
	}
	priority := in.Priority
	if priority == "" {
		priority = PriorityNormal
	}
	if !priority.Valid() {
		return nil, ErrPriorityInvalid
	}
	now := s.now()
	t := &SupportTicket{
		TenantID:      in.TenantID,
		UserID:        in.UserID,
		Title:         title,
		Status:        StatusOpen,
		Priority:      priority,
		MessageCount:  1,
		LastReplyAt:   now,
		LastReplyRole: AuthorUser,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	opening := &TicketMessage{
		TenantID:   in.TenantID,
		UserID:     in.UserID,
		AuthorRole: AuthorUser,
		Content:    content,
		CreatedAt:  now,
	}
	if err := s.repo.Create(ctx, t, opening); err != nil {
		return nil, err
	}
	return t, nil
}

// ---- 用户端 ----

func (s *service) ListForUser(ctx context.Context, userID int64, f TicketFilter) (*TicketPage, error) {
	f = normalizeFilter(f)
	items, total, err := s.repo.ListForUser(ctx, userID, f)
	if err != nil {
		return nil, err
	}
	return &TicketPage{Items: items, Total: total, Page: f.Page, PageSize: f.PageSize}, nil
}

func (s *service) GetForUser(ctx context.Context, userID, id int64) (*TicketDetail, error) {
	t, err := s.repo.GetForUser(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	return s.withMessages(ctx, t)
}

func (s *service) ReplyAsUser(ctx context.Context, userID, id int64, content string) (*TicketMessage, error) {
	t, err := s.repo.GetForUser(ctx, userID, id) // 归属复校（IDOR 防线）
	if err != nil {
		return nil, err
	}
	// 用户回复：已关闭工单不可回复；否则一律重置为 open（等待客服）。
	return s.appendReply(ctx, t, userID, AuthorUser, content, StatusOpen, Scope{UserID: userID})
}

func (s *service) CloseByUser(ctx context.Context, userID, id int64) error {
	t, err := s.repo.GetForUser(ctx, userID, id) // 归属复校
	if err != nil {
		return err
	}
	if err := s.checkTransition(t.Status, StatusClosed); err != nil {
		return err
	}
	return s.repo.SetStatus(ctx, id, StatusClosed, Scope{UserID: userID})
}

// ---- 代理端 ----

func (s *service) ListForTenant(ctx context.Context, tenantID int64, f TicketFilter) (*TicketPage, error) {
	f = normalizeFilter(f)
	items, total, err := s.repo.ListForTenant(ctx, tenantID, f)
	if err != nil {
		return nil, err
	}
	return &TicketPage{Items: items, Total: total, Page: f.Page, PageSize: f.PageSize}, nil
}

func (s *service) GetForTenant(ctx context.Context, tenantID, id int64) (*TicketDetail, error) {
	t, err := s.repo.GetForTenant(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	return s.withMessages(ctx, t)
}

func (s *service) ReplyAsAgent(ctx context.Context, tenantID, actorUserID, id int64, content string) (*TicketMessage, error) {
	t, err := s.repo.GetForTenant(ctx, tenantID, id) // 归属复校（按权威租户）
	if err != nil {
		return nil, err
	}
	// 客服回复：置 pending（等待用户）。
	return s.appendReply(ctx, t, actorUserID, AuthorAgent, content, StatusPending, Scope{TenantID: tenantID})
}

func (s *service) SetStatusAsAgent(ctx context.Context, tenantID, id int64, status TicketStatus) error {
	t, err := s.repo.GetForTenant(ctx, tenantID, id) // 归属复校
	if err != nil {
		return err
	}
	if err := s.checkTransition(t.Status, status); err != nil {
		return err
	}
	return s.repo.SetStatus(ctx, id, status, Scope{TenantID: tenantID})
}

// ---- 管理端（跨租户）----

func (s *service) ListForAdmin(ctx context.Context, f TicketFilter) (*TicketPage, error) {
	f = normalizeFilter(f)
	items, total, err := s.repo.ListForAdmin(ctx, f)
	if err != nil {
		return nil, err
	}
	return &TicketPage{Items: items, Total: total, Page: f.Page, PageSize: f.PageSize}, nil
}

func (s *service) GetForAdmin(ctx context.Context, id int64) (*TicketDetail, error) {
	t, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.withMessages(ctx, t)
}

func (s *service) ReplyAsAdmin(ctx context.Context, actorUserID, id int64, content string) (*TicketMessage, error) {
	t, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.appendReply(ctx, t, actorUserID, AuthorAdmin, content, StatusPending, Scope{})
}

func (s *service) SetStatusAsAdmin(ctx context.Context, id int64, status TicketStatus) error {
	t, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.checkTransition(t.Status, status); err != nil {
		return err
	}
	return s.repo.SetStatus(ctx, id, status, Scope{})
}

// ---- 共享内部逻辑 ----

// appendReply 校验回复内容与关闭态 → 组装消息 → 原子落库（含计数/状态更新）。
func (s *service) appendReply(ctx context.Context, t *SupportTicket, actorUserID int64, role AuthorRole, content string, newStatus TicketStatus, sc Scope) (*TicketMessage, error) {
	body := strings.TrimSpace(content)
	if body == "" {
		return nil, ErrReplyEmpty
	}
	if t.Status == StatusClosed {
		return nil, ErrClosed // 已关闭工单不可回复，需先重开
	}
	m := &TicketMessage{
		TicketID:   t.ID,
		TenantID:   t.TenantID,
		UserID:     actorUserID,
		AuthorRole: role,
		Content:    body,
		CreatedAt:  s.now(),
	}
	if err := s.repo.AddReply(ctx, m, newStatus, sc); err != nil {
		return nil, err
	}
	return m, nil
}

// withMessages 组装工单详情（工单 + 全部消息）。
func (s *service) withMessages(ctx context.Context, t *SupportTicket) (*TicketDetail, error) {
	msgs, err := s.repo.ListMessages(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	return &TicketDetail{Ticket: *t, Messages: msgs}, nil
}

// checkTransition 校验状态流转合法性：
//   - 目标必须是合法枚举（否则 ErrStatusInvalid）；
//   - 从 closed 只能重开为 open（closed→其他一律非法）；
//   - 其余非终态之间可自由流转（含幂等置同态）。
func (s *service) checkTransition(from, to TicketStatus) error {
	if !to.Valid() {
		return ErrStatusInvalid
	}
	if from == StatusClosed && to != StatusOpen {
		return ErrStatusInvalid
	}
	return nil
}

// normalizeFilter 归一分页（默认第 1 页、每页 20，上限 100）。
func normalizeFilter(f TicketFilter) TicketFilter {
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.PageSize <= 0 {
		f.PageSize = 20
	}
	if f.PageSize > 100 {
		f.PageSize = 100
	}
	return f
}
