package ticket

import "context"

// --- 对外服务接口（三端各一组；scoping key 作为显式参数强制隔离）---

// TicketService 是支持工单的领域服务：状态流转规则 + 输入校验 + 归属复校。
//
// 隔离契约：用户端方法带 userID（会话权威），代理端方法带 tenantID（AgentOwnerAuth 权威），
// 管理端方法不带作用域（受 AdminAuth 保护，跨租户）。每次回复/关闭/状态变更前都会用对应作用域
// 重新 Get 校验归属——不存在或跨作用域一律 ErrNotFound。
type TicketService interface {
	// Create 新建工单（status=open，开帖作为第一条 user 消息）。in.TenantID 由调用方从提交用户
	// users.tenant_id 派生固化，绝不来自客户端。
	Create(ctx context.Context, in CreateInput) (*SupportTicket, error)

	// --- 用户端（按会话 user_id 隔离）---
	ListForUser(ctx context.Context, userID int64, f TicketFilter) (*TicketPage, error)
	GetForUser(ctx context.Context, userID, id int64) (*TicketDetail, error)
	// ReplyAsUser 追加一条 user 消息；已关闭→ErrClosed；否则重置为 open（pending/resolved→open）。
	ReplyAsUser(ctx context.Context, userID, id int64, content string) (*TicketMessage, error)
	// CloseByUser 用户关闭自己的工单（open/pending/resolved→closed；已关闭→ErrStatusInvalid）。
	CloseByUser(ctx context.Context, userID, id int64) error

	// --- 代理端（按权威 tenant_id 隔离）---
	ListForTenant(ctx context.Context, tenantID int64, f TicketFilter) (*TicketPage, error)
	GetForTenant(ctx context.Context, tenantID, id int64) (*TicketDetail, error)
	// ReplyAsAgent 代理追加一条 agent 消息；已关闭→ErrClosed；否则置 pending。
	ReplyAsAgent(ctx context.Context, tenantID, actorUserID, id int64, content string) (*TicketMessage, error)
	// SetStatusAsAgent 代理改状态（校验流转合法性）。
	SetStatusAsAgent(ctx context.Context, tenantID, id int64, status TicketStatus) error

	// --- 管理端（跨租户，AdminAuth 保护）---
	ListForAdmin(ctx context.Context, f TicketFilter) (*TicketPage, error)
	GetForAdmin(ctx context.Context, id int64) (*TicketDetail, error)
	// ReplyAsAdmin 管理员追加一条 admin 消息；已关闭→ErrClosed；否则置 pending。
	ReplyAsAdmin(ctx context.Context, actorUserID, id int64, content string) (*TicketMessage, error)
	// SetStatusAsAdmin 管理员改状态（校验流转合法性）。
	SetStatusAsAdmin(ctx context.Context, id int64, status TicketStatus) error
}

// Scope 限定「变更类」仓储操作（AddReply/SetStatus）可作用的工单作用域，使隔离下沉到写操作本身：
// 用户端传 {UserID: 会话uid}，代理端传 {TenantID: 权威租户}，管理端传零值（跨租户）。
// 任一维度 >0 即作为 WHERE 约束；命中不到（含跨作用域）→ ErrNotFound。杜绝未来调用方绕过前置 Get 造成越权写。
type Scope struct {
	UserID   int64
	TenantID int64
}

// matches 报告工单 t 是否落在作用域内（零值维度不约束）。供内存实现使用。
func (sc Scope) matches(t SupportTicket) bool {
	if sc.UserID > 0 && t.UserID != sc.UserID {
		return false
	}
	if sc.TenantID > 0 && t.TenantID != sc.TenantID {
		return false
	}
	return true
}

// --- 消费者定义的持久化接口（本包声明，main/wire 装配具体实现）---

// TicketRepo 是工单持久化抽象。隔离在此层强制：*ForUser 必带 WHERE user_id=?，
// *ForTenant 必带 WHERE tenant_id=?，跨作用域读一律返回 ErrNotFound。
type TicketRepo interface {
	// Create 在单事务内插入工单 + 开帖消息，回填 t.ID / opening.ID。
	Create(ctx context.Context, t *SupportTicket, opening *TicketMessage) error

	// --- 用户作用域 ---
	ListForUser(ctx context.Context, userID int64, f TicketFilter) ([]SupportTicket, int, error)
	GetForUser(ctx context.Context, userID, id int64) (*SupportTicket, error)

	// --- 租户作用域 ---
	ListForTenant(ctx context.Context, tenantID int64, f TicketFilter) ([]SupportTicket, int, error)
	GetForTenant(ctx context.Context, tenantID, id int64) (*SupportTicket, error)

	// --- 管理作用域（无租户约束；f.TenantID 指针可选精确匹配）---
	ListForAdmin(ctx context.Context, f TicketFilter) ([]SupportTicket, int, error)
	GetByID(ctx context.Context, id int64) (*SupportTicket, error)

	// ListMessages 返回某工单的全部消息（按 id 升序）。调用方须已通过对应作用域 Get 校验归属。
	ListMessages(ctx context.Context, ticketID int64) ([]TicketMessage, error)

	// AddReply 在单事务内插入消息并原子更新工单计数/最后回复/状态。回填 m.ID。
	// sc 限定作用域：写操作自带 user_id/tenant_id 约束，跨作用域→ErrNotFound（纵深防御，不依赖调用方前置校验）。
	AddReply(ctx context.Context, m *TicketMessage, newStatus TicketStatus, sc Scope) error

	// SetStatus 更新状态 + updated_at；sc 限定作用域（同 AddReply）；工单不存在/跨作用域→ErrNotFound。
	SetStatus(ctx context.Context, id int64, status TicketStatus, sc Scope) error
}
