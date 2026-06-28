package mtwire

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/platform/appctx"
	"github.com/QuantumNous/new-api/internal/platform/apperr"
	"github.com/QuantumNous/new-api/internal/tenant"
	"github.com/QuantumNous/new-api/internal/tokenplan"
)

// ginKeyTenant 是 TenantMiddleware 注入解析结果的 gin ctx 键。
const ginKeyTenant = "mt_tenant"

// TenantMiddleware 按 Host 解析租户并注入请求上下文：
//   - 命中（含 demo 域名）：写 gin ctx 与 request context（appctx.Principal.TenantID）；
//   - 未知 Host / 保留 Host（如主站 api.wedreamhub.com）：放行不阻断，由各 handler 自行判定缺租户。
//
// 解析缓存写后失效顺延（Redis 适配）；本中间件只读不写。
func (a *App) TenantMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		t, err := a.TenantResolver.ResolveByHost(c.Request.Context(), c.Request.Host)
		if err == nil && t != nil {
			c.Set(ginKeyTenant, t)
			c.Request = c.Request.WithContext(tenant.ContextWithTenant(c.Request.Context(), t))
		}
		c.Next()
	}
}

// tenantFrom 读取中间件注入的租户；缺失返回 nil。
func tenantFrom(c *gin.Context) *tenant.Tenant {
	if v, ok := c.Get(ginKeyTenant); ok {
		if t, ok := v.(*tenant.Tenant); ok {
			return t
		}
	}
	return nil
}

// principalFrom 组装请求级 Principal：UserID/Role 取自 new-api 鉴权写入 gin ctx 的 id/role，
// TenantID 取自 Host 中间件。new-api 无 agent_owner 角色，故仅映射 admin / user。
func principalFrom(c *gin.Context) appctx.Principal {
	p := appctx.Principal{
		UserID: int64(c.GetInt("id")),
		Role:   appctx.RoleUser,
	}
	if c.GetInt("role") >= common.RoleAdminUser {
		p.Role = appctx.RoleAdmin
	}
	if t := tenantFrom(c); t != nil {
		p.TenantID = t.ID
	}
	return p
}

// reqCtx 返回带完整 Principal 的请求上下文（供 ctx 感知的下游做 scopeByTenant）。
func reqCtx(c *gin.Context) context.Context {
	return appctx.WithPrincipal(c.Request.Context(), principalFrom(c))
}

// ---- 统一响应（对齐 new-api 的 {success,message,data} 形态）----

func respondOK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": data})
}

// respondErr 用 AppError 建议的 HTTP 状态返回，并带稳定错误码，便于前端按码处理。
func respondErr(c *gin.Context, err error) {
	c.JSON(apperr.HTTPStatusOf(err), gin.H{
		"success": false,
		"message": humanMessage(err),
		"code":    apperr.CodeOf(err),
	})
}

// humanMessage 提取面向用户的描述；AppError 用其 Msg，否则回退 Error()。
func humanMessage(err error) string {
	var ae *apperr.AppError
	if errors.As(err, &ae) {
		return ae.Msg
	}
	return err.Error()
}

// ============================ 租户控制台（Host 维度） ============================

// HandleTenantCurrent GET /api/tenant/current —— 按 Host 返当前租户品牌（无需登录）。
func (a *App) HandleTenantCurrent(c *gin.Context) {
	t := tenantFrom(c)
	if t == nil {
		respondErr(c, tenant.ErrTenantNotFound)
		return
	}
	respondOK(c, gin.H{
		"id":                t.ID,
		"slug":              t.Slug,
		"site_name":         t.Name, // 一期品牌名取租户名；主题色/Logo 等装修字段顺延 siteconfig
		"status":            string(t.Status),
		"tokenplan_enabled": t.TokenplanEnabled,
	})
}

// HandleListTokenPlans GET /api/tenant/token-plans —— 当前租户已上架套餐（含零售价）。需 UserAuth。
func (a *App) HandleListTokenPlans(c *gin.Context) {
	t := tenantFrom(c)
	if t == nil {
		respondErr(c, tenant.ErrTenantNotFound)
		return
	}
	views, err := a.Retail.ListForTenant(reqCtx(c), t.ID)
	if err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, views)
}

// HandlePurchase POST /api/tenant/token-plans/:id/purchase —— 下单（返回支付凭据）。需 UserAuth。
// 可选 JSON body：{"device_id","real_name_id"}（Trial 限购维度，本阶段风控放行）。
func (a *App) HandlePurchase(c *gin.Context) {
	t := tenantFrom(c)
	if t == nil {
		respondErr(c, tenant.ErrTenantNotFound)
		return
	}
	planID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		respondErr(c, tokenplan.ErrPlanNotFound)
		return
	}
	var body struct {
		DeviceID   string `json:"device_id"`
		RealNameID string `json:"real_name_id"`
	}
	_ = c.ShouldBindJSON(&body) // body 可选

	ticket, err := a.Subscriptions.Purchase(reqCtx(c), tokenplan.PurchaseInput{
		TenantID:   t.ID,
		UserID:     int64(c.GetInt("id")),
		PlanID:     planID,
		DeviceID:   body.DeviceID,
		RealNameID: body.RealNameID,
	})
	if err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, ticket)
}

// HandleListSubscriptions GET /api/tenant/subscriptions —— 当前用户在本租户的订阅（含历史）。需 UserAuth。
func (a *App) HandleListSubscriptions(c *gin.Context) {
	t := tenantFrom(c)
	if t == nil {
		respondErr(c, tenant.ErrTenantNotFound)
		return
	}
	subs, err := a.TokenPlanRepo.ListSubscriptionsByUser(reqCtx(c), t.ID, int64(c.GetInt("id")), time.Now())
	if err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, subs)
}

// ============================ 主站管理（全局套餐目录） ============================

// ---- Phase 2 · 6a 前端契约对齐：管理端套餐 JSON 用 snake_case（*_cny/*_usd），状态 enabled/disabled ----

type adminPlanOut struct {
	ID             int64   `json:"id"`
	Code           string  `json:"code"`
	Name           string  `json:"name"`
	BasePriceCNY   float64 `json:"base_price_cny"`
	AnchorPriceCNY float64 `json:"anchor_price_cny"`
	CostPriceCNY   float64 `json:"cost_price_cny"`
	MinPriceCNY    float64 `json:"min_price_cny"`
	MonthLimitUSD  float64 `json:"month_limit_usd"`
	Multiplier     float64 `json:"multiplier"`
	ValidDays      int     `json:"valid_days"`
	DiscountLabel  string  `json:"discount_label,omitempty"`
	Badge          string  `json:"badge,omitempty"`
	IsRecommended  bool    `json:"is_recommended"`
	Sort           int     `json:"sort"`
	Status         string  `json:"status"`
}

func toAdminPlanOut(p tokenplan.Plan) adminPlanOut {
	return adminPlanOut{
		ID: p.ID, Code: p.Code, Name: p.Name,
		BasePriceCNY: p.BasePrice, AnchorPriceCNY: p.AnchorPrice,
		CostPriceCNY: p.AgentCostPrice, MinPriceCNY: p.MinPrice,
		MonthLimitUSD: p.MonthLimitUSD, Multiplier: p.Multiplier, ValidDays: p.ValidDays,
		DiscountLabel: p.DiscountLabel, Badge: p.Badge, IsRecommended: p.IsRecommended,
		Sort: p.Sort, Status: string(p.Status),
	}
}

// adminPlanIn 用指针字段支持 PATCH 局部更新（如仅传 status 的上下架切换）。
type adminPlanIn struct {
	Code           *string  `json:"code"`
	Name           *string  `json:"name"`
	BasePriceCNY   *float64 `json:"base_price_cny"`
	AnchorPriceCNY *float64 `json:"anchor_price_cny"`
	CostPriceCNY   *float64 `json:"cost_price_cny"`
	MinPriceCNY    *float64 `json:"min_price_cny"`
	MonthLimitUSD  *float64 `json:"month_limit_usd"`
	Multiplier     *float64 `json:"multiplier"`
	ValidDays      *int     `json:"valid_days"`
	DiscountLabel  *string  `json:"discount_label"`
	Badge          *string  `json:"badge"`
	IsRecommended  *bool    `json:"is_recommended"`
	Sort           *int     `json:"sort"`
	Status         *string  `json:"status"`
}

func planToInput(p tokenplan.Plan) tokenplan.PlanInput {
	return tokenplan.PlanInput{
		Code: p.Code, Name: p.Name, BasePrice: p.BasePrice, AnchorPrice: p.AnchorPrice,
		DiscountLabel: p.DiscountLabel, Multiplier: p.Multiplier, MonthLimitUSD: p.MonthLimitUSD,
		ValidDays: p.ValidDays, UpstreamCostEst: p.UpstreamCostEst, AgentCostPrice: p.AgentCostPrice,
		MinPrice: p.MinPrice, IsRecommended: p.IsRecommended, Badge: p.Badge, Sort: p.Sort, Status: p.Status,
	}
}

func (in adminPlanIn) applyTo(base tokenplan.PlanInput) tokenplan.PlanInput {
	if in.Code != nil {
		base.Code = *in.Code
	}
	if in.Name != nil {
		base.Name = *in.Name
	}
	if in.BasePriceCNY != nil {
		base.BasePrice = *in.BasePriceCNY
	}
	if in.AnchorPriceCNY != nil {
		base.AnchorPrice = *in.AnchorPriceCNY
	}
	if in.CostPriceCNY != nil {
		base.AgentCostPrice = *in.CostPriceCNY
	}
	if in.MinPriceCNY != nil {
		base.MinPrice = *in.MinPriceCNY
	}
	if in.MonthLimitUSD != nil {
		base.MonthLimitUSD = *in.MonthLimitUSD
	}
	if in.Multiplier != nil {
		base.Multiplier = *in.Multiplier
	}
	if in.ValidDays != nil {
		base.ValidDays = *in.ValidDays
	}
	if in.DiscountLabel != nil {
		base.DiscountLabel = *in.DiscountLabel
	}
	if in.Badge != nil {
		base.Badge = *in.Badge
	}
	if in.IsRecommended != nil {
		base.IsRecommended = *in.IsRecommended
	}
	if in.Sort != nil {
		base.Sort = *in.Sort
	}
	if in.Status != nil {
		base.Status = tokenplan.PlanStatus(*in.Status)
	}
	return base
}

// HandleAdminListPlans GET /api/admin/token-plans —— 列出全部套餐定义。需 AdminAuth。
func (a *App) HandleAdminListPlans(c *gin.Context) {
	plans, err := a.Catalog.List(reqCtx(c))
	if err != nil {
		respondErr(c, err)
		return
	}
	out := make([]adminPlanOut, 0, len(plans))
	for _, p := range plans {
		out = append(out, toAdminPlanOut(p))
	}
	respondOK(c, out)
}

// HandleAdminCreatePlan POST /api/admin/token-plans —— 新建套餐。需 AdminAuth。
func (a *App) HandleAdminCreatePlan(c *gin.Context) {
	var in adminPlanIn
	if err := c.ShouldBindJSON(&in); err != nil {
		respondErr(c, tokenplan.ErrPlanInputInvalid)
		return
	}
	p, err := a.Catalog.Create(reqCtx(c), in.applyTo(tokenplan.PlanInput{}))
	if err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, toAdminPlanOut(*p))
}

// HandleAdminUpdatePlan PATCH /api/admin/token-plans/:id —— 全量更新套餐（含上下架状态）。需 AdminAuth。
func (a *App) HandleAdminUpdatePlan(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		respondErr(c, tokenplan.ErrPlanNotFound)
		return
	}
	var in adminPlanIn
	if err := c.ShouldBindJSON(&in); err != nil {
		respondErr(c, tokenplan.ErrPlanInputInvalid)
		return
	}
	existing, err := a.Catalog.Get(reqCtx(c), id)
	if err != nil {
		respondErr(c, err)
		return
	}
	if err := a.Catalog.Update(reqCtx(c), id, in.applyTo(planToInput(*existing))); err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, gin.H{"id": id})
}
