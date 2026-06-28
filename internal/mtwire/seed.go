package mtwire

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/tenant"
	"github.com/QuantumNous/new-api/internal/tokenplan"
	"github.com/QuantumNous/new-api/model"
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

	t, created, err := a.ensureDemoTenant(ctx)
	if err != nil {
		return err
	}

	// 首次初始化（新库）时把前端主题固化为 default：我们所有页面都在新版前端 web/default，
	// new-api 默认 theme.frontend=classic 会服务经典前端、看不到我们的页面（见 RETRO「双前端」）。
	// 仅首次设置（与建租户同条件），尊重后续运营人工切换。
	if created {
		if err := model.UpdateOption("theme.frontend", "default"); err != nil {
			common.SysError("mtwire: 固化 theme.frontend=default 失败: " + err.Error())
		} else {
			common.SysLog("mtwire: 首次初始化，theme.frontend 已固化为 default")
		}
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

// ensureDemoTenant 返回 demo 租户 + 是否本次新建（created）；不存在则经 TenantService.Create
// 建租户 + 派生域名。created=true 表示首次初始化（新库），调用方据此固化一次性初始配置。
func (a *App) ensureDemoTenant(ctx context.Context) (*tenant.Tenant, bool, error) {
	t, err := a.TenantRepo.GetTenantBySlug(ctx, demoSlug)
	if err == nil {
		return t, false, nil
	}
	if !errors.Is(err, tenant.ErrTenantNotFound) {
		return nil, false, err
	}
	created, err := a.TenantService.Create(ctx, tenant.CreateTenantInput{
		Slug:             demoSlug,
		Name:             demoName,
		TokenplanEnabled: true,
	})
	if err != nil {
		return nil, false, err
	}
	return created, true, nil
}
