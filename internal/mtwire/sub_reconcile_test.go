package mtwire

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// seedSubOrder 直接落一条 SUB 订单（绕过购买流程），status/settled/updatedAt 可控以测扫描过滤。
func seedSubOrder(t *testing.T, db *gorm.DB, no, status string, settled bool, updatedAt time.Time) {
	t.Helper()
	if err := db.Create(&subscriptionOrderRow{
		OrderNo: no, TenantID: 1, UserID: 7, AmountCNY: 79, Status: status, Settled: settled, UpdatedAt: updatedAt,
	}).Error; err != nil {
		t.Fatalf("seed sub order %s: %v", no, err)
	}
}

// TestReconcileStuckSubscriptions SUB 卡单对账（含审计 M1 的「已激活未结算」补驱动）：
//   - pending+已付 → 查单确认后激活；pending+未付 → 不动；
//   - activated 且 settled=false → 跳过查单、直接幂等补驱动步骤③（settle）；
//   - fresh(before 之后) / activated 且 settled=true → 不扫。
//
// 查单与激活均打桩（激活链另有专测，此处只验对账派发）。
func TestReconcileStuckSubscriptions(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := migrateSubscriptionBridge(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	app := &App{DB: db}

	old := time.Unix(1000, 0)
	fresh := time.Unix(3000, 0)
	before := time.Unix(2000, 0)
	seedSubOrder(t, db, "SUB-paid", subOrderPending, false, old)      // pending 已付 → 激活
	seedSubOrder(t, db, "SUB-unpaid", subOrderPending, false, old)    // pending 未付 → 不动
	seedSubOrder(t, db, "SUB-fresh", subOrderPending, false, fresh)   // 在途（before 之后）→ 不扫
	seedSubOrder(t, db, "SUB-settled", subOrderActivated, true, old)  // 已激活且已结算 → 不扫
	seedSubOrder(t, db, "SUB-unsettled", subOrderActivated, false, old) // 已激活未结算 → 补驱动步骤③

	// 桩：查单 —— 仅 SUB-paid 已付（SUB-unsettled 走 settle 路径、不查单）。
	origQ := subOrderPaidQuery
	t.Cleanup(func() { subOrderPaidQuery = origQ })
	var queried []string
	subOrderPaidQuery = func(_ *App, _ context.Context, orderNo, _ string) (bool, error) {
		queried = append(queried, orderNo)
		return orderNo == "SUB-paid", nil
	}
	// 桩：激活 —— 计数（不跑真实激活链）。
	origA := activatePaidSubHook
	t.Cleanup(func() { activatePaidSubHook = origA })
	var activated []string
	activatePaidSubHook = func(_ *App, _ context.Context, orderNo string) error {
		activated = append(activated, orderNo)
		return nil
	}

	res, err := app.ReconcileStuckSubscriptions(context.Background(), before)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.Scanned != 3 {
		t.Fatalf("scanned=%d, want 3 (SUB-paid + SUB-unpaid + SUB-unsettled; fresh/settled excluded)", res.Scanned)
	}
	if !containsAll(res.Activated, "SUB-paid", "SUB-unsettled") || len(res.Activated) != 2 {
		t.Fatalf("activated=%v, want [SUB-paid SUB-unsettled]", res.Activated)
	}
	if len(res.Unpaid) != 1 || res.Unpaid[0] != "SUB-unpaid" {
		t.Fatalf("unpaid=%v, want [SUB-unpaid]", res.Unpaid)
	}
	// 已激活未结算单跳过查单（已确认支付）：只查了 pending 单。
	if !containsAll(queried, "SUB-paid", "SUB-unpaid") || len(queried) != 2 {
		t.Fatalf("queried=%v, want only pending [SUB-paid SUB-unpaid] (activated skips query)", queried)
	}
	if !containsAll(activated, "SUB-paid", "SUB-unsettled") || len(activated) != 2 {
		t.Fatalf("activate hook called %v, want [SUB-paid SUB-unsettled]", activated)
	}
}

// containsAll 报告 got 是否包含全部 want 元素（顺序无关，供对账派发断言用）。
func containsAll(got []string, want ...string) bool {
	set := make(map[string]bool, len(got))
	for _, g := range got {
		set[g] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}
