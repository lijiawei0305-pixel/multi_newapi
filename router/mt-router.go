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

	// 安装代理增量旁路钩子（消耗分润 / 注册归属）。所有节点都装（钩子由原生 service/controller 调用）。
	app.InstallHooks()
	// 安装租户用户组倍率覆盖钩子（作用于 /v1 计费的 groupRatio 单一解析点）。所有节点都装。
	app.InstallGroupRatioHook()

	if common.IsMasterNode {
		if err := app.Migrate(); err != nil {
			common.FatalLog("mt-router: AutoMigrate multitenant tables failed: " + err.Error())
			return
		}
		if err := app.Seed(); err != nil {
			// seed 失败不致命：记录后继续启动（路由仍注册）。
			common.SysError("mt-router: seed failed: " + err.Error())
		}
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
		// 用户兑换码（P1-UI-04）：UserAuth + Host 租户（不强制 owner）；单赢家 CAS → 原生 quota 入账。
		tenantGroup.POST("/redeem", middleware.UserAuth(), app.HandleRedeem)
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
			agentSelf.POST("/redemptions", app.HandleAgentCreateRedemptions)
			agentSelf.GET("/redemptions", app.HandleAgentListRedemptions)
			agentSelf.GET("/groups", app.HandleAgentListGroups)
			agentSelf.PUT("/groups/:group", app.HandleAgentSetGroupRatio)
		}
	}

	// 内网入账（目标③）：auth-service 验签后回调，仅内网 + 共享密钥头校验。
	// 安全：nginx 必须拒绝公网访问 /api/internal/*（见 deploy/nginx 配置）；此处不挂 UserAuth/TenantMiddleware。
	internalGroup := router.Group("/api/internal")
	{
		internalGroup.POST("/order/paid", app.HandleInternalOrderPaid)
	}

	// 主站套餐目录管理（全局，非租户维度），复用 new-api AdminAuth。
	adminPlanGroup := router.Group("/api/admin/token-plans")
	adminPlanGroup.Use(middleware.AdminAuth())
	{
		adminPlanGroup.GET("", app.HandleAdminListPlans)
		adminPlanGroup.POST("", app.HandleAdminCreatePlan)
		adminPlanGroup.PATCH("/:id", app.HandleAdminUpdatePlan)
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

	common.SysLog("multitenant (tenant + tokenplan + agent) routes registered")
}
