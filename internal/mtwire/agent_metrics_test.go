package mtwire

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	reportrepo "github.com/QuantumNous/new-api/internal/report/reportrepo"
)

// newAgentMetricsTestApp 造最小 App：sqlite + 报表聚合读到的最小裸表（沿用 grouphook_test 裸表习惯）。
func newAgentMetricsTestApp(t *testing.T) *App {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	for _, s := range []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL DEFAULT 0, deleted_at DATETIME)`,
		`CREATE TABLE payment_orders (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL DEFAULT 0, type TEXT, status TEXT, actual_paid REAL, created_at DATETIME)`,
		`CREATE TABLE agent_wallets (tenant_id INTEGER PRIMARY KEY, withdrawable_balance REAL DEFAULT 0, frozen_withdraw_amount REAL DEFAULT 0, total_earned REAL DEFAULT 0, api_balance REAL DEFAULT 0)`,
	} {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("create table: %v", err)
		}
	}
	return &App{DB: db, ReportRepo: reportrepo.New(db)}
}

// TestHandleAdminAgentMetrics_ReusesFinanceAggregates 验证端点复用 RechargePaid + WalletTotals
// 并加一条下级计数：租户 5 → 总充值 150、分润 23.8、下级用户 2（软删/别租户排除）。
func TestHandleAdminAgentMetrics_ReusesFinanceAggregates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := newAgentMetricsTestApp(t)
	// created_at/deleted_at 用固定 2023 UTC 秒，稳落在端点的 [1, now] 生命周期窗口内（避开 now 截断边界）。
	seedTS := time.Unix(1_700_000_000, 0).UTC()

	// 下级用户：租户 5 有 2 活跃(1,2) + 1 软删(3，排除)；租户 9 的 1 个(4，排除)。
	if err := app.DB.Exec(
		`INSERT INTO users (id, tenant_id, deleted_at) VALUES (1,5,NULL),(2,5,NULL),(3,5,?),(4,9,NULL)`, seedTS,
	).Error; err != nil {
		t.Fatalf("seed users: %v", err)
	}
	// 总充值：租户 5 两笔已入账 100+50=150；一笔 pending(999，排除)。
	if err := app.DB.Exec(
		`INSERT INTO payment_orders (tenant_id, type, status, actual_paid, created_at) VALUES
		 (5,'recharge','credited',100,?),(5,'recharge','credited',50,?),(5,'recharge','pending',999,?)`,
		seedTS, seedTS, seedTS,
	).Error; err != nil {
		t.Fatalf("seed payment_orders: %v", err)
	}
	// 分润收益：钱包累计已赚 23.8。
	if err := app.DB.Exec(`INSERT INTO agent_wallets (tenant_id, total_earned) VALUES (5, 23.8)`).Error; err != nil {
		t.Fatalf("seed agent_wallets: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/agents/5/metrics", nil)
	c.Params = gin.Params{{Key: "id", Value: "5"}}
	app.HandleAdminAgentMetrics(c)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var env struct {
		Success bool `json:"success"`
		Data    struct {
			RechargeTotalCNY    float64 `json:"recharge_total_cny"`
			CommissionEarnedCNY float64 `json:"commission_earned_cny"`
			DownstreamUserCount int64   `json:"downstream_user_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, w.Body.String())
	}
	if !env.Success {
		t.Fatalf("success=false; body=%s", w.Body.String())
	}
	if env.Data.RechargeTotalCNY != 150 {
		t.Fatalf("recharge_total_cny = %v, want 150", env.Data.RechargeTotalCNY)
	}
	if env.Data.CommissionEarnedCNY != 23.8 {
		t.Fatalf("commission_earned_cny = %v, want 23.8", env.Data.CommissionEarnedCNY)
	}
	if env.Data.DownstreamUserCount != 2 {
		t.Fatalf("downstream_user_count = %v, want 2", env.Data.DownstreamUserCount)
	}
}
