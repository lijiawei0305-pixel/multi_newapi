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
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/payment"
	"github.com/QuantumNous/new-api/internal/platform/apperr"
	"github.com/QuantumNous/new-api/internal/risk"
	"github.com/QuantumNous/new-api/internal/tokenplan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

type failingSubscriptionPayCreator struct {
	calls  int
	err    error
	cancel context.CancelFunc
}

func (f *failingSubscriptionPayCreator) CreatePay(context.Context, payment.Provider, string, string, float64, string, ...time.Time) (string, error) {
	f.calls++
	if f.cancel != nil {
		f.cancel()
	}
	return "", f.err
}

type observingPurchaseRisk struct {
	tokenplan.RiskEngine
	releaseCalls      int
	releaseContextErr error
}

func (r *observingPurchaseRisk) ReleasePurchaseClaim(ctx context.Context, in tokenplan.PurchaseLimitCheck) error {
	r.releaseCalls++
	r.releaseContextErr = ctx.Err()
	if r.releaseContextErr != nil {
		return r.releaseContextErr
	}
	return r.RiskEngine.ReleasePurchaseClaim(ctx, in)
}

// TestHandlePurchaseReleasesTrialClaimWhenCreatePayFails 通过真实 Gin Router + HandlePurchase 触发
// subscriptionPayURL/CreatePay 失败分支；连续两次相同 Trial 请求都必须到达 CreatePay，而不是第二次被
// PURCHASE_LIMIT_EXCEEDED 拦下。这会在生产补偿被删除、错序或漏调时直接失败。
func TestHandlePurchaseReleasesTrialClaimWhenCreatePayFails(t *testing.T) {
	app := newBuyerTestApp(t)
	ctx := context.Background()
	platformTenant, _, err := app.ensurePlatformTenant(ctx)
	require.NoError(t, err)
	planID, err := app.TokenPlanRepo.EnsurePlan(ctx, &tokenplan.Plan{
		Code: "trial", Name: "Trial", BasePrice: 0, Multiplier: 1,
		MonthLimitUSD: 1, ValidDays: 7, AgentCostPrice: 0, MinPrice: 0, Status: tokenplan.PlanEnabled,
	})
	require.NoError(t, err)
	require.NoError(t, app.TokenPlanRepo.EnsureListing(ctx, platformTenant.ID, planID, true, 0))

	engine := risk.NewEngine(risk.NewMemKVCache(nil))
	adapter := tokenplanRiskAdapter{eng: engine}
	app.Subscriptions = tokenplan.NewSubscriptionService(
		app.TokenPlanRepo, app.TokenPlanRepo,
		newSubPayment(newSubOrderStore(app.DB), app.TokenPlanRepo),
		adapter, noopEarnings{}, nil,
	)
	payErr := apperr.New("PAY_DOWN", "payment gateway unavailable", http.StatusBadGateway)
	creator := &failingSubscriptionPayCreator{err: payErr}
	app.subscriptionPayCreator = creator
	stubProviderConfigured(t, func(p payment.Provider) bool { return p == payment.ProviderWxpay })

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/tenant/token-plans/:id/purchase", func(c *gin.Context) {
		c.Set("id", 3001)
		app.HandlePurchase(c)
	})
	for attempt := 1; attempt <= 2; attempt++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost,
			"/api/tenant/token-plans/"+strconv.FormatInt(planID, 10)+"/purchase", strings.NewReader(`{}`))
		req.Host = "www.wedreamhub.com"
		req.RemoteAddr = "203.0.113.9:4321"
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(rec, req)
		resp := decodeResp(t, rec)
		assert.False(t, resp.Success)
		assert.Equal(t, "PAY_DOWN", resp.Code, "attempt %d must reach the injected CreatePay failure", attempt)
	}
	assert.Equal(t, 2, creator.calls, "the first Handler failure must release the Trial claim for retry")

	var orders []subscriptionOrderRow
	require.NoError(t, app.DB.Order("created_at ASC").Find(&orders).Error)
	require.Len(t, orders, 2, "each real Handler attempt must retain its auditable local order")
	for _, order := range orders {
		assert.Equal(t, subOrderPayFailed, order.Status,
			"CreatePay failure must not masquerade as an ordinary unpaid pending order")
		assert.Equal(t, string(payment.ProviderWxpay), order.Provider)
	}
	var pendingCount int64
	require.NoError(t, app.DB.Model(&subscriptionOrderRow{}).
		Where("status = ?", subOrderPending).Count(&pendingCount).Error)
	assert.Zero(t, pendingCount)
	var snapshotCount int64
	require.NoError(t, app.DB.Table("pending_subscription_orders").Count(&snapshotCount).Error)
	assert.Equal(t, int64(2), snapshotCount, "each failed provider attempt keeps its paired purchase snapshot")
}

func TestHandlePurchaseCompensationSurvivesClientCancellation(t *testing.T) {
	app := newBuyerTestApp(t)
	ctx := context.Background()
	platformTenant, _, err := app.ensurePlatformTenant(ctx)
	require.NoError(t, err)
	planID, err := app.TokenPlanRepo.EnsurePlan(ctx, &tokenplan.Plan{
		Code: "trial", Name: "Trial", BasePrice: 0, Multiplier: 1,
		MonthLimitUSD: 1, ValidDays: 7, AgentCostPrice: 0, MinPrice: 0, Status: tokenplan.PlanEnabled,
	})
	require.NoError(t, err)
	require.NoError(t, app.TokenPlanRepo.EnsureListing(ctx, platformTenant.ID, planID, true, 0))

	engine := risk.NewEngine(risk.NewMemKVCache(nil))
	observedRisk := &observingPurchaseRisk{RiskEngine: tokenplanRiskAdapter{eng: engine}}
	app.Subscriptions = tokenplan.NewSubscriptionService(
		app.TokenPlanRepo, app.TokenPlanRepo,
		newSubPayment(newSubOrderStore(app.DB), app.TokenPlanRepo),
		observedRisk, noopEarnings{}, nil,
	)
	payErr := apperr.New("PAY_DOWN", "payment gateway unavailable", http.StatusBadGateway)
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	creator := &failingSubscriptionPayCreator{err: payErr, cancel: cancelRequest}
	app.subscriptionPayCreator = creator
	stubProviderConfigured(t, func(p payment.Provider) bool { return p == payment.ProviderWxpay })

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/tenant/token-plans/:id/purchase", func(c *gin.Context) {
		c.Set("id", 3002)
		app.HandlePurchase(c)
	})
	performPurchase := func(requestContext context.Context) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost,
			"/api/tenant/token-plans/"+strconv.FormatInt(planID, 10)+"/purchase", strings.NewReader(`{}`)).WithContext(requestContext)
		request.Host = "www.wedreamhub.com"
		request.RemoteAddr = "203.0.113.10:4321"
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, request)
		return recorder
	}

	first := decodeResp(t, performPurchase(requestCtx))
	assert.False(t, first.Success)
	assert.Equal(t, "PAY_DOWN", first.Code)
	require.ErrorIs(t, requestCtx.Err(), context.Canceled)
	require.Equal(t, 1, observedRisk.releaseCalls)
	require.NoError(t, observedRisk.releaseContextErr, "compensation must detach from the canceled client context")

	creator.cancel = nil
	second := decodeResp(t, performPurchase(context.Background()))
	assert.False(t, second.Success)
	assert.Equal(t, "PAY_DOWN", second.Code, "a live compensation context must release the Trial claim for retry")
	assert.Equal(t, 2, creator.calls)
}

// TestReleasePurchaseClaim_NonTrial_ReleasesOnlyCurrentClaim：非 Trial 失败订单只原子递减本次占用，
// 多次 CreatePay/SavePending 失败补偿不能清掉同用户此前两次成功购买的计数。
func TestReleasePurchaseClaim_NonTrial_ReleasesOnlyCurrentClaim(t *testing.T) {
	ctx := context.Background()
	engine := risk.NewEngine(risk.NewMemKVCache(nil))
	adapter := tokenplanRiskAdapter{eng: engine}
	plan := risk.Plan{ID: 2, Code: "mini", PerUserLimit: 3}
	check := tokenplan.PurchaseLimitCheck{
		TenantID: 7, UserID: 4001, PlanID: plan.ID, PlanCode: plan.Code,
	}

	// 两次已成功购买必须始终保留。
	require.NoError(t, engine.CheckPurchaseLimit(ctx, check.UserID, plan))
	require.NoError(t, engine.CheckPurchaseLimit(ctx, check.UserID, plan))
	// 模拟十次“占到第三个名额后，支付/暂存失败”：每次只归还刚取得的那一次。
	for i := 0; i < 10; i++ {
		require.NoError(t, engine.CheckPurchaseLimit(ctx, check.UserID, plan))
		require.NoError(t, adapter.ReleasePurchaseClaim(ctx, check))
	}

	// 仍只剩一个名额；若补偿误用 DEL 清整键，这里会错误地再放行三次。
	require.NoError(t, engine.CheckPurchaseLimit(ctx, check.UserID, plan))
	assert.ErrorIs(t, engine.CheckPurchaseLimit(ctx, check.UserID, plan), risk.ErrPurchaseLimitExceeded)
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
