package mtwire

// 覆盖 ratio_markup（L1/独立档差价收益）在财务报表 HTTP 层（report.go）的暴露：summarySourceOrder
// 固定白名单曾漏了这个新来源，导致 GET /api/admin/finance/summary 的 earnings.by_source 静默丢行
// （total_earned_cny 仍正确，但 breakdown 对不上——见 reportrepo_test.go 的姊妹测试）；
// agentRankOut/HandleAdminFinanceAgents 同理漏了 ratio_markup_cny 字段。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	reportrepo "github.com/QuantumNous/new-api/internal/report/reportrepo"
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
