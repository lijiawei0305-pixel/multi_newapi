package tokenplan

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

// epoch 是测试基准时刻。
var epoch = time.Date(2026, 6, 28, 0, 0, 0, 0, time.UTC)

// newSubService 组装订阅服务及其依赖（空 repo + 假时钟）。
func newSubService(clock Clock) (SubscriptionService, *MemRepo, *fakePayment, *fakeRisk, *fakeEarnings) {
	repo := NewMemRepo()
	pay := newFakePayment()
	risk := &fakeRisk{}
	earn := newFakeEarnings()
	svc := NewSubscriptionService(repo, repo, pay, risk, earn, clock)
	return svc, repo, pay, risk, earn
}

// activeSub 直接经 repo 落一个 active 订阅（绕过购买流程，供计量/桶用例）。
func activeSub(repo *MemRepo, clock Clock, userID, tenantID int64, monthLimit float64, validDays int) *Subscription {
	now := clock.Now()
	sub := &Subscription{
		TenantID:      tenantID,
		UserID:        userID,
		PlanID:        1,
		MonthLimitUSD: monthLimit,
		Status:        SubActive,
		StartAt:       now,
		ExpireAt:      now.AddDate(0, 0, validDays),
		SourceOrderID: "seed-" + itoa(int(userID)),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	_, _ = repo.ActivateFromOrder(context.Background(), sub)
	return sub
}

// ---- Purchase ----

func TestPurchaseSuccess(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock(epoch)
	svc, repo, pay, risk, _ := newSubService(clock)
	plan := seedPlanInto(repo, basePlanInput())
	_ = NewRetailService(repo, &fakeGuard{}).SetListing(ctx, 7, plan.ID, true, 250)

	ticket, err := svc.Purchase(ctx, PurchaseInput{TenantID: 7, UserID: 11, PlanID: plan.ID, DeviceID: "dev1"})
	if err != nil {
		t.Fatalf("Purchase: %v", err)
	}
	if ticket.OrderID == "" || ticket.AmountCNY != 250 {
		t.Fatalf("ticket wrong: %+v", ticket)
	}
	if risk.calls != 1 || risk.last.DeviceID != "dev1" {
		t.Fatalf("risk not consulted with device: %+v", risk)
	}
	// 下单金额=代理零售价，类型=subscription。
	o, _ := pay.lastOrder()
	if o.Type != OrderTypeSubscription || o.AmountCNY != 250 {
		t.Fatalf("order wrong: %+v", o)
	}
	// 暂存购买意图，供激活还原。
	pp, err := repo.GetPendingPurchase(ctx, ticket.OrderID)
	if err != nil || pp.AgentCostPrice != plan.AgentCostPrice || pp.MonthLimitUSD != plan.MonthLimitUSD {
		t.Fatalf("pending purchase wrong: %+v err=%v", pp, err)
	}
}

func TestPurchasePlanNotListed(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, _, _ := newSubService(newFakeClock(epoch))
	plan := seedPlanInto(repo, basePlanInput()) // 存在但未上架
	if _, err := svc.Purchase(ctx, PurchaseInput{TenantID: 7, UserID: 11, PlanID: plan.ID}); apperr.CodeOf(err) != CodePlanNotListed {
		t.Fatalf("want PLAN_NOT_LISTED, got %v", err)
	}
}

func TestPurchaseListingDisabled(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, _, _ := newSubService(newFakeClock(epoch))
	plan := seedPlanInto(repo, basePlanInput())
	_ = NewRetailService(repo, &fakeGuard{}).SetListing(ctx, 7, plan.ID, false, 250) // 退出
	if _, err := svc.Purchase(ctx, PurchaseInput{TenantID: 7, UserID: 11, PlanID: plan.ID}); apperr.CodeOf(err) != CodePlanNotListed {
		t.Fatalf("withdrawn listing want PLAN_NOT_LISTED, got %v", err)
	}
}

func TestPurchasePlanDisabled(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, _, _ := newSubService(newFakeClock(epoch))
	in := basePlanInput()
	in.Status = PlanDisabled
	plan := seedPlanInto(repo, in)
	if _, err := svc.Purchase(ctx, PurchaseInput{TenantID: 7, UserID: 11, PlanID: plan.ID}); apperr.CodeOf(err) != CodePlanDisabled {
		t.Fatalf("want PLAN_DISABLED, got %v", err)
	}
}

func TestPurchasePlanNotFound(t *testing.T) {
	svc, _, _, _, _ := newSubService(newFakeClock(epoch))
	if _, err := svc.Purchase(context.Background(), PurchaseInput{TenantID: 7, UserID: 11, PlanID: 999}); apperr.CodeOf(err) != CodePlanNotFound {
		t.Fatalf("want PLAN_NOT_FOUND, got %v", err)
	}
}

func TestPurchaseLimitExceeded(t *testing.T) {
	ctx := context.Background()
	svc, repo, pay, risk, _ := newSubService(newFakeClock(epoch))
	risk.deny = true
	plan := seedPlanInto(repo, basePlanInput())
	_ = NewRetailService(repo, &fakeGuard{}).SetListing(ctx, 7, plan.ID, true, 250)

	if _, err := svc.Purchase(ctx, PurchaseInput{TenantID: 7, UserID: 11, PlanID: plan.ID}); apperr.CodeOf(err) != CodePurchaseLimitExceeded {
		t.Fatalf("want PURCHASE_LIMIT_EXCEEDED, got %v", err)
	}
	// 限购拦截后不得下单。
	if _, ok := pay.lastOrder(); ok {
		t.Fatal("must not create order when purchase limit exceeded")
	}
}

func TestPurchasePaymentError(t *testing.T) {
	ctx := context.Background()
	svc, repo, pay, _, _ := newSubService(newFakeClock(epoch))
	pay.err = apperr.New("PAY_DOWN", "x", 502)
	plan := seedPlanInto(repo, basePlanInput())
	_ = NewRetailService(repo, &fakeGuard{}).SetListing(ctx, 7, plan.ID, true, 250)
	if _, err := svc.Purchase(ctx, PurchaseInput{TenantID: 7, UserID: 11, PlanID: plan.ID}); apperr.CodeOf(err) != "PAY_DOWN" {
		t.Fatalf("payment error must propagate, got %v", err)
	}
}

// ---- 占键补偿（audit F2）：defer 只在"占键之后、成功之前"失败时归还，且入参与占键对称 ----

// TestPurchaseReleasesClaimOnPaymentError：占键成功后 CreateOrder 失败 → 归还占键恰一次（否则 Trial
// 终身键永久泄漏）。归还入参必须与占键入参对称（同 userID/plan/device），供归属校验删对键。
func TestPurchaseReleasesClaimOnPaymentError(t *testing.T) {
	ctx := context.Background()
	svc, repo, pay, risk, _ := newSubService(newFakeClock(epoch))
	pay.err = apperr.New("PAY_DOWN", "x", 502)
	plan := seedPlanInto(repo, basePlanInput())
	_ = NewRetailService(repo, &fakeGuard{}).SetListing(ctx, 7, plan.ID, true, 250)

	if _, err := svc.Purchase(ctx, PurchaseInput{TenantID: 7, UserID: 11, PlanID: plan.ID, DeviceID: "d1"}); apperr.CodeOf(err) != "PAY_DOWN" {
		t.Fatalf("want PAY_DOWN, got %v", err)
	}
	if risk.releaseCalls != 1 {
		t.Fatalf("CreateOrder 失败须归还占键恰 1 次，got %d", risk.releaseCalls)
	}
	if got := risk.released[0]; got.UserID != 11 || got.PlanID != plan.ID || got.DeviceID != "d1" || got.TenantID != 7 {
		t.Fatalf("归还入参与占键不对称: %+v", got)
	}
}

// TestPurchaseSuccessKeepsClaim：成功购买绝不归还占键（committed=true → defer no-op）。
func TestPurchaseSuccessKeepsClaim(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, risk, _ := newSubService(newFakeClock(epoch))
	plan := seedPlanInto(repo, basePlanInput())
	_ = NewRetailService(repo, &fakeGuard{}).SetListing(ctx, 7, plan.ID, true, 250)

	if _, err := svc.Purchase(ctx, PurchaseInput{TenantID: 7, UserID: 11, PlanID: plan.ID, DeviceID: "d1"}); err != nil {
		t.Fatalf("Purchase: %v", err)
	}
	if risk.releaseCalls != 0 {
		t.Fatalf("成功购买不得归还占键，got %d", risk.releaseCalls)
	}
}

// TestPurchasePreClaimFailureNoRelease：占键**之前**失败（PLAN_DISABLED）绝不归还——否则会误删用户
// 既有的合法 Trial 键（上次真买的），令其重复领取。defer 注册在占键成功之后，故此路径 0 归还。
func TestPurchasePreClaimFailureNoRelease(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, risk, _ := newSubService(newFakeClock(epoch))
	in := basePlanInput()
	in.Status = PlanDisabled
	plan := seedPlanInto(repo, in)

	if _, err := svc.Purchase(ctx, PurchaseInput{TenantID: 7, UserID: 11, PlanID: plan.ID, DeviceID: "d1"}); apperr.CodeOf(err) != CodePlanDisabled {
		t.Fatalf("want PLAN_DISABLED, got %v", err)
	}
	if risk.releaseCalls != 0 {
		t.Fatalf("占键前失败不得归还（防误删既有合法键），got %d", risk.releaseCalls)
	}
}

// TestPurchaseDeniedClaimNoRelease：限购拦截（CheckPurchaseLimit 返错、本次未占到键）绝不归还——
// 否则会归还他人/赢家已持有的键。
func TestPurchaseDeniedClaimNoRelease(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, risk, _ := newSubService(newFakeClock(epoch))
	risk.deny = true
	plan := seedPlanInto(repo, basePlanInput())
	_ = NewRetailService(repo, &fakeGuard{}).SetListing(ctx, 7, plan.ID, true, 250)

	if _, err := svc.Purchase(ctx, PurchaseInput{TenantID: 7, UserID: 11, PlanID: plan.ID, DeviceID: "d1"}); apperr.CodeOf(err) != CodePurchaseLimitExceeded {
		t.Fatalf("want PURCHASE_LIMIT_EXCEEDED, got %v", err)
	}
	if risk.releaseCalls != 0 {
		t.Fatalf("限购拦截（未占到键）不得归还，got %d", risk.releaseCalls)
	}
}

// ---- ActivateFromPayment ----

// purchaseAndActivate 跑一遍购买，返回订单号，供激活用例复用。
func purchaseAndActivate(t *testing.T, ctx context.Context, svc SubscriptionService, repo *MemRepo, retail float64) string {
	t.Helper()
	plan := seedPlanInto(repo, basePlanInput()) // AgentCostPrice=200
	_ = NewRetailService(repo, &fakeGuard{}).SetListing(ctx, 7, plan.ID, true, retail)
	ticket, err := svc.Purchase(ctx, PurchaseInput{TenantID: 7, UserID: 11, PlanID: plan.ID})
	if err != nil {
		t.Fatalf("Purchase: %v", err)
	}
	return ticket.OrderID
}

func TestActivateFromPaymentSuccess(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock(epoch)
	svc, repo, _, _, earn := newSubService(clock)
	order := purchaseAndActivate(t, ctx, svc, repo, 279) // spread = 279-200 = 79

	sub, err := svc.ActivateFromPayment(ctx, order)
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if sub.Status != SubActive || sub.UsedUSD != 0 {
		t.Fatalf("activated sub wrong: %+v", sub)
	}
	if !sub.ExpireAt.Equal(epoch.AddDate(0, 0, 30)) {
		t.Fatalf("expire_at=%v want +30d", sub.ExpireAt)
	}
	// tokenplan_spread 收益入账一次。
	if earn.callCount() != 1 || earn.count() != 1 {
		t.Fatalf("want exactly 1 earning, calls=%d entries=%d", earn.callCount(), earn.count())
	}
	e, _ := earn.last()
	if e.SourceType != EarningTokenplanSpread || e.Amount != 79 || e.SourceID != order {
		t.Fatalf("earning wrong: %+v", e)
	}
}

func TestActivateIdempotentSequential(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, _, earn := newSubService(newFakeClock(epoch))
	order := purchaseAndActivate(t, ctx, svc, repo, 279)

	s1, _ := svc.ActivateFromPayment(ctx, order)
	s2, _ := svc.ActivateFromPayment(ctx, order)
	s3, _ := svc.ActivateFromPayment(ctx, order)
	if s1.ID != s2.ID || s2.ID != s3.ID {
		t.Fatalf("idempotent activation must return same instance: %d %d %d", s1.ID, s2.ID, s3.ID)
	}
	// 多次重激活会多次调用 AddEarning（幂等、无 created 门控），但只落 1 条收益（idem_key 去重，安全审计 M1）。
	if earn.count() != 1 {
		t.Fatalf("re-activation must persist exactly 1 earning, got %d (calls=%d)", earn.count(), earn.callCount())
	}
}

func TestActivateZeroSpreadNoEarning(t *testing.T) {
	// 零售价 == 进货价 → 差价 0 → 不产生收益。需保护线==进货价方可压价到成本。
	ctx := context.Background()
	svc, repo, _, _, earn := newSubService(newFakeClock(epoch))
	in := basePlanInput()
	in.AgentCostPrice = 200
	in.MinPrice = 200 // 保护线==进货价，允许零售价压到成本价
	plan := seedPlanInto(repo, in)
	if err := NewRetailService(repo, &fakeGuard{}).SetListing(ctx, 7, plan.ID, true, 200); err != nil {
		t.Fatalf("SetListing: %v", err)
	}
	ticket, err := svc.Purchase(ctx, PurchaseInput{TenantID: 7, UserID: 11, PlanID: plan.ID})
	if err != nil {
		t.Fatalf("Purchase: %v", err)
	}
	if _, err := svc.ActivateFromPayment(ctx, ticket.OrderID); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if earn.callCount() != 0 {
		t.Fatalf("zero spread must not emit earning, got %d", earn.callCount())
	}
}

func TestActivateOrderNotFound(t *testing.T) {
	svc, _, _, _, _ := newSubService(newFakeClock(epoch))
	if _, err := svc.ActivateFromPayment(context.Background(), "ghost"); apperr.CodeOf(err) != CodeSubscriptionNotFound {
		t.Fatalf("want SUBSCRIPTION_NOT_FOUND, got %v", err)
	}
}

// TestActivateIdempotentConcurrent 是激活幂等的并发用例：同 orderID 并发激活只建 1 实例、1 收益。
func TestActivateIdempotentConcurrent(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, _, earn := newSubService(newFakeClock(epoch))
	order := purchaseAndActivate(t, ctx, svc, repo, 279)

	const workers = 200
	var (
		wg   sync.WaitGroup
		gate = make(chan struct{})
		ids  = make([]int64, workers)
		errc int64
	)
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(idx int) {
			defer wg.Done()
			<-gate
			s, err := svc.ActivateFromPayment(ctx, order)
			if err != nil {
				atomic.AddInt64(&errc, 1)
				return
			}
			ids[idx] = s.ID
		}(i)
	}
	close(gate)
	wg.Wait()

	if errc != 0 {
		t.Fatalf("no activation should error, got %d", errc)
	}
	// 全部返回同一实例 ID。
	for i := 1; i < workers; i++ {
		if ids[i] != ids[0] {
			t.Fatalf("instance divergence: ids[%d]=%d != ids[0]=%d", i, ids[i], ids[0])
		}
	}
	// 并发多次调用 AddEarning（幂等），最终只落 1 条收益（idem_key 去重，安全审计 M1）。
	if earn.count() != 1 {
		t.Fatalf("exactly 1 earning across concurrent activations, got %d (calls=%d)", earn.count(), earn.callCount())
	}
	// 该用户只有 1 个 active 实例。
	if a, _ := repo.GetActiveByUser(ctx, 11, epoch); a == nil || a.ID != ids[0] {
		t.Fatalf("active sub mismatch: %+v", a)
	}
}

// TestActivateEarningRetryAfterFailure 锁定安全审计 M1：ActivateFromOrder 成功、但首次 AddEarning
// 失败后，二次激活（对账重驱动）必须仍能补记 tokenplan_spread 收益——不因 created=false 永久漏记。
// 旧实现把 AddEarning 门控在 if created 内：重试时 created=false → 永久跳过 → 分润丢失（本用例复现并锁定修复）。
func TestActivateEarningRetryAfterFailure(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, _, earn := newSubService(newFakeClock(epoch))
	order := purchaseAndActivate(t, ctx, svc, repo, 279) // 仅下单：retail 279 > cost 200 → spread>0

	// 首次激活：注入 AddEarning 失败（订阅行已建 created=true，但收益事务失败）。
	earn.setErr(errEarningInjected)
	if _, err := svc.ActivateFromPayment(ctx, order); err == nil {
		t.Fatal("expected earning failure to surface on first activation")
	}
	if earn.count() != 0 {
		t.Fatalf("no earning must persist after failed activation, got %d", earn.count())
	}

	// 二次激活（对账重驱动）：AddEarning 恢复。旧代码 created=false → 永久跳过；修复后必须补记，恰好 1 条。
	earn.setErr(nil)
	if _, err := svc.ActivateFromPayment(ctx, order); err != nil {
		t.Fatalf("retry activation must succeed: %v", err)
	}
	if earn.count() != 1 {
		t.Fatalf("earning must be recovered on retry (M1), got %d", earn.count())
	}
	if e, ok := earn.last(); !ok || e.SourceID != order || e.SourceType != EarningTokenplanSpread {
		t.Fatalf("recovered earning wrong: %+v", e)
	}
}

// ---- GetActive / HasActive / lazy expiry ----

func TestGetActiveAndHasActive(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock(epoch)
	svc, repo, _, _, _ := newSubService(clock)

	if _, err := svc.GetActive(ctx, 11); apperr.CodeOf(err) != CodeSubscriptionNotFound {
		t.Fatalf("no sub: want SUBSCRIPTION_NOT_FOUND, got %v", err)
	}
	if has, _ := svc.HasActive(ctx, 11); has {
		t.Fatal("HasActive must be false with no sub")
	}

	sub := activeSub(repo, clock, 11, 7, 100, 30)
	got, err := svc.GetActive(ctx, 11)
	if err != nil || got.ID != sub.ID {
		t.Fatalf("GetActive: %+v err=%v", got, err)
	}
	if has, _ := svc.HasActive(ctx, 11); !has {
		t.Fatal("HasActive must be true with active sub")
	}
}

func TestGetActiveLazyExpiry(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock(epoch)
	svc, repo, _, _, _ := newSubService(clock)
	sub := activeSub(repo, clock, 11, 7, 100, 30)

	clock.advance(31 * 24 * time.Hour) // 越过 30 天有效期
	if _, err := svc.GetActive(ctx, 11); apperr.CodeOf(err) != CodeSubscriptionNotFound {
		t.Fatalf("expired sub must not be active, got %v", err)
	}
	if has, _ := svc.HasActive(ctx, 11); has {
		t.Fatal("HasActive must be false after expiry")
	}
	// 惰性翻态已落库。
	got, _ := repo.GetByID(ctx, sub.ID)
	if got.Status != SubExpired {
		t.Fatalf("lazy expiry must persist expired, got %s", got.Status)
	}
}

// ---- Meter ----

func TestMeterSuccess(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock(epoch)
	svc, repo, _, _, _ := newSubService(clock)
	sub := activeSub(repo, clock, 11, 7, 100, 30)

	if err := svc.Meter(ctx, sub.ID, 30); err != nil {
		t.Fatalf("Meter: %v", err)
	}
	got, _ := repo.GetByID(ctx, sub.ID)
	if got.UsedUSD != 30 || got.Status != SubActive {
		t.Fatalf("after meter: used=%v status=%s", got.UsedUSD, got.Status)
	}
	// 计量日志落一条。
	if logs := repo.UsageLogs(sub.ID); len(logs) != 1 || logs[0].UpstreamCostUSD != 30 {
		t.Fatalf("usage log wrong: %+v", logs)
	}
}

func TestMeterExactFillThenExhaust(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock(epoch)
	svc, repo, _, _, _ := newSubService(clock)
	sub := activeSub(repo, clock, 11, 7, 100, 30)

	if err := svc.Meter(ctx, sub.ID, 100); err != nil { // 恰好用满
		t.Fatalf("exact fill must pass: %v", err)
	}
	// 再扣任意正额 → 超额拒绝并置 exhausted。
	if err := svc.Meter(ctx, sub.ID, 0.01); apperr.CodeOf(err) != CodeSubscriptionExhausted {
		t.Fatalf("want SUBSCRIPTION_EXHAUSTED, got %v", err)
	}
	got, _ := repo.GetByID(ctx, sub.ID)
	if got.Status != SubExhausted || got.UsedUSD != 100 {
		t.Fatalf("exhausted state wrong: used=%v status=%s", got.UsedUSD, got.Status)
	}
}

func TestMeterOverLimitRejectedWhole(t *testing.T) {
	// 单笔超额：整笔拒绝、used 不变、置 exhausted（§6.2 0 行语义）。
	ctx := context.Background()
	clock := newFakeClock(epoch)
	svc, repo, _, _, _ := newSubService(clock)
	sub := activeSub(repo, clock, 11, 7, 100, 30)
	_ = svc.Meter(ctx, sub.ID, 99.5)

	if err := svc.Meter(ctx, sub.ID, 1); apperr.CodeOf(err) != CodeSubscriptionExhausted {
		t.Fatalf("want SUBSCRIPTION_EXHAUSTED, got %v", err)
	}
	got, _ := repo.GetByID(ctx, sub.ID)
	if got.UsedUSD != 99.5 { // 不部分扣减
		t.Fatalf("over-limit charge must be rejected whole, used=%v", got.UsedUSD)
	}
}

func TestMeterExpiredLazy(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock(epoch)
	svc, repo, _, _, _ := newSubService(clock)
	sub := activeSub(repo, clock, 11, 7, 100, 30)

	clock.advance(31 * 24 * time.Hour)
	if err := svc.Meter(ctx, sub.ID, 1); apperr.CodeOf(err) != CodeSubscriptionExpired {
		t.Fatalf("want SUBSCRIPTION_EXPIRED, got %v", err)
	}
	got, _ := repo.GetByID(ctx, sub.ID)
	if got.Status != SubExpired {
		t.Fatalf("meter must lazily expire, status=%s", got.Status)
	}
}

func TestMeterNotFound(t *testing.T) {
	svc, _, _, _, _ := newSubService(newFakeClock(epoch))
	if err := svc.Meter(context.Background(), 12345, 1); apperr.CodeOf(err) != CodeSubscriptionNotFound {
		t.Fatalf("want SUBSCRIPTION_NOT_FOUND, got %v", err)
	}
}

func TestMeterRejectsBadCost(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock(epoch)
	svc, repo, _, _, _ := newSubService(clock)
	sub := activeSub(repo, clock, 11, 7, 100, 30)
	if err := svc.Meter(ctx, sub.ID, -1); apperr.CodeOf(err) != CodeAmountInvalid {
		t.Fatalf("negative cost want TOKENPLAN_AMOUNT_INVALID, got %v", err)
	}
}
