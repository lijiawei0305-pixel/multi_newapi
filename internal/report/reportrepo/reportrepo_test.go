package reportrepo

// 覆盖 ratio_markup（L1/独立档差价收益，internal/agent/model.go SourceRatioMarkup）在财务报表聚合层的
// 分类/汇总：新来源必须像 consume_commission 一样计入 (a) 收益总额 (b) 按来源分桶 (c) 管理端排行的
// per-tenant 分桶——见 doc/finance-report-contract.md 与 CLAUDE.md 待办「违禁词屏蔽」旁的money-flow扩展。
//
// 测试用最小裸表（不复用 agent/gormrepo 的 AutoMigrate，保持本包一贯的「raw db.Table()，不 import
// 兄弟模块 model 结构」测试习惯，对齐包头注释与 internal/mtwire/agent_metrics_test.go 的 harness 风格）。

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// newFinanceTestDB 建 sqlite(:memory:) + reportrepo 全部 8 张只读依赖表（收益/钱包/提现/充值/套餐/
// 租户/用户/原生日志），足以驱动 SummaryEarnings/AgentRanking/TrendEarnings 等公开方法而不报
// "no such table"。多数表在具体用例中留空即可（COALESCE(SUM(...),0) 兜底 0）。
func newFinanceTestDB(t *testing.T) *gorm.DB {
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
		`CREATE TABLE mt_wallet_consume_log (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, user_id INTEGER NOT NULL DEFAULT 0, wallet_quota INTEGER NOT NULL DEFAULT 0, request_id TEXT NOT NULL, created_at DATETIME NOT NULL)`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("create table: %v\nSQL: %s", err, s)
		}
	}
	return db
}

// seedEarning 插入一条 agent_earning_logs 行（source_id 用 sourceType 前缀保证同租户多来源不冲突）。
func seedEarning(t *testing.T, db *gorm.DB, tenantID int64, sourceType string, amount float64, ts time.Time) {
	t.Helper()
	if err := db.Exec(
		`INSERT INTO agent_earning_logs (tenant_id, user_id, source_type, source_id, amount, created_at) VALUES (?,?,?,?,?,?)`,
		tenantID, tenantID, sourceType, sourceType+"-seed", amount, ts,
	).Error; err != nil {
		t.Fatalf("seed earning (tenant=%d source=%s): %v", tenantID, sourceType, err)
	}
}

// TestSummaryEarnings_IncludesRatioMarkup 是回归测试（SummaryEarnings 直接 GROUP BY source_type，
// 无白名单过滤，本就应含 ratio_markup；此测试锁定该正确行为，防止未来引入白名单式回归）。
func TestSummaryEarnings_IncludesRatioMarkup(t *testing.T) {
	db := newFinanceTestDB(t)
	repo := New(db)
	ctx := context.Background()
	base := time.Unix(1_700_000_000, 0).UTC()
	seedEarning(t, db, 9, "ratio_markup", 40.5, base)
	seedEarning(t, db, 5, "consume_commission", 10, base)

	start := base.Add(-time.Hour).Unix()
	end := base.Add(time.Hour).Unix()
	rows, err := repo.SummaryEarnings(ctx, nil, start, end)
	if err != nil {
		t.Fatalf("SummaryEarnings: %v", err)
	}
	got := map[string]float64{}
	for _, r := range rows {
		got[r.SourceType] = r.AmountCNY
	}
	if got["ratio_markup"] != 40.5 {
		t.Fatalf("ratio_markup amount = %v, want 40.5 (rows=%+v)", got["ratio_markup"], rows)
	}
	if got["consume_commission"] != 10 {
		t.Fatalf("consume_commission amount = %v, want 10 (rows=%+v)", got["consume_commission"], rows)
	}
}

// TestEarningsByTenant_RatioMarkupCountsTowardTotalAndOwnBucket 是本次修复的核心用例：L1 差价
// (ratio_markup) 必须计入该租户的收益总额，且拥有自己的分桶字段（不与 consume_commission 混淆、
// 不被静默丢弃）。租户 9 模拟 L1（ratio_markup + manual_adjustment 混合来源）；租户 5 模拟 L0
// （只有 consume_commission）——互不污染，镜像 internal/mtwire/agent_tiering_test.go 的
// TestCreditConsumeCommission_TierSwitch_NeverBothSources 断言风格。
func TestEarningsByTenant_RatioMarkupCountsTowardTotalAndOwnBucket(t *testing.T) {
	db := newFinanceTestDB(t)
	repo := New(db)
	ctx := context.Background()
	base := time.Unix(1_700_000_000, 0).UTC()
	seedEarning(t, db, 9, "ratio_markup", 40.5, base)
	seedEarning(t, db, 9, "manual_adjustment", 1.5, base)
	seedEarning(t, db, 5, "consume_commission", 10, base)

	start := base.Add(-time.Hour).Unix()
	end := base.Add(time.Hour).Unix()
	out, err := repo.earningsByTenant(ctx, start, end)
	if err != nil {
		t.Fatalf("earningsByTenant: %v", err)
	}

	t9 := out[9]
	if t9.total != 42.0 {
		t.Fatalf("tenant 9 total = %v, want 42.0 (40.5 ratio_markup + 1.5 manual_adjustment)", t9.total)
	}
	if t9.ratioMarkup != 40.5 {
		t.Fatalf("tenant 9 ratioMarkup bucket = %v, want 40.5", t9.ratioMarkup)
	}
	if t9.consume != 0 {
		t.Fatalf("tenant 9 consume bucket = %v, want 0 (L1 must never leak into consume_commission bucket)", t9.consume)
	}

	t5 := out[5]
	if t5.consume != 10 {
		t.Fatalf("tenant 5 consume bucket = %v, want 10", t5.consume)
	}
	if t5.ratioMarkup != 0 {
		t.Fatalf("tenant 5 ratioMarkup bucket = %v, want 0 (L0 must never leak into ratio_markup bucket)", t5.ratioMarkup)
	}
	if t5.total != 10 {
		t.Fatalf("tenant 5 total = %v, want 10", t5.total)
	}
}

// TestAgentRanking_RatioMarkupCountsTowardTotalAndOwnBucket 是端到端（公开 API）版本：管理端排行
// 一行里，ratio_markup 既要计入 TotalEarnedCNY，也要出现在自己的 RatioMarkupCNY 分桶列，不能只在
// 总额里隐身、breakdown 列全 0（对应 report.go summarySourceOrder 的同类 bug）。
func TestAgentRanking_RatioMarkupCountsTowardTotalAndOwnBucket(t *testing.T) {
	db := newFinanceTestDB(t)
	repo := New(db)
	ctx := context.Background()
	base := time.Unix(1_700_000_000, 0).UTC()
	seedEarning(t, db, 9, "ratio_markup", 40.5, base)
	seedEarning(t, db, 5, "consume_commission", 10, base)

	start := base.Add(-time.Hour).Unix()
	end := base.Add(time.Hour).Unix()
	rows, total, err := repo.AgentRanking(ctx, start, end, "total_earned_cny", "desc", 1, 20)
	if err != nil {
		t.Fatalf("AgentRanking: %v", err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2 (rows=%+v)", total, rows)
	}
	byTenant := map[int64]AgentRankRow{}
	for _, r := range rows {
		byTenant[r.TenantID] = r
	}

	r9, ok := byTenant[9]
	if !ok {
		t.Fatalf("tenant 9 missing from ranking rows=%+v", rows)
	}
	if r9.TotalEarnedCNY != 40.5 {
		t.Fatalf("tenant 9 TotalEarnedCNY = %v, want 40.5", r9.TotalEarnedCNY)
	}
	if r9.RatioMarkupCNY != 40.5 {
		t.Fatalf("tenant 9 RatioMarkupCNY = %v, want 40.5 (must not be silently dropped)", r9.RatioMarkupCNY)
	}
	if r9.ConsumeCommissionCNY != 0 {
		t.Fatalf("tenant 9 ConsumeCommissionCNY = %v, want 0", r9.ConsumeCommissionCNY)
	}

	r5, ok := byTenant[5]
	if !ok {
		t.Fatalf("tenant 5 missing from ranking rows=%+v", rows)
	}
	if r5.ConsumeCommissionCNY != 10 {
		t.Fatalf("tenant 5 ConsumeCommissionCNY = %v, want 10", r5.ConsumeCommissionCNY)
	}
	if r5.RatioMarkupCNY != 0 {
		t.Fatalf("tenant 5 RatioMarkupCNY = %v, want 0", r5.RatioMarkupCNY)
	}
}

// TestTrendEarnings_IncludesRatioMarkup 是回归测试（TrendEarnings 用 bucketedSumDatetime 对
// agent_earning_logs.amount 做无来源过滤的跨来源求和，本就应含 ratio_markup）。
func TestTrendEarnings_IncludesRatioMarkup(t *testing.T) {
	db := newFinanceTestDB(t)
	repo := New(db)
	ctx := context.Background()
	base := time.Unix(1_700_000_000, 0).UTC()
	seedEarning(t, db, 9, "ratio_markup", 40.5, base)
	seedEarning(t, db, 9, "consume_commission", 5, base.Add(time.Minute))

	start := base.Add(-time.Hour).Unix()
	end := base.Add(time.Hour).Unix()
	pts, err := repo.TrendEarnings(ctx, nil, start, end, "day")
	if err != nil {
		t.Fatalf("TrendEarnings: %v", err)
	}
	if len(pts) != 1 {
		t.Fatalf("buckets = %d, want 1 (pts=%+v)", len(pts), pts)
	}
	if pts[0].AmountCNY != 45.5 {
		t.Fatalf("bucket amount = %v, want 45.5 (40.5 ratio_markup + 5 consume_commission)", pts[0].AmountCNY)
	}
}

// TestTrendEarnings_SplitsWithdrawableBuckets 锁定代理「我的收益」3 线趋势图的数据源：TrendEarnings
// 除跨来源合计 AmountCNY 外，还要把 tokenplan_spread 与 (ratio_markup+consume_commission) 按天拆成
// 各自独立的子序列（字段口径镜像 agentFinanceOverviewOut），且互不污染、也不从 AmountCNY 里消失。
// day 1 混合四种来源（含一个两条子序列都不认领的 manual_adjustment），day 2 只有 tokenplan_spread，
// 覆盖「某天只有一条子序列非零、另一条应为 0 而非用上一天的值」的桶对齐场景。
func TestTrendEarnings_SplitsWithdrawableBuckets(t *testing.T) {
	db := newFinanceTestDB(t)
	repo := New(db)
	ctx := context.Background()
	day1 := time.Unix(1_700_000_000, 0).UTC()
	day2 := day1.Add(24 * time.Hour)

	seedEarning(t, db, 9, "tokenplan_spread", 12.5, day1)
	seedEarning(t, db, 9, "ratio_markup", 7, day1)
	seedEarning(t, db, 9, "consume_commission", 3, day1)
	seedEarning(t, db, 9, "manual_adjustment", 100, day1) // must NOT leak into either withdrawable bucket
	seedEarning(t, db, 9, "tokenplan_spread", 20, day2)

	start := day1.Add(-time.Hour).Unix()
	end := day2.Add(time.Hour).Unix()
	pts, err := repo.TrendEarnings(ctx, nil, start, end, "day")
	if err != nil {
		t.Fatalf("TrendEarnings: %v", err)
	}
	if len(pts) != 2 {
		t.Fatalf("buckets = %d, want 2 (pts=%+v)", len(pts), pts)
	}

	byBucket := map[string]EarningsTrendPoint{}
	for _, p := range pts {
		byBucket[p.Bucket] = p
	}
	day1Label := day1.Format("2006-01-02")
	day2Label := day2.Format("2006-01-02")
	d1, ok := byBucket[day1Label]
	if !ok {
		t.Fatalf("day1 bucket %q missing (pts=%+v)", day1Label, pts)
	}
	d2, ok := byBucket[day2Label]
	if !ok {
		t.Fatalf("day2 bucket %q missing (pts=%+v)", day2Label, pts)
	}

	if d1.AmountCNY != 122.5 {
		t.Fatalf("day1 AmountCNY = %v, want 122.5 (12.5+7+3+100)", d1.AmountCNY)
	}
	if d1.TokenplanWithdrawableCNY != 12.5 {
		t.Fatalf("day1 TokenplanWithdrawableCNY = %v, want 12.5", d1.TokenplanWithdrawableCNY)
	}
	if d1.ConsumptionWithdrawableCNY != 10 {
		t.Fatalf("day1 ConsumptionWithdrawableCNY = %v, want 10 (7 ratio_markup + 3 consume_commission)", d1.ConsumptionWithdrawableCNY)
	}

	if d2.AmountCNY != 20 {
		t.Fatalf("day2 AmountCNY = %v, want 20", d2.AmountCNY)
	}
	if d2.TokenplanWithdrawableCNY != 20 {
		t.Fatalf("day2 TokenplanWithdrawableCNY = %v, want 20", d2.TokenplanWithdrawableCNY)
	}
	if d2.ConsumptionWithdrawableCNY != 0 {
		t.Fatalf("day2 ConsumptionWithdrawableCNY = %v, want 0 (no ratio_markup/consume_commission that day)", d2.ConsumptionWithdrawableCNY)
	}
}
