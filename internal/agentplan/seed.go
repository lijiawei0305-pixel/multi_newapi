package agentplan

import "context"

// seedRow 是一档代理套餐的种子（占位默认；真实价格/授予能力由主站后台配置覆盖）。
type seedRow struct {
	code               string
	name               string
	desc               string
	price              float64
	anchorPrice        float64
	discountLabel      string
	grantLevel         int
	grantCanAPI        bool
	grantDiscountRatio float64
	validDays          int
	recommended        bool
}

// seedRows 是三档代理套餐默认基准：普通代理 / OEM 代理 / API 代理。
// 价格与授予能力均为占位默认，管理员可在后台改；有效期默认 365 天（一次性 + 有效期计费）。
var seedRows = []seedRow{
	{"basic", "普通代理", "适合个人或小团队，快速开始销售 AI 服务", 990, 1980, "5折", 0, false, 0.90, 365, true},
	{"oem", "OEM 代理", "品牌定制、独立域名，搭建专属 AI 平台", 4990, 9980, "5折", 1, false, 0.80, 365, false},
	{"api", "API 代理", "开放接口，为合作方提供 AI 能力", 9990, 19980, "5折", 1, true, 0.75, 365, false},
}

// SeedPlans 返回三档默认代理套餐（状态 enabled）。
func SeedPlans() []Plan {
	plans := make([]Plan, 0, len(seedRows))
	for i, r := range seedRows {
		plans = append(plans, Plan{
			Code:               r.code,
			Name:               r.name,
			Desc:               r.desc,
			Price:              r.price,
			AnchorPrice:        r.anchorPrice,
			DiscountLabel:      r.discountLabel,
			GrantLevel:         r.grantLevel,
			GrantCanAPI:        r.grantCanAPI,
			GrantDiscountRatio: r.grantDiscountRatio,
			ValidDays:          r.validDays,
			IsRecommended:      r.recommended,
			Sort:               i + 1,
			Status:             PlanEnabled,
		})
	}
	return plans
}

// PlanSeeder 是 seed 用的最小仓储契约（幂等落库，已存在不覆盖人工改动）。gormrepo.Repo 满足之。
type PlanSeeder interface {
	EnsurePlan(ctx context.Context, p *Plan) (int64, error)
}

// SeedInto 幂等地把三档默认代理套餐写入仓储（迁移种子用途）。
func SeedInto(repo PlanSeeder) error {
	ctx := context.Background()
	for _, p := range SeedPlans() {
		pp := p
		if _, err := repo.EnsurePlan(ctx, &pp); err != nil {
			return err
		}
	}
	return nil
}
