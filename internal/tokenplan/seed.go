package tokenplan

import (
	"context"
	"math"
)

// 种子默认派生系数（proposal §8.2 未直接给出 agent_cost_price / min_price 的数值，
// 仅给出售价/原价/折扣/月限额/成本估算，故此处按可配置默认派生，见交付报告默认假设 ④）：
//   - agent_cost_price = base_price × defaultWholesaleDiscount（主站给代理的批发进货价）
//   - min_price        = agent_cost_price × (1 + defaultMinMargin)（零售保护线）
//
// 二者均为占位默认，真实数值由主站后台配置覆盖。
const (
	defaultWholesaleDiscount = 0.8 // 代理批发 8 折进货
	defaultMinMargin         = 0.1 // 零售保护线在进货价上加 10% 最低利润
)

// round2 四舍五入到分（¥ 两位小数）。
func round2(v float64) float64 { return math.Round(v*100) / 100 }

// seedRow 是 proposal §8.2 套餐表的一行（仅文档直接给出的字段）。
type seedRow struct {
	code          string
	name          string
	basePrice     float64
	anchorPrice   float64
	discountLabel string
	monthLimitUSD float64
	upstreamEst   float64
	recommended   bool
}

// seedRows 是 proposal §8.2 主站基准 6 档（Trial~Max）。
var seedRows = []seedRow{
	{"trial", "Trial 引流体验包", 6.9, 1020, "-99%", 80, 6.00, false},
	{"mini", "Mini", 119, 1360, "-91%", 220, 16.50, false},
	{"solo", "Solo", 279, 3400, "-92%", 560, 42.00, false},
	{"lite", "Lite", 899, 13892, "-94%", 2200, 165.00, false},
	{"pro", "Pro", 2699, 45975, "-94%", 7200, 540.00, true},
	{"max", "Max", 8999, 160000, "-94%", 25000, 1875.00, false},
}

// SeedPlans 返回 proposal §8.2 的 6 档主站基准套餐（Trial/Mini/Solo/Lite/Pro/Max）。
// 倍率统一 1.0（x1），有效期 30 天，状态 enabled；agent_cost_price / min_price 按默认系数派生。
func SeedPlans() []Plan {
	plans := make([]Plan, 0, len(seedRows))
	for i, r := range seedRows {
		cost := round2(r.basePrice * defaultWholesaleDiscount)
		minPrice := round2(cost * (1 + defaultMinMargin))
		plans = append(plans, Plan{
			Code:            r.code,
			Name:            r.name,
			BasePrice:       r.basePrice,
			AnchorPrice:     r.anchorPrice,
			DiscountLabel:   r.discountLabel,
			Multiplier:      1.0,
			MonthLimitUSD:   r.monthLimitUSD,
			ValidDays:       30,
			UpstreamCostEst: r.upstreamEst,
			AgentCostPrice:  cost,
			MinPrice:        minPrice,
			IsRecommended:   r.recommended,
			Sort:            i + 1,
			Status:          PlanEnabled,
		})
	}
	return plans
}

// SeedInto 把 6 档基准套餐写入给定 PlanRepo（迁移种子用途；幂等性由调用方/真实迁移保证）。
func SeedInto(repo PlanRepo) error {
	ctx := context.Background()
	for _, p := range SeedPlans() {
		pp := p
		if err := repo.CreatePlan(ctx, &pp); err != nil {
			return err
		}
	}
	return nil
}
