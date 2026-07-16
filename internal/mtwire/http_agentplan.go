package mtwire

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/agentplan"
	"github.com/QuantumNous/new-api/internal/payment"
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
	ID             int64   `json:"id"`
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
		ID:             p.ID,
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

// ---- 购买（控制台，需登录）----

// purchaseAgentPlanIn 是 POST /api/tenant/agent-plans/:id/purchase 入参。
type purchaseAgentPlanIn struct {
	Provider string `json:"provider"` // wxpay|alipay（默认 wxpay）
	Slug     string `json:"slug"`     // 新代理子域名/标识（已是代理则忽略）
	Name     string `json:"name"`     // 站点名（已是代理则忽略）
}

// HandlePurchaseAgentPlan POST /api/tenant/agent-plans/:id/purchase —— 购买代理套餐（下单 + 出支付凭据）。
// 需 UserAuth。支付成功后平台回调按 AGT 前缀分发到 ActivatePaidAgentPlanOrder：开通/升级代理 + 记到期。
func (a *App) HandlePurchaseAgentPlan(c *gin.Context) {
	planID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		respondErr(c, agentplan.ErrPlanNotFound)
		return
	}
	ctx := reqCtx(c)
	plan, err := a.AgentCatalog.Get(ctx, planID)
	if err != nil {
		respondErr(c, err)
		return
	}
	if !plan.Status.IsEnabled() {
		respondErr(c, agentplan.ErrPlanDisabled)
		return
	}
	var body purchaseAgentPlanIn
	_ = c.ShouldBindJSON(&body) // body 可选

	provider := payment.ProviderWxpay
	if body.Provider != "" {
		provider = payment.Provider(body.Provider)
	}
	if !provider.Valid() {
		respondErr(c, payment.ErrOrderInvalid)
		return
	}
	if err := a.ensureProviderUsable(ctx, provider); err != nil {
		respondErr(c, err)
		return
	}
	userID := int64(c.GetInt("id"))
	if userID <= 0 {
		respondErr(c, payment.ErrOrderInvalid)
		return
	}
	// 付款前 fail-closed 校验 slug（对齐 tokenplan HandlePurchase：先校验再出支付凭据）。非法/保留/占用
	// 直接返回 SLUG_INVALID/SLUG_RESERVED/SLUG_DUPLICATE，绝不落 AGT 订单、绝不向平台下单收钱——否则
	// 买家付款后回调激活才校验 slug，钱已离账却确定性永久激活失败、订单永停 pending（付款黑洞）。
	if err := a.precheckAgentPurchaseSlug(ctx, userID, body.Slug); err != nil {
		respondErr(c, err)
		return
	}

	orderNo := AgentPlanOrderPrefix + strings.ToUpper(randToken(12))
	now := time.Now()
	if err := newAgentPlanOrderStore(a.DB).create(ctx, &agentPlanOrderRow{
		OrderNo:            orderNo,
		OwnerUserID:        userID,
		PlanID:             plan.ID,
		AmountCNY:          plan.Price,
		Provider:           string(provider),
		Status:             agtOrderPending,
		GrantLevel:         plan.GrantLevel,
		GrantCanAPI:        plan.GrantCanAPI,
		GrantDiscountRatio: plan.GrantDiscountRatio,
		ValidDays:          plan.ValidDays,
		Slug:               body.Slug,
		Name:               body.Name,
		PlanCode:           plan.Code,
		CreatedAt:          now,
		UpdatedAt:          now,
	}); err != nil {
		respondErr(c, err)
		return
	}

	payURL, err := a.agentPlanPayURL(ctx, orderNo, plan, provider)
	if err != nil {
		respondErr(c, err)
		return
	}
	pay := gin.H{}
	switch provider {
	case payment.ProviderWxpay:
		pay["wxpay_qr"] = payURL // 前端渲染二维码
	case payment.ProviderAlipay:
		pay["alipay_url"] = payURL // 前端跳转
	}
	respondOK(c, gin.H{
		"order_no":   orderNo,
		"pay_url":    payURL,
		"amount_cny": plan.Price,
		"plan_id":    plan.ID,
		"pay":        pay,
	})
}

// agentPlanPayURL 为一笔 AGT 订单向真实平台进程内下单取回支付凭据（复用 RCG/SUB 同一 providerManager）。
// providerMgr 未装配时回退占位 URL，保证可跑不 panic。
func (a *App) agentPlanPayURL(ctx context.Context, orderNo string, plan *agentplan.Plan, provider payment.Provider) (string, error) {
	if a.providerMgr == nil {
		return "/console/agent-plan/pay?order=" + orderNo, nil
	}
	notifyURL := resolveNotifyBase() + notifyPathFor(provider)
	return a.providerMgr.CreatePay(ctx, provider, orderNo, "开通代理套餐 "+plan.Name, plan.Price, notifyURL)
}
