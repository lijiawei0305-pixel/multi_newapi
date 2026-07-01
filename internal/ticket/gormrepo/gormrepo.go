// Package gormrepo 用真实 GORM 实现 ticket.TicketRepo（support_tickets + support_ticket_messages 两表）。
//
// 隔离不变量（每次读/写强制）：
//   - GetForUser/ListForUser 强制 WHERE user_id=?；
//   - GetForTenant/ListForTenant 强制 WHERE tenant_id=?；
//   - GetByID/ListForAdmin 无租户约束（管理端，AdminAuth 保护；f.TenantID 指针可选精确匹配）。
//
// 跨作用域读命中不到 → ticket.ErrNotFound（IDOR 坍缩，与 moderation scopeByTenant 同口径）。
// 表名 support_*：经核查与 new-api 原生表无冲突；不改任何原生 model struct。
package gormrepo

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/ticket"
)

// ticketRow 是 support_tickets 表的 GORM 模型。
type ticketRow struct {
	ID            int64     `gorm:"column:id;primaryKey;autoIncrement"`
	TenantID      int64     `gorm:"column:tenant_id;not null;default:0;index:idx_st_tenant;index:idx_st_tenant_status,priority:1"`
	UserID        int64     `gorm:"column:user_id;not null;index:idx_st_user;index:idx_st_user_created,priority:1"`
	Title         string    `gorm:"column:title;type:varchar(255);not null"`
	Status        string    `gorm:"column:status;type:varchar(16);not null;default:'open';index:idx_st_tenant_status,priority:2"`
	Priority      string    `gorm:"column:priority;type:varchar(16);not null;default:'normal'"`
	MessageCount  int       `gorm:"column:message_count;not null;default:0"`
	LastReplyAt   time.Time `gorm:"column:last_reply_at"`
	LastReplyRole string    `gorm:"column:last_reply_role;type:varchar(16);not null;default:''"`
	CreatedAt     time.Time `gorm:"column:created_at;index:idx_st_user_created,priority:2"`
	UpdatedAt     time.Time `gorm:"column:updated_at"`
}

// TableName 固定表名。
func (ticketRow) TableName() string { return "support_tickets" }

// ticketMessageRow 是 support_ticket_messages 表的 GORM 模型。
type ticketMessageRow struct {
	ID         int64     `gorm:"column:id;primaryKey;autoIncrement"`
	TicketID   int64     `gorm:"column:ticket_id;not null;index:idx_stm_ticket"`
	TenantID   int64     `gorm:"column:tenant_id;not null;default:0"`
	UserID     int64     `gorm:"column:user_id;not null;default:0"`
	AuthorRole string    `gorm:"column:author_role;type:varchar(16);not null;default:'user'"`
	Content    string    `gorm:"column:content;type:text;not null"`
	CreatedAt  time.Time `gorm:"column:created_at"`
}

// TableName 固定表名。
func (ticketMessageRow) TableName() string { return "support_ticket_messages" }

// Repo 是 ticket.TicketRepo 的 GORM 实现。
type Repo struct {
	db  *gorm.DB
	now func() time.Time
}

var _ ticket.TicketRepo = (*Repo)(nil)

// New 用已建立连接的 *gorm.DB 构造仓储。
func New(db *gorm.DB) *Repo { return &Repo{db: db, now: time.Now} }

// AutoMigrate 建/补工单两表结构。由 mtwire.Migrate 在 master 节点调用（幂等）。
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&ticketRow{}, &ticketMessageRow{})
}

// Create 在单事务内插入工单 + 开帖消息，回填 ID。
func (r *Repo) Create(ctx context.Context, t *ticket.SupportTicket, opening *ticket.TicketMessage) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row := toTicketRow(t)
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		t.ID = row.ID
		if opening != nil {
			opening.TicketID = row.ID
			mrow := toMsgRow(opening)
			if err := tx.Create(&mrow).Error; err != nil {
				return err
			}
			opening.ID = mrow.ID
		}
		return nil
	})
}

// --- 用户作用域 ---

func (r *Repo) ListForUser(ctx context.Context, userID int64, f ticket.TicketFilter) ([]ticket.SupportTicket, int, error) {
	return r.page(ctx, func(q *gorm.DB) *gorm.DB { return q.Where("user_id = ?", userID) }, f)
}

func (r *Repo) GetForUser(ctx context.Context, userID, id int64) (*ticket.SupportTicket, error) {
	return r.getScoped(ctx, func(q *gorm.DB) *gorm.DB { return q.Where("id = ? AND user_id = ?", id, userID) })
}

// --- 租户作用域 ---

func (r *Repo) ListForTenant(ctx context.Context, tenantID int64, f ticket.TicketFilter) ([]ticket.SupportTicket, int, error) {
	return r.page(ctx, func(q *gorm.DB) *gorm.DB { return q.Where("tenant_id = ?", tenantID) }, f)
}

func (r *Repo) GetForTenant(ctx context.Context, tenantID, id int64) (*ticket.SupportTicket, error) {
	return r.getScoped(ctx, func(q *gorm.DB) *gorm.DB { return q.Where("id = ? AND tenant_id = ?", id, tenantID) })
}

// --- 管理作用域（无租户约束）---

func (r *Repo) ListForAdmin(ctx context.Context, f ticket.TicketFilter) ([]ticket.SupportTicket, int, error) {
	return r.page(ctx, func(q *gorm.DB) *gorm.DB { return q }, f)
}

func (r *Repo) GetByID(ctx context.Context, id int64) (*ticket.SupportTicket, error) {
	return r.getScoped(ctx, func(q *gorm.DB) *gorm.DB { return q.Where("id = ?", id) })
}

// --- 消息 / 变更 ---

func (r *Repo) ListMessages(ctx context.Context, ticketID int64) ([]ticket.TicketMessage, error) {
	var rows []ticketMessageRow
	if err := r.db.WithContext(ctx).
		Where("ticket_id = ?", ticketID).
		Order("id asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ticket.TicketMessage, 0, len(rows))
	for i := range rows {
		out = append(out, toMessage(&rows[i]))
	}
	return out, nil
}

// AddReply 单事务：插入消息 + 原子递增 message_count、更新 last_reply_*、status、updated_at。
// 工单更新受 sc 作用域约束（user_id/tenant_id），跨作用域命中 0 行→回滚并 ErrNotFound。
func (r *Repo) AddReply(ctx context.Context, m *ticket.TicketMessage, newStatus ticket.TicketStatus, sc ticket.Scope) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		mrow := toMsgRow(m)
		if err := tx.Create(&mrow).Error; err != nil {
			return err
		}
		m.ID = mrow.ID
		res := scopeWrite(tx.Model(&ticketRow{}).Where("id = ?", m.TicketID), sc).Updates(map[string]any{
			"message_count":   gorm.Expr("message_count + 1"),
			"last_reply_at":   m.CreatedAt,
			"last_reply_role": string(m.AuthorRole),
			"status":          string(newStatus),
			"updated_at":      m.CreatedAt,
		})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ticket.ErrNotFound
		}
		return nil
	})
}

func (r *Repo) SetStatus(ctx context.Context, id int64, status ticket.TicketStatus, sc ticket.Scope) error {
	res := scopeWrite(r.db.WithContext(ctx).Model(&ticketRow{}).Where("id = ?", id), sc).Updates(map[string]any{
		"status":     string(status),
		"updated_at": r.now(),
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ticket.ErrNotFound
	}
	return nil
}

// ---- 内部辅助 ----

// scopeWrite 给「变更类」query 追加作用域谓词（user_id/tenant_id 任一 >0 即约束），
// 使写操作自带隔离；命中 0 行由调用方判为 ErrNotFound。
func scopeWrite(q *gorm.DB, sc ticket.Scope) *gorm.DB {
	if sc.UserID > 0 {
		q = q.Where("user_id = ?", sc.UserID)
	}
	if sc.TenantID > 0 {
		q = q.Where("tenant_id = ?", sc.TenantID)
	}
	return q
}

// getScoped 用作用域谓词取单张工单；未命中（含跨作用域）→ ErrNotFound。
func (r *Repo) getScoped(ctx context.Context, scope func(*gorm.DB) *gorm.DB) (*ticket.SupportTicket, error) {
	var row ticketRow
	q := scope(r.db.WithContext(ctx).Model(&ticketRow{}))
	if err := q.Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ticket.ErrNotFound
		}
		return nil, err
	}
	t := toTicket(&row)
	return &t, nil
}

// page 应用作用域 + 过滤 + 计数 + 排序分页。为避免 gorm Count 污染后续 Find，两次都从新建 query 组装。
func (r *Repo) page(ctx context.Context, scope func(*gorm.DB) *gorm.DB, f ticket.TicketFilter) ([]ticket.SupportTicket, int, error) {
	build := func() *gorm.DB {
		return applyFilters(scope(r.db.WithContext(ctx).Model(&ticketRow{})), f)
	}
	var total int64
	if err := build().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page, size := f.Page, f.PageSize
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	var rows []ticketRow
	if err := build().
		Order("last_reply_at desc, id desc").
		Limit(size).Offset((page - 1) * size).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	out := make([]ticket.SupportTicket, 0, len(rows))
	for i := range rows {
		out = append(out, toTicket(&rows[i]))
	}
	return out, int(total), nil
}

// applyFilters 追加 status/priority/user_id/keyword/tenant_id(指针) 过滤（零值/nil 表示不限）。
func applyFilters(q *gorm.DB, f ticket.TicketFilter) *gorm.DB {
	if f.Status != "" {
		q = q.Where("status = ?", string(f.Status))
	}
	if f.Priority != "" {
		q = q.Where("priority = ?", string(f.Priority))
	}
	if f.UserID > 0 {
		q = q.Where("user_id = ?", f.UserID)
	}
	if f.TenantID != nil {
		q = q.Where("tenant_id = ?", *f.TenantID)
	}
	if f.Keyword != "" {
		q = q.Where("title LIKE ?", "%"+f.Keyword+"%")
	}
	return q
}

func toTicketRow(t *ticket.SupportTicket) ticketRow {
	return ticketRow{
		ID:            t.ID,
		TenantID:      t.TenantID,
		UserID:        t.UserID,
		Title:         t.Title,
		Status:        string(t.Status),
		Priority:      string(t.Priority),
		MessageCount:  t.MessageCount,
		LastReplyAt:   t.LastReplyAt,
		LastReplyRole: string(t.LastReplyRole),
		CreatedAt:     t.CreatedAt,
		UpdatedAt:     t.UpdatedAt,
	}
}

func toMsgRow(m *ticket.TicketMessage) ticketMessageRow {
	return ticketMessageRow{
		ID:         m.ID,
		TicketID:   m.TicketID,
		TenantID:   m.TenantID,
		UserID:     m.UserID,
		AuthorRole: string(m.AuthorRole),
		Content:    m.Content,
		CreatedAt:  m.CreatedAt,
	}
}

func toTicket(row *ticketRow) ticket.SupportTicket {
	return ticket.SupportTicket{
		ID:            row.ID,
		TenantID:      row.TenantID,
		UserID:        row.UserID,
		Title:         row.Title,
		Status:        ticket.TicketStatus(row.Status),
		Priority:      ticket.TicketPriority(row.Priority),
		MessageCount:  row.MessageCount,
		LastReplyAt:   row.LastReplyAt,
		LastReplyRole: ticket.AuthorRole(row.LastReplyRole),
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
}

func toMessage(row *ticketMessageRow) ticket.TicketMessage {
	return ticket.TicketMessage{
		ID:         row.ID,
		TicketID:   row.TicketID,
		TenantID:   row.TenantID,
		UserID:     row.UserID,
		AuthorRole: ticket.AuthorRole(row.AuthorRole),
		Content:    row.Content,
		CreatedAt:  row.CreatedAt,
	}
}
