package mtwire

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// seedSubOrder 直接落一条 SUB 订单（绕过购买流程），status/updatedAt 可控以测扫描过滤。
func seedSubOrder(t *testing.T, db *gorm.DB, no, status string, updatedAt time.Time) {
	t.Helper()
	if err := db.Create(&subscriptionOrderRow{
		OrderNo: no, TenantID: 1, UserID: 7, AmountCNY: 79, Status: status, UpdatedAt: updatedAt,
	}).Error; err != nil {
		t.Fatalf("seed sub order %s: %v", no, err)
	}
}

// TestReconcileStuckSubscriptions SUB 卡单对账：pending+已付→激活; pending+未付→不动;
// fresh(before 之后)/已 activated → 不扫。查单与激活均打桩（激活链另有专测，此处只验对账派发）。
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
	seedSubOrder(t, db, "SUB-paid", subOrderPending, old)
	seedSubOrder(t, db, "SUB-unpaid", subOrderPending, old)
	seedSubOrder(t, db, "SUB-fresh", subOrderPending, fresh)   // 在途（before 之后）→ 不扫
	seedSubOrder(t, db, "SUB-done", subOrderActivated, old)    // 已激活 → 不扫

	// 桩：查单 —— 仅 SUB-paid 已付。
	origQ := subOrderPaidQuery
	t.Cleanup(func() { subOrderPaidQuery = origQ })
	subOrderPaidQuery = func(_ *App, _ context.Context, orderNo string) (bool, error) {
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
	if res.Scanned != 2 {
		t.Fatalf("scanned=%d, want 2 (fresh + activated excluded)", res.Scanned)
	}
	if len(res.Activated) != 1 || res.Activated[0] != "SUB-paid" {
		t.Fatalf("activated=%v, want [SUB-paid]", res.Activated)
	}
	if len(res.Unpaid) != 1 || res.Unpaid[0] != "SUB-unpaid" {
		t.Fatalf("unpaid=%v, want [SUB-unpaid]", res.Unpaid)
	}
	if len(activated) != 1 || activated[0] != "SUB-paid" {
		t.Fatalf("activate hook called %v, want [SUB-paid] exactly once", activated)
	}
}
