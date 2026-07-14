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

package gormrepo

// 用 sqlite(:memory:) 建裸表（不 import 兄弟模块 model，保持本包「raw db.Table()」测试习惯，
// 对齐 internal/report/reportrepo/reportrepo_test.go 的 harness 风格）。breakage_snapshots 由本包
// AutoMigrate 建。覆盖：4 指标聚合数值、【门 #4 租户隔离】、惰性过期口径（按 expire_at）、
// Detail 筛选/分页/告警级、Upsert 幂等、回填幂等（重跑不翻倍）、快照趋势分桶。

import (
	"context"
	"fmt"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/breakage"
)

// nowSec 是所有用例共用的「当下」锚点（epoch 秒，固定值使区间/到期判定确定）。
const nowSec int64 = 1_700_000_000

// newBreakageTestDB 建 sqlite(:memory:) + breakage 依赖裸表（订阅/套餐/钱包/订单/租户/用户），
// 并调本包 AutoMigrate 建 breakage_snapshots。DATETIME 列（expire_at/created_at）与生产同型。
func newBreakageTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1) // sqlite(:memory:) 单连接，避免多连接看不到同一内存库
	stmts := []string{
		`CREATE TABLE tokenplan_subscriptions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL DEFAULT 0,
			user_id INTEGER NOT NULL DEFAULT 0,
			plan_id INTEGER NOT NULL DEFAULT 0,
			month_limit_usd REAL NOT NULL DEFAULT 0,
			used_usd REAL NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'active',
			start_at DATETIME,
			expire_at DATETIME,
			source_order_id TEXT
		)`,
		// 原生桶真源 + 桥接（used_usd 死列 → 读侧投影 amount_used/QuotaPerUnit，见 nativeUsedUSD）。
		`CREATE TABLE mt_subscription_orders (order_no TEXT PRIMARY KEY, native_sub_id INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE user_subscriptions (id INTEGER PRIMARY KEY AUTOINCREMENT, amount_used INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE token_plans (id INTEGER PRIMARY KEY, code TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE user_balances (tenant_id INTEGER NOT NULL, user_id INTEGER NOT NULL, balance_usd REAL NOT NULL DEFAULT 0)`,
		`CREATE TABLE payment_orders (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL DEFAULT 0, status TEXT, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE tenants (id INTEGER PRIMARY KEY, name TEXT, owner_user_id INTEGER)`,
		`CREATE TABLE users (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL DEFAULT 0, username TEXT, deleted_at DATETIME)`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("create table: %v\nSQL: %s", err, s)
		}
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate breakage_snapshots: %v", err)
	}
	return db
}

// ---- seed helpers（裸 SQL 插入，不经 struct 映射）----

// subSpec 描述一条订阅（时间用 epoch 秒，seed 时转 UTC DATETIME）。
type subSpec struct {
	tenantID    int64
	userID      int64
	planID      int64
	monthLimit  float64
	used        float64
	status      string
	expireEpoch int64
}

func seedSub(t *testing.T, db *gorm.DB, s subSpec) {
	t.Helper()
	// 用量走生产真源：原生 user_subscriptions.amount_used(quota)，经 mt_subscription_orders.native_sub_id
	// 关联。tokenplan_subscriptions.used_usd 是死列，恒置 0（坐实读侧只投影原生桶、不读死列，P2-BRK-02）。
	if err := db.Exec(`INSERT INTO user_subscriptions (amount_used) VALUES (?)`,
		int64(s.used*common.QuotaPerUnit)).Error; err != nil {
		t.Fatalf("seed native sub: %v", err)
	}
	var nativeSubID int64
	if err := db.Raw(`SELECT last_insert_rowid()`).Scan(&nativeSubID).Error; err != nil {
		t.Fatalf("native sub id: %v", err)
	}
	orderNo := fmt.Sprintf("ord-%d", nativeSubID)
	if err := db.Exec(`INSERT INTO mt_subscription_orders (order_no, native_sub_id) VALUES (?,?)`,
		orderNo, nativeSubID).Error; err != nil {
		t.Fatalf("seed bridge order: %v", err)
	}
	if err := db.Exec(
		`INSERT INTO tokenplan_subscriptions (tenant_id, user_id, plan_id, month_limit_usd, used_usd, status, start_at, expire_at, source_order_id)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		s.tenantID, s.userID, s.planID, s.monthLimit, 0, s.status,
		unixT(s.expireEpoch-86400), unixT(s.expireEpoch), orderNo,
	).Error; err != nil {
		t.Fatalf("seed sub (tenant=%d user=%d): %v", s.tenantID, s.userID, err)
	}
}

func seedPlan(t *testing.T, db *gorm.DB, id int64, code string) {
	t.Helper()
	if err := db.Exec(`INSERT INTO token_plans (id, code) VALUES (?,?)`, id, code).Error; err != nil {
		t.Fatalf("seed plan: %v", err)
	}
}

func seedBalance(t *testing.T, db *gorm.DB, tenantID, userID int64, balance float64) {
	t.Helper()
	if err := db.Exec(`INSERT INTO user_balances (tenant_id, user_id, balance_usd) VALUES (?,?,?)`,
		tenantID, userID, balance).Error; err != nil {
		t.Fatalf("seed balance: %v", err)
	}
}

func seedOrder(t *testing.T, db *gorm.DB, tenantID int64, status string, ageEpoch int64) {
	t.Helper()
	// created_at 与 updated_at 同置为 ageEpoch：AnomalyCount 按 updated_at 判「超 minAge 未变动」。
	if err := db.Exec(`INSERT INTO payment_orders (tenant_id, status, created_at, updated_at) VALUES (?,?,?,?)`,
		tenantID, status, unixT(ageEpoch), unixT(ageEpoch)).Error; err != nil {
		t.Fatalf("seed order: %v", err)
	}
}

// approxEq 是 float64 近似比较（decimal(20,8) 经 sqlite REAL 往返有微误差）。
func approxEq(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-6
}

// ============================================================================
// Overview —— 4 指标数值 + 惰性过期口径
// ============================================================================

func TestOverview_AggregatesFourMetrics(t *testing.T) {
	db := newBreakageTestDB(t)
	repo := New(db)
	ctx := context.Background()

	// 租户 5：一条活跃（limit 100 / used 20 → 剩余 80，未过期）+ 一条已过期未用完（limit 50 / used 10 → 沉淀 40）。
	seedSub(t, db, subSpec{tenantID: 5, userID: 1, planID: 1, monthLimit: 100, used: 20, status: "active", expireEpoch: nowSec + 86400})
	seedSub(t, db, subSpec{tenantID: 5, userID: 2, planID: 1, monthLimit: 50, used: 10, status: "expired", expireEpoch: nowSec - 86400})
	// 一条过期但已用满（limit 30 / used 30 → 剩余 0，不计入到期未用）。
	seedSub(t, db, subSpec{tenantID: 5, userID: 3, planID: 1, monthLimit: 30, used: 30, status: "expired", expireEpoch: nowSec - 86400})

	// 钱包未消耗：租户 5 两个用户各 5 / 7 → 12。
	seedBalance(t, db, 5, 1, 5)
	seedBalance(t, db, 5, 2, 7)

	// 异常卡单：只计 paid（已支付未入账）。一条 paid 超 5min（计入，唯一真异常）；两条 created（已下单
	// 未支付＝废单，一条超 5min 一条刚落单，均不计）；一条 credited（终态，不计）。
	seedOrder(t, db, 5, "paid", nowSec-anomalyMinAgeSec-10)   // 计入
	seedOrder(t, db, 5, "created", nowSec-anomalyMinAgeSec-1) // 废单：不计（即便超 5min）
	seedOrder(t, db, 5, "created", nowSec-10)                 // 废单且未超 5min：不计
	seedOrder(t, db, 5, "credited", nowSec-3600)              // 终态：不计

	tid := int64(5)
	ov, err := repo.Overview(ctx, &tid, nowSec)
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if !approxEq(ov.ActiveRemainingUSD, 80) {
		t.Fatalf("ActiveRemainingUSD = %v, want 80", ov.ActiveRemainingUSD)
	}
	if !approxEq(ov.ExpiredUnusedUSD, 40) {
		t.Fatalf("ExpiredUnusedUSD = %v, want 40", ov.ExpiredUnusedUSD)
	}
	if !approxEq(ov.WalletUnusedUSD, 12) {
		t.Fatalf("WalletUnusedUSD = %v, want 12", ov.WalletUnusedUSD)
	}
	if ov.AnomalyCount != 1 {
		t.Fatalf("AnomalyCount = %d, want 1（仅 paid-stuck 计入；created 废单排除）", ov.AnomalyCount)
	}
}

// TestOverview_LazyExpiryByExpireAt 锁定惰性过期口径：一条 expire_at<now 但 status 仍持久化为 'active'
// 的订阅，必须计入「到期未用」（按 expire_at 判到期，不信 status），且不计入「活跃剩余」。
func TestOverview_LazyExpiryByExpireAt(t *testing.T) {
	db := newBreakageTestDB(t)
	repo := New(db)
	ctx := context.Background()

	// status 仍 'active'，但 expire_at 已过去；limit 100 / used 30 → 到期未用 70，活跃剩余 0。
	seedSub(t, db, subSpec{tenantID: 7, userID: 1, planID: 1, monthLimit: 100, used: 30, status: "active", expireEpoch: nowSec - 1})

	tid := int64(7)
	ov, err := repo.Overview(ctx, &tid, nowSec)
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if !approxEq(ov.ActiveRemainingUSD, 0) {
		t.Fatalf("ActiveRemainingUSD = %v, want 0 (expired despite status=active)", ov.ActiveRemainingUSD)
	}
	if !approxEq(ov.ExpiredUnusedUSD, 70) {
		t.Fatalf("ExpiredUnusedUSD = %v, want 70 (by expire_at, not persisted status)", ov.ExpiredUnusedUSD)
	}
}

// TestOverview_TenantIsolation 锁定门 #4：租户 A 的作用域查询绝不含租户 B 的数据；
// 跨租户（nil）汇总含 A+B 但排除 tenant_id=0。
func TestOverview_TenantIsolation(t *testing.T) {
	db := newBreakageTestDB(t)
	repo := New(db)
	ctx := context.Background()

	// 租户 5：活跃剩余 80。
	seedSub(t, db, subSpec{tenantID: 5, userID: 1, planID: 1, monthLimit: 100, used: 20, status: "active", expireEpoch: nowSec + 86400})
	seedBalance(t, db, 5, 1, 5)
	// 租户 8：活跃剩余 40。
	seedSub(t, db, subSpec{tenantID: 8, userID: 9, planID: 1, monthLimit: 60, used: 20, status: "active", expireEpoch: nowSec + 86400})
	seedBalance(t, db, 8, 9, 3)
	// tenant_id=0（主站未归属）：活跃剩余 1000，钱包 999 —— 跨租户聚合必须排除。
	seedSub(t, db, subSpec{tenantID: 0, userID: 100, planID: 1, monthLimit: 1000, used: 0, status: "active", expireEpoch: nowSec + 86400})
	seedBalance(t, db, 0, 100, 999)

	// 租户 5 作用域：只见自己。
	t5 := int64(5)
	ov5, err := repo.Overview(ctx, &t5, nowSec)
	if err != nil {
		t.Fatalf("Overview t5: %v", err)
	}
	if !approxEq(ov5.ActiveRemainingUSD, 80) {
		t.Fatalf("t5 ActiveRemainingUSD = %v, want 80 (must not leak t8/t0)", ov5.ActiveRemainingUSD)
	}
	if !approxEq(ov5.WalletUnusedUSD, 5) {
		t.Fatalf("t5 WalletUnusedUSD = %v, want 5", ov5.WalletUnusedUSD)
	}

	// 跨租户（nil）：含 5+8，排除 0。活跃剩余 80+40=120；钱包 5+3=8。
	ovAll, err := repo.Overview(ctx, nil, nowSec)
	if err != nil {
		t.Fatalf("Overview nil: %v", err)
	}
	if !approxEq(ovAll.ActiveRemainingUSD, 120) {
		t.Fatalf("cross-tenant ActiveRemainingUSD = %v, want 120 (exclude tenant_id=0)", ovAll.ActiveRemainingUSD)
	}
	if !approxEq(ovAll.WalletUnusedUSD, 8) {
		t.Fatalf("cross-tenant WalletUnusedUSD = %v, want 8 (exclude tenant_id=0)", ovAll.WalletUnusedUSD)
	}
}

// ============================================================================
// Detail —— JOIN 回填 / 派生字段 / 筛选 / 分页 / 隔离
// ============================================================================

func TestDetail_DerivedFieldsAndPlanCodeJoin(t *testing.T) {
	db := newBreakageTestDB(t)
	repo := New(db)
	ctx := context.Background()

	seedPlan(t, db, 1, "pro")
	if err := db.Exec(`INSERT INTO tenants (id, name, owner_user_id) VALUES (5,'代理甲',0)`).Error; err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if err := db.Exec(`INSERT INTO users (id, tenant_id, username) VALUES (1,5,'alice')`).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	// limit 100 / used 96 → unused 4，pct 96 → critical。
	seedSub(t, db, subSpec{tenantID: 5, userID: 1, planID: 1, monthLimit: 100, used: 96, status: "active", expireEpoch: nowSec + 86400})

	tid := int64(5)
	rows, total, err := repo.Detail(ctx, breakage.Filter{TenantID: &tid, Page: 1, PageSize: 20}, nowSec)
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if total != 1 || len(rows) != 1 {
		t.Fatalf("total=%d len=%d, want 1/1", total, len(rows))
	}
	r0 := rows[0]
	if r0.PlanCode != "pro" {
		t.Fatalf("PlanCode = %q, want pro (join token_plans)", r0.PlanCode)
	}
	if r0.TenantName != "代理甲" || r0.Username != "alice" {
		t.Fatalf("name backfill: tenant=%q user=%q", r0.TenantName, r0.Username)
	}
	if !approxEq(r0.UnusedUSD, 4) {
		t.Fatalf("UnusedUSD = %v, want 4", r0.UnusedUSD)
	}
	if !approxEq(r0.UsagePct, 96) {
		t.Fatalf("UsagePct = %v, want 96", r0.UsagePct)
	}
	if r0.AlertLevel != breakage.AlertCritical {
		t.Fatalf("AlertLevel = %q, want critical", r0.AlertLevel)
	}
	if r0.PeriodEnd != nowSec+86400 {
		t.Fatalf("PeriodEnd = %d, want %d", r0.PeriodEnd, nowSec+86400)
	}
}

func TestDetail_UnusedClampAndZeroLimit(t *testing.T) {
	db := newBreakageTestDB(t)
	repo := New(db)
	ctx := context.Background()

	// used > limit（异常数据）→ unused clamp 到 0，pct 记 200（>100 → exhausted）。
	seedSub(t, db, subSpec{tenantID: 5, userID: 1, planID: 1, monthLimit: 10, used: 20, status: "active", expireEpoch: nowSec + 86400})
	// limit=0 → pct 记 0（防除零 → none/healthy）。
	seedSub(t, db, subSpec{tenantID: 5, userID: 2, planID: 1, monthLimit: 0, used: 0, status: "active", expireEpoch: nowSec + 86400})

	tid := int64(5)
	rows, _, err := repo.Detail(ctx, breakage.Filter{TenantID: &tid, Page: 1, PageSize: 20}, nowSec)
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	byUser := map[int64]breakage.DetailRow{}
	for _, r := range rows {
		byUser[r.UserID] = r
	}
	if got := byUser[1]; !approxEq(got.UnusedUSD, 0) || got.AlertLevel != breakage.AlertExhausted {
		t.Fatalf("user1 unused=%v level=%q, want 0/exhausted", got.UnusedUSD, got.AlertLevel)
	}
	if got := byUser[2]; !approxEq(got.UsagePct, 0) || got.AlertLevel != breakage.AlertNone {
		t.Fatalf("user2 pct=%v level=%q, want 0/none", got.UsagePct, got.AlertLevel)
	}
}

func TestDetail_FilterAndPaginate(t *testing.T) {
	db := newBreakageTestDB(t)
	repo := New(db)
	ctx := context.Background()

	seedPlan(t, db, 1, "basic")
	seedPlan(t, db, 2, "pro")
	// 3 条 basic + 1 条 pro，全租户 5。
	for i := int64(1); i <= 3; i++ {
		seedSub(t, db, subSpec{tenantID: 5, userID: i, planID: 1, monthLimit: 100, used: 10, status: "active", expireEpoch: nowSec + int64(i)*86400})
	}
	seedSub(t, db, subSpec{tenantID: 5, userID: 4, planID: 2, monthLimit: 100, used: 10, status: "active", expireEpoch: nowSec + 86400})

	tid := int64(5)
	// 按 plan_code=basic 过滤 → 3 条。
	_, total, err := repo.Detail(ctx, breakage.Filter{TenantID: &tid, PlanCode: "basic", Page: 1, PageSize: 20}, nowSec)
	if err != nil {
		t.Fatalf("Detail filter: %v", err)
	}
	if total != 3 {
		t.Fatalf("basic total = %d, want 3", total)
	}
	// 分页：pageSize=2 → 第 1 页 2 条、第 2 页 1 条，total 恒 3。
	p1, total, err := repo.Detail(ctx, breakage.Filter{TenantID: &tid, PlanCode: "basic", Page: 1, PageSize: 2}, nowSec)
	if err != nil {
		t.Fatalf("Detail page1: %v", err)
	}
	if len(p1) != 2 || total != 3 {
		t.Fatalf("page1 len=%d total=%d, want 2/3", len(p1), total)
	}
	p2, _, err := repo.Detail(ctx, breakage.Filter{TenantID: &tid, PlanCode: "basic", Page: 2, PageSize: 2}, nowSec)
	if err != nil {
		t.Fatalf("Detail page2: %v", err)
	}
	if len(p2) != 1 {
		t.Fatalf("page2 len=%d, want 1", len(p2))
	}
}

func TestDetail_AlertLevelFilter(t *testing.T) {
	db := newBreakageTestDB(t)
	repo := New(db)
	ctx := context.Background()

	// healthy(50%) / warn(85%) / critical(96%) 各一条。
	seedSub(t, db, subSpec{tenantID: 5, userID: 1, planID: 1, monthLimit: 100, used: 50, status: "active", expireEpoch: nowSec + 86400})
	seedSub(t, db, subSpec{tenantID: 5, userID: 2, planID: 1, monthLimit: 100, used: 85, status: "active", expireEpoch: nowSec + 86400})
	seedSub(t, db, subSpec{tenantID: 5, userID: 3, planID: 1, monthLimit: 100, used: 96, status: "active", expireEpoch: nowSec + 86400})

	tid := int64(5)
	rows, total, err := repo.Detail(ctx, breakage.Filter{TenantID: &tid, AlertLevel: breakage.AlertWarn, Page: 1, PageSize: 20}, nowSec)
	if err != nil {
		t.Fatalf("Detail alert filter: %v", err)
	}
	if total != 1 || len(rows) != 1 || rows[0].UserID != 2 {
		t.Fatalf("warn filter total=%d rows=%+v, want 1 row user2", total, rows)
	}
}

// TestDetail_LazyExpiryStatusFlip 锁定明细读路径的惰性过期口径：expire_at<now 但库里仍 'active'
// 的订阅，明细行 Status 必须展示为 expired（只读定格，不回写库），与 CollectSnapshots 口径一致。
func TestDetail_LazyExpiryStatusFlip(t *testing.T) {
	db := newBreakageTestDB(t)
	repo := New(db)
	ctx := context.Background()

	// 库里 status='active'，但 expire_at 已过去。
	seedSub(t, db, subSpec{tenantID: 5, userID: 1, planID: 1, monthLimit: 100, used: 30, status: "active", expireEpoch: nowSec - 1})

	tid := int64(5)
	rows, _, err := repo.Detail(ctx, breakage.Filter{TenantID: &tid, Page: 1, PageSize: 20}, nowSec)
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].Status != "expired" {
		t.Fatalf("Status = %q, want expired (lazy flip by expire_at<now, not persisted 'active')", rows[0].Status)
	}
	// 库里未被改动（只读投影）——回查仍是 active。
	var dbStatus string
	if err := db.Table("tokenplan_subscriptions").Select("status").Where("user_id = ?", 1).Scan(&dbStatus).Error; err != nil {
		t.Fatalf("read back status: %v", err)
	}
	if dbStatus != "active" {
		t.Fatalf("DB status = %q, want active (Detail must not write back)", dbStatus)
	}
}

func TestDetail_TenantIsolation(t *testing.T) {
	db := newBreakageTestDB(t)
	repo := New(db)
	ctx := context.Background()

	seedSub(t, db, subSpec{tenantID: 5, userID: 1, planID: 1, monthLimit: 100, used: 10, status: "active", expireEpoch: nowSec + 86400})
	seedSub(t, db, subSpec{tenantID: 8, userID: 2, planID: 1, monthLimit: 100, used: 10, status: "active", expireEpoch: nowSec + 86400})

	t5 := int64(5)
	rows, total, err := repo.Detail(ctx, breakage.Filter{TenantID: &t5, Page: 1, PageSize: 20}, nowSec)
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if total != 1 || len(rows) != 1 || rows[0].TenantID != 5 {
		t.Fatalf("t5 detail leaked: total=%d rows=%+v", total, rows)
	}
}

// ============================================================================
// 快照：采集 / Upsert 幂等 / 回填幂等 / 趋势
// ============================================================================

func TestCollectSnapshots_LazyExpiryStatus(t *testing.T) {
	db := newBreakageTestDB(t)
	repo := New(db)
	ctx := context.Background()
	seedPlan(t, db, 1, "pro")

	// active 未过期 → 快照 status=active，unused=70。
	seedSub(t, db, subSpec{tenantID: 5, userID: 1, planID: 1, monthLimit: 100, used: 30, status: "active", expireEpoch: nowSec + 86400})
	// active 已过期（库里仍 active）→ 快照 status 应定格为 expired，unused=40。
	seedSub(t, db, subSpec{tenantID: 5, userID: 2, planID: 1, monthLimit: 50, used: 10, status: "active", expireEpoch: nowSec - 86400})

	snaps, err := repo.CollectSnapshots(ctx, nil, nowSec)
	if err != nil {
		t.Fatalf("CollectSnapshots: %v", err)
	}
	if len(snaps) != 2 {
		t.Fatalf("collected %d snapshots, want 2", len(snaps))
	}
	byUser := map[int64]breakage.Snapshot{}
	for _, s := range snaps {
		byUser[s.UserID] = s
	}
	if s := byUser[1]; s.Status != "active" || !approxEq(s.UnusedUSD, 70) || s.PlanCode != "pro" {
		t.Fatalf("user1 snap = %+v, want active/70/pro", s)
	}
	if s := byUser[2]; s.Status != "expired" {
		t.Fatalf("user2 snap status = %q, want expired (lazy by expire_at)", s.Status)
	}
	if s := byUser[2]; s.PeriodEnd != nowSec-86400 || s.SnapshotAt != nowSec {
		t.Fatalf("user2 snap period=%d snapAt=%d, want %d/%d", s.PeriodEnd, s.SnapshotAt, nowSec-86400, nowSec)
	}
}

func TestUpsertSnapshots_Idempotent(t *testing.T) {
	db := newBreakageTestDB(t)
	repo := New(db)
	ctx := context.Background()

	base := []breakage.Snapshot{
		{TenantID: 5, UserID: 1, SubID: 10, PlanCode: "pro", LimitUSD: 100, UsedUSD: 30, UnusedUSD: 70, Status: "active", PeriodEnd: nowSec, SnapshotAt: nowSec},
	}
	if err := repo.UpsertSnapshots(ctx, base); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	// 重跑同键、值变更 → 更新而非新增（唯一键 tenant/user/sub/period 命中）。
	updated := []breakage.Snapshot{
		{TenantID: 5, UserID: 1, SubID: 10, PlanCode: "pro", LimitUSD: 100, UsedUSD: 40, UnusedUSD: 60, Status: "expired", PeriodEnd: nowSec, SnapshotAt: nowSec + 100},
	}
	if err := repo.UpsertSnapshots(ctx, updated); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	var count int64
	if err := db.Table("breakage_snapshots").Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("row count = %d, want 1 (upsert must not duplicate on unique key)", count)
	}
	var row struct {
		UsedUSD    float64
		UnusedUSD  float64
		Status     string
		SnapshotAt int64
	}
	if err := db.Table("breakage_snapshots").
		Select("used_usd, unused_usd, status, snapshot_at").
		Where("tenant_id = ? AND user_id = ? AND sub_id = ? AND period_end = ?", 5, 1, 10, nowSec).
		Scan(&row).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !approxEq(row.UsedUSD, 40) || !approxEq(row.UnusedUSD, 60) || row.Status != "expired" || row.SnapshotAt != nowSec+100 {
		t.Fatalf("upsert did not update fields: %+v", row)
	}
}

// TestBackfill_Idempotent 模拟历史回填：Collect→Upsert 跑两遍，行数不翻倍（回填幂等，硬约束 C-迁移幂等）。
func TestBackfill_Idempotent(t *testing.T) {
	db := newBreakageTestDB(t)
	repo := New(db)
	ctx := context.Background()
	seedPlan(t, db, 1, "pro")

	seedSub(t, db, subSpec{tenantID: 5, userID: 1, planID: 1, monthLimit: 100, used: 30, status: "expired", expireEpoch: nowSec - 86400})
	seedSub(t, db, subSpec{tenantID: 8, userID: 2, planID: 1, monthLimit: 60, used: 10, status: "expired", expireEpoch: nowSec - 86400})

	runOnce := func() {
		snaps, err := repo.CollectSnapshots(ctx, nil, nowSec)
		if err != nil {
			t.Fatalf("collect: %v", err)
		}
		if err := repo.UpsertSnapshots(ctx, snaps); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}
	runOnce()
	runOnce() // 重跑

	var count int64
	if err := db.Table("breakage_snapshots").Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Fatalf("backfill row count = %d, want 2 (re-run must not double)", count)
	}
}

func TestSnapshotTrend_BucketsByPeriodAndIsolates(t *testing.T) {
	db := newBreakageTestDB(t)
	repo := New(db)
	ctx := context.Background()

	p1 := nowSec - 2*86400
	p2 := nowSec - 86400
	// 期 p1（租户 5）：两条——expired(unused10)→到期未用，active(unused20)→活跃剩余（两列互补）。
	snaps := []breakage.Snapshot{
		{TenantID: 5, UserID: 1, SubID: 1, PlanCode: "pro", LimitUSD: 100, UsedUSD: 90, UnusedUSD: 10, Status: "expired", PeriodEnd: p1, SnapshotAt: nowSec},
		{TenantID: 5, UserID: 2, SubID: 2, PlanCode: "pro", LimitUSD: 100, UsedUSD: 80, UnusedUSD: 20, Status: "active", PeriodEnd: p1, SnapshotAt: nowSec},
		// 期 p2（租户 5）：一条 unused 5。
		{TenantID: 5, UserID: 3, SubID: 3, PlanCode: "pro", LimitUSD: 100, UsedUSD: 95, UnusedUSD: 5, Status: "expired", PeriodEnd: p2, SnapshotAt: nowSec},
		// 租户 8（同期 p1）：unused 999 —— 租户 5 作用域必须查不到。
		{TenantID: 8, UserID: 9, SubID: 9, PlanCode: "pro", LimitUSD: 1000, UsedUSD: 1, UnusedUSD: 999, Status: "expired", PeriodEnd: p1, SnapshotAt: nowSec},
	}
	if err := repo.UpsertSnapshots(ctx, snaps); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	t5 := int64(5)
	pts, err := repo.SnapshotTrend(ctx, &t5, p1-1, nowSec)
	if err != nil {
		t.Fatalf("SnapshotTrend: %v", err)
	}
	if len(pts) != 2 {
		t.Fatalf("got %d buckets, want 2 (p1,p2)", len(pts))
	}
	// 升序：pts[0]=p1, pts[1]=p2。
	if pts[0].PeriodEnd != p1 || pts[1].PeriodEnd != p2 {
		t.Fatalf("bucket order = %d,%d, want %d,%d (asc)", pts[0].PeriodEnd, pts[1].PeriodEnd, p1, p2)
	}
	if !approxEq(pts[0].ExpiredUnusedUSD, 10) {
		t.Fatalf("p1 ExpiredUnusedUSD = %v, want 10 (only status<>active row; isolate t8's 999)", pts[0].ExpiredUnusedUSD)
	}
	if !approxEq(pts[0].ActiveRemainingUSD, 20) {
		t.Fatalf("p1 ActiveRemainingUSD = %v, want 20 (only status=active row)", pts[0].ActiveRemainingUSD)
	}
	if pts[0].SubscriptionCount != 2 {
		t.Fatalf("p1 SubscriptionCount = %d, want 2", pts[0].SubscriptionCount)
	}
	if !approxEq(pts[1].ExpiredUnusedUSD, 5) || pts[1].SubscriptionCount != 1 {
		t.Fatalf("p2 = %+v, want unused 5 count 1", pts[1])
	}
}

// TestSnapshotTrend_CrossTenantExcludesZero 锁定跨租户趋势排除 tenant_id=0。
func TestSnapshotTrend_CrossTenantExcludesZero(t *testing.T) {
	db := newBreakageTestDB(t)
	repo := New(db)
	ctx := context.Background()

	p := nowSec - 86400
	snaps := []breakage.Snapshot{
		{TenantID: 5, UserID: 1, SubID: 1, LimitUSD: 100, UsedUSD: 90, UnusedUSD: 10, Status: "expired", PeriodEnd: p, SnapshotAt: nowSec},
		{TenantID: 0, UserID: 2, SubID: 2, LimitUSD: 100, UsedUSD: 0, UnusedUSD: 100, Status: "expired", PeriodEnd: p, SnapshotAt: nowSec},
	}
	if err := repo.UpsertSnapshots(ctx, snaps); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	pts, err := repo.SnapshotTrend(ctx, nil, p-1, nowSec)
	if err != nil {
		t.Fatalf("SnapshotTrend: %v", err)
	}
	if len(pts) != 1 {
		t.Fatalf("got %d buckets, want 1", len(pts))
	}
	if !approxEq(pts[0].ExpiredUnusedUSD, 10) {
		t.Fatalf("cross-tenant trend = %v, want 10 (exclude tenant_id=0's 100)", pts[0].ExpiredUnusedUSD)
	}
}
