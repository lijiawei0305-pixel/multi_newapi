package mtwire

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/tokenplan"
	tprepo "github.com/QuantumNous/new-api/internal/tokenplan/gormrepo"
	"github.com/QuantumNous/new-api/model"
)

// noopEarnings 是测试用占位收益接收器（不入账）。生产装配已改用 tokenplanEarningAdapter（见 agent.go），
// 本桩仅用于桥接单测里构造不关心分润落账的 SubscriptionService。
type noopEarnings struct{}

func (noopEarnings) AddEarning(_ context.Context, _ tokenplan.EarningEntry) error { return nil }

// newBridgeTestApp 建一个仅含 tokenplan + 桥接所需依赖的最小 App（不经 New，避免耦合 Track 2
// 并发演进中的 recharge 装配）。sqlite(:memory:) 单连接（每连接独立库）。
func newBridgeTestApp(t *testing.T) (*App, *gorm.DB) {
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
	if err := tprepo.AutoMigrate(db); err != nil {
		t.Fatalf("tokenplan migrate: %v", err)
	}
	if err := migrateSubscriptionBridge(db); err != nil {
		t.Fatalf("bridge migrate: %v", err)
	}
	tp := tprepo.New(db)
	app := &App{
		DB:            db,
		TokenPlanRepo: tp,
		Subscriptions: tokenplan.NewSubscriptionService(
			tp, tp, newSubPayment(newSubOrderStore(db)), allowAllRisk{}, noopEarnings{}, nil),
	}
	return app, db
}

func seedPlan(t *testing.T, app *App) int64 {
	t.Helper()
	id, err := app.TokenPlanRepo.EnsurePlan(context.Background(), &tokenplan.Plan{
		Code: "pro", Name: "Pro", BasePrice: 10, AnchorPrice: 20, Multiplier: 1,
		MonthLimitUSD: 20, ValidDays: 30, AgentCostPrice: 5, MinPrice: 8, Status: tokenplan.PlanEnabled,
	})
	if err != nil {
		t.Fatalf("seed plan: %v", err)
	}
	return id
}

// TestActivatePaidTokenplanOrder_Idempotent 核心要求：同 orderNo 调两次，只激活一次。
//   - 原生订阅激活（seam）只触发一次（CAS 守门）；
//   - 我们的 tokenplan 订阅记录只落一条（ActivateFromPayment 按 source_order_id 幂等）；
//   - 订单终态为 activated 且回填了原生 id。
func TestActivatePaidTokenplanOrder_Idempotent(t *testing.T) {
	app, db := newBridgeTestApp(t)
	ctx := context.Background()
	const orderNo = "SUBTEST000000000001"

	planID := seedPlan(t, app)
	if err := app.TokenPlanRepo.SavePendingPurchase(ctx, &tokenplan.PendingPurchase{
		OrderID: orderNo, TenantID: 7, UserID: 100, PlanID: planID,
		RetailPrice: 30, AgentCostPrice: 5, MonthLimitUSD: 20, ValidDays: 30,
	}); err != nil {
		t.Fatalf("save pending: %v", err)
	}
	if err := newSubOrderStore(db).create(ctx, &subscriptionOrderRow{
		OrderNo: orderNo, TenantID: 7, UserID: 100, AmountCNY: 30,
		Status: subOrderPending, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("seed order: %v", err)
	}

	// 替换原生激活 seam 为计数桩（不触原生 model.DB / 原生表）。
	orig := activateNativeSubHook
	t.Cleanup(func() { activateNativeSubHook = orig })
	var nativeCalls int
	activateNativeSubHook = func(_ *App, _ context.Context, _ *gorm.DB, snap *tokenplan.PendingPurchase) (int64, int64, error) {
		nativeCalls++
		if snap.MonthLimitUSD != 20 || snap.ValidDays != 30 {
			t.Errorf("snapshot not passed through: %+v", snap)
		}
		return int64(900 + nativeCalls), 5, nil
	}

	// 调两次（模拟回调重试）。
	if err := app.ActivatePaidTokenplanOrder(ctx, orderNo); err != nil {
		t.Fatalf("activate #1: %v", err)
	}
	if err := app.ActivatePaidTokenplanOrder(ctx, orderNo); err != nil {
		t.Fatalf("activate #2: %v", err)
	}

	if nativeCalls != 1 {
		t.Fatalf("native subscription must be activated exactly once, got %d", nativeCalls)
	}
	var subCount int64
	if err := db.Table("tokenplan_subscriptions").Where("source_order_id = ?", orderNo).Count(&subCount).Error; err != nil {
		t.Fatalf("count subs: %v", err)
	}
	if subCount != 1 {
		t.Fatalf("want exactly 1 tokenplan subscription record, got %d", subCount)
	}
	var ord subscriptionOrderRow
	if err := db.Take(&ord, "order_no = ?", orderNo).Error; err != nil {
		t.Fatalf("reload order: %v", err)
	}
	if ord.Status != subOrderActivated || ord.NativeSubID == 0 || ord.NativePlanID == 0 {
		t.Fatalf("order not finalized: %+v", ord)
	}
}

// TestActivatePaidTokenplanOrder_Errors 覆盖前缀错配与未知订单。
func TestActivatePaidTokenplanOrder_Errors(t *testing.T) {
	app, _ := newBridgeTestApp(t)
	ctx := context.Background()

	if err := app.ActivatePaidTokenplanOrder(ctx, "RCG123"); err == nil {
		t.Fatalf("non-SUB order must be rejected")
	}
	if err := app.ActivatePaidTokenplanOrder(ctx, "SUBunknown"); err == nil {
		t.Fatalf("unknown order must error (no pending snapshot)")
	}
}

// TestSubPaymentCreatesPendingOrder 验证 Purchase 网关产出真实 SUB 待支付订单。
func TestSubPaymentCreatesPendingOrder(t *testing.T) {
	_, db := newBridgeTestApp(t)
	ctx := context.Background()
	pay := newSubPayment(newSubOrderStore(db))

	ticket, err := pay.CreateOrder(ctx, tokenplan.OrderInput{
		TenantID: 7, UserID: 100, Type: tokenplan.OrderTypeSubscription,
		AmountCNY: 30, Reference: "pro", Subject: "Pro",
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if !IsSubscriptionOrderNo(ticket.OrderID) {
		t.Fatalf("order id must carry SUB prefix, got %q", ticket.OrderID)
	}
	var ord subscriptionOrderRow
	if err := db.Take(&ord, "order_no = ?", ticket.OrderID).Error; err != nil {
		t.Fatalf("order not persisted: %v", err)
	}
	if ord.Status != subOrderPending || ord.AmountCNY != 30 || ord.UserID != 100 {
		t.Fatalf("unexpected order row: %+v", ord)
	}
}

func TestUsdToQuota(t *testing.T) {
	if got, want := usdToQuota(20), int64(20*common.QuotaPerUnit); got != want {
		t.Fatalf("usdToQuota(20)=%d want %d", got, want)
	}
	if usdToQuota(0) != 0 || usdToQuota(-5) != 0 {
		t.Fatalf("non-positive limit must map to 0")
	}
}

func TestBuildNativeBridgePlan(t *testing.T) {
	p := buildNativeBridgePlan(42, &tokenplan.PendingPurchase{MonthLimitUSD: 20, ValidDays: 30})
	if p.Id != 42 {
		t.Fatalf("Id=%d want 42 (FK to persisted native plan)", p.Id)
	}
	if p.TotalAmount != usdToQuota(20) {
		t.Fatalf("TotalAmount=%d want %d (month_limit×QuotaPerUnit)", p.TotalAmount, usdToQuota(20))
	}
	if p.DurationUnit != model.SubscriptionDurationDay || p.DurationValue != 30 {
		t.Fatalf("duration=%s/%d want day/30 (valid_days)", p.DurationUnit, p.DurationValue)
	}
	if p.QuotaResetPeriod != model.SubscriptionResetNever {
		t.Fatalf("reset=%s want never (whole-window budget)", p.QuotaResetPeriod)
	}
	if p.AllowWalletOverflow == nil || *p.AllowWalletOverflow {
		t.Fatalf("AllowWalletOverflow must be false (hard cap)")
	}
	if p.UpgradeGroup != "" || p.DowngradeGroup != "" {
		t.Fatalf("must not change user group (x1, no group stacking)")
	}
}

// TestActivatePaidTokenplanOrder_NativeIntegration 跑通真实原生路径（不桩 seam）：
// 真激活后须存在 1 条 active 原生 UserSubscription（额度=month_limit×QuotaPerUnit、周期=valid_days），
// 且 model.HasActiveUserSubscription==true —— 即 /v1（默认 subscription_first）会自动走订阅桶计量。
// 重复调用仍只 1 条原生订阅 + 1 条我们的记录。
func TestActivatePaidTokenplanOrder_NativeIntegration(t *testing.T) {
	// shared-cache 文件内存库：允许多连接，避免 tx 持连时 model.GetDBTimestamp 取连阻塞。
	db, err := gorm.Open(sqlite.Open("file:bridgeitest?mode=memory&cache=shared"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	prevDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = prevDB })

	if err := db.AutoMigrate(&model.UserSubscription{}, &model.SubscriptionPlan{}); err != nil {
		t.Fatalf("native migrate: %v", err)
	}
	if err := tprepo.AutoMigrate(db); err != nil {
		t.Fatalf("tokenplan migrate: %v", err)
	}
	if err := migrateSubscriptionBridge(db); err != nil {
		t.Fatalf("bridge migrate: %v", err)
	}
	tp := tprepo.New(db)
	app := &App{
		DB:            db,
		TokenPlanRepo: tp,
		Subscriptions: tokenplan.NewSubscriptionService(
			tp, tp, newSubPayment(newSubOrderStore(db)), allowAllRisk{}, noopEarnings{}, nil),
	}
	ctx := context.Background()
	const orderNo = "SUBINTEG00000000001"
	const userID = 100

	planID := seedPlan(t, app)
	if err := app.TokenPlanRepo.SavePendingPurchase(ctx, &tokenplan.PendingPurchase{
		OrderID: orderNo, TenantID: 7, UserID: userID, PlanID: planID,
		RetailPrice: 30, AgentCostPrice: 5, MonthLimitUSD: 20, ValidDays: 30,
	}); err != nil {
		t.Fatalf("save pending: %v", err)
	}
	if err := newSubOrderStore(db).create(ctx, &subscriptionOrderRow{
		OrderNo: orderNo, TenantID: 7, UserID: userID, AmountCNY: 30,
		Status: subOrderPending, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("seed order: %v", err)
	}

	for i := 0; i < 2; i++ {
		if err := app.ActivatePaidTokenplanOrder(ctx, orderNo); err != nil {
			t.Fatalf("activate #%d: %v", i+1, err)
		}
	}

	// 原生订阅：恰好 1 条，active，额度/周期正确。
	var natSubs []model.UserSubscription
	if err := db.Where("user_id = ?", userID).Find(&natSubs).Error; err != nil {
		t.Fatalf("load native subs: %v", err)
	}
	if len(natSubs) != 1 {
		t.Fatalf("want exactly 1 native UserSubscription, got %d", len(natSubs))
	}
	ns := natSubs[0]
	if ns.Status != "active" {
		t.Fatalf("native sub status=%q want active", ns.Status)
	}
	if ns.AmountTotal != usdToQuota(20) {
		t.Fatalf("native AmountTotal=%d want %d (month_limit×QuotaPerUnit)", ns.AmountTotal, usdToQuota(20))
	}
	if ns.EndTime <= ns.StartTime {
		t.Fatalf("native sub window invalid: start=%d end=%d", ns.StartTime, ns.EndTime)
	}

	// /v1 路由判据：有 active 订阅 → 自动走订阅桶。
	hasSub, err := model.HasActiveUserSubscription(userID)
	if err != nil {
		t.Fatalf("HasActiveUserSubscription: %v", err)
	}
	if !hasSub {
		t.Fatalf("HasActiveUserSubscription must be true after activation (drives /v1 subscription bucket)")
	}

	// 我们的台账：恰好 1 条；套餐→原生 plan 映射：1 条。
	var ourSubs, planMaps int64
	db.Table("tokenplan_subscriptions").Where("source_order_id = ?", orderNo).Count(&ourSubs)
	db.Table("mt_native_subscription_plans").Count(&planMaps)
	if ourSubs != 1 {
		t.Fatalf("want 1 tokenplan_subscriptions record, got %d", ourSubs)
	}
	if planMaps != 1 {
		t.Fatalf("want 1 native plan mapping, got %d", planMaps)
	}
}

func TestIsSubscriptionOrderNo(t *testing.T) {
	if !IsSubscriptionOrderNo("SUBABC123") {
		t.Fatalf("SUB-prefixed order must be recognized")
	}
	if IsSubscriptionOrderNo("RCGABC123") || IsSubscriptionOrderNo("") {
		t.Fatalf("non-SUB order must not be recognized")
	}
}
