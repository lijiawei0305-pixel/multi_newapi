package mtwire

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/agentplan"
)

// ============================ 代理套餐（agentplan）端点 ============================
//
// 管理员 CRUD（/api/admin/agent-plans，AdminAuth）+ 公开只读展示（/api/agent-plans/public，无需登录）。
// 购买/激活在 P3（AGT 订单 → 支付回调 → SetAgentType + 记到期），此文件仅目录读写与公开展示。

// ---- 管理端 DTO ----

// adminAgentPlanOut 是管理端代理套餐条目（含授予能力等内部字段）。
type adminAgentPlanOut struct {
	ID                 int64   `json:"id"`
	Code               string  `json:"code"`
	Name               string  `json:"name"`
	Desc               string  `json:"description"`
	PriceCNY           float64 `json:"price_cny"`
	AnchorPriceCNY     float64 `json:"anchor_price_cny"`
	DiscountLabel      string  `json:"discount_label"`
	GrantLevel         int     `json:"grant_level"`
	GrantCanAPI        bool    `json:"grant_can_api"`
	GrantDiscountRatio float64 `json:"grant_discount_ratio"`
	ValidDays          int     `json:"valid_days"`
	IsRecommended      bool    `json:"is_recommended"`
	Badge              string  `json:"badge"`
	Sort               int     `json:"sort"`
	Status             string  `json:"status"`
}

func toAdminAgentPlanOut(p agentplan.Plan) adminAgentPlanOut {
	return adminAgentPlanOut{
		ID:                 p.ID,
		Code:               p.Code,
		Name:               p.Name,
		Desc:               p.Desc,
		PriceCNY:           p.Price,
		AnchorPriceCNY:     p.AnchorPrice,
		DiscountLabel:      p.DiscountLabel,
		GrantLevel:         p.GrantLevel,
		GrantCanAPI:        p.GrantCanAPI,
		GrantDiscountRatio: p.GrantDiscountRatio,
		ValidDays:          p.ValidDays,
		IsRecommended:      p.IsRecommended,
		Badge:              p.Badge,
		Sort:               p.Sort,
		Status:             string(p.Status),
	}
}

// adminAgentPlanIn 是管理员创建/更新入参（指针字段 → 支持部分更新，缺省沿用既有值）。
type adminAgentPlanIn struct {
	Code               *string  `json:"code"`
	Name               *string  `json:"name"`
	Desc               *string  `json:"description"`
	PriceCNY           *float64 `json:"price_cny"`
	AnchorPriceCNY     *float64 `json:"anchor_price_cny"`
	DiscountLabel      *string  `json:"discount_label"`
	GrantLevel         *int     `json:"grant_level"`
	GrantCanAPI        *bool    `json:"grant_can_api"`
	GrantDiscountRatio *float64 `json:"grant_discount_ratio"`
	ValidDays          *int     `json:"valid_days"`
	IsRecommended      *bool    `json:"is_recommended"`
	Badge              *string  `json:"badge"`
	Sort               *int     `json:"sort"`
	Status             *string  `json:"status"`
}

// agentPlanToInput 把领域对象铺平为更新基线（PATCH 缺省字段沿用既有值）。
func agentPlanToInput(p agentplan.Plan) agentplan.PlanInput {
	return agentplan.PlanInput{
		Code:               p.Code,
		Name:               p.Name,
		Desc:               p.Desc,
		Price:              p.Price,
		AnchorPrice:        p.AnchorPrice,
		DiscountLabel:      p.DiscountLabel,
		GrantLevel:         p.GrantLevel,
		GrantCanAPI:        p.GrantCanAPI,
		GrantDiscountRatio: p.GrantDiscountRatio,
		ValidDays:          p.ValidDays,
		IsRecommended:      p.IsRecommended,
		Badge:              p.Badge,
		Sort:               p.Sort,
		Status:             p.Status,
	}
}

func (in adminAgentPlanIn) applyTo(base agentplan.PlanInput) agentplan.PlanInput {
	if in.Code != nil {
		base.Code = *in.Code
	}
	if in.Name != nil {
		base.Name = *in.Name
	}
	if in.Desc != nil {
		base.Desc = *in.Desc
	}
	if in.PriceCNY != nil {
		base.Price = *in.PriceCNY
	}
	if in.AnchorPriceCNY != nil {
		base.AnchorPrice = *in.AnchorPriceCNY
	}
	if in.DiscountLabel != nil {
		base.DiscountLabel = *in.DiscountLabel
	}
	if in.GrantLevel != nil {
		base.GrantLevel = *in.GrantLevel
	}
	if in.GrantCanAPI != nil {
		base.GrantCanAPI = *in.GrantCanAPI
	}
	if in.GrantDiscountRatio != nil {
		base.GrantDiscountRatio = *in.GrantDiscountRatio
	}
	if in.ValidDays != nil {
		base.ValidDays = *in.ValidDays
	}
	if in.IsRecommended != nil {
		base.IsRecommended = *in.IsRecommended
	}
	if in.Badge != nil {
		base.Badge = *in.Badge
	}
	if in.Sort != nil {
		base.Sort = *in.Sort
	}
	if in.Status != nil {
		base.Status = agentplan.PlanStatus(*in.Status)
	}
	return base
}

// ---- 管理端 handlers ----

// HandleAdminListAgentPlans GET /api/admin/agent-plans —— 列出全部代理套餐。需 AdminAuth。
func (a *App) HandleAdminListAgentPlans(c *gin.Context) {
	plans, err := a.AgentCatalog.List(reqCtx(c))
	if err != nil {
		respondErr(c, err)
		return
	}
	out := make([]adminAgentPlanOut, 0, len(plans))
	for _, p := range plans {
		out = append(out, toAdminAgentPlanOut(p))
	}
	respondOK(c, out)
}

// HandleAdminCreateAgentPlan POST /api/admin/agent-plans —— 新建代理套餐。需 AdminAuth。
func (a *App) HandleAdminCreateAgentPlan(c *gin.Context) {
	var in adminAgentPlanIn
	if err := c.ShouldBindJSON(&in); err != nil {
		respondErr(c, agentplan.ErrPlanInputInvalid)
		return
	}
	// 新建缺省：enabled、有效期 365 天（未显式给出时）。
	p, err := a.AgentCatalog.Create(reqCtx(c), in.applyTo(agentplan.PlanInput{
		Status:    agentplan.PlanEnabled,
		ValidDays: 365,
	}))
	if err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, toAdminAgentPlanOut(*p))
}

// HandleAdminUpdateAgentPlan PATCH /api/admin/agent-plans/:id —— 全量更新（含上下架状态）。需 AdminAuth。
func (a *App) HandleAdminUpdateAgentPlan(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		respondErr(c, agentplan.ErrPlanNotFound)
		return
	}
	var in adminAgentPlanIn
	if err := c.ShouldBindJSON(&in); err != nil {
		respondErr(c, agentplan.ErrPlanInputInvalid)
		return
	}
	existing, err := a.AgentCatalog.Get(reqCtx(c), id)
	if err != nil {
		respondErr(c, err)
		return
	}
	if err := a.AgentCatalog.Update(reqCtx(c), id, in.applyTo(agentPlanToInput(*existing))); err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, gin.H{"id": id})
}

// ---- 公开只读展示 ----

// publicAgentPlanOut 是公开代理套餐卡片（仅展示字段；不含 grant_discount_ratio 等内部成本口径）。
// grant_level / grant_can_api 作为「能力标识」保留，供前端展示「独立域名 / 开放 API」等卖点。
type publicAgentPlanOut struct {
	Code           string  `json:"code"`
	Name           string  `json:"name"`
	Desc           string  `json:"description"`
	PriceCNY       float64 `json:"price_cny"`
	AnchorPriceCNY float64 `json:"anchor_price_cny"`
	DiscountLabel  string  `json:"discount_label"`
	Badge          string  `json:"badge"`
	IsRecommended  bool    `json:"is_recommended"`
	ValidDays      int     `json:"valid_days"`
	GrantLevel     int     `json:"grant_level"`
	GrantCanAPI    bool    `json:"grant_can_api"`
	Sort           int     `json:"sort"`
}

func toPublicAgentPlanOut(p agentplan.Plan) publicAgentPlanOut {
	return publicAgentPlanOut{
		Code:           p.Code,
		Name:           p.Name,
		Desc:           p.Desc,
		PriceCNY:       p.Price,
		AnchorPriceCNY: p.AnchorPrice,
		DiscountLabel:  p.DiscountLabel,
		Badge:          p.Badge,
		IsRecommended:  p.IsRecommended,
		ValidDays:      p.ValidDays,
		GrantLevel:     p.GrantLevel,
		GrantCanAPI:    p.GrantCanAPI,
		Sort:           p.Sort,
	}
}

// HandleListPublicAgentPlans GET /api/agent-plans/public —— 主站已上架代理套餐（公开只读、无需登录）。
// 供公开落地页（代理加盟）动态展示价目。仅返回 enabled 套餐的展示字段。
func (a *App) HandleListPublicAgentPlans(c *gin.Context) {
	plans, err := a.AgentCatalog.List(reqCtx(c))
	if err != nil {
		respondErr(c, err)
		return
	}
	out := make([]publicAgentPlanOut, 0, len(plans))
	for _, p := range plans {
		if !p.Status.IsEnabled() {
			continue
		}
		out = append(out, toPublicAgentPlanOut(p))
	}
	respondOK(c, out)
}
