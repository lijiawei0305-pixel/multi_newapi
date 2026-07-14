package mtwire

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/payment"
)

// seedSubOrder 直接落一条 SUB 订单（绕过购买流程），status/settled/updatedAt 可控以测扫描过滤。
// created_at 置为与 updated_at 同（真实废单如此：下单后未再变动）；过期兜底按 created_at 判下单时长。
func seedSubOrder(t *testing.T, db *gorm.DB, no, status string, settled bool, updatedAt time.Time) {
	t.Helper()
	if err := db.Create(&subscriptionOrderRow{
		OrderNo: no, TenantID: 1, UserID: 7, AmountCNY: 79, Status: status, Settled: settled,
		CreatedAt: updatedAt, UpdatedAt: updatedAt,
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
	seedSubOrder(t, db, "SUB-paid", subOrderPending, false, old)        // pending 已付 → 激活
	seedSubOrder(t, db, "SUB-unpaid", subOrderPending, false, old)      // pending 未付 → 不动
	seedSubOrder(t, db, "SUB-fresh", subOrderPending, false, fresh)     // 在途（before 之后）→ 不扫
	seedSubOrder(t, db, "SUB-settled", subOrderActivated, true, old)    // 已激活且已结算 → 不扫
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

// TestReconcileStuckSubscriptions_ExpiryFallback SUB 对账 2h 过期兜底 + 26h maxAge 兜底：
//   - 查无此单(ErrOrderNotExist)+超2h → 过期；未超2h → Failed（不误杀）；
//   - 确认未付+超2h → 过期；未付+未超2h → Unpaid；
//   - 瞬时错误(超时/限流)在 2h~26h 窗口内 → Failed（绝不因瞬时错误误杀）；超26h → 兜底过期（网关不可达也清）；
//   - 已付即便超26h → 仍幂等激活（maxAge 不覆盖已付，绝不漏真实付款）。
func TestReconcileStuckSubscriptions_ExpiryFallback(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := migrateSubscriptionBridge(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	app := &App{DB: db}

	// now=before+reconcileMinAge(≈100300)；expireCutoff=now-2h(≈93100)；maxAgeCutoff=now-26h(≈6700)。
	before := time.Unix(100000, 0)
	recentT := time.Unix(99000, 0)                                                 // <2h：未超时
	midT := time.Unix(50000, 0)                                                    // >2h 且 <26h：过期窗口内、未达 maxAge
	ancientT := time.Unix(0, 0)                                                    // >26h：ancient，兜底强制过期
	seedSubOrder(t, db, "SUB-notexist-mid", subOrderPending, false, midT)          // 查无此单+超2h → 过期
	seedSubOrder(t, db, "SUB-notexist-recent", subOrderPending, false, recentT)    // 查无此单+未超2h → Failed
	seedSubOrder(t, db, "SUB-unpaid-mid", subOrderPending, false, midT)            // 未付+超2h → 过期
	seedSubOrder(t, db, "SUB-unpaid-recent", subOrderPending, false, recentT)      // 未付+未超2h → Unpaid
	seedSubOrder(t, db, "SUB-transient-mid", subOrderPending, false, midT)         // 瞬时错误+窗口内(未达maxAge) → Failed
	seedSubOrder(t, db, "SUB-transient-ancient", subOrderPending, false, ancientT) // 瞬时错误+超26h → 兜底过期
	seedSubOrder(t, db, "SUB-paid-ancient", subOrderPending, false, ancientT)      // 已付+超26h → 仍激活

	origQ := subOrderPaidQuery
	t.Cleanup(func() { subOrderPaidQuery = origQ })
	subOrderPaidQuery = func(_ *App, _ context.Context, orderNo, _ string) (bool, error) {
		switch orderNo {
		case "SUB-notexist-mid", "SUB-notexist-recent":
			return false, fmt.Errorf("wxpay query: %w", payment.ErrOrderNotExist)
		case "SUB-transient-mid", "SUB-transient-ancient":
			return false, errors.New("context deadline exceeded (Client.Timeout exceeded)") // 瞬时错误，非 not-exist
		case "SUB-paid-ancient":
			return true, nil
		default:
			return false, nil // unpaid
		}
	}
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

	// 过期：查无此单+超2h、未付+超2h、瞬时错误但超26h(兜底)。
	if !containsAll(res.Expired, "SUB-notexist-mid", "SUB-unpaid-mid", "SUB-transient-ancient") || len(res.Expired) != 3 {
		t.Fatalf("expired=%v, want [SUB-notexist-mid SUB-unpaid-mid SUB-transient-ancient]", res.Expired)
	}
	// 已过期单落库终态 → 不再被扫。
	for _, no := range []string{"SUB-notexist-mid", "SUB-unpaid-mid", "SUB-transient-ancient"} {
		var row subscriptionOrderRow
		db.First(&row, "order_no = ?", no)
		if row.Status != subOrderExpired {
			t.Fatalf("%s status=%q, want %q", no, row.Status, subOrderExpired)
		}
	}
	// 未超2h的查无此单 + 窗口内(未达maxAge)的瞬时错误 → Failed，绝不误杀。
	if _, ok := res.Failed["SUB-notexist-recent"]; !ok {
		t.Fatalf("SUB-notexist-recent 未超2h不应过期，应 Failed；got Failed=%v", res.Failed)
	}
	if _, ok := res.Failed["SUB-transient-mid"]; !ok {
		t.Fatalf("SUB-transient-mid 窗口内瞬时错误不应过期，应 Failed；got Failed=%v", res.Failed)
	}
	if len(res.Failed) != 2 {
		t.Fatalf("Failed=%v, want 恰 2 条(notexist-recent + transient-mid)", res.Failed)
	}
	// 未付未超2h → Unpaid。
	if len(res.Unpaid) != 1 || res.Unpaid[0] != "SUB-unpaid-recent" {
		t.Fatalf("unpaid=%v, want [SUB-unpaid-recent]", res.Unpaid)
	}
	// 已付即便超26h仍激活（maxAge 不覆盖已付），且不被误置 expired。
	if len(activated) != 1 || activated[0] != "SUB-paid-ancient" {
		t.Fatalf("activated=%v, want [SUB-paid-ancient]", activated)
	}
	var paidRow subscriptionOrderRow
	db.First(&paidRow, "order_no = ?", "SUB-paid-ancient")
	if paidRow.Status == subOrderExpired {
		t.Fatalf("SUB-paid-ancient 被误置 expired")
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
