package mtwire

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/internal/tenant"
	"github.com/QuantumNous/new-api/internal/tokenplan"
)

// demo 租户常量（proposal §8 / 任务要求）。域名由 tenant.Service.Create 自动派生为
// `<slug>.wedreamhub.com`（= tokendream.wedreamhub.com）。
const (
	demoSlug = "tokendream"
	demoName = "TokenDream"
)

// Seed 幂等地写入演示数据（仅 master 节点调用，见 router/mt-router.go）：
//  1. demo 租户 tokendream（+ 自动域名 tokendream.wedreamhub.com），开启 tokenplan；
//  2. proposal §8.2 的 6 档主站套餐写入 token_plans（按 code 幂等）；
//  3. 6 档套餐为 demo 租户上架（零售价默认取 BasePrice，已存在不覆盖人工改动）。
//
// 注意：root/初始用户由 new-api setup 流程创建，本 seed **不触碰** new-api 用户。
func (a *App) Seed() error {
	ctx := context.Background()

	t, err := a.ensureDemoTenant(ctx)
	if err != nil {
		return err
	}

	// 6 档套餐 + 为 demo 租户上架（均幂等）。
	for _, p := range tokenplan.SeedPlans() {
		plan := p
		planID, err := a.TokenPlanRepo.EnsurePlan(ctx, &plan)
		if err != nil {
			return err
		}
		if err := a.TokenPlanRepo.EnsureListing(ctx, t.ID, planID, true, plan.BasePrice); err != nil {
			return err
		}
	}
	return nil
}

// ensureDemoTenant 返回 demo 租户；不存在则经 TenantService.Create 建租户 + 派生域名。
func (a *App) ensureDemoTenant(ctx context.Context) (*tenant.Tenant, error) {
	t, err := a.TenantRepo.GetTenantBySlug(ctx, demoSlug)
	if err == nil {
		return t, nil
	}
	if !errors.Is(err, tenant.ErrTenantNotFound) {
		return nil, err
	}
	return a.TenantService.Create(ctx, tenant.CreateTenantInput{
		Slug:             demoSlug,
		Name:             demoName,
		TokenplanEnabled: true,
	})
}
