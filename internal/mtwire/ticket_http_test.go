package mtwire

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/ticket"
	ticketrepo "github.com/QuantumNous/new-api/internal/ticket/gormrepo"
)

// ticketTestUser 是映射到 users 表的最简结构，供 userTenantID / usernamesByIDs 读取（id/username/tenant_id）。
type ticketTestUser struct {
	ID       int64  `gorm:"column:id;primaryKey"`
	Username string `gorm:"column:username"`
	TenantID int64  `gorm:"column:tenant_id"`
}

func (ticketTestUser) TableName() string { return "users" }

func newTicketApp(t *testing.T) (*App, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// sqlite :memory: 每连接独立库；工单创建/回复用事务，限单连接确保同库（同 recharge_test/distribution_test 约定）。
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := ticketrepo.AutoMigrate(db); err != nil {
		t.Fatalf("migrate tickets: %v", err)
	}
	if err := db.AutoMigrate(&ticketTestUser{}); err != nil {
		t.Fatalf("migrate users: %v", err)
	}
	repo := ticketrepo.New(db)
	app := &App{DB: db, TicketRepo: repo, TicketService: ticket.NewService(repo)}
	return app, db
}

func seedTicketUser(t *testing.T, db *gorm.DB, id, tenantID int64, name string) {
	t.Helper()
	if err := db.Create(&ticketTestUser{ID: id, TenantID: tenantID, Username: name}).Error; err != nil {
		t.Fatalf("seed user %d: %v", id, err)
	}
}

// testCtx 组装一个带 JSON body 的 gin 测试上下文。
func testCtx(method, target, body string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	c.Request = req
	return c, w
}

func setID(c *gin.Context, id int64) { c.Set("id", int(id)) }
func setParam(c *gin.Context, id int64) {
	c.Params = gin.Params{gin.Param{Key: "id", Value: strconv.FormatInt(id, 10)}}
}
func setAgentTenant(c *gin.Context, t int64) { c.Set(ginKeyAgentTenant, t) }

// apiResp / decodeResp 复用 distribution_test.go 的 {success,code,data} 解码器（同包）。

type pageData struct {
	Items    []map[string]any `json:"items"`
	Total    int              `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
}

// createTicket 走用户端 handler 建工单，返回其 id。
func createTicket(t *testing.T, app *App, userID int64, title string) int64 {
	t.Helper()
	c, w := testCtx("POST", "/api/tenant/tickets", `{"title":"`+title+`","content":"body"}`)
	setID(c, userID)
	app.HandleUserCreateTicket(c)
	r := decodeResp(t, w)
	if !r.Success {
		t.Fatalf("create ticket failed: %s (%s)", r.Code, w.Body.String())
	}
	var out struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(r.Data, &out)
	if out.ID == 0 {
		t.Fatalf("created ticket id = 0, resp %s", w.Body.String())
	}
	return out.ID
}

// TestUserCreateDerivesTenantAndIsolates：创建工单 tenant_id 派生自用户 users.tenant_id；用户 A 看不到用户 B。
func TestUserCreateDerivesTenantAndIsolates(t *testing.T) {
	app, db := newTicketApp(t)
	seedTicketUser(t, db, 100, 1, "alice")
	seedTicketUser(t, db, 200, 2, "bob")

	t1 := createTicket(t, app, 100, "alice needs help")
	t2 := createTicket(t, app, 200, "bob needs help")

	// tenant_id 派生自 users.tenant_id（alice→1, bob→2），非客户端可控。
	tk, _ := app.TicketRepo.GetByID(context.Background(), t1)
	if tk.TenantID != 1 {
		t.Fatalf("ticket t1 tenant = %d, want 1 (derived from user)", tk.TenantID)
	}

	// 用户 A 列表仅见自己。
	c, w := testCtx("GET", "/api/tenant/tickets", "")
	setID(c, 100)
	app.HandleUserListTickets(c)
	r := decodeResp(t, w)
	var pd pageData
	_ = json.Unmarshal(r.Data, &pd)
	if pd.Total != 1 || len(pd.Items) != 1 {
		t.Fatalf("alice list total = %d, want 1", pd.Total)
	}

	// 用户 B 读用户 A 的工单 → 404 TICKET_NOT_FOUND（IDOR）。
	c, w = testCtx("GET", "/api/tenant/tickets/"+strconv.FormatInt(t1, 10), "")
	setID(c, 200)
	setParam(c, t1)
	app.HandleUserGetTicket(c)
	r = decodeResp(t, w)
	if r.Success || r.Code != "TICKET_NOT_FOUND" || w.Code != http.StatusNotFound {
		t.Fatalf("cross-user get = %d %s, want 404 TICKET_NOT_FOUND", w.Code, r.Code)
	}

	// 用户 B 关闭用户 A 的工单 → 404。
	c, w = testCtx("POST", "/api/tenant/tickets/"+strconv.FormatInt(t1, 10)+"/close", "")
	setID(c, 200)
	setParam(c, t1)
	app.HandleUserCloseTicket(c)
	r = decodeResp(t, w)
	if r.Code != "TICKET_NOT_FOUND" {
		t.Fatalf("cross-user close code = %s, want TICKET_NOT_FOUND", r.Code)
	}
	_ = t2
}

// TestAgentTenantScopeAndClientTenantIgnored：代理只见本租户工单；客户端传 tenant_id 不改变作用域。
func TestAgentTenantScopeAndClientTenantIgnored(t *testing.T) {
	app, db := newTicketApp(t)
	seedTicketUser(t, db, 100, 1, "alice") // 租户 1 下级
	seedTicketUser(t, db, 200, 2, "bob")   // 租户 2 下级

	t1 := createTicket(t, app, 100, "t1")
	t2 := createTicket(t, app, 200, "t2")

	// 代理 A（权威 tenant=1）列表：即使 query 传 tenant_id=2，作用域仍为 1，只见 t1。
	c, w := testCtx("GET", "/api/tenant/agent/tickets?tenant_id=2", "")
	setID(c, 10)
	setAgentTenant(c, 1)
	app.HandleAgentListTickets(c)
	r := decodeResp(t, w)
	var pd pageData
	_ = json.Unmarshal(r.Data, &pd)
	if pd.Total != 1 || pd.Items[0]["id"].(float64) != float64(t1) {
		t.Fatalf("agent(tenant1) list with client tenant_id=2 leaked scope: %+v", pd)
	}

	// 代理 A 读租户 2 的工单 t2 → 404（跨租户）。
	c, w = testCtx("GET", "/api/tenant/agent/tickets/"+strconv.FormatInt(t2, 10), "")
	setID(c, 10)
	setAgentTenant(c, 1)
	setParam(c, t2)
	app.HandleAgentGetTicket(c)
	r = decodeResp(t, w)
	if r.Code != "TICKET_NOT_FOUND" {
		t.Fatalf("agent cross-tenant get code = %s, want TICKET_NOT_FOUND", r.Code)
	}

	// 代理 A 改租户 2 工单状态 → 404（越权拦截，不只在列表过滤）。
	c, w = testCtx("POST", "/api/tenant/agent/tickets/"+strconv.FormatInt(t2, 10)+"/status", `{"status":"resolved"}`)
	setID(c, 10)
	setAgentTenant(c, 1)
	setParam(c, t2)
	app.HandleAgentSetTicketStatus(c)
	r = decodeResp(t, w)
	if r.Code != "TICKET_NOT_FOUND" {
		t.Fatalf("agent cross-tenant status code = %s, want TICKET_NOT_FOUND", r.Code)
	}
	_ = t1
}

// TestAgentEndpointForbiddenForNonOwner：未经 AgentOwnerAuth（agentTenantID<=0）访问代理接口 → 403。
func TestAgentEndpointForbiddenForNonOwner(t *testing.T) {
	app, db := newTicketApp(t)
	seedTicketUser(t, db, 100, 1, "alice")

	// 普通用户（无 agentTenant）打代理列表接口 → AGENT_FORBIDDEN。
	c, w := testCtx("GET", "/api/tenant/agent/tickets", "")
	setID(c, 100) // 已登录但非 owner；中间件不会设 ginKeyAgentTenant
	app.HandleAgentListTickets(c)
	r := decodeResp(t, w)
	if r.Success || r.Code != "AGENT_FORBIDDEN" || w.Code != http.StatusForbidden {
		t.Fatalf("non-owner agent list = %d %s, want 403 AGENT_FORBIDDEN", w.Code, r.Code)
	}

	// 回复接口同样 fail-closed。
	c, w = testCtx("POST", "/api/tenant/agent/tickets/1/replies", `{"content":"x"}`)
	setID(c, 100)
	setParam(c, 1)
	app.HandleAgentReplyTicket(c)
	r = decodeResp(t, w)
	if r.Code != "AGENT_FORBIDDEN" {
		t.Fatalf("non-owner agent reply code = %s, want AGENT_FORBIDDEN", r.Code)
	}
}

// TestAdminCrossTenantAccess：管理员可跨租户列表 + 读取任意工单。
func TestAdminCrossTenantAccess(t *testing.T) {
	app, db := newTicketApp(t)
	seedTicketUser(t, db, 100, 1, "alice")
	seedTicketUser(t, db, 200, 2, "bob")
	t1 := createTicket(t, app, 100, "t1")
	t2 := createTicket(t, app, 200, "t2")

	c, w := testCtx("GET", "/api/admin/tickets", "")
	app.HandleAdminListTickets(c)
	r := decodeResp(t, w)
	var pd pageData
	_ = json.Unmarshal(r.Data, &pd)
	if pd.Total != 2 {
		t.Fatalf("admin list total = %d, want 2 (cross-tenant)", pd.Total)
	}

	for _, id := range []int64{t1, t2} {
		c, w = testCtx("GET", "/api/admin/tickets/"+strconv.FormatInt(id, 10), "")
		setParam(c, id)
		app.HandleAdminGetTicket(c)
		r = decodeResp(t, w)
		if !r.Success {
			t.Fatalf("admin get %d failed: %s", id, r.Code)
		}
	}

	// 管理端按 tenant_id 精确筛选。
	c, w = testCtx("GET", "/api/admin/tickets?tenant_id=2", "")
	app.HandleAdminListTickets(c)
	r = decodeResp(t, w)
	_ = json.Unmarshal(r.Data, &pd)
	if pd.Total != 1 || pd.Items[0]["tenant_id"].(float64) != 2 {
		t.Fatalf("admin tenant_id=2 filter = %+v", pd)
	}
}

// TestReplyFlowAndClosedGuard：用户回复重开、代理回复置 pending、已关闭工单回复 → 409 TICKET_CLOSED。
func TestReplyFlowAndClosedGuard(t *testing.T) {
	app, db := newTicketApp(t)
	seedTicketUser(t, db, 100, 1, "alice")
	id := createTicket(t, app, 100, "help")

	// 用户关闭。
	c, w := testCtx("POST", "/api/tenant/tickets/"+strconv.FormatInt(id, 10)+"/close", "")
	setID(c, 100)
	setParam(c, id)
	app.HandleUserCloseTicket(c)
	if r := decodeResp(t, w); !r.Success {
		t.Fatalf("close failed: %s", r.Code)
	}
	// 已关闭工单回复 → 409 TICKET_CLOSED。
	c, w = testCtx("POST", "/api/tenant/tickets/"+strconv.FormatInt(id, 10)+"/replies", `{"content":"reopen?"}`)
	setID(c, 100)
	setParam(c, id)
	app.HandleUserReplyTicket(c)
	r := decodeResp(t, w)
	if r.Code != "TICKET_CLOSED" || w.Code != http.StatusConflict {
		t.Fatalf("reply-to-closed = %d %s, want 409 TICKET_CLOSED", w.Code, r.Code)
	}
}

// TestTicketRoutesRegisterNoConflict：三端路由注册无 gin 路径冲突（用户 /tickets/:id 与代理 /agent/tickets/:id 共存）。
func TestTicketRoutesRegisterNoConflict(t *testing.T) {
	app, _ := newTicketApp(t)
	gin.SetMode(gin.TestMode)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("route registration panicked (path conflict): %v", r)
		}
	}()

	r := gin.New()
	tenantGroup := r.Group("/api/tenant")
	{
		tenantGroup.GET("/tickets", app.HandleUserListTickets)
		tenantGroup.POST("/tickets", app.HandleUserCreateTicket)
		tenantGroup.GET("/tickets/:id", app.HandleUserGetTicket)
		tenantGroup.POST("/tickets/:id/replies", app.HandleUserReplyTicket)
		tenantGroup.POST("/tickets/:id/close", app.HandleUserCloseTicket)
		agentSelf := tenantGroup.Group("")
		{
			agentSelf.GET("/agent/tickets", app.HandleAgentListTickets)
			agentSelf.GET("/agent/tickets/:id", app.HandleAgentGetTicket)
			agentSelf.POST("/agent/tickets/:id/replies", app.HandleAgentReplyTicket)
			agentSelf.POST("/agent/tickets/:id/status", app.HandleAgentSetTicketStatus)
		}
	}
	adminTicketGroup := r.Group("/api/admin/tickets")
	{
		adminTicketGroup.GET("", app.HandleAdminListTickets)
		adminTicketGroup.GET("/:id", app.HandleAdminGetTicket)
		adminTicketGroup.POST("/:id/replies", app.HandleAdminReplyTicket)
		adminTicketGroup.POST("/:id/status", app.HandleAdminSetTicketStatus)
	}

	want := map[string]bool{
		"GET /api/tenant/tickets":                    false,
		"POST /api/tenant/tickets":                   false,
		"GET /api/tenant/tickets/:id":                false,
		"POST /api/tenant/tickets/:id/replies":       false,
		"POST /api/tenant/tickets/:id/close":         false,
		"GET /api/tenant/agent/tickets":              false,
		"GET /api/tenant/agent/tickets/:id":          false,
		"POST /api/tenant/agent/tickets/:id/replies": false,
		"POST /api/tenant/agent/tickets/:id/status":  false,
		"GET /api/admin/tickets":                     false,
		"GET /api/admin/tickets/:id":                 false,
		"POST /api/admin/tickets/:id/replies":        false,
		"POST /api/admin/tickets/:id/status":         false,
	}
	for _, ri := range r.Routes() {
		want[ri.Method+" "+ri.Path] = true
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("route not registered: %s", k)
		}
	}
}
