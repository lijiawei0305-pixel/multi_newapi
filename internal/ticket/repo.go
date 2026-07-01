package ticket

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
)

// MemRepo 是 TicketRepo 的并发安全内存假实现，作为单测默认替身（真实持久化见 gormrepo 子包）。
// 隔离与真实实现同口径：*ForUser 按 user_id、*ForTenant 按 tenant_id 过滤，跨作用域读返回 ErrNotFound。
type MemRepo struct {
	mu         sync.RWMutex
	tickets    map[int64]SupportTicket
	messages   map[int64][]TicketMessage // ticketID -> messages
	nextTID    int64
	nextMID    int64
	now        func() time.Time
}

// NewMemRepo 构造内存仓储。
func NewMemRepo() *MemRepo {
	return &MemRepo{
		tickets:  map[int64]SupportTicket{},
		messages: map[int64][]TicketMessage{},
		now:      time.Now,
	}
}

var _ TicketRepo = (*MemRepo)(nil)

func (r *MemRepo) Create(ctx context.Context, t *SupportTicket, opening *TicketMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextTID++
	t.ID = r.nextTID
	r.tickets[t.ID] = *t
	if opening != nil {
		r.nextMID++
		opening.ID = r.nextMID
		opening.TicketID = t.ID
		r.messages[t.ID] = append(r.messages[t.ID], *opening)
	}
	return nil
}

func (r *MemRepo) GetForUser(ctx context.Context, userID, id int64) (*SupportTicket, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tickets[id]
	if !ok || t.UserID != userID {
		return nil, ErrNotFound
	}
	cp := t
	return &cp, nil
}

func (r *MemRepo) GetForTenant(ctx context.Context, tenantID, id int64) (*SupportTicket, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tickets[id]
	if !ok || t.TenantID != tenantID {
		return nil, ErrNotFound
	}
	cp := t
	return &cp, nil
}

func (r *MemRepo) GetByID(ctx context.Context, id int64) (*SupportTicket, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tickets[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := t
	return &cp, nil
}

func (r *MemRepo) ListForUser(ctx context.Context, userID int64, f TicketFilter) ([]SupportTicket, int, error) {
	return r.list(f, func(t SupportTicket) bool { return t.UserID == userID })
}

func (r *MemRepo) ListForTenant(ctx context.Context, tenantID int64, f TicketFilter) ([]SupportTicket, int, error) {
	return r.list(f, func(t SupportTicket) bool { return t.TenantID == tenantID })
}

func (r *MemRepo) ListForAdmin(ctx context.Context, f TicketFilter) ([]SupportTicket, int, error) {
	return r.list(f, func(t SupportTicket) bool {
		if f.TenantID != nil && t.TenantID != *f.TenantID {
			return false
		}
		return true
	})
}

// list 应用作用域谓词 + 过滤条件 + 排序（last_reply_at 倒序）+ 分页；返回本页项与匹配总数。
func (r *MemRepo) list(f TicketFilter, scope func(SupportTicket) bool) ([]SupportTicket, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	matched := make([]SupportTicket, 0)
	for _, t := range r.tickets {
		if !scope(t) {
			continue
		}
		if f.Status != "" && t.Status != f.Status {
			continue
		}
		if f.Priority != "" && t.Priority != f.Priority {
			continue
		}
		if f.UserID > 0 && t.UserID != f.UserID {
			continue
		}
		if f.Keyword != "" && !strings.Contains(strings.ToLower(t.Title), strings.ToLower(f.Keyword)) {
			continue
		}
		matched = append(matched, t)
	}
	sort.Slice(matched, func(i, j int) bool {
		if matched[i].LastReplyAt.Equal(matched[j].LastReplyAt) {
			return matched[i].ID > matched[j].ID
		}
		return matched[i].LastReplyAt.After(matched[j].LastReplyAt)
	})
	total := len(matched)
	page, size := f.Page, f.PageSize
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = total
	}
	start := (page - 1) * size
	if start >= total {
		return []SupportTicket{}, total, nil
	}
	end := start + size
	if end > total {
		end = total
	}
	return matched[start:end], total, nil
}

func (r *MemRepo) ListMessages(ctx context.Context, ticketID int64) ([]TicketMessage, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	msgs := append([]TicketMessage(nil), r.messages[ticketID]...)
	sort.Slice(msgs, func(i, j int) bool { return msgs[i].ID < msgs[j].ID })
	return msgs, nil
}

func (r *MemRepo) AddReply(ctx context.Context, m *TicketMessage, newStatus TicketStatus, sc Scope) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tickets[m.TicketID]
	if !ok || !sc.matches(t) {
		return ErrNotFound
	}
	r.nextMID++
	m.ID = r.nextMID
	r.messages[m.TicketID] = append(r.messages[m.TicketID], *m)
	t.MessageCount++
	t.LastReplyAt = m.CreatedAt
	t.LastReplyRole = m.AuthorRole
	t.Status = newStatus
	t.UpdatedAt = m.CreatedAt
	r.tickets[m.TicketID] = t
	return nil
}

func (r *MemRepo) SetStatus(ctx context.Context, id int64, status TicketStatus, sc Scope) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tickets[id]
	if !ok || !sc.matches(t) {
		return ErrNotFound
	}
	t.Status = status
	t.UpdatedAt = r.now()
	r.tickets[id] = t
	return nil
}
