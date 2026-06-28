package tokenplan

import "context"

// retailService 是 PlanRetailService 的实现（代理上架/改价）。
type retailService struct {
	repo  PlanRepo
	guard PricingGuard
}

// 编译期断言。
var _ PlanRetailService = (*retailService)(nil)

// NewRetailService 组装 PlanRetailService（注入成本保护守卫）。
func NewRetailService(repo PlanRepo, guard PricingGuard) PlanRetailService {
	return &retailService{repo: repo, guard: guard}
}

// SetListing 上架/退出并设零售价（detailed-design §2.7 / proposal §8.3）。
//
//	套餐不存在        -> PLAN_NOT_FOUND
//	套餐已停用        -> PLAN_DISABLED（不可上架停用套餐）
//	上架且零售价击穿保护线（retail < min_price，经 PricingGuard 校验）-> RETAIL_BELOW_MIN
//
// 退出（enabled=false）不校验零售价。月限额/有效期/结构由主站锁定，代理仅可改零售价。
func (s *retailService) SetListing(ctx context.Context, tenantID, planID int64, enabled bool, retail float64) error {
	plan, err := s.repo.GetPlan(ctx, planID)
	if err != nil {
		return err // PLAN_NOT_FOUND
	}
	if !plan.Status.IsEnabled() {
		return ErrPlanDisabled
	}
	if enabled {
		if !validAmount(retail) || retail < 0 {
			return ErrRetailBelowMin // 非法金额一律视为击穿保护线（NaN<x 为假，必须显式拦截）
		}
		// 经消费者 PricingGuard 校验 retail >= min_price：min_price 已内含主站最低利润，
		// 故以 cost=min_price、minMargin=0 传入（floor=min_price）。守卫拒绝即 RETAIL_BELOW_MIN。
		if err := s.guard.ValidateRetailPrice(retail, plan.MinPrice, 0); err != nil {
			return ErrRetailBelowMin
		}
	}
	return s.repo.UpsertListing(ctx, &TenantPlan{
		TenantID:    tenantID,
		PlanID:      planID,
		Enabled:     enabled,
		RetailPrice: retail,
	})
}

// ListForTenant 返回代理视角的套餐列表：全部启用套餐 + 本租户上架态/零售价（未上架回退 BasePrice）。
func (s *retailService) ListForTenant(ctx context.Context, tenantID int64) ([]TenantPlanView, error) {
	plans, err := s.repo.ListPlans(ctx)
	if err != nil {
		return nil, err
	}
	listings, err := s.repo.ListListings(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	byPlan := make(map[int64]TenantPlan, len(listings))
	for _, l := range listings {
		byPlan[l.PlanID] = l
	}
	views := make([]TenantPlanView, 0, len(plans))
	for _, p := range plans {
		if !p.Status.IsEnabled() {
			continue // 主站停用的套餐不向代理展示
		}
		v := TenantPlanView{Plan: p, RetailPrice: p.BasePrice}
		if l, ok := byPlan[p.ID]; ok {
			v.Listed = true
			v.Enabled = l.Enabled
			v.RetailPrice = l.RetailPrice
		}
		views = append(views, v)
	}
	return views, nil
}
