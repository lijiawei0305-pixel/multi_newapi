package mtwire

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/agent"
	"github.com/QuantumNous/new-api/internal/agentplan"
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

	// 平台（主站）直销租户：产品侧已确认「主站自身也直销 tokenplan 套餐 + 接受直充」（非仅代理转售），
	// 见 platformTenant / http.go resolveBuyerTenant。slug 非保留词（见 tenant.ReservedSlugs），可正常
	// 通过 tenant.Create 校验；自动派生的 platform.wedreamhub.com 域名本身也是一个可正常访问、与主站
	// Host 兜底等价的合法租户站点，无副作用。
	platformSlug = "platform"
	platformName = "主站直销"
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

	// 三档代理套餐默认基准（主站全局、非按租户上架；幂等，已存在不覆盖管理员改动）。
	if err := agentplan.SeedInto(a.AgentPlanRepo); err != nil {
		return err
	}

	// demo 代理：把 tokendream 的 owner 设为 demo 代理用户，并写一条 agent_profile + 钱包（幂等）。
	// 仅当尚未设代理（owner==0）才设置，避免覆盖运营人工改派。
	if t.OwnerUserID == 0 {
		if err := a.seedDemoAgent(ctx, t.ID); err != nil {
			common.SysError("mtwire: seed demo agent failed: " + err.Error())
		}
	}

	// 平台（主站）直销租户：失败不影响以上 demo 租户主链路，仅记日志——届时主站买家端点
	// （HandleListTokenPlans/HandlePurchase/HandleListSubscriptions/HandleWalletRecharge/
	// HandleTenantRechargeMethods）会继续回退 TENANT_NOT_FOUND，下次启动会重试
	// （ensurePlatformTenant/EnsurePlan/EnsureListing/SetOwnerUserID 均幂等，见 seedPlatformTenant）。
	if err := a.seedPlatformTenant(ctx); err != nil {
		common.SysError("mtwire: seed platform tenant failed: " + err.Error())
	}
	return nil
}

// seedPlatformTenant 幂等地建"平台（主站）直销"租户 + 上架主站基准 6 档套餐 + 挂靠
// owner=root/首个管理员，供主站 Host（tenant.IsMainSiteHost）买家套餐/充值端点兜底解析
// （见 http.go resolveBuyerTenant）。
//
// 刻意不调用 AgentService.SetAgentType / AgentRepo.EnsureWallet：平台租户不是可管理的「代理」——
// HandleAdminListAgents 只读 agent_profiles 表（AgentRepo.ListProfiles），没有该表行 = 不会出现在
// 代理列表；callerOwnedTenant / AgentOwnerAuthByUser 也显式排除它（isPlatformTenant），管理员不会
// 因"拥有"平台租户被误判为代理 owner（agent-self UI 门控 + owner-based 鉴权两处，见 agent.go）。
func (a *App) seedPlatformTenant(ctx context.Context) error {
	pt, _, err := a.ensurePlatformTenant(ctx)
	if err != nil {
		return err
	}

	// 主站基准 6 档套餐同样为平台租户上架（零售价默认取 BasePrice，即主站官方售价——
	// 平台直销场景下"零售价"与"官方售价"本就是同一个数，不存在代理差价）。
	for _, p := range tokenplan.SeedPlans() {
		plan := p
		planID, err := a.TokenPlanRepo.EnsurePlan(ctx, &plan)
		if err != nil {
			return err
		}
		if err := a.TokenPlanRepo.EnsureListing(ctx, pt.ID, planID, true, plan.BasePrice); err != nil {
			return err
		}
	}

	if pt.OwnerUserID != 0 {
		return nil // 已挂靠：尊重既有归属（含后续人工改派），不重新查找/覆盖
	}
	ownerID, found, err := a.firstAdminUserID(ctx)
	if err != nil {
		return err
	}
	if !found {
		return nil // 新库 / setup 向导尚未创建管理员：本轮跳过，下次启动 OwnerUserID==0 会重试
	}
	return a.TenantRepo.SetOwnerUserID(ctx, pt.ID, ownerID)
}

// platformTenant 返回已 seed 的"平台（主站）直销"租户；不存在时原样透传 tenant.ErrTenantNotFound
// （seed 尚未跑过，或曾因 DB 故障失败——调用方按既有"站点未开通/租户不存在"语义处理，见
// http.go resolveBuyerTenant）。
func (a *App) platformTenant(ctx context.Context) (*tenant.Tenant, error) {
	return a.TenantRepo.GetTenantBySlug(ctx, platformSlug)
}

// ensurePlatformTenant 幂等取/建平台租户；返回 (租户, 是否本次新建, error)。镜像 ensureDemoTenant
// 的取/建结构，但平台租户没有"首次初始化固化主题/分组"的需要（那是 demo 租户 E2E 演示专属逻辑）。
func (a *App) ensurePlatformTenant(ctx context.Context) (*tenant.Tenant, bool, error) {
	t, err := a.platformTenant(ctx)
	if err == nil {
		return t, false, nil
	}
	if !errors.Is(err, tenant.ErrTenantNotFound) {
		return nil, false, err
	}
	created, err := a.TenantService.Create(ctx, tenant.CreateTenantInput{
		Slug:             platformSlug,
		Name:             platformName,
		TokenplanEnabled: true,
	})
	if err != nil {
		return nil, false, err
	}
	return created, true, nil
}

// isPlatformTenant 报告 t 是否为"平台（主站）直销"租户（seedPlatformTenant，slug=platformSlug）。
// 平台租户的 owner 是首个管理员（seedPlatformTenant 挂靠），但它不是可管理的代理——
// callerOwnedTenant / AgentOwnerAuthByUser（agent.go）据此排除它，防止管理员因"拥有"平台租户
// 被误判为 agent owner（既影响前端门控信号 HandleAgentContext，也影响 agent-self 组的真实鉴权）。
func isPlatformTenant(t *tenant.Tenant) bool {
	return t != nil && t.Slug == platformSlug
}

// firstAdminUserID 找"root/首个管理员"用户：role >= common.RoleAdminUser（含 RoleRootUser）里
// id 最小的一个——绝大多数部署中就是 new-api 建库时创建的首个 root 用户。直读共享 *gorm.DB 原始
// users 表（与 http.go usernamesByIDs / agent.go userTenantIDStrict 同一约定，不引入 new-api model
// 包；.Select("id") 同 userTenantIDStrict 惯例，避免对宽表做 SELECT *），找不到（新库、setup 向导
// 尚未完成）返回 (0, false, nil)，非错误。
func (a *App) firstAdminUserID(ctx context.Context) (int64, bool, error) {
	var row struct{ ID int64 }
	err := a.DB.WithContext(ctx).Table("users").Select("id").
		Where("role >= ?", common.RoleAdminUser).
		Order("id asc").Limit(1).Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return row.ID, true, nil
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
