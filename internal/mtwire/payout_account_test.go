package mtwire

// 提现闭环补强的 HTTP 层测试：#1 收款账户（GET/PUT /api/tenant/payout-account）+
// #2 mark-paid（POST /api/admin/withdrawals/:id/mark-paid）。核心状态机/钱流已在
// internal/agent（服务+仓储层）穷尽覆盖，这里只验证 handler 接线：鉴权门禁、JSON 绑定、
// 错误码透传、响应形态——不重复造轮子断言金额细节。

import (
	"bytes"
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

	"github.com/QuantumNous/new-api/internal/agent"
	agentrepo "github.com/QuantumNous/new-api/internal/agent/gormrepo"
	"github.com/QuantumNous/new-api/internal/tenant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newPayoutTestApp 组装一个仅含 AgentService/Withdrawals 的最小 App：收款账户 + mark-paid
// 两组新 handler 均不触碰 TenantService/DB，纯内存 MemRepo 即可隔离测试。
func newPayoutTestApp() (*App, *agent.MemRepo) {
	repo := agent.NewMemRepo()
	return &App{
		AgentService: agent.NewService(repo, nil),
		Withdrawals:  agent.NewWithdrawalService(repo),
	}, repo
}

func TestReviewWithdrawalRejectsMalformedOptionalBodyBeforeStateChange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, repo := newPayoutTestApp()
	ctx := context.Background()
	require.NoError(t, repo.SetPayoutAccount(ctx, 1, agent.PayoutAccount{
		Method: agent.PayoutAlipay, Account: "alice@example.com", Name: "Alice",
	}))
	_, err := repo.AppendEarning(ctx, agent.EarningEntry{
		TenantID: 1, SourceType: agent.SourceManualAdjustment, SourceID: "malformed-review", Amount: 100,
	})
	require.NoError(t, err)
	withdrawal, err := app.Withdrawals.Request(ctx, agent.WithdrawInput{TenantID: 1, Amount: 40})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/withdrawals/1/reject", strings.NewReader(`{"remark":`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatInt(withdrawal.ID, 10)}}

	app.HandleAdminRejectWithdrawal(c)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	stored, err := repo.GetWithdrawal(ctx, withdrawal.ID)
	require.NoError(t, err)
	assert.Equal(t, agent.WithdrawPending, stored.Status)
}

func TestAgentFinancialMutationBodiesAreBoundedBeforeStateChange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oversized := strings.Repeat(" ", int(agentFinancialRequestBodyLimit)+1)

	t.Run("set payout account", func(t *testing.T) {
		app, repo := newPayoutTestApp()
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPut, "/api/tenant/payout-account", strings.NewReader(oversized))
		c.Set(ginKeyAgentTenant, int64(1))

		app.HandleAgentSetPayoutAccount(c)

		assert.Equal(t, http.StatusBadRequest, recorder.Code)
		assert.Equal(t, "AGENT_INPUT_INVALID", decodeEnvelope(t, recorder)["code"])
		_, found, err := repo.GetPayoutAccount(context.Background(), 1)
		require.NoError(t, err)
		assert.False(t, found)
	})

	t.Run("request withdrawal", func(t *testing.T) {
		app, repo := newPayoutTestApp()
		ctx := context.Background()
		require.NoError(t, repo.SetPayoutAccount(ctx, 1, agent.PayoutAccount{
			Method: agent.PayoutAlipay, Account: "a@example.com", Name: "Alice",
		}))
		_, err := repo.AppendEarning(ctx, agent.EarningEntry{
			TenantID: 1, SourceType: agent.SourceManualAdjustment, SourceID: "body-limit", Amount: 100,
		})
		require.NoError(t, err)
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/tenant/withdrawals", strings.NewReader(oversized))
		c.Set(ginKeyAgentTenant, int64(1))

		app.HandleAgentRequestWithdrawal(c)

		assert.Equal(t, http.StatusBadRequest, recorder.Code)
		wallet, err := repo.GetWallet(ctx, 1)
		require.NoError(t, err)
		assert.Equal(t, float64(100), wallet.WithdrawableBalance)
		assert.Zero(t, wallet.FrozenWithdrawAmount)
	})

	t.Run("review withdrawal", func(t *testing.T) {
		app, repo := newPayoutTestApp()
		id := seedPendingWithdrawal(t, app, repo, 1, 100)
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/withdrawals/1/reject", strings.NewReader(oversized))
		c.Params = gin.Params{{Key: "id", Value: strconv.FormatInt(id, 10)}}

		app.HandleAdminRejectWithdrawal(c)

		assert.Equal(t, http.StatusBadRequest, recorder.Code)
		stored, err := repo.GetWithdrawal(context.Background(), id)
		require.NoError(t, err)
		assert.Equal(t, agent.WithdrawPending, stored.Status)
	})

	t.Run("mark withdrawal paid", func(t *testing.T) {
		app, repo := newPayoutTestApp()
		id := seedApprovedWithdrawal(t, app, repo, 1, 100)
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/withdrawals/1/mark-paid", strings.NewReader(oversized))
		c.Params = gin.Params{{Key: "id", Value: strconv.FormatInt(id, 10)}}

		app.HandleAdminMarkPaidWithdrawal(c)

		assert.Equal(t, http.StatusBadRequest, recorder.Code)
		stored, err := repo.GetWithdrawal(context.Background(), id)
		require.NoError(t, err)
		assert.Equal(t, agent.WithdrawApproved, stored.Status)
	})
}

func TestHandleAgentRequestWithdrawal_ForwardsIdempotencyKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, repo := newPayoutTestApp()
	ctx := context.Background()
	require.NoError(t, repo.SetPayoutAccount(ctx, 1, agent.PayoutAccount{
		Method: agent.PayoutAlipay, Account: "alice@example.com", Name: "Alice",
	}))
	_, err := repo.AppendEarning(ctx, agent.EarningEntry{
		TenantID: 1, UserID: 5, SourceType: agent.SourceManualAdjustment, SourceID: "http-idempotency", Amount: 100,
	})
	require.NoError(t, err)

	request := func(amount float64) (*httptest.ResponseRecorder, map[string]interface{}) {
		c, recorder := newJSONCtx(http.MethodPost, "/api/tenant/withdrawals", map[string]float64{"amount_cny": amount})
		c.Set(ginKeyAgentTenant, int64(1))
		c.Set("id", 5)
		c.Request.Header.Set("Idempotency-Key", "http-withdrawal-key")
		app.HandleAgentRequestWithdrawal(c)
		return recorder, decodeEnvelope(t, recorder)
	}

	firstRecorder, first := request(30)
	require.Equal(t, http.StatusOK, firstRecorder.Code)
	secondRecorder, second := request(30)
	require.Equal(t, http.StatusOK, secondRecorder.Code)
	firstID := first["data"].(map[string]interface{})["id"]
	secondID := second["data"].(map[string]interface{})["id"]
	assert.Equal(t, firstID, secondID)

	conflictRecorder, conflict := request(31)
	assert.Equal(t, http.StatusConflict, conflictRecorder.Code)
	assert.Equal(t, agent.CodeWithdrawIdempotencyConflict, conflict["code"])
	wallet, err := repo.GetWallet(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, float64(70), wallet.WithdrawableBalance)
	assert.Equal(t, float64(30), wallet.FrozenWithdrawAmount)
}

// newJSONCtx 建一个带（可选）JSON body 的 gin 测试上下文。
func newJSONCtx(method, path string, body any) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	var req *http.Request
	if body != nil {
		b, _ := json.Marshal(body)
		req = httptest.NewRequest(method, path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	c.Request = req
	return c, w
}

// decodeEnvelope 解出 {success,message,data,code} 响应信封。
func decodeEnvelope(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var env map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, w.Body.String())
	}
	return env
}

// ---- #1 收款账户 ----

func TestHandleAgentGetPayoutAccount_NotConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, _ := newPayoutTestApp()
	c, w := newJSONCtx(http.MethodGet, "/api/tenant/payout-account", nil)
	c.Set(ginKeyAgentTenant, int64(7))

	app.HandleAgentGetPayoutAccount(c)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body=%s", w.Code, w.Body.String())
	}
	data := decodeEnvelope(t, w)["data"].(map[string]interface{})
	if data["configured"] != false {
		t.Fatalf("configured = %v, want false", data["configured"])
	}
}

func TestHandleAgentGetPayoutAccount_Forbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, _ := newPayoutTestApp()
	c, w := newJSONCtx(http.MethodGet, "/api/tenant/payout-account", nil)
	// 不设置 ginKeyAgentTenant：模拟未经 AgentOwnerAuthByUser 校验（handler 自身也要挡）。

	app.HandleAgentGetPayoutAccount(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("code = %d, want 403", w.Code)
	}
}

func TestHandleAgentSetPayoutAccount_ValidPersistsAndRoundTrips(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, repo := newPayoutTestApp()
	c, w := newJSONCtx(http.MethodPut, "/api/tenant/payout-account", map[string]string{
		"payout_method":  "alipay",
		"payout_account": "alice@example.com",
		"payout_name":    "Alice",
	})
	c.Set(ginKeyAgentTenant, int64(7))

	app.HandleAgentSetPayoutAccount(c)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body=%s", w.Code, w.Body.String())
	}
	data := decodeEnvelope(t, w)["data"].(map[string]interface{})
	if data["configured"] != true || data["payout_account"] != "alice@example.com" {
		t.Fatalf("PUT response data = %+v", data)
	}
	got, found, err := repo.GetPayoutAccount(context.Background(), 7)
	if err != nil || !found || got.Account != "alice@example.com" || got.Name != "Alice" {
		t.Fatalf("persisted = %+v found=%v err=%v", got, found, err)
	}

	// 再 GET 应看到刚设置的账户（configured=true）。
	c2, w2 := newJSONCtx(http.MethodGet, "/api/tenant/payout-account", nil)
	c2.Set(ginKeyAgentTenant, int64(7))
	app.HandleAgentGetPayoutAccount(c2)
	data2 := decodeEnvelope(t, w2)["data"].(map[string]interface{})
	if data2["configured"] != true || data2["payout_method"] != "alipay" {
		t.Fatalf("GET after PUT = %+v", data2)
	}
}

func TestHandleAgentSetPayoutAccount_InvalidMethodRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, _ := newPayoutTestApp()
	c, w := newJSONCtx(http.MethodPut, "/api/tenant/payout-account", map[string]string{
		"payout_method":  "wechat", // 不支持的收款方式
		"payout_account": "x",
		"payout_name":    "y",
	})
	c.Set(ginKeyAgentTenant, int64(7))

	app.HandleAgentSetPayoutAccount(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", w.Code)
	}
	if got := decodeEnvelope(t, w)["code"]; got != "PAYOUT_ACCOUNT_INVALID" {
		t.Fatalf("error code = %v, want PAYOUT_ACCOUNT_INVALID", got)
	}
}

func TestHandleAgentSetPayoutAccount_BankMissingBankNameRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, _ := newPayoutTestApp()
	c, w := newJSONCtx(http.MethodPut, "/api/tenant/payout-account", map[string]string{
		"payout_method":  "bank",
		"payout_account": "6222000000",
		"payout_name":    "Alice",
		// 缺 payout_bank
	})
	c.Set(ginKeyAgentTenant, int64(7))

	app.HandleAgentSetPayoutAccount(c)

	if got := decodeEnvelope(t, w)["code"]; got != "PAYOUT_ACCOUNT_INVALID" {
		t.Fatalf("error code = %v, want PAYOUT_ACCOUNT_INVALID, body=%s", got, w.Body.String())
	}
}

// ---- #2 mark-paid ----

// seedPendingWithdrawal 造一笔 pending 提现单，返回其 id。
func seedPendingWithdrawal(t *testing.T, app *App, repo *agent.MemRepo, tenantID int64, amount float64) int64 {
	t.Helper()
	ctx := context.Background()
	if _, err := repo.AppendEarning(ctx, agent.EarningEntry{
		TenantID: tenantID, SourceType: agent.SourceRechargeSpread, SourceID: "seed", Amount: amount,
	}); err != nil {
		t.Fatalf("seed earning: %v", err)
	}
	if err := repo.SetPayoutAccount(ctx, tenantID, agent.PayoutAccount{
		Method: agent.PayoutAlipay, Account: "a@example.com", Name: "Alice",
	}); err != nil {
		t.Fatalf("seed payout account: %v", err)
	}
	wd, err := app.Withdrawals.Request(ctx, agent.WithdrawInput{TenantID: tenantID, Amount: amount / 2})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	return wd.ID
}

// seedApprovedWithdrawal 造一笔已 approved 的提现单，返回其 id。
func seedApprovedWithdrawal(t *testing.T, app *App, repo *agent.MemRepo, tenantID int64, amount float64) int64 {
	t.Helper()
	id := seedPendingWithdrawal(t, app, repo, tenantID, amount)
	ctx := context.Background()
	if err := app.Withdrawals.Review(ctx, id, true, ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	return id
}

func markPaidCtx(id int64, payoutRef string) (*gin.Context, *httptest.ResponseRecorder) {
	body := map[string]string{}
	if payoutRef != "" {
		body["payout_ref"] = payoutRef
	}
	path := "/api/admin/withdrawals/" + strconv.FormatInt(id, 10) + "/mark-paid"
	c, w := newJSONCtx(http.MethodPost, path, body)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatInt(id, 10)}}
	return c, w
}

func TestHandleAdminMarkPaidWithdrawal_HappyPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, repo := newPayoutTestApp()
	id := seedApprovedWithdrawal(t, app, repo, 1, 100)

	c, w := markPaidCtx(id, "WX20260704001")
	app.HandleAdminMarkPaidWithdrawal(c)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body=%s", w.Code, w.Body.String())
	}
	got, err := repo.GetWithdrawal(context.Background(), id)
	if err != nil || got.Status != agent.WithdrawPaid || got.PayoutRef != "WX20260704001" || got.PaidAt.IsZero() {
		t.Fatalf("withdrawal after mark-paid = %+v (err %v)", got, err)
	}
}

func TestHandleAdminMarkPaidWithdrawal_NotApprovedRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, repo := newPayoutTestApp()
	ctx := context.Background()
	_, _ = repo.AppendEarning(ctx, agent.EarningEntry{TenantID: 1, SourceType: agent.SourceRechargeSpread, SourceID: "seed", Amount: 100})
	_ = repo.SetPayoutAccount(ctx, 1, agent.PayoutAccount{Method: agent.PayoutAlipay, Account: "a", Name: "b"})
	wd, err := app.Withdrawals.Request(ctx, agent.WithdrawInput{TenantID: 1, Amount: 40})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	// 未 approve，仍是 pending。

	c, w := markPaidCtx(wd.ID, "ref-1")
	app.HandleAdminMarkPaidWithdrawal(c)

	if w.Code != http.StatusConflict {
		t.Fatalf("code = %d, want 409, body=%s", w.Code, w.Body.String())
	}
	if got := decodeEnvelope(t, w)["code"]; got != "WITHDRAW_NOT_APPROVED" {
		t.Fatalf("error code = %v, want WITHDRAW_NOT_APPROVED", got)
	}
}

func TestHandleAdminMarkPaidWithdrawal_MissingPayoutRefRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, repo := newPayoutTestApp()
	id := seedApprovedWithdrawal(t, app, repo, 1, 100)

	c, w := markPaidCtx(id, "") // 不带 payout_ref
	app.HandleAdminMarkPaidWithdrawal(c)

	if got := decodeEnvelope(t, w)["code"]; got != "PAYOUT_REF_REQUIRED" {
		t.Fatalf("error code = %v, want PAYOUT_REF_REQUIRED, body=%s", got, w.Body.String())
	}
	// 未真正扣款（拒绝的调用不能移动资金）。
	got, _ := repo.GetWithdrawal(context.Background(), id)
	if got.Status != agent.WithdrawApproved {
		t.Fatalf("status = %q, want still approved after rejected mark-paid", got.Status)
	}
}

func TestHandleAdminMarkPaidWithdrawal_DoubleMarkPaidRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, repo := newPayoutTestApp()
	id := seedApprovedWithdrawal(t, app, repo, 1, 100)

	c1, w1 := markPaidCtx(id, "ref-1")
	app.HandleAdminMarkPaidWithdrawal(c1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first mark-paid code = %d, body=%s", w1.Code, w1.Body.String())
	}

	c2, w2 := markPaidCtx(id, "ref-2")
	app.HandleAdminMarkPaidWithdrawal(c2)
	if got := decodeEnvelope(t, w2)["code"]; got != "WITHDRAW_NOT_APPROVED" {
		t.Fatalf("second mark-paid code = %v, want WITHDRAW_NOT_APPROVED", got)
	}
}

// ---- 列表响应契约：#1 收款快照 + #3 驳回理由都必须透传（真实 GORM 仓储，管理端/代理自助两个列表端点都验）----

// newDBBackedTestApp 造一个真实 GORM(sqlite) 支撑的最小 App：AgentRepo 用具体类型（HandleAdminListWithdrawals
// 等依赖非接口方法），TenantService 用固定桩（toWithdrawalOut 需要解出 agent_name）。
func newDBBackedTestApp(t *testing.T) *App {
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
	if err := agentrepo.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Exec(`CREATE TABLE tenants (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`).Error; err != nil {
		t.Fatalf("create tenants: %v", err)
	}
	if err := db.Exec(`INSERT INTO tenants (id, name) VALUES (1, 'Acme代理')`).Error; err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	ar := agentrepo.New(db)
	return &App{
		DB:            db,
		AgentRepo:     ar,
		AgentService:  agent.NewService(ar, nil),
		Withdrawals:   agent.NewWithdrawalService(ar),
		TenantService: &failingSubdomainTenantService{tn: &tenant.Tenant{ID: 1, Name: "Acme代理"}},
	}
}

// TestWithdrawalListResponses_IncludeSnapshotAndRemark 验证：
//   - 代理自助列表（GET /api/tenant/withdrawals）与管理端列表（GET /api/admin/withdrawals）
//     都带出申请时快照的收款账户（提现闭环补强 #1）；
//   - 驳回后，两个列表都能看到持久化的 remark（提现闭环补强 #3：字段名统一 remark，且不再「丢」）。
func TestWithdrawalListResponses_IncludeSnapshotAndRemark(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := newDBBackedTestApp(t)
	ctx := context.Background()

	if _, err := app.AgentRepo.AppendEarning(ctx, agent.EarningEntry{
		TenantID: 1, SourceType: agent.SourceRechargeSpread, SourceID: "seed", Amount: 100,
	}); err != nil {
		t.Fatalf("seed earning: %v", err)
	}
	if err := app.AgentRepo.SetPayoutAccount(ctx, 1, agent.PayoutAccount{
		Method: agent.PayoutAlipay, Account: "alice@example.com", Name: "Alice",
	}); err != nil {
		t.Fatalf("seed payout account: %v", err)
	}
	wd, err := app.Withdrawals.Request(ctx, agent.WithdrawInput{TenantID: 1, Amount: 40})
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	// 代理自助列表：申请后即应带出收款快照。
	cAgent, wAgent := newJSONCtx(http.MethodGet, "/api/tenant/withdrawals?page=1&page_size=20", nil)
	cAgent.Set(ginKeyAgentTenant, int64(1))
	app.HandleAgentListWithdrawals(cAgent)
	agentPage, ok := decodeEnvelope(t, wAgent)["data"].(map[string]interface{})
	require.True(t, ok)
	agentRows, ok := agentPage["items"].([]interface{})
	if !ok || len(agentRows) != 1 {
		t.Fatalf("agent list = %v, want 1 row", agentPage)
	}
	assert.Equal(t, float64(1), agentPage["total"])
	assert.Equal(t, float64(1), agentPage["page"])
	assert.Equal(t, float64(defaultWithdrawalPageSize), agentPage["page_size"])
	agentRow := agentRows[0].(map[string]interface{})
	if agentRow["payout_method"] != "alipay" || agentRow["payout_account"] != "alice@example.com" {
		t.Fatalf("agent-list row missing payout snapshot: %+v", agentRow)
	}
	assert.Equal(t, "Acme代理", agentRow["agent_name"])

	// 驳回，带 remark。
	cReject, wReject := newJSONCtx(http.MethodPost, "/api/admin/withdrawals/"+strconv.FormatInt(wd.ID, 10)+"/reject",
		map[string]string{"remark": "资料不符"})
	cReject.Params = gin.Params{{Key: "id", Value: strconv.FormatInt(wd.ID, 10)}}
	app.HandleAdminRejectWithdrawal(cReject)
	if wReject.Code != http.StatusOK {
		t.Fatalf("reject code = %d, body=%s", wReject.Code, wReject.Body.String())
	}

	// 管理端列表：driven 应看到 status=rejected + remark=资料不符 + 收款快照仍在。
	cAdmin, wAdmin := newJSONCtx(http.MethodGet, "/api/admin/withdrawals?page=1&page_size=20", nil)
	app.HandleAdminListWithdrawals(cAdmin)
	adminPage := decodeEnvelope(t, wAdmin)["data"].(map[string]interface{})
	adminRows, ok := adminPage["items"].([]interface{})
	if !ok || len(adminRows) != 1 {
		t.Fatalf("admin list = %v, want 1 row", adminPage)
	}
	adminRow := adminRows[0].(map[string]interface{})
	if adminRow["status"] != "rejected" || adminRow["remark"] != "资料不符" {
		t.Fatalf("admin-list row missing remark: %+v", adminRow)
	}
	if adminRow["payout_account"] != "alice@example.com" {
		t.Fatalf("admin-list row missing payout snapshot after reject: %+v", adminRow)
	}

	// Older SPA builds did not send pagination parameters. Keep their array
	// response shape during rolling deployments, while the query itself is now
	// bounded to the newest 100 rows.
	cLegacy, wLegacy := newJSONCtx(http.MethodGet, "/api/admin/withdrawals", nil)
	app.HandleAdminListWithdrawals(cLegacy)
	legacyRows, ok := decodeEnvelope(t, wLegacy)["data"].([]interface{})
	require.True(t, ok)
	require.Len(t, legacyRows, 1)

	// 代理自助列表同样要看到驳回理由（历史表「备注/驳回原因」列的数据源）。
	cAgent2, wAgent2 := newJSONCtx(http.MethodGet, "/api/tenant/withdrawals?page=1&page_size=20", nil)
	cAgent2.Set(ginKeyAgentTenant, int64(1))
	app.HandleAgentListWithdrawals(cAgent2)
	agentPage2 := decodeEnvelope(t, wAgent2)["data"].(map[string]interface{})
	agentRows2 := agentPage2["items"].([]interface{})
	agentRow2 := agentRows2[0].(map[string]interface{})
	if agentRow2["remark"] != "资料不符" {
		t.Fatalf("agent-list row missing remark after reject: %+v", agentRow2)
	}
}

func TestAdminWithdrawalListUsesServerPaginationAndOneTenantLookup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := newDBBackedTestApp(t)
	ctx := context.Background()
	_, err := app.AgentRepo.AppendEarning(ctx, agent.EarningEntry{
		TenantID: 1, UserID: 5, SourceType: agent.SourceManualAdjustment,
		SourceID: "pagination-balance", Amount: 30,
	})
	require.NoError(t, err)
	require.NoError(t, app.AgentRepo.SetPayoutAccount(ctx, 1, agent.PayoutAccount{
		Method: agent.PayoutAlipay, Account: "alice@example.com", Name: "Alice",
	}))
	for i := 0; i < 25; i++ {
		_, err := app.Withdrawals.Request(ctx, agent.WithdrawInput{TenantID: 1, UserID: 5, Amount: 1})
		require.NoError(t, err)
	}

	tenantQueries := 0
	callbackName := "test:count-withdrawal-tenant-lookups"
	require.NoError(t, app.DB.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "tenants" {
			tenantQueries++
		}
	}))
	t.Cleanup(func() {
		_ = app.DB.Callback().Query().Remove(callbackName)
	})

	c, recorder := newJSONCtx(http.MethodGet, "/api/admin/withdrawals?page=2&page_size=10", nil)
	app.HandleAdminListWithdrawals(c)

	assert.Equal(t, http.StatusOK, recorder.Code)
	page := decodeEnvelope(t, recorder)["data"].(map[string]interface{})
	assert.Equal(t, float64(25), page["total"])
	assert.Equal(t, float64(2), page["page"])
	assert.Equal(t, float64(10), page["page_size"])
	items := page["items"].([]interface{})
	require.Len(t, items, 10)
	assert.Equal(t, float64(15), items[0].(map[string]interface{})["id"])
	assert.Equal(t, float64(6), items[9].(map[string]interface{})["id"])
	assert.Equal(t, "Acme代理", items[0].(map[string]interface{})["agent_name"])
	assert.Equal(t, 1, tenantQueries, "agent names must be resolved by one batched tenants query")
}
