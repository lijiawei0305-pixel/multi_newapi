package mtwire

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/agent"
	"github.com/QuantumNous/new-api/internal/tenant"
	"github.com/QuantumNous/new-api/internal/tokenplan"
	"github.com/QuantumNous/new-api/model"
)

// demo 租户常量（proposal §8 / 任务要求）。域名由 tenant.Service.Create 自动派生为
// `<slug>.wedreamhub.com`（= tokendream.wedreamhub.com）。
const (
	demoSlug = "tokendream"
	demoName = "TokenDream"

	// demo 代理 owner 用户（便于 UI/E2E 有可登录的代理样例）。
	demoAgentUsername = "demoagent"
	demoAgentPassword = "demoagent123" // 8-20 位，满足 new-api 校验；演示用，部署后请改。
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

		// 分组（vip/svip/kiro 等）一律由管理员后台配置 + 给用户分配（设 User.Group），
		// 用户不得在建 API Key 时自选高级分组。new-api 默认 UserUsableGroups={default,vip}
		// 把 vip 暴露成"所有人可自选"（见 RETRO「分组自选越权」）；这里固化为仅 default：
		// 用户只能用自己被管理员分配的分组（GetUserUsableGroups 总会补上用户自身 group）。
		if err := model.UpdateOption("UserUsableGroups", `{"default":"默认分组"}`); err != nil {
			common.SysError("mtwire: 固化 UserUsableGroups=default-only 失败: " + err.Error())
		} else {
			common.SysLog("mtwire: 首次初始化，UserUsableGroups 已固化为仅 default（高级分组仅管理员分配）")
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

	// demo 代理：把 tokendream 的 owner 设为 demo 代理用户，并写一条 agent_profile + 钱包（幂等）。
	// 仅当尚未设代理（owner==0）才设置，避免覆盖运营人工改派。
	if t.OwnerUserID == 0 {
		if err := a.seedDemoAgent(ctx, t.ID); err != nil {
			common.SysError("mtwire: seed demo agent failed: " + err.Error())
		}
	}
	return nil
}

// seedDemoAgent 确保 demo 代理用户存在，并把其设为 tenantID 的 owner + 写 agent_profile + 钱包。
func (a *App) seedDemoAgent(ctx context.Context, tenantID int64) error {
	ownerID, err := a.ensureDemoAgentUser()
	if err != nil {
		return err
	}
	if err := a.TenantRepo.SetOwnerUserID(ctx, tenantID, ownerID); err != nil {
		return err
	}
	if err := a.AgentService.SetAgentType(ctx, tenantID, agent.AgentParams{CostPrice: 50, PackageDiscount: 0.9, CommissionRatio: 0.2, Level: 1}); err != nil {
		return err
	}
	return a.AgentRepo.EnsureWallet(ctx, tenantID, ownerID)
}

// ensureDemoAgentUser 幂等地取/建 demo 代理用户（new-api 原生 users 表，经 model.User.Insert 正确散列口令）。
func (a *App) ensureDemoAgentUser() (int64, error) {
	var u model.User
	err := model.DB.Where("username = ?", demoAgentUsername).First(&u).Error
	if err == nil {
		return int64(u.Id), nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	nu := model.User{
		Username:    demoAgentUsername,
		Password:    demoAgentPassword,
		DisplayName: "Demo Agent",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	if err := nu.Insert(0); err != nil {
		return 0, err
	}
	return int64(nu.Id), nil
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
		// demo 代理 seed 为 L1（见 seedDemoAgent），保留子域名 tokendream.wedreamhub.com。
	})
	if err != nil {
		return nil, false, err
	}
	return created, true, nil
}
