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

// TestReconcileStuckSubscriptions_ExpiryFallback SUB 对账 2h 过期兜底：
//   - 查无此单(ErrOrderNotExist)+超时 → 过期终态；未超时 → Failed（不误杀）；
//   - 确认未付+超时 → 过期；未付+未超时 → Unpaid；
//   - 瞬时错误+超时 → Failed（绝不因超时误杀可重试错误）；
//   - 已付即便超时 → 仍幂等激活（绝不漏真实付款）。
func TestReconcileStuckSubscriptions_ExpiryFallback(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := migrateSubscriptionBridge(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	app := &App{DB: db}

	before := time.Unix(100000, 0)                                              // now=before+reconcileMinAge；expireCutoff=now-2h
	oldT := time.Unix(0, 0)                                                     // 下单远早于 expireCutoff → 已超 2h
	recentT := time.Unix(99000, 0)                                              // 已被扫(updated<before)但下单未超 2h(> expireCutoff)
	seedSubOrder(t, db, "SUB-notexist-old", subOrderPending, false, oldT)       // 查无此单+超时 → 过期
	seedSubOrder(t, db, "SUB-notexist-recent", subOrderPending, false, recentT) // 查无此单+未超时 → Failed
	seedSubOrder(t, db, "SUB-unpaid-old", subOrderPending, false, oldT)         // 未付+超时 → 过期
	seedSubOrder(t, db, "SUB-unpaid-recent", subOrderPending, false, recentT)   // 未付+未超时 → Unpaid
	seedSubOrder(t, db, "SUB-transient-old", subOrderPending, false, oldT)      // 瞬时错误+超时 → Failed
	seedSubOrder(t, db, "SUB-paid-old", subOrderPending, false, oldT)           // 已付+超时 → 仍激活

	origQ := subOrderPaidQuery
	t.Cleanup(func() { subOrderPaidQuery = origQ })
	subOrderPaidQuery = func(_ *App, _ context.Context, orderNo, _ string) (bool, error) {
		switch orderNo {
		case "SUB-notexist-old", "SUB-notexist-recent":
			return false, fmt.Errorf("wxpay query: %w", payment.ErrOrderNotExist)
		case "SUB-transient-old":
			return false, errors.New("dial tcp: i/o timeout") // 瞬时错误，非 not-exist
		case "SUB-paid-old":
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

	// 过期：查无此单+超时、确认未付+超时。
	if !containsAll(res.Expired, "SUB-notexist-old", "SUB-unpaid-old") || len(res.Expired) != 2 {
		t.Fatalf("expired=%v, want [SUB-notexist-old SUB-unpaid-old]", res.Expired)
	}
	// 已过期单落库终态 → 不再被扫。
	for _, no := range []string{"SUB-notexist-old", "SUB-unpaid-old"} {
		var row subscriptionOrderRow
		db.First(&row, "order_no = ?", no)
		if row.Status != subOrderExpired {
			t.Fatalf("%s status=%q, want %q", no, row.Status, subOrderExpired)
		}
	}
	// 未超时的查无此单 + 瞬时错误(即便超时) → Failed，绝不误杀。
	if _, ok := res.Failed["SUB-notexist-recent"]; !ok {
		t.Fatalf("SUB-notexist-recent 未超时不应过期，应 Failed；got Failed=%v", res.Failed)
	}
	if _, ok := res.Failed["SUB-transient-old"]; !ok {
		t.Fatalf("SUB-transient-old 瞬时错误绝不过期，应 Failed；got Failed=%v", res.Failed)
	}
	if len(res.Failed) != 2 {
		t.Fatalf("Failed=%v, want 恰 2 条(notexist-recent + transient-old)", res.Failed)
	}
	// 未付未超时 → Unpaid。
	if len(res.Unpaid) != 1 || res.Unpaid[0] != "SUB-unpaid-recent" {
		t.Fatalf("unpaid=%v, want [SUB-unpaid-recent]", res.Unpaid)
	}
	// 已付即便超时仍激活（不漏真实付款），且不被误置 expired。
	if len(activated) != 1 || activated[0] != "SUB-paid-old" {
		t.Fatalf("activated=%v, want [SUB-paid-old]", activated)
	}
	var paidRow subscriptionOrderRow
	db.First(&paidRow, "order_no = ?", "SUB-paid-old")
	if paidRow.Status == subOrderExpired {
		t.Fatalf("SUB-paid-old 被误置 expired")
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
