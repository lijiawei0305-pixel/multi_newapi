package router

import (
	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/mtwire"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
)

// SetMtRouter 把「多租户增量」（tenant + tokenplan）挂到 new-api 的 gin engine。
//
// 集成点：由 router.SetRouter 调用（main.go → InitResources 已先跑 model.InitDB，故 model.DB 就绪）。
//   - 复用 new-api 共享 model.DB 装配我们的 gormrepo（不自开连接）；
//   - 仅 master 节点 AutoMigrate 我们的表 + 幂等 seed（对齐 new-api 的迁移门控）；
//   - 鉴权复用 new-api：console 用 UserAuth（session→id/role），admin 用 AdminAuth；
//   - 租户来自 Host：tenant 组前置 mtwire.TenantMiddleware（Host→tenant 注入 ctx）。
//
// 路由前缀 /api/tenant/** 与 /api/admin/token-plans 均为新路径，与 new-api 既有 /api/** 不冲突。
func SetMtRouter(router *gin.Engine) {
	if model.DB == nil {
		common.SysError("mt-router: model.DB is nil, skip multitenant wiring")
		return
	}

	app := mtwire.New(model.DB)

	// 安装代理增量旁路钩子（消耗分润 / 注册归属 / 2D 倍率层级×模型分组，含代理 per-tenant 覆盖）。
	// 所有节点都装（钩子由原生 service/controller/计费侧调用）。租户用户组倍率覆盖已并入 2D 的 modelFactor
	// （只对模型分组生效、受组合下限保护，见 §2.15 Phase 2），故不再单设独立钩子。
	app.InstallHooks()

	if common.IsMasterNode {
		if err := app.Migrate(); err != nil {
			common.FatalLog("mt-router: AutoMigrate multitenant tables failed: " + err.Error())
			return
		}
		if err := app.Seed(); err != nil {
			// seed 失败不致命：记录后继续启动（路由仍注册）。
			common.SysError("mt-router: seed failed: " + err.Error())
		}
		// 支付卡单对账兜底定时任务（master-only）：周期扫 RCG/SUB 卡单补入账/补激活。
		app.StartReconcileLoop()
	}

	// 租户控制台（Host 维度）。GET /current 公开；其余复用 new-api UserAuth。
	tenantGroup := router.Group("/api/tenant")
	tenantGroup.Use(app.TenantMiddleware())
	{
		tenantGroup.GET("/current", app.HandleTenantCurrent)
		tenantGroup.GET("/token-plans", middleware.UserAuth(), app.HandleListTokenPlans)
		tenantGroup.POST("/token-plans/:id/purchase", middleware.UserAuth(), app.HandlePurchase)
		tenantGroup.GET("/subscriptions", middleware.UserAuth(), app.HandleListSubscriptions)
		// 充值下单（目标③）：UserAuth + Host 租户；下单 → 调 auth-service → 返支付凭据。
		tenantGroup.POST("/wallet/recharge", middleware.UserAuth(), app.HandleWalletRecharge)
		// 买家可用充值渠道：UserAuth + Host 租户；返回 enabled && configured 的渠道（wxpay/alipay）。
		tenantGroup.GET("/wallet/recharge/methods", middleware.UserAuth(), app.HandleTenantRechargeMethods)
		// 用户兑换码（P1-UI-04）：UserAuth + Host 租户（不强制 owner）；单赢家 CAS → 原生 quota 入账。
		tenantGroup.POST("/redeem", middleware.UserAuth(), app.HandleRedeem)
		// 代理身份门控信号：UserAuth + Host 租户（**不挂 AgentOwnerAuth**，任何登录用户可调）。
		// 返回 {is_agent_owner}（权威判断 = Host 租户 owner == 当前用户），供前端隐藏代理自助菜单 +
		// 路由 beforeLoad 拦截，避免普通用户/别站代理触发 AGENT_FORBIDDEN。
		tenantGroup.GET("/agent-context", middleware.UserAuth(), app.HandleAgentContext)
		// 代理自助（owner 维度）：UserAuth + AgentOwnerAuth（权威校验 Host 租户 owner == 当前用户）。
		agentSelf := tenantGroup.Group("", middleware.UserAuth(), app.AgentOwnerAuth())
		{
			agentSelf.POST("/withdrawals", app.HandleAgentRequestWithdrawal)
			agentSelf.GET("/withdrawals", app.HandleAgentListWithdrawals)
			agentSelf.GET("/earnings", app.HandleAgentListEarnings)
			// P1-UI-04 代理自助分销：套餐上架改价 / 推广渠道 / 我的用户 / 兑换码（建/列） / 用户组倍率。
			agentSelf.GET("/token-plans/listings", app.HandleAgentListPlanListings)
			agentSelf.PUT("/token-plans/listings/:planId", app.HandleAgentSetPlanListing)
			agentSelf.GET("/promotion/channels", app.HandleAgentListChannels)
			agentSelf.POST("/promotion/channels", app.HandleAgentCreateChannel)
			agentSelf.GET("/users", app.HandleAgentListUsers)
			// 代理给本租户下级用户设层级（default/vip）：改 User.Group + 刷用户缓存（§2.15 Phase 2）。
			agentSelf.PUT("/users/:id/tier", app.HandleAgentSetUserTier)
			agentSelf.POST("/redemptions", app.HandleAgentCreateRedemptions)
			agentSelf.GET("/redemptions", app.HandleAgentListRedemptions)
			// 我的模型分组倍率：列表（平台基准 + 本租户覆盖）/ 设覆盖（仅模型分组、≥ 平台基准）。
			agentSelf.GET("/groups", app.HandleAgentListGroups)
			agentSelf.PUT("/groups/:group", app.HandleAgentSetGroupRatio)
			// 6e 违禁词：代理管理本租户词库（scopeByTenant）。
			agentSelf.GET("/moderation/words", app.HandleAgentListModerationWords)
			agentSelf.POST("/moderation/words", app.HandleAgentUpsertModerationWord)
			agentSelf.DELETE("/moderation/words/:id", app.HandleAgentDeleteModerationWord)
			agentSelf.GET("/moderation/base-words", app.HandleAgentListBaseWords)  // 只读：全站基础库
			agentSelf.GET("/moderation/violations", app.HandleAgentListViolations) // 本租户违规日志
		}
	}

	// 支付平台异步回调（目标③，支付重构后）：微信/支付宝 POST 到此，handler 内验签（无 UserAuth/TenantMiddleware）。
	// 签名校验在 providerManager.VerifyNotify；金额/幂等以库内订单为权威。公开路由（平台来源 IP 不固定）。
	payGroup := router.Group("/api/pay")
	{
		payGroup.POST("/wechat/notify", app.HandleWechatNotify)
		payGroup.POST("/alipay/notify", app.HandleAlipayNotify)
	}

	// 主站套餐目录管理（全局，非租户维度），复用 new-api AdminAuth。
	adminPlanGroup := router.Group("/api/admin/token-plans")
	adminPlanGroup.Use(middleware.AdminAuth())
	{
		adminPlanGroup.GET("", app.HandleAdminListPlans)
		adminPlanGroup.POST("", app.HandleAdminCreatePlan)
		adminPlanGroup.PATCH("/:id", app.HandleAdminUpdatePlan)
	}

	// 支付渠道配置已移至系统设置（setting.*Enabled + 凭据，DB option）：渠道启用/凭据由设置页管理，
	// 买家可用渠道经 GET /api/tenant/wallet/recharge/methods 暴露（configured 进程内判断），故此处无独立管理路由。

	// 支付卡单对账（兜底）管理：列当前卡单 + 手动立即对账。复用 new-api AdminAuth。
	adminReconcileGroup := router.Group("/api/admin/reconcile")
	adminReconcileGroup.Use(middleware.AdminAuth())
	{
		adminReconcileGroup.GET("/stuck", app.HandleAdminListStuck)
		adminReconcileGroup.POST("/run", app.HandleAdminRunReconcile)
	}

	// 管理端订阅监控（当前租户维度，租户来自 Host）。前置 TenantMiddleware + new-api AdminAuth。
	adminSubGroup := router.Group("/api/admin/subscriptions")
	adminSubGroup.Use(app.TenantMiddleware(), middleware.AdminAuth())
	{
		adminSubGroup.GET("", app.HandleAdminListSubscriptions)
	}

	// 主站代理管理（全局，非租户维度）：设代理 / 列表 / 改代理。复用 new-api AdminAuth。
	adminAgentGroup := router.Group("/api/admin/agents")
	adminAgentGroup.Use(middleware.AdminAuth())
	{
		adminAgentGroup.GET("", app.HandleAdminListAgents)
		adminAgentGroup.POST("", app.HandleAdminCreateAgent)
		adminAgentGroup.PATCH("/:id", app.HandleAdminUpdateAgent)
	}

	// 主站提现审核（全局，非租户维度）：列表 / 通过 / 拒绝。复用 new-api AdminAuth。
	adminWithdrawGroup := router.Group("/api/admin/withdrawals")
	adminWithdrawGroup.Use(middleware.AdminAuth())
	{
		adminWithdrawGroup.GET("", app.HandleAdminListWithdrawals)
		adminWithdrawGroup.POST("/:id/approve", app.HandleAdminApproveWithdrawal)
		adminWithdrawGroup.POST("/:id/reject", app.HandleAdminRejectWithdrawal)
	}

	// 主站模型分组管理（全局，非租户维度，§2.15）：增删改 + 设倍率/绑渠道。复用 new-api AdminAuth。
	// 写操作同步真源 GroupRatio + UserUsableGroups（见 internal/mtwire/modelgroup.go）。
	adminModelGroupGroup := router.Group("/api/admin/model-groups")
	adminModelGroupGroup.Use(middleware.AdminAuth())
	{
		adminModelGroupGroup.GET("", app.HandleAdminListModelGroups)
		adminModelGroupGroup.POST("", app.HandleAdminCreateModelGroup)
		adminModelGroupGroup.PUT("/:name", app.HandleAdminUpdateModelGroup)
		adminModelGroupGroup.DELETE("/:name", app.HandleAdminDeleteModelGroup)
	}

	// 主站违禁词审核（6e · §2.14）：全站基础库（tenant_id=0）词库 CRUD + 违规日志（当前 Host 租户）。
	// 前置 TenantMiddleware（违规日志按 Host 租户隔离）+ new-api AdminAuth。
	adminModerationGroup := router.Group("/api/admin/moderation")
	adminModerationGroup.Use(app.TenantMiddleware(), middleware.AdminAuth())
	{
		adminModerationGroup.GET("/words", app.HandleAdminListModerationWords)
		adminModerationGroup.POST("/words", app.HandleAdminUpsertModerationWord)
		adminModerationGroup.DELETE("/words/:id", app.HandleAdminDeleteModerationWord)
		adminModerationGroup.GET("/violations", app.HandleAdminListViolations)
	}

	common.SysLog("multitenant (tenant + tokenplan + agent) routes registered")
}
