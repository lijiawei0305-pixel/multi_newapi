package mtwire

// audit F2 回归：Trial 限购占键（checkTrialLimit 的 SetNX，生产 TTL=0 = 永不过期的终身键）与后续
// 下单/暂存/支付凭据创建跨 Redis/MySQL/支付网关三层、非原子。任一后续步骤失败若不归还占键，用户
// 没付款、订单没成，终身 Trial 资格却被永久烧掉，只能走客服。
//
// 本文件用**真实 risk 引擎**（risk.NewEngine + MemKVCache，经 tokenplanRiskAdapter 桥接进真实
// tokenplan.SubscriptionService）验证「补偿后可重新购买」这一外部可观测性质——不 mock 被测逻辑。
// tokenplan 包内只有 fakeRisk（假件），故这类真引擎集成测试落在 mtwire 包（接线同 risk_purchase_test.go）。

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
	"github.com/QuantumNous/new-api/internal/risk"
	"github.com/QuantumNous/new-api/internal/tokenplan"
)

// ---- 测试装配辅助 ----

// seedTrialPlan 直接经 MemRepo 落一个 Trial 套餐（Code="trial"）并为 tenantID 上架，返回 planID。
// enabled=false 落停用态（供「占键前失败」的反向守卫用例）。刻意绕过 Catalog/PricingGuard 校验：
// 本文件只关心限购占键的补偿，不测定价。
func seedTrialPlan(t *testing.T, repo *tokenplan.MemRepo, tenantID int64, enabled bool) int64 {
	t.Helper()
	status := tokenplan.PlanEnabled
	if !enabled {
		status = tokenplan.PlanDisabled
	}
	plan := &tokenplan.Plan{
		Code: "trial", Name: "体验", BasePrice: 0, Multiplier: 1,
		MonthLimitUSD: 1, ValidDays: 7, AgentCostPrice: 0, MinPrice: 0, Status: status,
	}
	if err := repo.CreatePlan(context.Background(), plan); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := repo.UpsertListing(context.Background(), &tokenplan.TenantPlan{
		TenantID: tenantID, PlanID: plan.ID, Enabled: true, RetailPrice: 0,
	}); err != nil {
		t.Fatalf("UpsertListing: %v", err)
	}
	return plan.ID
}

// failingCreateOrder：CreateOrder 恒失败的 PaymentGateway（触发 Purchase 在占键之后、下单这一步失败）。
type failingCreateOrder struct{ err error }

func (p failingCreateOrder) CreateOrder(_ context.Context, _ tokenplan.OrderInput) (*tokenplan.PayOrder, error) {
	return nil, p.err
}

// okCreateOrder：CreateOrder 恒成功（供 SavePendingPurchase 失败与正常占键用例）。
type okCreateOrder struct{}

func (okCreateOrder) CreateOrder(_ context.Context, _ tokenplan.OrderInput) (*tokenplan.PayOrder, error) {
	return &tokenplan.PayOrder{OrderID: "ord-x", PayURL: "u"}, nil
}

// savePendingFailRepo 包装真实 SubscriptionRepo，只让 SavePendingPurchase 失败（下单成功、暂存失败，
// 触发 Purchase 在占键之后、暂存这一步失败）。
type savePendingFailRepo struct {
	tokenplan.SubscriptionRepo
	err error
}

func (r savePendingFailRepo) SavePendingPurchase(_ context.Context, _ *tokenplan.PendingPurchase) error {
	return r.err
}

// ---- Level A：Purchase 内部失败点（CreateOrder / SavePendingPurchase）须归还占键 ----

// TestPurchase_TrialClaimReleased_OnCreateOrderFailure：真实引擎下，Trial 占键成功后 CreateOrder 失败，
// 补偿须归还占键——否则第二次购买被终身键永久拒（未修代码此处先红）。
func TestPurchase_TrialClaimReleased_OnCreateOrderFailure(t *testing.T) {
	ctx := context.Background()
	engine := risk.NewEngine(risk.NewMemKVCache(nil))
	adapter := tokenplanRiskAdapter{eng: engine}
	repo := tokenplan.NewMemRepo()
	planID := seedTrialPlan(t, repo, 7, true)

	payErr := apperr.New("PAY_DOWN", "下单失败", 502)
	svc := tokenplan.NewSubscriptionService(repo, repo, failingCreateOrder{err: payErr}, adapter, noopEarnings{}, nil)

	in := tokenplan.PurchaseInput{TenantID: 7, UserID: 1001, PlanID: planID, DeviceID: "dev-A"}
	if _, err := svc.Purchase(ctx, in); apperr.CodeOf(err) != "PAY_DOWN" {
		t.Fatalf("CreateOrder 失败须上浮 PAY_DOWN，得 %v", err)
	}

	// 外部可观测性质：同一 userID+device 再次通过限购应放行（占键已归还）。
	reclaim := tokenplan.PurchaseLimitCheck{TenantID: 7, UserID: 1001, PlanID: planID, PlanCode: "trial", DeviceID: "dev-A"}
	if err := adapter.CheckPurchaseLimit(ctx, reclaim); err != nil {
		t.Fatalf("CreateOrder 失败后占键须归还、可重新购买，却被拒: %v", err)
	}
}

// TestPurchase_TrialClaimReleased_OnSavePendingFailure：占键成功、CreateOrder 成功、SavePendingPurchase
// 失败，补偿同样须归还占键（覆盖第二个泄漏点）。
func TestPurchase_TrialClaimReleased_OnSavePendingFailure(t *testing.T) {
	ctx := context.Background()
	engine := risk.NewEngine(risk.NewMemKVCache(nil))
	adapter := tokenplanRiskAdapter{eng: engine}
	repo := tokenplan.NewMemRepo()
	planID := seedTrialPlan(t, repo, 7, true)

	saveErr := apperr.New("SAVE_DOWN", "暂存失败", 500)
	subs := savePendingFailRepo{SubscriptionRepo: repo, err: saveErr}
	svc := tokenplan.NewSubscriptionService(subs, repo, okCreateOrder{}, adapter, noopEarnings{}, nil)

	in := tokenplan.PurchaseInput{TenantID: 7, UserID: 1002, PlanID: planID, DeviceID: "dev-B"}
	if _, err := svc.Purchase(ctx, in); apperr.CodeOf(err) != "SAVE_DOWN" {
		t.Fatalf("SavePendingPurchase 失败须上浮 SAVE_DOWN，得 %v", err)
	}

	reclaim := tokenplan.PurchaseLimitCheck{TenantID: 7, UserID: 1002, PlanID: planID, PlanCode: "trial", DeviceID: "dev-B"}
	if err := adapter.CheckPurchaseLimit(ctx, reclaim); err != nil {
		t.Fatalf("SavePendingPurchase 失败后占键须归还、可重新购买，却被拒: %v", err)
	}
}

// ---- Level A 反向守卫：占键**之前**失败绝不能误删用户既有的合法 Trial 键 ----

// TestPurchase_PreClaimFailure_KeepsExistingClaim：Purchase 在 CheckPurchaseLimit 之前失败
// （PLAN_DISABLED），若用户已有既有 Trial 键（上次真买的），补偿绝不能触发、既有键须存活。
// 这守卫「见错就释放」的错误修法——那会让用户重复领取 Trial。
func TestPurchase_PreClaimFailure_KeepsExistingClaim(t *testing.T) {
	ctx := context.Background()
	engine := risk.NewEngine(risk.NewMemKVCache(nil))
	adapter := tokenplanRiskAdapter{eng: engine}
	repo := tokenplan.NewMemRepo()
	planID := seedTrialPlan(t, repo, 7, false) // 停用套餐 → Purchase 在占键前即 PLAN_DISABLED

	// 用户 2001 先合法占用 Trial（模拟上次真买过；占键与套餐存在与否无关，直接经引擎写键）。
	existing := tokenplan.PurchaseLimitCheck{TenantID: 7, UserID: 2001, PlanID: planID, PlanCode: "trial", DeviceID: "dev-X"}
	if err := adapter.CheckPurchaseLimit(ctx, existing); err != nil {
		t.Fatalf("既有合法占键应先放行: %v", err)
	}

	// 停用套餐下，2001 再走完整 Purchase → 在占键之前即 PLAN_DISABLED 失败。
	svc := tokenplan.NewSubscriptionService(repo, repo, okCreateOrder{}, adapter, noopEarnings{}, nil)
	in := tokenplan.PurchaseInput{TenantID: 7, UserID: 2001, PlanID: planID, DeviceID: "dev-X"}
	if _, err := svc.Purchase(ctx, in); apperr.CodeOf(err) != tokenplan.CodePlanDisabled {
		t.Fatalf("停用套餐应 PLAN_DISABLED，得 %v", err)
	}

	// 守卫①：2001 自身 userK 仍在（补偿未触发）——再 check 仍被拒。
	if err := adapter.CheckPurchaseLimit(ctx, existing); apperr.CodeOf(err) != tokenplan.CodePurchaseLimitExceeded {
		t.Fatalf("既有 user 键被误删：占键前失败不该触发归还，得 %v", err)
	}
	// 守卫②：跨用户共享的 device 键仍归 2001——无关新账号 2002 从同设备领取仍应被拒。
	probe := tokenplan.PurchaseLimitCheck{TenantID: 7, UserID: 2002, PlanID: planID, PlanCode: "trial", DeviceID: "dev-X"}
	if err := adapter.CheckPurchaseLimit(ctx, probe); apperr.CodeOf(err) != tokenplan.CodePurchaseLimitExceeded {
		t.Fatalf("既有 device 键被误删：新账号同设备本应被拒，得 %v", err)
	}
}

// ---- Level B：CreatePay 失败点（Purchase 返回之后）由 HandlePurchase 显式归还 ----

// TestReleasePurchaseClaim_TrialReclaimable：模拟 HandlePurchase 在 subscriptionPayURL→CreatePay 失败后
// 调用 SubscriptionService.ReleasePurchaseClaim 归还占键（该失败点在 Purchase 返回之后，函数内 defer
// 触不到，故必须由上层显式补偿，audit F2 旗舰场景）。
//
// 注：不在此处装配「CreatePay 失败」的完整 HTTP 路径——providerMgr 为具体类型 *providerManager，
// 需真实支付 SDK 方能令 CreatePay 返错，成本过高（见 http.go subscriptionPayURL）。本用例直接断言
// ReleasePurchaseClaim 这一补偿原语（delegate→adapter→ReleaseTrialLimit）端到端可令占键重新可用，
// 即 HandlePurchase Level B 分支所依赖的机制；HTTP 落点见 http.go HandlePurchase 的归还片段。
func TestReleasePurchaseClaim_TrialReclaimable(t *testing.T) {
	ctx := context.Background()
	engine := risk.NewEngine(risk.NewMemKVCache(nil))
	adapter := tokenplanRiskAdapter{eng: engine}
	repo := tokenplan.NewMemRepo()
	planID := seedTrialPlan(t, repo, 7, true)
	svc := tokenplan.NewSubscriptionService(repo, repo, okCreateOrder{}, adapter, noopEarnings{}, nil)

	in := tokenplan.PurchaseInput{TenantID: 7, UserID: 3001, PlanID: planID, DeviceID: "dev-C"}
	ticket, err := svc.Purchase(ctx, in)
	if err != nil {
		t.Fatalf("首次购买应成功（占键 + 下单 + 暂存均成功）: %v", err)
	}
	// PlanCode 须随 ticket 透传（补偿判定 Trial 档所需，design C）。
	if ticket.PlanCode != "trial" {
		t.Fatalf("ticket.PlanCode 应为 trial（供补偿判定档位），得 %q", ticket.PlanCode)
	}
	// 二次购买应被限购拦截（键已占）。
	if _, err := svc.Purchase(ctx, in); apperr.CodeOf(err) != tokenplan.CodePurchaseLimitExceeded {
		t.Fatalf("二次购买应被限购拦截，得 %v", err)
	}

	// 模拟 CreatePay 失败后 HandlePurchase 的归还（入参取自 ticket，同 HandlePurchase）。
	if err := svc.ReleasePurchaseClaim(ctx, tokenplan.PurchaseLimitCheck{
		TenantID: 7, UserID: 3001, PlanID: ticket.PlanID, PlanCode: ticket.PlanCode, DeviceID: "dev-C",
	}); err != nil {
		t.Fatalf("ReleasePurchaseClaim: %v", err)
	}
	// 归还后可重新购买。
	if _, err := svc.Purchase(ctx, in); err != nil {
		t.Fatalf("归还占键后应能重新购买，却被拒: %v", err)
	}
}

// TestReleasePurchaseClaim_NonTrial_NoOp：非 Trial 档归还是 no-op（不引入过度释放）。
// 非 Trial 用 Incr 计数键，若用 Del 整键清零会在 PerUserLimit≥2 且已有合法计数时过度释放；
// 正确做法是原子递减原语，列为后续。本用例锁定「非 Trial 归还不动任何键」的现状承诺。
func TestReleasePurchaseClaim_NonTrial_NoOp(t *testing.T) {
	ctx := context.Background()
	engine := risk.NewEngine(risk.NewMemKVCache(nil))
	adapter := tokenplanRiskAdapter{eng: engine}

	// 归还非 Trial 档：应无错、且不触碰任何键（此处仅断言不报错——引擎侧非 Trial 无终身键语义）。
	err := adapter.ReleasePurchaseClaim(ctx, tokenplan.PurchaseLimitCheck{
		TenantID: 7, UserID: 4001, PlanID: 2, PlanCode: "mini",
	})
	if err != nil {
		t.Fatalf("非 Trial 归还应为 no-op、不报错，得 %v", err)
	}
}

// TestReleasePurchaseClaim_NilEngine_NoOp：无风控引擎（Redis 关）时归还优雅 no-op，不 panic。
func TestReleasePurchaseClaim_NilEngine_NoOp(t *testing.T) {
	a := tokenplanRiskAdapter{eng: nil}
	if err := a.ReleasePurchaseClaim(context.Background(), tokenplan.PurchaseLimitCheck{
		UserID: 5001, PlanCode: "trial", DeviceID: "dev-Z",
	}); err != nil {
		t.Fatalf("nil 引擎归还应 no-op，得 %v", err)
	}
}
