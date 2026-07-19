package ticket

import "time"

// TicketStatus 工单状态。流转：open → pending → resolved → closed（可从任意非终态互转，
// closed 仅可被重开为 open；具体规则见 service.checkTransition）。
type TicketStatus string

const (
	// StatusOpen 待处理（新建，或用户回复后重新等待客服）。
	StatusOpen TicketStatus = "open"
	// StatusPending 处理中（客服/代理/管理员已回复，等待用户）。
	StatusPending TicketStatus = "pending"
	// StatusResolved 已解决（客服标记完成，用户仍可回复重开）。
	StatusResolved TicketStatus = "resolved"
	// StatusClosed 已关闭（终态；用户/客服关闭，只能重开为 open，不能再回复）。
	StatusClosed TicketStatus = "closed"
)

// Valid 报告状态是否为合法枚举值。
func (s TicketStatus) Valid() bool {
	switch s {
	case StatusOpen, StatusPending, StatusResolved, StatusClosed:
		return true
	default:
		return false
	}
}

// TicketPriority 工单优先级（默认 normal）。
type TicketPriority string

const (
	PriorityLow    TicketPriority = "low"
	PriorityNormal TicketPriority = "normal"
	PriorityHigh   TicketPriority = "high"
	PriorityUrgent TicketPriority = "urgent"
)

// Valid 报告优先级是否为合法枚举值。
func (p TicketPriority) Valid() bool {
	switch p {
	case PriorityLow, PriorityNormal, PriorityHigh, PriorityUrgent:
		return true
	default:
		return false
	}
}

// AuthorRole 一条工单消息的作者身份（驱动前端左右气泡渲染与「等待谁回复」）。
type AuthorRole string

const (
	AuthorUser  AuthorRole = "user"
	AuthorAgent AuthorRole = "agent"
	AuthorAdmin AuthorRole = "admin"
)

// SupportTicket 是一张支持工单。
//
// TenantID 与 UserID 是隔离键：TenantID 在创建时由服务端从提交用户的 users.tenant_id 派生并固化
// （0 = 主站平台工单，仅管理员可见），此后不可变、绝不接受客户端传入；UserID 为提交者（取自 UserAuth）。
type SupportTicket struct {
	ID            int64
	TenantID      int64
	UserID        int64
	Title         string
	Status        TicketStatus
	Priority      TicketPriority
	MessageCount  int        // 冗余：消息总数（含开帖），供列表徽标
	LastReplyAt   time.Time  // 最后回复时间（收件箱排序键）
	LastReplyRole AuthorRole // 最后回复者角色（「等待谁回复」提示）
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// TicketMessage 是一条工单消息（开帖正文即第一条 author_role=user 的消息，渲染统一）。
type TicketMessage struct {
	ID         int64
	TicketID   int64
	TenantID   int64 // 冗余隔离键（纵深防御）
	UserID     int64 // 消息作者
	AuthorRole AuthorRole
	Content    string
	CreatedAt  time.Time
}

// TicketDetail 是工单详情（工单主体 + 全部消息，按时间升序）。
type TicketDetail struct {
	Ticket   SupportTicket
	Messages []TicketMessage
}

// TicketPage 是分页列表结果（对齐 api-contract 分页信封 data:{items,total,page,page_size}）。
type TicketPage struct {
	Items    []SupportTicket
	Total    int
	Page     int
	PageSize int
}

// TicketFilter 是列表查询过滤条件（零值字段表示不限）。
//
// 隔离作用域（user_id / tenant_id）不放这里——由 Repo 方法的显式参数承载，防止调用方绕过。
// TenantID 指针仅供「管理端可选按租户筛选」：nil = 全租户，非 nil（含 0）= 精确匹配该租户。
type TicketFilter struct {
	Status   TicketStatus
	Priority TicketPriority
	Keyword  string // 标题模糊匹配
	UserID   int64  // >0 时按提交用户过滤（管理端/代理端）
	TenantID *int64 // 仅管理端：nil=全租户，非 nil=精确租户
	Page     int
	PageSize int
}

// CreateInput 是新建工单入参。TenantID 由调用方（handler）从提交用户 users.tenant_id 解析后填入，
// 绝不来自客户端请求体。
type CreateInput struct {
	TenantID int64
	UserID   int64
	Title    string
	Content  string
	Priority TicketPriority
}
