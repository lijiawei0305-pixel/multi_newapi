package mtwire

// 支持工单 HTTP 层（用户 / 代理 / 管理员三端）。装配 App.TicketService，复用 respondOK/respondErr/
// reqCtx/tenantFrom/agentTenantID/usernamesByIDs 等既有辅助（同 moderation_http.go / agent.go 约定）。
//
// 隔离红线（最高优先级）：
//   - 用户端 user_id 一律取自 UserAuth 写入的会话（c.GetInt("id")），绝不信任客户端传入 user_id；
//   - 代理端 tenant 一律取自 AgentOwnerAuth 校验过的 agentTenantID(c)，绝不接受 query/body/path 的 tenant_id；
//   - 管理端经 AdminAuth 可跨租户；tenant_id 仅作为「可选筛选」而非作用域越权入口。
// 状态变更/回复/关闭/重开的归属复校在 ticket.service 层完成（每次操作前按作用域 Get 校验）。

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/ticket"
	"github.com/QuantumNous/new-api/model"
)

// ---- DTO（snake_case；时间 ISO-8601 UTC，对齐 api-contract §1）----

type ticketOut struct {
	ID            int64  `json:"id"`
	TenantID      int64  `json:"tenant_id"`
	UserID        int64  `json:"user_id"`
	Username      string `json:"username"`
	Title         string `json:"title"`
	Status        string `json:"status"`
	Priority      string `json:"priority"`
	MessageCount  int    `json:"message_count"`
	LastReplyAt   string `json:"last_reply_at"`
	LastReplyRole string `json:"last_reply_role"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

type ticketMessageOut struct {
	ID         int64  `json:"id"`
	TicketID   int64  `json:"ticket_id"`
	UserID     int64  `json:"user_id"`
	Username   string `json:"username"`
	AuthorRole string `json:"author_role"`
	Content    string `json:"content"`
	CreatedAt  string `json:"created_at"`
}

type ticketDetailOut struct {
	Ticket   ticketOut          `json:"ticket"`
	Messages []ticketMessageOut `json:"messages"`
}

type ticketCreateInput struct {
	Title    string `json:"title"`
	Content  string `json:"content"`
	Priority string `json:"priority"`
}

type ticketReplyInput struct {
	Content string `json:"content"`
}

type ticketStatusInput struct {
	Status string `json:"status"`
}

func toTicketOut(t ticket.SupportTicket, names map[int64]string) ticketOut {
	return ticketOut{
		ID:            t.ID,
		TenantID:      t.TenantID,
		UserID:        t.UserID,
		Username:      names[t.UserID],
		Title:         t.Title,
		Status:        string(t.Status),
		Priority:      string(t.Priority),
		MessageCount:  t.MessageCount,
		LastReplyAt:   isoUTC(t.LastReplyAt),
		LastReplyRole: string(t.LastReplyRole),
		CreatedAt:     isoUTC(t.CreatedAt),
		UpdatedAt:     isoUTC(t.UpdatedAt),
	}
}

func toTicketMessageOut(m ticket.TicketMessage, names map[int64]string) ticketMessageOut {
	return ticketMessageOut{
		ID:         m.ID,
		TicketID:   m.TicketID,
		UserID:     m.UserID,
		Username:   names[m.UserID],
		AuthorRole: string(m.AuthorRole),
		Content:    m.Content,
		CreatedAt:  isoUTC(m.CreatedAt),
	}
}

// respondTicketPage 输出分页信封 data:{items,total,page,page_size}（含用户名回填）。
func (a *App) respondTicketPage(c *gin.Context, page *ticket.TicketPage) {
	ids := make([]int64, 0, len(page.Items))
	for _, t := range page.Items {
		ids = append(ids, t.UserID)
	}
	names := a.usernamesByIDs(reqCtx(c), ids)
	items := make([]ticketOut, 0, len(page.Items))
	for _, t := range page.Items {
		items = append(items, toTicketOut(t, names))
	}
	respondOK(c, gin.H{
		"items":     items,
		"total":     page.Total,
		"page":      page.Page,
		"page_size": page.PageSize,
	})
}

// respondTicketDetail 输出 {ticket, messages[]}（含用户名回填）。
func (a *App) respondTicketDetail(c *gin.Context, d *ticket.TicketDetail) {
	ids := make([]int64, 0, len(d.Messages)+1)
	ids = append(ids, d.Ticket.UserID)
	for _, m := range d.Messages {
		ids = append(ids, m.UserID)
	}
	names := a.usernamesByIDs(reqCtx(c), ids)
	msgs := make([]ticketMessageOut, 0, len(d.Messages))
	for _, m := range d.Messages {
		msgs = append(msgs, toTicketMessageOut(m, names))
	}
	respondOK(c, ticketDetailOut{Ticket: toTicketOut(d.Ticket, names), Messages: msgs})
}

// ticketFilterFromQuery 解析公共列表过滤（status/priority/keyword/page/page_size）。
func ticketFilterFromQuery(c *gin.Context) ticket.TicketFilter {
	f := ticket.TicketFilter{
		Status:   ticket.TicketStatus(c.Query("status")),
		Priority: ticket.TicketPriority(c.Query("priority")),
		Keyword:  c.Query("keyword"),
	}
	if v, err := strconv.Atoi(c.Query("page")); err == nil {
		f.Page = v
	}
	if v, err := strconv.Atoi(c.Query("page_size")); err == nil {
		f.PageSize = v
	}
	return f
}

// ticketIDParam 解析 :id；非法一律 TICKET_NOT_FOUND（不泄漏存在性）。
func ticketIDParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// auditTicket 记录工单审计到既有日志系统（LogTypeManage）。best-effort：LOG_DB 未装配（单测）时 recover 兜底。
func auditTicket(userID int64, action string, ticketID int64) {
	defer func() { _ = recover() }()
	model.RecordLog(int(userID), model.LogTypeManage, fmt.Sprintf("[ticket] %s ticket#%d", action, ticketID))
}

// ============================ 用户端（/api/tenant/tickets，UserAuth，按 session user_id 隔离）============================

// HandleUserListTickets GET /api/tenant/tickets —— 我的工单（仅本人，分页/筛选）。
func (a *App) HandleUserListTickets(c *gin.Context) {
	userID := int64(c.GetInt("id"))
	if userID <= 0 {
		respondErr(c, ticket.ErrNotFound)
		return
	}
	page, err := a.TicketService.ListForUser(reqCtx(c), userID, ticketFilterFromQuery(c))
	if err != nil {
		respondErr(c, err)
		return
	}
	a.respondTicketPage(c, page)
}

// HandleUserCreateTicket POST /api/tenant/tickets —— 新建工单。
// tenant_id 服务端派生自「提交用户的 users.tenant_id」（归属其所在代理/主站），绝不来自请求体。
func (a *App) HandleUserCreateTicket(c *gin.Context) {
	userID := int64(c.GetInt("id"))
	if userID <= 0 {
		respondErr(c, ticket.ErrNotFound)
		return
	}
	var in ticketCreateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		respondErr(c, ticket.ErrInputInvalid)
		return
	}
	ctx := reqCtx(c)
	// 权威归属租户（0=主站平台工单）。查询失败即拒绝（可重试），避免瞬时故障把租户用户工单静默落为平台工单、对拥有的代理不可见。
	tenantID, err := a.userTenantIDStrict(ctx, userID)
	if err != nil {
		respondErr(c, ticket.ErrTenantLookup)
		return
	}
	tk, err := a.TicketService.Create(ctx, ticket.CreateInput{
		TenantID: tenantID,
		UserID:   userID,
		Title:    in.Title,
		Content:  in.Content,
		Priority: ticket.TicketPriority(in.Priority),
	})
	if err != nil {
		respondErr(c, err)
		return
	}
	auditTicket(userID, "create", tk.ID)
	names := a.usernamesByIDs(ctx, []int64{userID})
	respondOK(c, toTicketOut(*tk, names))
}

// HandleUserGetTicket GET /api/tenant/tickets/:id —— 我的工单详情（跨用户 → TICKET_NOT_FOUND）。
func (a *App) HandleUserGetTicket(c *gin.Context) {
	userID := int64(c.GetInt("id"))
	id, ok := ticketIDParam(c)
	if userID <= 0 || !ok {
		respondErr(c, ticket.ErrNotFound)
		return
	}
	d, err := a.TicketService.GetForUser(reqCtx(c), userID, id)
	if err != nil {
		respondErr(c, err)
		return
	}
	a.respondTicketDetail(c, d)
}

// HandleUserReplyTicket POST /api/tenant/tickets/:id/replies —— 追加回复（已关闭→TICKET_CLOSED；重开为 open）。
func (a *App) HandleUserReplyTicket(c *gin.Context) {
	userID := int64(c.GetInt("id"))
	id, ok := ticketIDParam(c)
	if userID <= 0 || !ok {
		respondErr(c, ticket.ErrNotFound)
		return
	}
	var in ticketReplyInput
	if err := c.ShouldBindJSON(&in); err != nil {
		respondErr(c, ticket.ErrReplyEmpty)
		return
	}
	m, err := a.TicketService.ReplyAsUser(reqCtx(c), userID, id, in.Content)
	if err != nil {
		respondErr(c, err)
		return
	}
	auditTicket(userID, "reply", id)
	names := a.usernamesByIDs(reqCtx(c), []int64{userID})
	respondOK(c, toTicketMessageOut(*m, names))
}

// HandleUserCloseTicket POST /api/tenant/tickets/:id/close —— 用户关闭自己的工单。
func (a *App) HandleUserCloseTicket(c *gin.Context) {
	userID := int64(c.GetInt("id"))
	id, ok := ticketIDParam(c)
	if userID <= 0 || !ok {
		respondErr(c, ticket.ErrNotFound)
		return
	}
	if err := a.TicketService.CloseByUser(reqCtx(c), userID, id); err != nil {
		respondErr(c, err)
		return
	}
	auditTicket(userID, "close", id)
	respondOK(c, gin.H{"id": id, "status": string(ticket.StatusClosed)})
}

// ============================ 代理端（/api/tenant/agent/tickets，AgentOwnerAuth，按权威 tenant 隔离）============================

// HandleAgentListTickets GET /api/tenant/agent/tickets —— 本租户工单（tenant 取自 AgentOwnerAuth）。
func (a *App) HandleAgentListTickets(c *gin.Context) {
	tid := agentTenantID(c)
	if tid <= 0 {
		respondErr(c, errAgentForbidden)
		return
	}
	f := ticketFilterFromQuery(c)
	if v, err := strconv.ParseInt(c.Query("user_id"), 10, 64); err == nil && v > 0 {
		f.UserID = v // 代理可按下级用户筛选（仍限本租户作用域）
	}
	page, err := a.TicketService.ListForTenant(reqCtx(c), tid, f)
	if err != nil {
		respondErr(c, err)
		return
	}
	a.respondTicketPage(c, page)
}

// HandleAgentGetTicket GET /api/tenant/agent/tickets/:id —— 详情（scope=agentTenant；跨租户→TICKET_NOT_FOUND）。
func (a *App) HandleAgentGetTicket(c *gin.Context) {
	tid := agentTenantID(c)
	id, ok := ticketIDParam(c)
	if tid <= 0 {
		respondErr(c, errAgentForbidden)
		return
	}
	if !ok {
		respondErr(c, ticket.ErrNotFound)
		return
	}
	d, err := a.TicketService.GetForTenant(reqCtx(c), tid, id)
	if err != nil {
		respondErr(c, err)
		return
	}
	a.respondTicketDetail(c, d)
}

// HandleAgentReplyTicket POST /api/tenant/agent/tickets/:id/replies —— 代理回复（author_role=agent；置 pending）。
func (a *App) HandleAgentReplyTicket(c *gin.Context) {
	tid := agentTenantID(c)
	id, ok := ticketIDParam(c)
	if tid <= 0 {
		respondErr(c, errAgentForbidden)
		return
	}
	if !ok {
		respondErr(c, ticket.ErrNotFound)
		return
	}
	var in ticketReplyInput
	if err := c.ShouldBindJSON(&in); err != nil {
		respondErr(c, ticket.ErrReplyEmpty)
		return
	}
	actorUserID := int64(c.GetInt("id"))
	m, err := a.TicketService.ReplyAsAgent(reqCtx(c), tid, actorUserID, id, in.Content)
	if err != nil {
		respondErr(c, err)
		return
	}
	auditTicket(actorUserID, "agent_reply", id)
	names := a.usernamesByIDs(reqCtx(c), []int64{actorUserID})
	respondOK(c, toTicketMessageOut(*m, names))
}

// HandleAgentSetTicketStatus POST /api/tenant/agent/tickets/:id/status —— 代理改状态（校验流转）。
func (a *App) HandleAgentSetTicketStatus(c *gin.Context) {
	tid := agentTenantID(c)
	id, ok := ticketIDParam(c)
	if tid <= 0 {
		respondErr(c, errAgentForbidden)
		return
	}
	if !ok {
		respondErr(c, ticket.ErrNotFound)
		return
	}
	var in ticketStatusInput
	if err := c.ShouldBindJSON(&in); err != nil {
		respondErr(c, ticket.ErrStatusInvalid)
		return
	}
	if err := a.TicketService.SetStatusAsAgent(reqCtx(c), tid, id, ticket.TicketStatus(in.Status)); err != nil {
		respondErr(c, err)
		return
	}
	auditTicket(int64(c.GetInt("id")), "agent_status:"+in.Status, id)
	respondOK(c, gin.H{"id": id, "status": in.Status})
}

// ============================ 管理端（/api/admin/tickets，AdminAuth，跨租户）============================

// HandleAdminListTickets GET /api/admin/tickets —— 全部工单（可选 tenant_id/user_id/status/priority/keyword 过滤）。
func (a *App) HandleAdminListTickets(c *gin.Context) {
	f := ticketFilterFromQuery(c)
	if v, err := strconv.ParseInt(c.Query("user_id"), 10, 64); err == nil && v > 0 {
		f.UserID = v
	}
	if s := c.Query("tenant_id"); s != "" {
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			f.TenantID = &v // 含 0（主站平台工单）：精确匹配
		}
	}
	page, err := a.TicketService.ListForAdmin(reqCtx(c), f)
	if err != nil {
		respondErr(c, err)
		return
	}
	a.respondTicketPage(c, page)
}

// HandleAdminGetTicket GET /api/admin/tickets/:id —— 任意工单详情（跨租户）。
func (a *App) HandleAdminGetTicket(c *gin.Context) {
	id, ok := ticketIDParam(c)
	if !ok {
		respondErr(c, ticket.ErrNotFound)
		return
	}
	d, err := a.TicketService.GetForAdmin(reqCtx(c), id)
	if err != nil {
		respondErr(c, err)
		return
	}
	a.respondTicketDetail(c, d)
}

// HandleAdminReplyTicket POST /api/admin/tickets/:id/replies —— 管理员回复（author_role=admin；置 pending）。
func (a *App) HandleAdminReplyTicket(c *gin.Context) {
	id, ok := ticketIDParam(c)
	if !ok {
		respondErr(c, ticket.ErrNotFound)
		return
	}
	var in ticketReplyInput
	if err := c.ShouldBindJSON(&in); err != nil {
		respondErr(c, ticket.ErrReplyEmpty)
		return
	}
	actorUserID := int64(c.GetInt("id"))
	m, err := a.TicketService.ReplyAsAdmin(reqCtx(c), actorUserID, id, in.Content)
	if err != nil {
		respondErr(c, err)
		return
	}
	auditTicket(actorUserID, "admin_reply", id)
	names := a.usernamesByIDs(reqCtx(c), []int64{actorUserID})
	respondOK(c, toTicketMessageOut(*m, names))
}

// HandleAdminSetTicketStatus POST /api/admin/tickets/:id/status —— 管理员改状态（校验流转）。
func (a *App) HandleAdminSetTicketStatus(c *gin.Context) {
	id, ok := ticketIDParam(c)
	if !ok {
		respondErr(c, ticket.ErrNotFound)
		return
	}
	var in ticketStatusInput
	if err := c.ShouldBindJSON(&in); err != nil {
		respondErr(c, ticket.ErrStatusInvalid)
		return
	}
	if err := a.TicketService.SetStatusAsAdmin(reqCtx(c), id, ticket.TicketStatus(in.Status)); err != nil {
		respondErr(c, err)
		return
	}
	auditTicket(int64(c.GetInt("id")), "admin_status:"+in.Status, id)
	respondOK(c, gin.H{"id": id, "status": in.Status})
}
