package gormrepo

// Detail 的 alert_level 过滤 + 分页下推 SQL 的等价性/正确性回归(修性能审计 M8:原实现全表物化后
// 再 Go 过滤+分页,管理端跨租户有 OOM 风险)。核心断言:SQL 下推的 alertLevelWhere 谓词与
// breakage.AlertLevelForPct 在各级别/边界上逐一相符;分页在带过滤时仍正确。

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/internal/breakage"
)

// detailPushdownSpec 是等价性测试的一条订阅种子。
type detailPushdownSpec struct {
	userID     int64
	monthLimit float64
	used       float64
	status     string
}

// expectedLevel 用 Go 侧权威函数算该种子的告警级(种子均为未来到期 → 无惰性过期翻牌,status 即种子值)。
func (s detailPushdownSpec) expectedLevel() breakage.AlertLevel {
	pct := 0.0
	if s.monthLimit > 0 {
		pct = s.used / s.monthLimit * 100
	}
	return breakage.AlertLevelForPct(pct, s.status == "exhausted")
}

// 覆盖各级别与边界:0/79/80(warn 下界)/94/95(critical 下界)/99/100(exhausted 下界)/120% +
// limit=0(除零守卫)+ status=exhausted 但 used 偏低(exhausted 优先)。
var detailPushdownSpecs = []detailPushdownSpec{
	{userID: 1, monthLimit: 100, used: 0, status: "active"},      // none
	{userID: 2, monthLimit: 100, used: 79, status: "active"},     // none
	{userID: 3, monthLimit: 100, used: 80, status: "active"},     // warn(下界)
	{userID: 4, monthLimit: 100, used: 94, status: "active"},     // warn
	{userID: 5, monthLimit: 100, used: 95, status: "active"},     // critical(下界)
	{userID: 6, monthLimit: 100, used: 99, status: "active"},     // critical
	{userID: 7, monthLimit: 100, used: 100, status: "active"},    // exhausted(pct>=100)
	{userID: 8, monthLimit: 100, used: 120, status: "active"},    // exhausted
	{userID: 9, monthLimit: 0, used: 0, status: "active"},        // none(零 limit 守卫)
	{userID: 10, monthLimit: 100, used: 50, status: "exhausted"}, // exhausted(状态优先,pct 仅 50)
}

func seedPushdownSpecs(t *testing.T, repo *Repo) {
	t.Helper()
	db := repo.db
	seedPlan(t, db, 1, "basic")
	for _, s := range detailPushdownSpecs {
		seedSub(t, db, subSpec{
			tenantID: 5, userID: s.userID, planID: 1,
			monthLimit: s.monthLimit, used: s.used, status: s.status,
			expireEpoch: nowSec + 30*86400, // 未来到期,避免惰性过期翻牌干扰 status
		})
	}
}

// TestDetail_AlertLevelSQLPushdownEquivalence:对每个可过滤级别,SQL 下推返回的用户集必须与 Go 侧
// AlertLevelForPct 判定完全一致;无过滤时返回全部;total 恒等于匹配数。
func TestDetail_AlertLevelSQLPushdownEquivalence(t *testing.T) {
	repo := New(newBreakageTestDB(t))
	seedPushdownSpecs(t, repo)
	ctx := context.Background()
	tid := int64(5)

	levels := []breakage.AlertLevel{
		breakage.AlertNone, // "" = 不过滤 → 全部
		breakage.AlertWarn,
		breakage.AlertCritical,
		breakage.AlertExhausted,
	}
	for _, lvl := range levels {
		// Go 侧权威期望集。
		want := map[int64]bool{}
		for _, s := range detailPushdownSpecs {
			if lvl == breakage.AlertNone || s.expectedLevel() == lvl {
				want[s.userID] = true
			}
		}
		// pageSize 足够大,一页取回全部匹配,便于比对集合。
		rows, total, err := repo.Detail(ctx, breakage.Filter{
			TenantID: &tid, AlertLevel: lvl, Page: 1, PageSize: 100,
		}, nowSec)
		if err != nil {
			t.Fatalf("level %q: Detail: %v", lvl, err)
		}
		if int(total) != len(want) {
			t.Fatalf("level %q: total = %d, want %d", lvl, total, len(want))
		}
		got := map[int64]bool{}
		for _, r := range rows {
			got[r.UserID] = true
			// 附带自洽校验:非 AlertNone 时,返回行的派生级必须正是被过滤的级。
			if lvl != breakage.AlertNone && r.AlertLevel != lvl {
				t.Fatalf("level %q: 返回行 user%d 的 AlertLevel=%q(与过滤级不符)", lvl, r.UserID, r.AlertLevel)
			}
		}
		if len(got) != len(want) {
			t.Fatalf("level %q: got %d rows, want %d", lvl, len(got), len(want))
		}
		for uid := range want {
			if !got[uid] {
				t.Fatalf("level %q: 缺 user%d(SQL 谓词与 AlertLevelForPct 不等价)", lvl, uid)
			}
		}
	}
}

// TestDetail_PushdownPaginationWithAlertFilter:带 alert 过滤时,SQL LIMIT/OFFSET 分页仍正确——
// 每页大小对、跨页无重叠无遗漏、total 恒为匹配总数(而非全表数)。
func TestDetail_PushdownPaginationWithAlertFilter(t *testing.T) {
	repo := New(newBreakageTestDB(t))
	seedPushdownSpecs(t, repo)
	ctx := context.Background()
	tid := int64(5)

	// warn 级匹配 user3/user4 共 2 条;pageSize=1 → 两页各 1 条,合并恰为该 2 人、无重叠。
	seen := map[int64]int{}
	var lastTotal int64
	for page := 1; page <= 2; page++ {
		rows, total, err := repo.Detail(ctx, breakage.Filter{
			TenantID: &tid, AlertLevel: breakage.AlertWarn, Page: page, PageSize: 1,
		}, nowSec)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		lastTotal = total
		if len(rows) != 1 {
			t.Fatalf("page %d: len=%d, want 1", page, len(rows))
		}
		seen[rows[0].UserID]++
	}
	if lastTotal != 2 {
		t.Fatalf("warn total = %d, want 2(应为匹配总数,非全表)", lastTotal)
	}
	if len(seen) != 2 || seen[3] != 1 || seen[4] != 1 {
		t.Fatalf("两页合并 = %v, want {3:1,4:1}(跨页无重叠无遗漏)", seen)
	}

	// 第 3 页越界 → 空。
	rows, _, err := repo.Detail(ctx, breakage.Filter{
		TenantID: &tid, AlertLevel: breakage.AlertWarn, Page: 3, PageSize: 1,
	}, nowSec)
	if err != nil {
		t.Fatalf("page 3: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("page3 越界 len=%d, want 0", len(rows))
	}
}
