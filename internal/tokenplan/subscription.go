package tokenplan

import "context"

// subscriptionService 是 SubscriptionService 的实现（购买、激活、计量）。
// 依赖以接口注入，便于单测（mock Payment/Risk/Earning/Repo + 假时钟）。
type subscriptionService struct {
	subs     SubscriptionRepo
	plans    PlanRepo
	payment  PaymentGateway
	risk     RiskEngine
	earnings EarningSink
	clock    Clock
}

// 编译期断言。
var _ SubscriptionService = (*subscriptionService)(nil)

// NewSubscriptionService 组装 SubscriptionService。clock 为 nil 时回退真实时钟。
func NewSubscriptionService(
	subs SubscriptionRepo,
	plans PlanRepo,
	payment PaymentGateway,
	risk RiskEngine,
	earnings EarningSink,
	clock Clock,
) SubscriptionService {
	return &subscriptionService{
		subs:     subs,
		plans:    plans,
		payment:  payment,
		risk:     risk,
		earnings: earnings,
		clock:    orSystemClock(clock),
	}
}

// Purchase 校验上架/限购后下单（detailed-design §3.2）：
//
//	查套餐(停用→PLAN_DISABLED) → 查上架(未上架/退出→PLAN_NOT_LISTED)
//	  → RiskEngine 限购(超限→PURCHASE_LIMIT_EXCEEDED) → PaymentGateway 下单(type=subscription)
//	  → 暂存购买意图(供 ActivateFromPayment 还原)
//
// 真实实现下单+暂存应同事务，本轮内存假实现按序执行（见报告 TODO）。
func (s *subscriptionService) Purchase(ctx context.Context, in PurchaseInput) (*PurchaseTicket, error) {
	plan, err := s.plans.GetPlan(ctx, in.PlanID)
	if err != nil {
		return nil, err // PLAN_NOT_FOUND
	}
	if !plan.Status.IsEnabled() {
		return nil, ErrPlanDisabled
	}
	listing, err := s.plans.GetListing(ctx, in.TenantID, in.PlanID)
	if err != nil {
		return nil, err
	}
	if listing == nil || !listing.Enabled {
		return nil, ErrPlanNotListed
	}

	if err := s.risk.CheckPurchaseLimit(ctx, PurchaseLimitCheck{
		TenantID:   in.TenantID,
		UserID:     in.UserID,
		PlanID:     in.PlanID,
		PlanCode:   plan.Code,
		DeviceID:   in.DeviceID,
		RealNameID: in.RealNameID,
	}); err != nil {
		return nil, err // PURCHASE_LIMIT_EXCEEDED
	}

	order, err := s.payment.CreateOrder(ctx, OrderInput{
		TenantID:  in.TenantID,
		UserID:    in.UserID,
		Type:      OrderTypeSubscription,
		AmountCNY: listing.RetailPrice,
		Reference: plan.Code,
		Subject:   plan.Name,
	})
	if err != nil {
		return nil, err
	}

	// 代理进货成本价：设了折扣系数就按「主站官方售价 BasePrice × 系数」得 per-agent 成本；否则回退套餐
	// 自带的 AgentCostPrice（对所有代理一样，现状）。差价 = 零售 − 此成本，在 ActivateFromPayment 结算。
	agentCost := plan.AgentCostPrice
	if in.DiscountRatio > 0 {
		agentCost = plan.BasePrice * in.DiscountRatio
	}
	if err := s.subs.SavePendingPurchase(ctx, &PendingPurchase{
		OrderID:        order.OrderID,
		TenantID:       in.TenantID,
		UserID:         in.UserID,
		PlanID:         in.PlanID,
		RetailPrice:    listing.RetailPrice,
		AgentCostPrice: agentCost,
		MonthLimitUSD:  plan.MonthLimitUSD,
		ValidDays:      plan.ValidDays,
	}); err != nil {
		return nil, err
	}

	return &PurchaseTicket{
		OrderID:   order.OrderID,
		PayURL:    order.PayURL,
		AmountCNY: listing.RetailPrice,
		PlanID:    in.PlanID,
	}, nil
}

// ActivateFromPayment 凭订单号幂等创建 active 实例并触发 tokenplan_spread 收益（detailed-design §3.2）。
//
// 幂等：同 orderID 多次只建一个实例（Repo 按 source_order_id 去重），收益仅在首建时入账
// （EarningSink 自身亦按 (SourceType, SourceID) 幂等，双保险）。订单不存在返回 SUBSCRIPTION_NOT_FOUND。
func (s *subscriptionService) ActivateFromPayment(ctx context.Context, orderID string) (*Subscription, error) {
	pp, err := s.subs.GetPendingPurchase(ctx, orderID)
	if err != nil {
		return nil, err // SUBSCRIPTION_NOT_FOUND
	}
	now := s.clock.Now()
	sub := &Subscription{
		TenantID:       pp.TenantID,
		UserID:         pp.UserID,
		PlanID:         pp.PlanID,
		PurchasedPrice: pp.RetailPrice,
		MonthLimitUSD:  pp.MonthLimitUSD,
		UsedUSD:        0,
		Status:         SubActive,
		StartAt:        now,
		ExpireAt:       now.AddDate(0, 0, pp.ValidDays),
		SourceOrderID:  orderID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	created, err := s.subs.ActivateFromOrder(ctx, sub)
	if err != nil {
		return nil, err
	}
	if created {
		if spread := tokenplanSpread(pp.RetailPrice, pp.AgentCostPrice); spread > 0 {
			if err := s.earnings.AddEarning(ctx, EarningEntry{
				TenantID:   pp.TenantID,
				UserID:     pp.UserID,
				SourceType: EarningTokenplanSpread,
				SourceID:   orderID,
				Amount:     spread,
				Reference:  orderID,
			}); err != nil {
				return nil, err
			}
		}
	}
	return sub, nil
}

// GetActive 返回用户当前 active 订阅（惰性过期）；无 active 返回 SUBSCRIPTION_NOT_FOUND。
func (s *subscriptionService) GetActive(ctx context.Context, userID int64) (*Subscription, error) {
	sub, err := s.subs.GetActiveByUser(ctx, userID, s.clock.Now())
	if err != nil {
		return nil, err
	}
	if sub == nil {
		return nil, ErrSubscriptionNotFound
	}
	return sub, nil
}

// HasActive 报告用户是否存在 active 套餐（供 billing.SubscriptionChecker 选桶）。
func (s *subscriptionService) HasActive(ctx context.Context, userID int64) (bool, error) {
	sub, err := s.subs.GetActiveByUser(ctx, userID, s.clock.Now())
	if err != nil {
		return false, err
	}
	return sub != nil, nil
}

// Meter 原子累加 used_usd（detailed-design §6.2）；超额/过期由 Repo 置终态并上浮对应错误码。
func (s *subscriptionService) Meter(ctx context.Context, subID int64, costUSD float64) error {
	_, err := s.subs.Meter(ctx, subID, costUSD, s.clock.Now())
	return err
}
