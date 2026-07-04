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
		// 代理套餐到期降级定时任务（P4，master-only）：周期扫 mt_agent_memberships 到期会员 → 撤销付费授予。
		app.StartAgentPlanExpiryLoop()
	}

	// 租户控制台（Host 维度）。GET /current 公开；其余复用 new-api UserAuth。
	tenantGroup := router.Group("/api/tenant")
	tenantGroup.Use(app.TenantMiddleware())
	{
		tenantGroup.GET("/current", app.HandleTenantCurrent)
		tenantGroup.GET("/token-plans", middleware.UserAuth(), app.HandleListTokenPlans)
		tenantGroup.POST("/token-plans/:id/purchase", middleware.UserAuth(), app.HandlePurchase)
		// 购买代理套餐（P3）：登录用户下单 → realpay 出凭据 → 回调激活开通/升级代理。
		tenantGroup.POST("/agent-plans/:id/purchase", middleware.UserAuth(), app.HandlePurchaseAgentPlan)
		tenantGroup.GET("/subscriptions", middleware.UserAuth(), app.HandleListSubscriptions)
		// 支持工单（用户端）：UserAuth + Host 租户；仅按会话 user_id 隔离（不信任客户端 user_id）。
		// tenant_id 于创建时由服务端从提交用户 users.tenant_id 派生固化，不接受请求体传入。
		tenantGroup.GET("/tickets", middleware.UserAuth(), app.HandleUserListTickets)
		tenantGroup.POST("/tickets", middleware.UserAuth(), app.HandleUserCreateTicket)
		tenantGroup.GET("/tickets/:id", middleware.UserAuth(), app.HandleUserGetTicket)
		tenantGroup.POST("/tickets/:id/replies", middleware.UserAuth(), app.HandleUserReplyTicket)
		tenantGroup.POST("/tickets/:id/close", middleware.UserAuth(), app.HandleUserCloseTicket)
		// 充值下单（目标③）：UserAuth + Host 租户；下单 → 调 auth-service → 返支付凭据。
		tenantGroup.POST("/wallet/recharge", middleware.UserAuth(), app.HandleWalletRecharge)
		// 充值订单状态查询（目标③ 修复）：UserAuth + Host 租户；前端扫码支付后轮询探活，仅本人订单（越权 404）。
		tenantGroup.GET("/wallet/recharge/status", middleware.UserAuth(), app.HandleWalletRechargeStatus)
		// 买家可用充值渠道：UserAuth + Host 租户；返回 enabled && configured 的渠道（wxpay/alipay）。
		tenantGroup.GET("/wallet/recharge/methods", middleware.UserAuth(), app.HandleTenantRechargeMethods)
		// 用户兑换码（P1-UI-04）：UserAuth + Host 租户（不强制 owner）；单赢家 CAS → 原生 quota 入账。
		tenantGroup.POST("/redeem", middleware.UserAuth(), app.HandleRedeem)
		// 代理身份门控信号：UserAuth + Host 租户（**不挂 AgentOwnerAuth**，任何登录用户可调）。
		// 返回 {is_agent_owner}（权威判断 = Host 租户 owner == 当前用户），供前端隐藏代理自助菜单 +
		// 路由 beforeLoad 拦截，避免普通用户/别站代理触发 AGENT_FORBIDDEN。
		tenantGroup.GET("/agent-context", middleware.UserAuth(), app.HandleAgentContext)
		// 代理自助（owner 维度）：UserAuth + AgentOwnerAuthByUser（从登录用户「拥有的租户」解析 agentTenantID，
		// 与 Host 无关 → L0 无子域名也能在主站访问自己的控制台；L1 在子域名同样解析到自己的租户）。
		agentSelf := tenantGroup.Group("", middleware.UserAuth(), app.AgentOwnerAuthByUser())
		{
			agentSelf.POST("/withdrawals", app.HandleAgentRequestWithdrawal)
			agentSelf.GET("/withdrawals", app.HandleAgentListWithdrawals)
			// 收款账户（提现闭环补强 #1）：申请提现前必须先设置，管理员审核/打款据此转账。
			agentSelf.GET("/payout-account", app.HandleAgentGetPayoutAccount)
			agentSelf.PUT("/payout-account", app.HandleAgentSetPayoutAccount)
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
			// 支持工单（代理端）：tenant 取自 AgentOwnerAuth（agentTenantID），绝不接受客户端 tenant_id。
			// 用 /agent/tickets 独立子前缀，避免与用户 /tickets/:id 的 gin 通配路径冲突。
			agentSelf.GET("/agent/tickets", app.HandleAgentListTickets)
			agentSelf.GET("/agent/tickets/:id", app.HandleAgentGetTicket)
			agentSelf.POST("/agent/tickets/:id/replies", app.HandleAgentReplyTicket)
			agentSelf.POST("/agent/tickets/:id/status", app.HandleAgentSetTicketStatus)
			// 独立档能力（level>=1）：自定义域名 + 站点装修。挂 RequireAgentLevel(1)（AgentOwnerAuth 之后）。
			agentIndependent := agentSelf.Group("", app.RequireAgentLevel(1))
			{
				// 自定义域名（OEM，§6.2/§6.3）：绑定 / 查状态 / 触发 TXT 校验 / 解绑（owner + level>=1）。
				agentIndependent.POST("/custom-domain", app.HandleAgentBindCustomDomain)
				agentIndependent.GET("/custom-domain", app.HandleAgentGetCustomDomain)
				agentIndependent.POST("/custom-domain/verify", app.HandleAgentVerifyCustomDomain)
				agentIndependent.DELETE("/custom-domain", app.HandleAgentUnbindCustomDomain)
				// 站点装修（OEM 最小版，§5/§9）：读/改装修配置 + 上传 Logo（owner + level>=1）。
				agentIndependent.GET("/site-config", app.HandleAgentGetSiteConfig)
				agentIndependent.PUT("/site-config", app.HandleAgentUpdateSiteConfig)
				agentIndependent.POST("/site-config/logo", app.HandleAgentUploadLogo)
				// 我的层级折扣力度（代理自设 vip 覆盖；Change 1，spec §9.6.1）：列表 / 设覆盖（仅可代理
				// 覆盖层级——今仅 vip；default 不可覆盖）。route-level level>=1 门禁 + handler 内 ensureAgentLevel
				// 双保险（同自定义域名/站点装修的既有模式）。
				agentIndependent.GET("/tier-ratio", app.HandleAgentListTierRatios)
				agentIndependent.PUT("/tier-ratio/:tier", app.HandleAgentSetTierRatio)
			}
			// 财务报表（代理自助，单租户，tenant_id 取自 AgentOwnerAuth）：汇总 / 趋势 / 明细（?format=csv|pdf 导出）。
			agentSelf.GET("/finance/summary", app.HandleTenantFinanceSummary)
			agentSelf.GET("/finance/trend", app.HandleTenantFinanceTrend)
			agentSelf.GET("/finance/detail", app.HandleTenantFinanceDetail)
		}
	}

	// 内网回写（自定义域名证书签发，§6.5）：共享密钥 X-Internal-Secret 校验（deny-by-default）。
	// Nginx 边界另以 `location ^~ /api/internal/ { return 404; }` 拒绝公网；签发脚本走 127.0.0.1:3100 直连本组。
	internalDomainGroup := router.Group("/api/internal/domain")
	internalDomainGroup.Use(app.InternalSecretAuth())
	{
		internalDomainGroup.GET("/pending-cert", app.HandleInternalListPendingCert) // 列待发证（dns_verified）域名
		internalDomainGroup.POST("/cert-issued", app.HandleInternalCertIssued)      // 证书就绪 → active + 失效缓存
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

	// 主站代理套餐目录管理（购买代理套餐；全局，非租户维度），复用 new-api AdminAuth。
	adminAgentPlanGroup := router.Group("/api/admin/agent-plans")
	adminAgentPlanGroup.Use(middleware.AdminAuth())
	{
		adminAgentPlanGroup.GET("", app.HandleAdminListAgentPlans)
		adminAgentPlanGroup.POST("", app.HandleAdminCreateAgentPlan)
		adminAgentPlanGroup.PATCH("/:id", app.HandleAdminUpdateAgentPlan)
	}

	// 公开代理套餐价目（无需登录）：供公开落地页（代理加盟）动态展示。全局目录，无 TenantMiddleware。
	router.GET("/api/agent-plans/public", app.HandleListPublicAgentPlans)

	// 支付渠道配置已移至系统设置（setting.*Enabled + 凭据，DB option）：渠道启用/凭据由设置页管理，
	// 买家可用渠道经 GET /api/tenant/wallet/recharge/methods 暴露（configured 进程内判断），故此处无独立管理路由。

	// 支付卡单对账（兜底）管理：列当前卡单 + 手动立即对账。复用 new-api AdminAuth。
	adminReconcileGroup := router.Group("/api/admin/reconcile")
	adminReconcileGroup.Use(middleware.AdminAuth())
	{
		adminReconcileGroup.GET("/stuck", app.HandleAdminListStuck)
		adminReconcileGroup.POST("/run", app.HandleAdminRunReconcile)
		adminReconcileGroup.GET("/history", app.HandleAdminListHistory)
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
		adminAgentGroup.PUT("/:id/domain", app.HandleAdminSetAgentDomain) // 设子域名（label → <label>.wedreamhub.com）
		adminAgentGroup.DELETE("/:id", app.HandleAdminDeleteAgent)        // 删除代理（归档软删 + 迁用户回主站）
		adminAgentGroup.GET("/:id/metrics", app.HandleAdminAgentMetrics)  // 升档决策指标（只读）
	}

	// 主站财务报表（全局跨租户，非 Host 维度）：汇总 / 趋势 / 代理排行 / 明细（明细支持 ?format=csv|pdf 导出）。
	// 仅 AdminAuth，不挂 TenantMiddleware（排行跨租户 GROUP BY tenant_id，消耗/汇总均排除 tenant_id=0）。
	financeAdminGroup := router.Group("/api/admin/finance")
	financeAdminGroup.Use(middleware.AdminAuth())
	{
		financeAdminGroup.GET("/summary", app.HandleAdminFinanceSummary)
		financeAdminGroup.GET("/trend", app.HandleAdminFinanceTrend)
		financeAdminGroup.GET("/net-trend", app.HandleAdminNetIncomeTrend) // 净收入趋势（套餐净/api净，总净前端相加）
		financeAdminGroup.GET("/agents", app.HandleAdminFinanceAgents)
		financeAdminGroup.GET("/detail", app.HandleAdminFinanceDetail)
	}

	// 主站提现审核（全局，非租户维度）：列表 / 通过 / 拒绝。复用 new-api AdminAuth。
	adminWithdrawGroup := router.Group("/api/admin/withdrawals")
	adminWithdrawGroup.Use(middleware.AdminAuth())
	{
		adminWithdrawGroup.GET("", app.HandleAdminListWithdrawals)
		adminWithdrawGroup.POST("/:id/approve", app.HandleAdminApproveWithdrawal)
		adminWithdrawGroup.POST("/:id/reject", app.HandleAdminRejectWithdrawal)
		// 标记已打款（提现闭环补强 #2）：approved→paid，CAS + 扣减冻结（真正出账）+ 记打款单号/时间。
		adminWithdrawGroup.POST("/:id/mark-paid", app.HandleAdminMarkPaidWithdrawal)
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

	// 主站自定义域名管理（全局，非租户维度，§6）：跨租户查看所有代理站自定义域名 + 强制解绑。复用 new-api AdminAuth。
	adminCustomDomainGroup := router.Group("/api/admin/custom-domains")
	adminCustomDomainGroup.Use(middleware.AdminAuth())
	{
		adminCustomDomainGroup.GET("", app.HandleAdminListCustomDomains)
		adminCustomDomainGroup.DELETE("/:id", app.HandleAdminUnbindCustomDomain)
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

	// 主站支持工单（全局跨租户，非 Host 维度）：列表/详情/回复/状态。仅 AdminAuth，不挂 TenantMiddleware
	// （跨租户查看所有工单，tenant_id 仅作可选筛选；镜像 financeAdminGroup）。
	adminTicketGroup := router.Group("/api/admin/tickets")
	adminTicketGroup.Use(middleware.AdminAuth())
	{
		adminTicketGroup.GET("", app.HandleAdminListTickets)
		adminTicketGroup.GET("/:id", app.HandleAdminGetTicket)
		adminTicketGroup.POST("/:id/replies", app.HandleAdminReplyTicket)
		adminTicketGroup.POST("/:id/status", app.HandleAdminSetTicketStatus)
	}

	common.SysLog("multitenant (tenant + tokenplan + agent) routes registered")
}
