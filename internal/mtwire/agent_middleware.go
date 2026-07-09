package mtwire

// 代理端鉴权 / 门控中间件 + 门控信号端点（agent-self 端点的权威防线）。

import (
	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/tenant"
)

// ginKeyAgentTenant 是 AgentOwnerAuth 校验通过后写入的「权威租户 ID」键；代理自助 handler 只认它。
const ginKeyAgentTenant = "mt_agent_tenant"

// ============================================================================
// agent_owner 鉴权中间件（代理自助端点的权威防线）
// ============================================================================

// AgentOwnerAuth 校验当前 session 用户确为 Host 所指租户的 owner（直读 DB owner_user_id，绕过解析缓存）。
// 通过则把校验过的 tenant_id 写入 ctx（handler 只认它，不接受客户端传 tenant_id）；否则 403 中止。
// 须挂在 TenantMiddleware + new-api UserAuth 之后。
func (a *App) AgentOwnerAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		t := tenantFrom(c)
		if t == nil {
			respondErr(c, tenant.ErrTenantNotFound)
			c.Abort()
			return
		}
		userID := int64(c.GetInt("id"))
		if userID <= 0 {
			respondErr(c, errAgentForbidden)
			c.Abort()
			return
		}
		owner, err := a.TenantRepo.OwnerUserID(c.Request.Context(), t.ID)
		if err != nil {
			respondErr(c, err)
			c.Abort()
			return
		}
		if owner != userID {
			respondErr(c, errAgentForbidden)
			c.Abort()
			return
		}
		c.Set(ginKeyAgentTenant, t.ID)
		c.Next()
	}
}

// AgentOwnerAuthByUser 是代理自助端点的 owner-based 权威防线：从登录用户「拥有的租户」
// （tenants.owner_user_id == 当前 session 用户，1:1）解析 agentTenantID，与 Host 无关——
// 故 L0 无子域名也能在主站访问自己的控制台，L1 在子域名同样解析到自己的租户。
// 安全不变量：只解析到「当前用户拥有的」那一个租户；绝不接受客户端传 tenant_id；
// 未登录 / 不拥有任何租户 / 拥有的是平台（主站）直销租户（isPlatformTenant，见 seed.go）→ 403 中止
// （不放行）。须挂在 new-api UserAuth 之后。
// 替代 agent-self 组原先的 Host-based AgentOwnerAuth（后者留作 HandleAgentContext 的 Host 判定，不删）。
//
// 平台租户排除说明：seedPlatformTenant 把平台租户挂靠给首个管理员（root），使其在
// TenantByOwner 反查下"看起来"像拥有一个租户。但平台租户不是可管理的代理（无 agent_profiles
// 行），排除它是这条鉴权真正的安全边界——否则管理员会被当作 agent-self 组全部端点
// （提现申请/收益台账/推广渠道/站点装修/自定义域名/…）的合法 owner，凭自己的登录态操作
// "平台租户"这一并非代理的资源。此判定与 callerOwnedTenant（下方，供 HandleAgentContext
// 门控信号用）保持一致：同一个 isPlatformTenant 排除规则。
func (a *App) AgentOwnerAuthByUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := int64(c.GetInt("id"))
		if userID <= 0 {
			respondErr(c, errAgentForbidden)
			c.Abort()
			return
		}
		t, err := a.TenantRepo.TenantByOwner(c.Request.Context(), userID)
		if err != nil || isPlatformTenant(t) {
			// 该用户不拥有任何代理租户（含 ErrTenantNotFound）、或拥有的是平台直销租户 → 403，
			// 绝不放行、绝不回退 Host。
			respondErr(c, errAgentForbidden)
			c.Abort()
			return
		}
		c.Set(ginKeyAgentTenant, t.ID) // handler 只认这个已校验的租户 ID
		c.Next()
	}
}

// agentTenantID 取 AgentOwnerAuth 校验过的租户 ID（0 = 未经校验，handler 应已被中间件挡下）。
func agentTenantID(c *gin.Context) int64 {
	if v, ok := c.Get(ginKeyAgentTenant); ok {
		if id, ok := v.(int64); ok {
			return id
		}
	}
	return 0
}

// ensureAgentLevel 纵深校验当前代理租户档位 ≥ min：不足以 AGENT_LEVEL_LOCKED 响应并返回 false。
// 用于路由中间件（RequireAgentLevel）与 handler 入口双保险（spec §5.2.3 纵深）。
func (a *App) ensureAgentLevel(c *gin.Context, min int) bool {
	tenantID := agentTenantID(c)
	if tenantID <= 0 {
		respondErr(c, errAgentForbidden)
		return false
	}
	lvl, err := a.AgentService.AgentLevel(c.Request.Context(), tenantID)
	if err != nil {
		respondErr(c, err)
		return false
	}
	if lvl < min {
		respondErr(c, errAgentLevelLocked)
		return false
	}
	return true
}

// RequireAgentLevel 是「独立能力」路由门禁：须挂在 AgentOwnerAuth 之后（依赖其写入的 agentTenantID）。
func (a *App) RequireAgentLevel(min int) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !a.ensureAgentLevel(c, min) {
			c.Abort()
			return
		}
		c.Next()
	}
}

// callerOwnedTenant 返回当前 session 用户拥有的租户（owner-based：TenantByOwner，与 Host 无关，
// 镜像 AgentOwnerAuthByUser 的解析口径）——L0 无子域名、请求打在主站 Host 上时也能命中自己的租户。
// 未登录 / 不拥有任何租户（含 ErrTenantNotFound）/ 拥有的是平台（主站）直销租户（isPlatformTenant，
// 见 seed.go）/ 查询失败一律 nil（绝不抛错，调用方保守判非 owner）。
//
// 平台租户排除说明：seedPlatformTenant 把平台租户挂靠给首个管理员（root），若不排除，
// HandleAgentContext（唯一调用方）会对该管理员返回 is_agent_owner:true——前端据此展示整套
// 代理自助菜单/路由守卫，而管理员其实只是"拥有"主站直销这一并非代理的记账实体，并非真正的代理。
// 与 AgentOwnerAuthByUser（真正的后端鉴权防线）用同一 isPlatformTenant 判定，保持口径一致——
// 门控信号（这里）与实际授权（那里）必须对同一用户给出相同答案，否则会出现"前端隐藏了菜单，
// 后端却仍会放行"的门面式修复（信号和授权脱节）。
func (a *App) callerOwnedTenant(c *gin.Context) *tenant.Tenant {
	userID := int64(c.GetInt("id"))
	if userID <= 0 {
		return nil // 未登录
	}
	t, err := a.TenantRepo.TenantByOwner(c.Request.Context(), userID)
	if err != nil || isPlatformTenant(t) {
		return nil // 不拥有任何租户 / 拥有的是平台直销租户 / 查询失败：保守判 false
	}
	return t
}

// isAgentOwner 判定当前 session 用户是否拥有某个代理租户（owner-based、Host 无关，见
// callerOwnedTenant）。供前端门控信号端点（HandleAgentContext）使用。
func (a *App) isAgentOwner(c *gin.Context) bool {
	return a.callerOwnedTenant(c) != nil
}

// HandleAgentContext GET /api/tenant/agent-context —— 代理身份门控信号。**仅 UserAuth**（不挂
// AgentOwnerAuth/AgentOwnerAuthByUser），任何登录用户可调。两个正交信号：
//
//   - is_agent_owner / level / can_api：owner-based 解析（TenantByOwner），与 Host 无关——身份事实。
//     主站钱包页 L0「邀请返现」面板（is_agent_owner && level==0，doc/l0-agent-wallet-referral.md）
//     依赖它在主站 Host 下也为 true，语义不可回退成 Host 判定（Fix 1 教训：L0 无子域名，Host 判定
//     使其 UI 永远不可达）。
//   - on_own_site：当前 Host 解析到的租户（TenantMiddleware → tenantFrom）是否 == 自己拥有的租户
//     ——位置事实。前端「代理自助」侧栏 + 代理路由守卫改门 is_agent_owner && on_own_site：
//     代理控制台只在自己的代理站（子域名/自定义域名）出现，不泄漏到主站或别家代理站
//     （2026-07-07 用户报 bug：L1 在主站控制台看到代理自助）。L0 没有自己的站 → on_own_site
//     永远 false → 侧栏对 L0 永不显示，L0 的界面只有主站钱包返现面板（与文档一致）。
//
// 后端 agent-self 真实鉴权（AgentOwnerAuthByUser）保持 owner-based 不动：Host 可伪造，
// 不是安全边界；on_own_site 只是产品/UI 边界。永远 200：未登录/非 owner/查询失败 →
// is_agent_owner=false（不 abort）。
func (a *App) HandleAgentContext(c *gin.Context) {
	out := agentContextOut{}
	if t := a.callerOwnedTenant(c); t != nil {
		out.IsAgentOwner = true
		if hostT := tenantFrom(c); hostT != nil && hostT.ID == t.ID {
			out.OnOwnSite = true
		}
		if p, found, err := a.AgentRepo.GetAgentType(c.Request.Context(), t.ID); err == nil && found {
			out.Level = p.Level
			out.CanAPI = p.CanAPI
		}
	}
	respondOK(c, out)
}

// agentContextOut 是 GET /api/tenant/agent-context 响应：前端据此隐藏菜单 + 路由守卫 gate。
// IsAgentOwner=身份（Host 无关，钱包 L0 卡消费）；OnOwnSite=位置（Host==自己的站，侧栏/守卫消费）。
type agentContextOut struct {
	IsAgentOwner bool `json:"is_agent_owner"`
	Level        int  `json:"level"`
	CanAPI       bool `json:"can_api"`
	OnOwnSite    bool `json:"on_own_site"`
}
