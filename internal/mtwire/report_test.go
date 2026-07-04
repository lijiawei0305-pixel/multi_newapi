package mtwire

// 覆盖 ratio_markup（L1/独立档差价收益）在财务报表 HTTP 层（report.go）的暴露：summarySourceOrder
// 固定白名单曾漏了这个新来源，导致 GET /api/admin/finance/summary 的 earnings.by_source 静默丢行
// （total_earned_cny 仍正确，但 breakdown 对不上——见 reportrepo_test.go 的姊妹测试）；
// agentRankOut/HandleAdminFinanceAgents 同理漏了 ratio_markup_cny 字段。

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	reportrepo "github.com/QuantumNous/new-api/internal/report/reportrepo"
	"github.com/QuantumNous/new-api/internal/tenant"
	tenantrepo "github.com/QuantumNous/new-api/internal/tenant/gormrepo"
	"github.com/QuantumNous/new-api/model"
)

// newFinanceReportTestApp 造最小 App：sqlite + 财务报表全部 8 张只读依赖表（裸表风格，对齐
// agent_metrics_test.go 的 newAgentMetricsTestApp 与 reportrepo 包自身的 newFinanceTestDB）。
func newFinanceReportTestApp(t *testing.T) *App {
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
	stmts := []string{
		`CREATE TABLE agent_earning_logs (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, user_id INTEGER NOT NULL DEFAULT 0, source_type TEXT NOT NULL, source_id TEXT NOT NULL DEFAULT '', amount REAL NOT NULL, remark TEXT NOT NULL DEFAULT '', created_at DATETIME NOT NULL)`,
		`CREATE TABLE agent_wallets (tenant_id INTEGER PRIMARY KEY, user_id INTEGER NOT NULL DEFAULT 0, api_balance REAL NOT NULL DEFAULT 0, withdrawable_balance REAL NOT NULL DEFAULT 0, frozen_withdraw_amount REAL NOT NULL DEFAULT 0, total_earned REAL NOT NULL DEFAULT 0, updated_at DATETIME)`,
		`CREATE TABLE agent_withdrawals (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, amount REAL NOT NULL, status TEXT NOT NULL DEFAULT 'pending', created_at DATETIME NOT NULL)`,
		`CREATE TABLE payment_orders (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL DEFAULT 0, type TEXT, status TEXT, actual_paid REAL, created_at DATETIME)`,
		`CREATE TABLE pending_subscription_orders (order_id TEXT, tenant_id INTEGER NOT NULL DEFAULT 0, retail_price REAL, agent_cost_price REAL)`,
		`CREATE TABLE mt_subscription_orders (order_no TEXT, tenant_id INTEGER NOT NULL DEFAULT 0, status TEXT, created_at DATETIME)`,
		`CREATE TABLE tenants (id INTEGER PRIMARY KEY, name TEXT, owner_user_id INTEGER)`,
		`CREATE TABLE users (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL DEFAULT 0, deleted_at DATETIME)`,
		`CREATE TABLE logs (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER NOT NULL, type INTEGER, quota INTEGER, prompt_tokens INTEGER, completion_tokens INTEGER, created_at INTEGER)`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("create table: %v\nSQL: %s", err, s)
		}
	}
	// 钱包消耗台账（财务报表 v3「钱包消耗」口径的数据源）：用真实迁移建表，顺带在 sqlite 上验证
	// AutoMigrate + (user_id,request_id) 唯一索引可用。
	if err := migrateWalletConsumeLog(db); err != nil {
		t.Fatalf("wallet consume migrate: %v", err)
	}
	return &App{DB: db, ReportRepo: reportrepo.New(db)}
}

func seedFinanceEarning(t *testing.T, app *App, tenantID int64, sourceType string, amount float64, ts time.Time) {
	t.Helper()
	if err := app.DB.Exec(
		`INSERT INTO agent_earning_logs (tenant_id, user_id, source_type, source_id, amount, created_at) VALUES (?,?,?,?,?,?)`,
		tenantID, tenantID, sourceType, sourceType+"-seed", amount, ts,
	).Error; err != nil {
		t.Fatalf("seed earning (tenant=%d source=%s): %v", tenantID, sourceType, err)
	}
}

// TestHandleAdminFinanceSummary_BySourceIncludesRatioMarkup 锁定 GET /api/admin/finance/summary：
// ratio_markup 必须作为 earnings.by_source 里一个具名条目出现（不是静默折进 total 后从明细里消失），
// 且全部 by_source 条目之和必须等于 total_earned_cny——这是"某来源被白名单漏掉"的判别式：漏了就对不上。
func TestHandleAdminFinanceSummary_BySourceIncludesRatioMarkup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := newFinanceReportTestApp(t)
	base := time.Unix(1_700_000_000, 0).UTC()
	seedFinanceEarning(t, app, 9, "ratio_markup", 40.5, base)
	seedFinanceEarning(t, app, 5, "consume_commission", 10, base)

	start := base.Add(-time.Hour).Unix()
	end := base.Add(time.Hour).Unix()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/admin/finance/summary?start_timestamp=%d&end_timestamp=%d", start, end), nil)
	app.HandleAdminFinanceSummary(c)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var env struct {
		Success bool `json:"success"`
		Data    struct {
			Earnings struct {
				TotalEarnedCNY float64 `json:"total_earned_cny"`
				BySource       []struct {
					SourceType string  `json:"source_type"`
					AmountCNY  float64 `json:"amount_cny"`
				} `json:"by_source"`
			} `json:"earnings"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, w.Body.String())
	}
	if !env.Success {
		t.Fatalf("success=false; body=%s", w.Body.String())
	}
	if env.Data.Earnings.TotalEarnedCNY != 50.5 {
		t.Fatalf("total_earned_cny = %v, want 50.5", env.Data.Earnings.TotalEarnedCNY)
	}

	var found bool
	var sumBySource float64
	for _, s := range env.Data.Earnings.BySource {
		sumBySource += s.AmountCNY
		if s.SourceType == "ratio_markup" {
			found = true
			if s.AmountCNY != 40.5 {
				t.Fatalf("ratio_markup amount_cny = %v, want 40.5", s.AmountCNY)
			}
		}
	}
	if !found {
		t.Fatalf("earnings.by_source missing a labeled ratio_markup entry entirely; got %+v", env.Data.Earnings.BySource)
	}
	if sumBySource != env.Data.Earnings.TotalEarnedCNY {
		t.Fatalf("sum(by_source amounts)=%v != total_earned_cny=%v — a source is being silently dropped from the breakdown",
			sumBySource, env.Data.Earnings.TotalEarnedCNY)
	}
}

// TestHandleAdminFinanceAgents_RatioMarkupCNYField 锁定 GET /api/admin/finance/agents：排行的每一行
// 需带 ratio_markup_cny 分桶字段（镜像 consume_commission_cny），且该值计入同一行的 total_earned_cny。
func TestHandleAdminFinanceAgents_RatioMarkupCNYField(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := newFinanceReportTestApp(t)
	base := time.Unix(1_700_000_000, 0).UTC()
	seedFinanceEarning(t, app, 9, "ratio_markup", 40.5, base)

	start := base.Add(-time.Hour).Unix()
	end := base.Add(time.Hour).Unix()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/admin/finance/agents?start_timestamp=%d&end_timestamp=%d", start, end), nil)
	app.HandleAdminFinanceAgents(c)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var env struct {
		Success bool `json:"success"`
		Data    struct {
			Items []struct {
				TenantID             int64   `json:"tenant_id"`
				TotalEarnedCNY       float64 `json:"total_earned_cny"`
				RatioMarkupCNY       float64 `json:"ratio_markup_cny"`
				ConsumeCommissionCNY float64 `json:"consume_commission_cny"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, w.Body.String())
	}
	if len(env.Data.Items) != 1 {
		t.Fatalf("items = %d, want 1; body=%s", len(env.Data.Items), w.Body.String())
	}
	item := env.Data.Items[0]
	if item.TenantID != 9 {
		t.Fatalf("tenant_id = %d, want 9", item.TenantID)
	}
	if item.TotalEarnedCNY != 40.5 {
		t.Fatalf("total_earned_cny = %v, want 40.5", item.TotalEarnedCNY)
	}
	if item.RatioMarkupCNY != 40.5 {
		t.Fatalf("ratio_markup_cny = %v, want 40.5 (field must exist and be populated, not silently 0)", item.RatioMarkupCNY)
	}
	if item.ConsumeCommissionCNY != 0 {
		t.Fatalf("consume_commission_cny = %v, want 0", item.ConsumeCommissionCNY)
	}
}

// ============================================================================
// 财务报表 v3 总览（doc/finance-model-report-v3.md §二）：代理 4 项 / 管理端 6 项。
// ============================================================================

// newFinanceOverviewTestApp 造 App：真实 tenantrepo（AutoMigrate，含 slug 列）+ 财务报表其余 8 张
// 裸表（同 newFinanceReportTestApp 风格）。总览的管理端「主站/代理站」拆分要靠 tenants.slug="platform"
// 判定平台租户（resolvePlatformTenantID → platformTenant → TenantRepo.GetTenantBySlug），裸表版
// newFinanceReportTestApp 的 tenants 表没有 slug 列，不够用，故单独建一个带真实 tenantrepo 的 App。
func newFinanceOverviewTestApp(t *testing.T) *App {
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
	if err := tenantrepo.AutoMigrate(db); err != nil {
		t.Fatalf("tenant automigrate: %v", err)
	}
	stmts := []string{
		`CREATE TABLE agent_earning_logs (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, user_id INTEGER NOT NULL DEFAULT 0, source_type TEXT NOT NULL, source_id TEXT NOT NULL DEFAULT '', amount REAL NOT NULL, remark TEXT NOT NULL DEFAULT '', created_at DATETIME NOT NULL)`,
		`CREATE TABLE agent_wallets (tenant_id INTEGER PRIMARY KEY, user_id INTEGER NOT NULL DEFAULT 0, api_balance REAL NOT NULL DEFAULT 0, withdrawable_balance REAL NOT NULL DEFAULT 0, frozen_withdraw_amount REAL NOT NULL DEFAULT 0, total_earned REAL NOT NULL DEFAULT 0, updated_at DATETIME)`,
		`CREATE TABLE agent_withdrawals (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, amount REAL NOT NULL, status TEXT NOT NULL DEFAULT 'pending', created_at DATETIME NOT NULL)`,
		`CREATE TABLE payment_orders (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL DEFAULT 0, type TEXT, status TEXT, actual_paid REAL, created_at DATETIME)`,
		`CREATE TABLE pending_subscription_orders (order_id TEXT, tenant_id INTEGER NOT NULL DEFAULT 0, retail_price REAL, agent_cost_price REAL)`,
		`CREATE TABLE mt_subscription_orders (order_no TEXT, tenant_id INTEGER NOT NULL DEFAULT 0, status TEXT, created_at DATETIME)`,
		`CREATE TABLE users (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL DEFAULT 0, deleted_at DATETIME)`,
		`CREATE TABLE logs (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER NOT NULL, type INTEGER, quota INTEGER, prompt_tokens INTEGER, completion_tokens INTEGER, created_at INTEGER)`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("create table: %v\nSQL: %s", err, s)
		}
	}
	// 钱包消耗台账（overview 的「主站/代理站钱包消耗」精确口径的数据源）。
	if err := migrateWalletConsumeLog(db); err != nil {
		t.Fatalf("wallet consume migrate: %v", err)
	}
	return &App{DB: db, ReportRepo: reportrepo.New(db), TenantRepo: tenantrepo.New(db)}
}

// seedWalletConsume 造一条钱包桶消耗流水（mt_wallet_consume_log），供 overview 的钱包消耗字段聚合。
// 这是「纯钱包消耗」的唯一数据源——套餐桶消耗不写此表，故不会出现在这里。
func seedWalletConsume(t *testing.T, app *App, tenantID, userID, walletQuota int64, requestID string, ts time.Time) {
	t.Helper()
	if err := app.DB.Exec(
		`INSERT INTO mt_wallet_consume_log (tenant_id, user_id, wallet_quota, request_id, created_at) VALUES (?,?,?,?,?)`,
		tenantID, userID, walletQuota, requestID, ts,
	).Error; err != nil {
		t.Fatalf("seed wallet consume (tenant=%d req=%s): %v", tenantID, requestID, err)
	}
}

// seedSubscriptionOrder 造一笔已激活套餐订单（pending_subscription_orders JOIN mt_subscription_orders
// ON order_id=order_no），供 SubscriptionPaidCost 聚合（= tokenplan_revenue_cny 的数据源）。
func seedSubscriptionOrder(t *testing.T, db *gorm.DB, orderNo string, tenantID int64, retail, agentCost float64, ts time.Time) {
	t.Helper()
	if err := db.Exec(`INSERT INTO pending_subscription_orders (order_id, tenant_id, retail_price, agent_cost_price) VALUES (?,?,?,?)`,
		orderNo, tenantID, retail, agentCost).Error; err != nil {
		t.Fatalf("seed pending_subscription_orders: %v", err)
	}
	if err := db.Exec(`INSERT INTO mt_subscription_orders (order_no, tenant_id, status, created_at) VALUES (?,?,?,?)`,
		orderNo, tenantID, "activated", ts).Error; err != nil {
		t.Fatalf("seed mt_subscription_orders: %v", err)
	}
}

// seedConsumeLog 造一条 logs 消耗行（type=LogTypeConsume），供 ConsumptionByTenant 聚合。
func seedConsumeLog(t *testing.T, db *gorm.DB, userID int64, quota int64, ts time.Time) {
	t.Helper()
	if err := db.Exec(`INSERT INTO logs (user_id, type, quota, prompt_tokens, completion_tokens, created_at) VALUES (?,?,?,?,?,?)`,
		userID, model.LogTypeConsume, quota, 0, 0, ts.Unix()).Error; err != nil {
		t.Fatalf("seed logs: %v", err)
	}
}

// TestHandleAdminFinanceSummary_OverviewV3_MainsiteVsAgentSplit 是财务报表 v3 管理端总览的端到端
// 覆盖（doc/finance-model-report-v3.md §二，6 项，主站/代理站分列）：造 1 笔主站(平台租户)套餐销售
// （售价=代理成本价，spread=0，真实 ActivateFromPayment 行为下主站不会给自己发 tokenplan_spread）
// + 1 笔代理站套餐销售（售价 100/代理成本 60→差价 40，对应一条 tokenplan_spread 收益）+ 代理站钱包
// 消耗差价/提成（ratio_markup 20 + consume_commission 5）+ 主站/代理站各一笔钱包桶消耗流水
// （mt_wallet_consume_log），并给代理站叠加一笔更大的 logs 全量消耗，断言 6 个 admin overview 字段
// 精确拆分、互不串号，且钱包消耗只读钱包台账（排除套餐桶，不取 logs 上界）。
func TestHandleAdminFinanceSummary_OverviewV3_MainsiteVsAgentSplit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := newFinanceOverviewTestApp(t)
	ctx := context.Background()

	// 平台（主站）租户：slug="platform"（isPlatformTenant/platformSlug 判据），供
	// resolvePlatformTenantID 命中。
	platform := &tenant.Tenant{Slug: "platform", Name: "主站直销"}
	if err := app.TenantRepo.CreateTenant(ctx, platform); err != nil {
		t.Fatalf("create platform tenant: %v", err)
	}
	// 代理站租户。
	agentT := &tenant.Tenant{Slug: "acme", Name: "Acme代理"}
	if err := app.TenantRepo.CreateTenant(ctx, agentT); err != nil {
		t.Fatalf("create agent tenant: %v", err)
	}

	base := time.Unix(1_700_000_000, 0).UTC()
	start := base.Add(-time.Hour).Unix()
	end := base.Add(time.Hour).Unix()

	// 套餐销售：主站 1 笔（retail=100=agent_cost→spread=0，不产生 tokenplan_spread 行——镜像真实
	// ActivateFromPayment 的 `if spread > 0` 守卫）；代理站 1 笔（retail=100, cost=60→差价 40）。
	seedSubscriptionOrder(t, app.DB, "ORD-MAIN-1", platform.ID, 100, 100, base)
	seedSubscriptionOrder(t, app.DB, "ORD-AGENT-1", agentT.ID, 100, 60, base)
	seedFinanceEarning(t, app, agentT.ID, "tokenplan_spread", 40, base)

	// 钱包消耗差价/提成：代理站 ratio_markup 20 + consume_commission 5 → 需返现代理合计 25。
	seedFinanceEarning(t, app, agentT.ID, "ratio_markup", 20, base)
	seedFinanceEarning(t, app, agentT.ID, "consume_commission", 5, base)

	// 钱包桶消耗（overview 钱包消耗字段的唯一数据源 = mt_wallet_consume_log）：主站(平台租户)钱包消耗 $2、
	// 代理站钱包消耗 $3。
	seedWalletConsume(t, app, platform.ID, 501, int64(2*common.QuotaPerUnit), "req-main-wallet", base)
	seedWalletConsume(t, app, agentT.ID, 502, int64(3*common.QuotaPerUnit), "req-agent-wallet", base)
	// 关键：再给代理站叠加一笔更大的 logs 全量消耗（$10 = $3 钱包 + $7 套餐桶，套餐桶不写钱包台账），
	// 断言 overview 代理站钱包消耗仍 = $3——证明已精确排除套餐桶消耗，不再取 logs 全量上界。
	if err := app.DB.Exec(`INSERT INTO users (id, tenant_id) VALUES (?,?)`, 502, agentT.ID).Error; err != nil {
		t.Fatalf("seed agent user: %v", err)
	}
	seedConsumeLog(t, app.DB, 502, int64(10*common.QuotaPerUnit), base)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/admin/finance/summary?start_timestamp=%d&end_timestamp=%d", start, end), nil)
	app.HandleAdminFinanceSummary(c)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var env struct {
		Success bool `json:"success"`
		Data    struct {
			Overview struct {
				MainsiteTokenplanRevenueCNY  float64 `json:"mainsite_tokenplan_revenue_cny"`
				AgentTokenplanRevenueCNY     float64 `json:"agent_tokenplan_revenue_cny"`
				TokenplanRebateCNY           float64 `json:"tokenplan_rebate_cny"`
				MainsiteWalletConsumptionCNY float64 `json:"mainsite_wallet_consumption_cny"`
				AgentWalletConsumptionCNY    float64 `json:"agent_wallet_consumption_cny"`
				AgentAPIRebateCNY            float64 `json:"agent_api_rebate_cny"`
			} `json:"overview"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, w.Body.String())
	}
	if !env.Success {
		t.Fatalf("success=false; body=%s", w.Body.String())
	}
	ov := env.Data.Overview
	if ov.MainsiteTokenplanRevenueCNY != 100 {
		t.Fatalf("mainsite_tokenplan_revenue_cny = %v, want 100", ov.MainsiteTokenplanRevenueCNY)
	}
	if ov.AgentTokenplanRevenueCNY != 100 {
		t.Fatalf("agent_tokenplan_revenue_cny = %v, want 100", ov.AgentTokenplanRevenueCNY)
	}
	if ov.TokenplanRebateCNY != 40 {
		t.Fatalf("tokenplan_rebate_cny = %v, want 40 (agent-site only, must exclude mainsite)", ov.TokenplanRebateCNY)
	}
	if ov.AgentAPIRebateCNY != 25 {
		t.Fatalf("agent_api_rebate_cny = %v, want 25 (20 ratio_markup + 5 consume_commission)", ov.AgentAPIRebateCNY)
	}
	wantMainCons := reportrepo.QuotaToCNY(int64(2 * common.QuotaPerUnit))
	wantAgentCons := reportrepo.QuotaToCNY(int64(3 * common.QuotaPerUnit))
	if math.Abs(ov.MainsiteWalletConsumptionCNY-wantMainCons) > 1e-6 {
		t.Fatalf("mainsite_wallet_consumption_cny = %v, want %v", ov.MainsiteWalletConsumptionCNY, wantMainCons)
	}
	if math.Abs(ov.AgentWalletConsumptionCNY-wantAgentCons) > 1e-6 {
		t.Fatalf("agent_wallet_consumption_cny = %v, want %v", ov.AgentWalletConsumptionCNY, wantAgentCons)
	}
}

// TestHandleTenantFinanceSummary_OverviewV3_AgentFourFields 覆盖代理自助总览（4 项）：套餐收益
// （subscription_paid）、套餐可提现（tokenplan_spread）、apikey 消费收益（= 纯钱包桶消耗
// mt_wallet_consume_log，严格排除套餐桶；见 agentFinanceOverviewOut.ApikeyConsumptionCNY）、消耗可提现
// （ratio_markup+consume_commission），单租户 scope 下互不干扰其他租户的数据；并叠加更大的 logs 全量消耗
// 证明 apikey 消费收益只读钱包台账、不取 logs 上界。
func TestHandleTenantFinanceSummary_OverviewV3_AgentFourFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := newFinanceOverviewTestApp(t)
	ctx := context.Background()

	agentT := &tenant.Tenant{Slug: "acme", Name: "Acme代理"}
	if err := app.TenantRepo.CreateTenant(ctx, agentT); err != nil {
		t.Fatalf("create agent tenant: %v", err)
	}
	other := &tenant.Tenant{Slug: "other", Name: "Other代理"}
	if err := app.TenantRepo.CreateTenant(ctx, other); err != nil {
		t.Fatalf("create other tenant: %v", err)
	}

	base := time.Unix(1_700_000_000, 0).UTC()
	start := base.Add(-time.Hour).Unix()
	end := base.Add(time.Hour).Unix()

	seedSubscriptionOrder(t, app.DB, "ORD-ACME-1", agentT.ID, 100, 60, base)
	seedFinanceEarning(t, app, agentT.ID, "tokenplan_spread", 40, base)
	seedFinanceEarning(t, app, agentT.ID, "ratio_markup", 20, base)
	seedFinanceEarning(t, app, agentT.ID, "consume_commission", 5, base)
	// apikey 消费收益 = 纯钱包桶消耗（mt_wallet_consume_log）：acme 钱包消耗 $4。再叠加一笔更大的 logs
	// 全量消耗（$12 = $4 钱包 + $8 套餐桶）证明 overview 只读钱包台账、排除套餐桶。
	seedWalletConsume(t, app, agentT.ID, 601, int64(4*common.QuotaPerUnit), "req-acme-wallet", base)
	if err := app.DB.Exec(`INSERT INTO users (id, tenant_id) VALUES (?,?)`, 601, agentT.ID).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	seedConsumeLog(t, app.DB, 601, int64(12*common.QuotaPerUnit), base)

	// 另一个租户的数据：必须完全不泄漏进 acme 的单租户 scope（钱包台账同样按 tenant 隔离）。
	seedSubscriptionOrder(t, app.DB, "ORD-OTHER-1", other.ID, 999, 999, base)
	seedFinanceEarning(t, app, other.ID, "tokenplan_spread", 999, base)
	seedFinanceEarning(t, app, other.ID, "ratio_markup", 999, base)
	seedWalletConsume(t, app, other.ID, 602, int64(999*common.QuotaPerUnit), "req-other-wallet", base)
	if err := app.DB.Exec(`INSERT INTO users (id, tenant_id) VALUES (?,?)`, 602, other.ID).Error; err != nil {
		t.Fatalf("seed other user: %v", err)
	}
	seedConsumeLog(t, app.DB, 602, int64(999*common.QuotaPerUnit), base)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/tenant/finance/summary?start_timestamp=%d&end_timestamp=%d", start, end), nil)
	c.Set(ginKeyAgentTenant, agentT.ID)
	app.HandleTenantFinanceSummary(c)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var env struct {
		Success bool `json:"success"`
		Data    struct {
			Overview struct {
				TokenplanRevenueCNY        float64 `json:"tokenplan_revenue_cny"`
				TokenplanWithdrawableCNY   float64 `json:"tokenplan_withdrawable_cny"`
				ApikeyConsumptionCNY       float64 `json:"apikey_consumption_cny"`
				ConsumptionWithdrawableCNY float64 `json:"consumption_withdrawable_cny"`
			} `json:"overview"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, w.Body.String())
	}
	if !env.Success {
		t.Fatalf("success=false; body=%s", w.Body.String())
	}
	ov := env.Data.Overview
	if ov.TokenplanRevenueCNY != 100 {
		t.Fatalf("tokenplan_revenue_cny = %v, want 100 (not 999 from the other tenant)", ov.TokenplanRevenueCNY)
	}
	if ov.TokenplanWithdrawableCNY != 40 {
		t.Fatalf("tokenplan_withdrawable_cny = %v, want 40", ov.TokenplanWithdrawableCNY)
	}
	if ov.ConsumptionWithdrawableCNY != 25 {
		t.Fatalf("consumption_withdrawable_cny = %v, want 25 (20 ratio_markup + 5 consume_commission)", ov.ConsumptionWithdrawableCNY)
	}
	wantCons := reportrepo.QuotaToCNY(int64(4 * common.QuotaPerUnit))
	if math.Abs(ov.ApikeyConsumptionCNY-wantCons) > 1e-6 {
		t.Fatalf("apikey_consumption_cny = %v, want %v (not the other tenant's 999)", ov.ApikeyConsumptionCNY, wantCons)
	}
}

// TestHandleTenantFinanceTrend_EarningsLens_SplitsWithdrawableBuckets 覆盖 GET
// /api/tenant/finance/trend?lens=earnings：每个按天桶除跨来源合计 amount_cny 外，还要带
// tokenplan_withdrawable_cny / consumption_withdrawable_cny 两个子序列——这是代理「我的收益」3 线
// 趋势图（doc/agent-earnings-simplify.md §三）的数据源，第三条「总和」线由前端把两者相加。
func TestHandleTenantFinanceTrend_EarningsLens_SplitsWithdrawableBuckets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := newFinanceReportTestApp(t)
	base := time.Unix(1_700_000_000, 0).UTC()
	seedFinanceEarning(t, app, 9, "tokenplan_spread", 12.5, base)
	seedFinanceEarning(t, app, 9, "ratio_markup", 7, base)
	seedFinanceEarning(t, app, 9, "consume_commission", 3, base)
	seedFinanceEarning(t, app, 9, "manual_adjustment", 100, base) // must not leak into either withdrawable bucket

	start := base.Add(-time.Hour).Unix()
	end := base.Add(time.Hour).Unix()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/tenant/finance/trend?start_timestamp=%d&end_timestamp=%d&granularity=day&lens=earnings", start, end), nil)
	c.Set(ginKeyAgentTenant, int64(9))
	app.HandleTenantFinanceTrend(c)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var env struct {
		Success bool `json:"success"`
		Data    struct {
			Series []struct {
				Bucket                     string  `json:"bucket"`
				AmountCNY                  float64 `json:"amount_cny"`
				TokenplanWithdrawableCNY   float64 `json:"tokenplan_withdrawable_cny"`
				ConsumptionWithdrawableCNY float64 `json:"consumption_withdrawable_cny"`
			} `json:"series"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, w.Body.String())
	}
	if !env.Success {
		t.Fatalf("success=false; body=%s", w.Body.String())
	}
	if len(env.Data.Series) != 1 {
		t.Fatalf("series buckets = %d, want 1; body=%s", len(env.Data.Series), w.Body.String())
	}
	pt := env.Data.Series[0]
	if pt.AmountCNY != 122.5 {
		t.Fatalf("amount_cny = %v, want 122.5 (12.5+7+3+100)", pt.AmountCNY)
	}
	if pt.TokenplanWithdrawableCNY != 12.5 {
		t.Fatalf("tokenplan_withdrawable_cny = %v, want 12.5", pt.TokenplanWithdrawableCNY)
	}
	if pt.ConsumptionWithdrawableCNY != 10 {
		t.Fatalf("consumption_withdrawable_cny = %v, want 10 (7 ratio_markup + 3 consume_commission)", pt.ConsumptionWithdrawableCNY)
	}
}
