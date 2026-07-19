package mtwire

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

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

	// demo 代理 owner 用户（仅显式启用的本地/测试环境创建）。
	demoAgentUsername       = "demoagent"
	demoAgentPasswordEnv    = "MT_DEMO_AGENT_PASSWORD"
	demoAgentSeedEnabledEnv = "MT_ENABLE_DEMO_AGENT_SEED"

	// 平台（主站）直销租户：产品侧已确认「主站自身也直销 tokenplan 套餐 + 接受直充」（非仅代理转售），
	// 见 platformTenant / http.go resolveBuyerTenant。slug 非保留词（见 tenant.ReservedSlugs），可正常
	// 通过 tenant.Create 校验；自动派生的 platform.wedreamhub.com 域名本身也是一个可正常访问、与主站
	// Host 兜底等价的合法租户站点，无副作用。
	platformSlug = "platform"
	platformName = "主站直销"
)

// ErrDemoAgentSecurity marks a failure to retire or safely configure the
// historical demo login. Master startup must fail closed on this class of seed
// error because continuing could leave a reserved demo login enabled unexpectedly.
var ErrDemoAgentSecurity = errors.New("demo agent security reconciliation failed")

// Seed 幂等地写入演示数据（仅 master 节点调用，见 router/mt-router.go）：
//  1. demo 租户 tokendream（+ 自动域名 tokendream.wedreamhub.com），开启 tokenplan；
//  2. proposal §8.2 的 6 档主站套餐写入 token_plans（按 code 幂等）；
//  3. 6 档套餐为 demo 租户上架（零售价默认取 BasePrice，已存在不覆盖人工改动）。
//
// 注意：root/初始用户仍由 new-api setup 流程创建；可登录 demoagent 默认不创建。
// 仅显式 development opt-in 才会创建它；默认路径会禁用这个保留用户名的任何既有账号。
func (a *App) Seed() error {
	ctx := context.Background()
	password, demoAgentSeedEnabled, err := a.prepareDemoAgentSeed()
	if err != nil {
		return err
	}

	t, created, err := a.ensureDemoTenant(ctx)
	if err != nil {
		return err
	}
	if demoAgentSeedEnabled {
		if err := a.seedDemoAgent(ctx, t.ID, password); err != nil {
			return fmt.Errorf("%w: %w", ErrDemoAgentSecurity, err)
		}
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

// seedDemoAgent 仅认领尚无 owner 的 demo 租户。已有 owner 时不建用户、不改归属，避免
// 启动 seed 覆盖运营人工改派；若 owner 本来就是 demoagent，则按显式开发配置轮换口令。
func (a *App) seedDemoAgent(ctx context.Context, tenantID int64, password string) error {
	currentOwnerID, err := a.TenantRepo.OwnerUserID(ctx, tenantID)
	if err != nil {
		return err
	}
	if currentOwnerID != 0 {
		var existing model.User
		err := model.DB.Select("id", "password").Where("username = ?", demoAgentUsername).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if int64(existing.Id) != currentOwnerID {
			return nil
		}
		_, err = a.ensureDemoAgentUser(password)
		return err
	}

	ownerID, err := a.ensureDemoAgentUser(password)
	if err != nil {
		return err
	}
	// Compare-and-set closes the gap between the owner read above and this write:
	// an operator assigning an owner concurrently always wins over the demo seed.
	claim := a.DB.WithContext(ctx).Table("tenants").
		Where("id = ? AND owner_user_id = ?", tenantID, 0).
		Update("owner_user_id", ownerID)
	if claim.Error != nil {
		return claim.Error
	}
	if claim.RowsAffected == 0 {
		return nil
	}
	if err := a.AgentService.SetAgentType(ctx, tenantID, agent.AgentParams{
		UserID: ownerID, CostPrice: 50, PackageDiscount: 0.9, CommissionRatio: 0.2, Level: 1,
	}); err != nil {
		return err
	}
	return a.AgentRepo.EnsureWallet(ctx, tenantID, ownerID)
}

// reconcileDemoAgentSeed keeps the login absent by default. Invalid opt-in
// configuration (including any production opt-in) is treated as disabled and
// still disables the reserved demo username.
func (a *App) reconcileDemoAgentSeed(ctx context.Context, tenantID int64) error {
	password, enabled, err := a.prepareDemoAgentSeed()
	if err != nil || !enabled {
		return err
	}
	if err := a.seedDemoAgent(ctx, tenantID, password); err != nil {
		return fmt.Errorf("%w: %w", ErrDemoAgentSecurity, err)
	}
	return nil
}

// prepareDemoAgentSeed runs before any non-security seed work. The default and
// every invalid opt-in path first disable the reserved demo username. Any
// failure is tagged so master startup can
// stop instead of serving traffic with uncertain demo-login state.
func (a *App) prepareDemoAgentSeed() (string, bool, error) {
	password, enabled, configErr := demoAgentSeedPassword()
	if enabled {
		return password, true, nil
	}
	if err := errors.Join(configErr, a.disableDemoAgentByDefault()); err != nil {
		return "", false, fmt.Errorf("%w: %w", ErrDemoAgentSecurity, err)
	}
	return "", false, nil
}

func demoAgentSeedPassword() (string, bool, error) {
	seedSetting := strings.TrimSpace(os.Getenv(demoAgentSeedEnabledEnv))
	if seedSetting == "" || strings.EqualFold(seedSetting, "false") {
		return "", false, nil
	}
	if !strings.EqualFold(seedSetting, "true") {
		return "", false, fmt.Errorf("%s must be exactly true or false", demoAgentSeedEnabledEnv)
	}
	if common.ProductionDeployment {
		return "", false, errors.New("automatic demo-agent creation is forbidden in production")
	}
	password := os.Getenv(demoAgentPasswordEnv)
	if !utf8.ValidString(password) {
		return "", false, fmt.Errorf("%s must contain valid UTF-8", demoAgentPasswordEnv)
	}
	passwordLength := utf8.RuneCountInString(password)
	if passwordLength < 16 || passwordLength > 20 {
		return "", false, fmt.Errorf("%s must contain 16-20 characters", demoAgentPasswordEnv)
	}
	if len(password) > 72 {
		return "", false, fmt.Errorf("%s must not exceed bcrypt's 72-byte input limit", demoAgentPasswordEnv)
	}
	return password, true, nil
}

// ensureDemoAgentUser 幂等地取/建 demo 代理用户（new-api 原生 users 表，经 model.User.Insert 正确散列口令）。
func (a *App) ensureDemoAgentUser(password string) (int64, error) {
	var u model.User
	err := model.DB.Where("username = ?", demoAgentUsername).First(&u).Error
	if err == nil {
		if common.ValidatePasswordAndHash(password, u.Password) && u.Status == common.UserStatusEnabled {
			return int64(u.Id), nil
		}
		newHash, err := common.Password2Hash(password)
		if err != nil {
			return 0, err
		}
		rotated := model.DB.Model(&model.User{}).
			Where("id = ? AND password = ?", u.Id, u.Password).
			Updates(map[string]interface{}{"password": newHash, "status": common.UserStatusEnabled})
		if rotated.Error != nil {
			return 0, rotated.Error
		}
		if rotated.RowsAffected > 0 {
			if err := errors.Join(model.InvalidateUserCache(u.Id), model.InvalidateUserTokensCache(u.Id)); err != nil {
				return 0, err
			}
		}
		return int64(u.Id), nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	nu := model.User{
		Username:    demoAgentUsername,
		Password:    password,
		DisplayName: "Demo Agent",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	if err := nu.Insert(0); err != nil {
		return 0, err
	}
	return int64(nu.Id), nil
}

// disableDemoAgentByDefault reserves the demo username for explicit development
// opt-in. This avoids retaining any historical credential material in source and
// guarantees production cannot silently keep a login under that known username.
func (a *App) disableDemoAgentByDefault() error {
	var user model.User
	err := model.DB.Where("username = ?", demoAgentUsername).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if user.Status == common.UserStatusDisabled {
		return nil
	}
	disabled := model.DB.Model(&model.User{}).
		Where("id = ? AND password = ? AND status <> ?", user.Id, user.Password, common.UserStatusDisabled).
		Update("status", common.UserStatusDisabled)
	if disabled.Error != nil {
		return disabled.Error
	}
	if disabled.RowsAffected == 0 {
		return nil
	}
	return errors.Join(model.InvalidateUserCache(user.Id), model.InvalidateUserTokensCache(user.Id))
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
