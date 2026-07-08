/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

package mtwire

// breakage 监控 HTTP 层单测（P2-BRK-01）：锁定 3 端点的
//   - 【门 #4 租户隔离】代理作用域（tenantFrom(c) 命中）只见本租户；主站作用域（未命中）看全平台且排除 tenant_id=0；
//   - CSV 导出（?format=csv）：头/行随作用域切换，代理省略 tenant_id/tenant_name 列；
//   - 筛选参数解析（plan_code / alert_level / 分页）与非法错误码（区间/告警级/格式）。
//
// 数据层用真实 breakagerepo（sqlite:memory），只造聚合 SQL 依赖的裸表（对齐 report_test.go 风格）。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/alert"
	"github.com/QuantumNous/new-api/internal/breakage"
	breakagerepo "github.com/QuantumNous/new-api/internal/breakage/gormrepo"
	"github.com/QuantumNous/new-api/internal/tenant"
)

// newBreakageTestApp 造最小 App：sqlite + breakage 聚合依赖的裸表 + 真实 breakage_snapshots（AutoMigrate）。
// sink=nil、cfg 禁用：handler 路径不触发告警（告警旁路在 job 层，另测）。
func newBreakageTestApp(t *testing.T) *App {
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
		`CREATE TABLE tokenplan_subscriptions (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL DEFAULT 0, user_id INTEGER NOT NULL DEFAULT 0, plan_id INTEGER NOT NULL DEFAULT 0, month_limit_usd REAL NOT NULL DEFAULT 0, used_usd REAL NOT NULL DEFAULT 0, status TEXT NOT NULL DEFAULT '', start_at DATETIME, expire_at DATETIME)`,
		`CREATE TABLE token_plans (id INTEGER PRIMARY KEY, code TEXT)`,
		`CREATE TABLE user_balances (user_id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL DEFAULT 0, balance_usd REAL NOT NULL DEFAULT 0)`,
		`CREATE TABLE payment_orders (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL DEFAULT 0, status TEXT, created_at DATETIME)`,
		`CREATE TABLE tenants (id INTEGER PRIMARY KEY, name TEXT)`,
		`CREATE TABLE users (id INTEGER PRIMARY KEY, username TEXT)`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("create table: %v\nSQL: %s", err, s)
		}
	}
	if err := breakagerepo.AutoMigrate(db); err != nil {
		t.Fatalf("breakage automigrate: %v", err)
	}

	repo := breakagerepo.New(db)
	return &App{
		DB:        db,
		Breakage:  breakage.NewService(repo, nil, alert.Config{}),
		AlertSink: nil,
	}
}

// brk_seedSub 造一条订阅（DATETIME 列用 time.Time；聚合按 expire_at 与 now 比较判惰性过期）。
func brk_seedSub(t *testing.T, app *App, tenantID, userID, planID int64, limit, used float64, status string, expireAt time.Time) {
	t.Helper()
	if err := app.DB.Exec(
		`INSERT INTO tokenplan_subscriptions (tenant_id, user_id, plan_id, month_limit_usd, used_usd, status, start_at, expire_at) VALUES (?,?,?,?,?,?,?,?)`,
		tenantID, userID, planID, limit, used, status, expireAt.Add(-30*24*time.Hour), expireAt,
	).Error; err != nil {
		t.Fatalf("seed sub (tenant=%d user=%d): %v", tenantID, userID, err)
	}
}

func brk_seedPlan(t *testing.T, app *App, id int64, code string) {
	t.Helper()
	if err := app.DB.Exec(`INSERT INTO token_plans (id, code) VALUES (?,?)`, id, code).Error; err != nil {
		t.Fatalf("seed plan: %v", err)
	}
}

func brk_seedBalance(t *testing.T, app *App, userID, tenantID int64, balanceUSD float64) {
	t.Helper()
	if err := app.DB.Exec(`INSERT INTO user_balances (user_id, tenant_id, balance_usd) VALUES (?,?,?)`, userID, tenantID, balanceUSD).Error; err != nil {
		t.Fatalf("seed balance: %v", err)
	}
}

func brk_seedTenantName(t *testing.T, app *App, id int64, name string) {
	t.Helper()
	if err := app.DB.Exec(`INSERT INTO tenants (id, name) VALUES (?,?)`, id, name).Error; err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
}

func brk_seedUsername(t *testing.T, app *App, id int64, name string) {
	t.Helper()
	if err := app.DB.Exec(`INSERT INTO users (id, username) VALUES (?,?)`, id, name).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
}

// setAgentScope 把请求置为「代理作用域」：注入已解析租户（tenantFrom 读 ginKeyTenant），使 handler 隔离本租户。
func setAgentScope(c *gin.Context, tenantID int64) {
	c.Set(ginKeyTenant, &tenant.Tenant{ID: tenantID})
}

// ============================================================================
// overview —— 租户隔离（代理只见本租户 / 主站看全平台且排除 tenant_id=0）
// ============================================================================

func TestHandleAdminBreakageOverview_TenantIsolation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := newBreakageTestApp(t)
	now := time.Now().UTC()
	past := now.Add(-24 * time.Hour)   // 已到期
	future := now.Add(24 * time.Hour)  // 未到期

	// 租户 4：活跃剩余 (100-30)=70；到期未用 (50-5)=45；钱包 20。
	brk_seedSub(t, app, 4, 40, 1, 100, 30, "active", future)
	brk_seedSub(t, app, 4, 41, 1, 50, 5, "active", past) // status 仍 active 但已过期 → 惰性过期计入到期未用
	brk_seedBalance(t, app, 40, 4, 20)

	// 租户 7：活跃剩余 (200-100)=100；到期未用 (80-0)=80；钱包 33。
	brk_seedSub(t, app, 7, 70, 1, 200, 100, "active", future)
	brk_seedSub(t, app, 7, 71, 1, 80, 0, "expired", past)
	brk_seedBalance(t, app, 70, 7, 33)

	// tenant_id=0（主站未归属）：绝不能进任何跨租户聚合。
	brk_seedSub(t, app, 0, 90, 1, 999, 0, "expired", past)
	brk_seedBalance(t, app, 90, 0, 999)

	// 卡单：租户 4 一笔 created（超 5min），租户 7 一笔 paid（超 5min），tenant_id=0 一笔（跨租户须排除）。
	old := now.Add(-10 * time.Minute)
	brk_seedPaymentOrder(t, app, 4, "created", old)
	brk_seedPaymentOrder(t, app, 7, "paid", old)
	brk_seedPaymentOrder(t, app, 0, "created", old)

	// ---- 代理作用域（租户 4）：只见本租户 ----
	ov4 := getBreakageOverview(t, app, func(c *gin.Context) { setAgentScope(c, 4) })
	if ov4.ActiveRemainingUSD != 70 {
		t.Fatalf("agent(4) active_remaining_usd = %v, want 70", ov4.ActiveRemainingUSD)
	}
	if ov4.ExpiredUnusedUSD != 45 {
		t.Fatalf("agent(4) expired_unused_usd = %v, want 45", ov4.ExpiredUnusedUSD)
	}
	if ov4.WalletUnusedUSD != 20 {
		t.Fatalf("agent(4) wallet_unused_usd = %v, want 20 (must not see tenant 7 or 0)", ov4.WalletUnusedUSD)
	}
	if ov4.AnomalyCount != 1 {
		t.Fatalf("agent(4) anomaly_count = %d, want 1", ov4.AnomalyCount)
	}

	// ---- 主站作用域（未命中租户）：看全平台，排除 tenant_id=0 ----
	ovAll := getBreakageOverview(t, app, nil)
	if ovAll.ActiveRemainingUSD != 170 {
		t.Fatalf("mainsite active_remaining_usd = %v, want 170 (70+100, exclude tenant_id=0)", ovAll.ActiveRemainingUSD)
	}
	if ovAll.ExpiredUnusedUSD != 125 {
		t.Fatalf("mainsite expired_unused_usd = %v, want 125 (45+80, exclude tenant_id=0's 999)", ovAll.ExpiredUnusedUSD)
	}
	if ovAll.WalletUnusedUSD != 53 {
		t.Fatalf("mainsite wallet_unused_usd = %v, want 53 (20+33, exclude tenant_id=0's 999)", ovAll.WalletUnusedUSD)
	}
	if ovAll.AnomalyCount != 2 {
		t.Fatalf("mainsite anomaly_count = %d, want 2 (exclude tenant_id=0)", ovAll.AnomalyCount)
	}
}

func brk_seedPaymentOrder(t *testing.T, app *App, tenantID int64, status string, createdAt time.Time) {
	t.Helper()
	if err := app.DB.Exec(`INSERT INTO payment_orders (tenant_id, status, created_at) VALUES (?,?,?)`, tenantID, status, createdAt).Error; err != nil {
		t.Fatalf("seed payment order: %v", err)
	}
}

// ============================================================================
// detail —— 租户隔离 + 筛选参数 + CSV 导出 + 非法错误码
// ============================================================================

func TestHandleAdminBreakageDetail_TenantIsolationAndFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := newBreakageTestApp(t)
	now := time.Now().UTC()
	past := now.Add(-24 * time.Hour)

	brk_seedPlan(t, app, 1, "starter")
	brk_seedPlan(t, app, 2, "pro")
	brk_seedTenantName(t, app, 4, "代理A")
	brk_seedTenantName(t, app, 7, "代理B")
	brk_seedUsername(t, app, 40, "alice")
	brk_seedUsername(t, app, 70, "bob")

	brk_seedSub(t, app, 4, 40, 1, 50, 0.5, "expired", past)  // 租户4 starter
	brk_seedSub(t, app, 7, 70, 2, 100, 100, "exhausted", past) // 租户7 pro，满额

	// ---- 代理作用域（租户 4）：只见本租户 1 行，且省略 tenant_id/tenant_name ----
	body := getBreakageDetailJSON(t, app, "", func(c *gin.Context) { setAgentScope(c, 4) })
	if body.Total != 1 || len(body.Items) != 1 {
		t.Fatalf("agent(4) total=%d items=%d, want 1/1; body=%+v", body.Total, len(body.Items), body)
	}
	it := body.Items[0]
	if it.UserID != 40 || it.PlanCode != "starter" {
		t.Fatalf("agent(4) row mismatch: %+v", it)
	}
	if it.TenantID != 0 || it.TenantName != "" {
		t.Fatalf("agent(4) must omit tenant_id/tenant_name (omitempty), got tenant_id=%d name=%q", it.TenantID, it.TenantName)
	}

	// ---- 主站作用域：全平台 2 行，含 tenant_id/tenant_name ----
	all := getBreakageDetailJSON(t, app, "", nil)
	if all.Total != 2 || len(all.Items) != 2 {
		t.Fatalf("mainsite total=%d items=%d, want 2/2", all.Total, len(all.Items))
	}
	var sawTenantName bool
	for _, r := range all.Items {
		if r.TenantID == 7 && r.TenantName == "代理B" {
			sawTenantName = true
		}
	}
	if !sawTenantName {
		t.Fatalf("mainsite rows must carry tenant_id/tenant_name; got %+v", all.Items)
	}

	// ---- plan_code 筛选（主站，仅 pro）----
	proOnly := getBreakageDetailJSON(t, app, "plan_code=pro", nil)
	if proOnly.Total != 1 || proOnly.Items[0].PlanCode != "pro" {
		t.Fatalf("plan_code=pro filter: total=%d items=%+v", proOnly.Total, proOnly.Items)
	}

	// ---- alert_level 筛选（主站，仅 exhausted）----
	exhaustedOnly := getBreakageDetailJSON(t, app, "alert_level=exhausted", nil)
	if exhaustedOnly.Total != 1 || exhaustedOnly.Items[0].AlertLevel != "exhausted" {
		t.Fatalf("alert_level=exhausted filter: total=%d items=%+v", exhaustedOnly.Total, exhaustedOnly.Items)
	}
}

func TestHandleAdminBreakageDetail_CSVExport(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := newBreakageTestApp(t)
	now := time.Now().UTC()
	past := now.Add(-24 * time.Hour)

	brk_seedPlan(t, app, 1, "starter")
	brk_seedTenantName(t, app, 4, "代理A")
	brk_seedUsername(t, app, 40, "alice")
	brk_seedSub(t, app, 4, 40, 1, 50, 0.5, "expired", past)

	// ---- 主站作用域 CSV：头含 tenant_id/tenant_name ----
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/breakage/detail?format=csv&start_timestamp=1&end_timestamp=2", nil)
	app.HandleAdminBreakageDetail(c)
	if w.Code != http.StatusOK {
		t.Fatalf("csv code = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "breakage-detail-1-2.csv") {
		t.Fatalf("Content-Disposition = %q, want filename breakage-detail-1-2.csv", cd)
	}
	csv := w.Body.String()
	if !strings.Contains(csv, "tenant_id") || !strings.Contains(csv, "tenant_name") {
		t.Fatalf("mainsite CSV header must include tenant_id,tenant_name; got:\n%s", csv)
	}
	if !strings.Contains(csv, "starter") || !strings.Contains(csv, "alice") {
		t.Fatalf("mainsite CSV must contain data row; got:\n%s", csv)
	}

	// ---- 代理作用域 CSV：头省略 tenant_id/tenant_name ----
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodGet, "/api/admin/breakage/detail?format=csv&start_timestamp=1&end_timestamp=2", nil)
	setAgentScope(c2, 4)
	app.HandleAdminBreakageDetail(c2)
	if w2.Code != http.StatusOK {
		t.Fatalf("agent csv code = %d, want 200", w2.Code)
	}
	csv2 := w2.Body.String()
	// 表头行是第一行（BOM 之后）：断言表头不含 tenant_id 列（值行里恰好没有该子串即可，用表头行判定）。
	headerLine := strings.SplitN(strings.TrimPrefix(csv2, "﻿"), "\r\n", 2)[0]
	if strings.Contains(headerLine, "tenant_id") || strings.Contains(headerLine, "tenant_name") {
		t.Fatalf("agent CSV header must omit tenant_id/tenant_name; header=%q", headerLine)
	}
	if !strings.Contains(headerLine, "user_id") {
		t.Fatalf("agent CSV header must include user_id; header=%q", headerLine)
	}
}

func TestHandleAdminBreakageDetail_InvalidParams(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := newBreakageTestApp(t)

	cases := []struct {
		name  string
		query string
		want  string
	}{
		{"bad alert_level", "alert_level=bogus", "BREAKAGE_ALERT_LEVEL_INVALID"},
		{"bad format", "format=pdf", "BREAKAGE_FORMAT_INVALID"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/breakage/detail?"+tc.query, nil)
			app.HandleAdminBreakageDetail(c)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("%s: code = %d, want 400; body=%s", tc.name, w.Code, w.Body.String())
			}
			if code := errCodeOf(t, w.Body.Bytes()); code != tc.want {
				t.Fatalf("%s: code = %q, want %q", tc.name, code, tc.want)
			}
		})
	}
}

// ============================================================================
// snapshots —— 区间必填 + 非法错误码 + 趋势读
// ============================================================================

func TestHandleAdminBreakageSnapshots_RangeRequiredAndTrend(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := newBreakageTestApp(t)

	// 缺区间 → BREAKAGE_RANGE_INVALID(400)。
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/breakage/snapshots", nil)
	app.HandleAdminBreakageSnapshots(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing range: code = %d, want 400", w.Code)
	}
	if code := errCodeOf(t, w.Body.Bytes()); code != "BREAKAGE_RANGE_INVALID" {
		t.Fatalf("missing range: code = %q, want BREAKAGE_RANGE_INVALID", code)
	}

	// start>end → 非法。
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodGet, "/api/admin/breakage/snapshots?start_timestamp=200&end_timestamp=100", nil)
	app.HandleAdminBreakageSnapshots(c2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("start>end: code = %d, want 400", w2.Code)
	}

	// 合法区间 → 200 + series（造一批快照后按 tenant 隔离 + 期末区间聚合）。
	periodEnd := int64(1_749_340_800)
	brk_insertSnapshot(t, app, 4, 40, 1, periodEnd, 50, 5, 45, "expired")
	brk_insertSnapshot(t, app, 4, 41, 1, periodEnd, 30, 30, 0, "active")
	brk_insertSnapshot(t, app, 7, 70, 1, periodEnd, 80, 0, 80, "expired") // 另一租户

	// 代理作用域（租户 4）：series 1 桶，只含本租户 2 条 → expired_unused=45（unused_usd 之和 45+0），count=2。
	body := getBreakageSnapshotsJSON(t, app, periodEnd-100, periodEnd+100, func(c *gin.Context) { setAgentScope(c, 4) })
	if len(body.Series) != 1 {
		t.Fatalf("agent(4) series = %d, want 1; body=%+v", len(body.Series), body)
	}
	p := body.Series[0]
	if p.PeriodEnd != periodEnd {
		t.Fatalf("period_end = %d, want %d (epoch seconds anchor)", p.PeriodEnd, periodEnd)
	}
	if p.SubscriptionCount != 2 {
		t.Fatalf("agent(4) subscription_count = %d, want 2 (must not see tenant 7)", p.SubscriptionCount)
	}
	if p.ExpiredUnusedUSD != 45 {
		t.Fatalf("agent(4) expired_unused_usd = %v, want 45 (45+0)", p.ExpiredUnusedUSD)
	}
}

// brk_insertSnapshot 直插一行 breakage_snapshots（复用真实表结构）。
func brk_insertSnapshot(t *testing.T, app *App, tenantID, userID, subID, periodEnd int64, limit, used, unused float64, status string) {
	t.Helper()
	if err := app.DB.Exec(
		`INSERT INTO breakage_snapshots (tenant_id, user_id, sub_id, period_end, plan_code, limit_usd, used_usd, unused_usd, status, snapshot_at) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		tenantID, userID, subID, periodEnd, "starter", limit, used, unused, status, periodEnd,
	).Error; err != nil {
		t.Fatalf("insert snapshot: %v", err)
	}
}

// ============================================================================
// 小工具：调 handler 并解出信封
// ============================================================================

func getBreakageOverview(t *testing.T, app *App, prep func(*gin.Context)) breakageOverviewOut {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/breakage/overview", nil)
	if prep != nil {
		prep(c)
	}
	app.HandleAdminBreakageOverview(c)
	if w.Code != http.StatusOK {
		t.Fatalf("overview code = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var env struct {
		Success bool                `json:"success"`
		Data    breakageOverviewOut `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal overview: %v; body=%s", err, w.Body.String())
	}
	if !env.Success {
		t.Fatalf("overview success=false; body=%s", w.Body.String())
	}
	return env.Data
}

func getBreakageDetailJSON(t *testing.T, app *App, query string, prep func(*gin.Context)) breakageDetailOut {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	url := "/api/admin/breakage/detail"
	if query != "" {
		url += "?" + query
	}
	c.Request = httptest.NewRequest(http.MethodGet, url, nil)
	if prep != nil {
		prep(c)
	}
	app.HandleAdminBreakageDetail(c)
	if w.Code != http.StatusOK {
		t.Fatalf("detail code = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var env struct {
		Success bool              `json:"success"`
		Data    breakageDetailOut `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal detail: %v; body=%s", err, w.Body.String())
	}
	if !env.Success {
		t.Fatalf("detail success=false; body=%s", w.Body.String())
	}
	return env.Data
}

func getBreakageSnapshotsJSON(t *testing.T, app *App, start, end int64, prep func(*gin.Context)) breakageSnapshotsOut {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet,
		"/api/admin/breakage/snapshots?start_timestamp="+itoa(start)+"&end_timestamp="+itoa(end), nil)
	if prep != nil {
		prep(c)
	}
	app.HandleAdminBreakageSnapshots(c)
	if w.Code != http.StatusOK {
		t.Fatalf("snapshots code = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var env struct {
		Success bool                 `json:"success"`
		Data    breakageSnapshotsOut `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal snapshots: %v; body=%s", err, w.Body.String())
	}
	if !env.Success {
		t.Fatalf("snapshots success=false; body=%s", w.Body.String())
	}
	return env.Data
}

// errCodeOf 从错误信封解出 code 字段（respondErr 形态：{success,message,code}）。
func errCodeOf(t *testing.T, body []byte) string {
	t.Helper()
	var env struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("unmarshal err envelope: %v; body=%s", err, string(body))
	}
	if env.Success {
		t.Fatalf("expected error envelope (success=false); body=%s", string(body))
	}
	return env.Code
}

// itoa 是 strconv.FormatInt 的十进制简写。
func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}
