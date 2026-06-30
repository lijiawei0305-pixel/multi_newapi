package mtwire

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/payment"
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
// TenantID 取自 Host 中间件。角色映射：admin（new-api 管理员）> agent_owner（Host 租户 owner==当前用户）> user。
//
// agent_owner 识别用「Host 解析出的租户.OwnerUserID == 当前 session 用户」。此处取自（可能缓存的）
// 已解析租户，作为下游 ctx 的角色提示；代理自助端点的**权威**鉴权另由 App.AgentOwnerAuth 做直读 DB 校验
// （绕过解析缓存），故角色提示即便因缓存短暂滞后也不影响安全边界。
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
		if p.Role != appctx.RoleAdmin && p.UserID > 0 && t.OwnerUserID == p.UserID {
			p.Role = appctx.RoleAgentOwner
		}
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
	out := make([]buyerPlanOut, 0, len(views))
	for _, v := range views {
		out = append(out, toBuyerPlanOut(v))
	}
	respondOK(c, out)
}

// purchaseRequest 是 POST /api/tenant/token-plans/:id/purchase 入参（全部可选）。
// provider 缺省 wxpay；device_id/real_name_id 为 Trial 限购维度（本阶段风控放行）。
type purchaseRequest struct {
	DeviceID   string `json:"device_id"`
	RealNameID string `json:"real_name_id"`
	Provider   string `json:"provider"` // wxpay | alipay（默认 wxpay）
}

// HandlePurchase POST /api/tenant/token-plans/:id/purchase —— 下单（返回支付凭据）。需 UserAuth。
//
// 流程：校验套餐/上架/限购 → Purchase 落 SUB 待支付订单 + 购买快照 → 像 recharge 一样经
// providerManager 进程内向平台下单拿支付凭据 → 返回 snake_case DTO（与充值响应同形）：
// {order_no, pay_url, amount_cny, plan_id, pay:{wxpay_qr|alipay_url}}。pay_url 与 pay.* 同值：
// 微信端渲染二维码、支付宝端跳转。用户支付 → 平台异步回调 /api/pay/{wechat,alipay}/notify →
// handlePayNotify 验签 → 按 SUB 前缀分发 → ActivatePaidTokenplanOrder（激活原生订阅，链路已就绪）。
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
	var body purchaseRequest
	_ = c.ShouldBindJSON(&body) // body 可选

	provider := payment.ProviderWxpay // 默认微信
	if body.Provider != "" {
		provider = payment.Provider(body.Provider)
	}
	if !provider.Valid() {
		respondErr(c, payment.ErrOrderInvalid)
		return
	}

	ctx := reqCtx(c)
	ticket, err := a.Subscriptions.Purchase(ctx, tokenplan.PurchaseInput{
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

	payURL, err := a.subscriptionPayURL(ctx, ticket, provider)
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
		"order_no":   ticket.OrderID,
		"pay_url":    payURL,
		"amount_cny": ticket.AmountCNY,
		"plan_id":    ticket.PlanID,
		"pay":        pay,
	})
}

// subscriptionPayURL 为一笔已落库的 SUB 套餐订单向真实平台进程内下单，取回支付凭据
// （微信 code_url / 支付宝跳转 URL），复用 RCG 充值同一 providerManager。
// 金额仅人民币（amount_cny=零售价）：套餐额度在激活时按 month_limit_usd 注入原生订阅桶，非充值额度，
// 故下单只传人民币应付额。notify_url 用契约回调路径（base + notifyPathFor(provider)）。
// providerMgr 未装配（如单测直构 App）时回退占位 PayURL，保证可跑不 panic。
func (a *App) subscriptionPayURL(ctx context.Context, ticket *tokenplan.PurchaseTicket, provider payment.Provider) (string, error) {
	// 回填订单支付渠道（供真实回调路由 + 卡单对账主动查单识别渠道）；best-effort，失败不阻断下单。
	if err := newSubOrderStore(a.DB).setProvider(ctx, ticket.OrderID, string(provider)); err != nil {
		common.SysLog("set sub order provider failed: " + err.Error())
	}
	if a.providerMgr == nil {
		return ticket.PayURL, nil
	}
	notifyURL := resolveNotifyBase() + notifyPathFor(provider)
	payURL, err := a.providerMgr.CreatePay(ctx, provider, ticket.OrderID,
		"套餐购买 #"+strconv.FormatInt(ticket.PlanID, 10), ticket.AmountCNY, notifyURL)
	if err != nil {
		return "", err
	}
	return payURL, nil
}

// HandleListSubscriptions GET /api/tenant/subscriptions —— 当前用户在本租户的订阅（含历史）。需 UserAuth。
func (a *App) HandleListSubscriptions(c *gin.Context) {
	t := tenantFrom(c)
	if t == nil {
		respondErr(c, tenant.ErrTenantNotFound)
		return
	}
	ctx := reqCtx(c)
	subs, err := a.TokenPlanRepo.ListSubscriptionsByUser(ctx, t.ID, int64(c.GetInt("id")), time.Now())
	if err != nil {
		respondErr(c, err)
		return
	}
	plans, err := a.planByID(ctx)
	if err != nil {
		respondErr(c, err)
		return
	}
	out := make([]buyerSubOut, 0, len(subs))
	for _, s := range subs {
		p := plans[s.PlanID] // 套餐已删则零值，plan_code/plan_name 留空
		out = append(out, buyerSubOut{
			PlanCode:    p.Code,
			PlanName:    p.Name,
			Status:      string(s.Status),
			UsedUSD:     s.UsedUSD,
			LimitUSD:    s.MonthLimitUSD,
			UsagePct:    usagePct(s.UsedUSD, s.MonthLimitUSD),
			PeriodStart: isoUTC(s.StartAt),
			PeriodEnd:   isoUTC(s.ExpireAt),
			CreatedAt:   isoUTC(s.CreatedAt),
		})
	}
	respondOK(c, out)
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

// ============================ 买家 / 管理端订阅监控 DTO（Phase 2 · 6a） ============================
//
// 前端契约对齐：买家套餐/订阅 + 管理端订阅监控统一用 snake_case，货币后缀区分
// （`*_cny`=人民币、`*_usd`=美元额度，见 doc/api-contract.md §1）；时间为 ISO-8601 UTC。

// buyerPlanOut 是买家「可购套餐」卡片。retail_price_cny 为本租户上架价（未上架回退主站售价）。
type buyerPlanOut struct {
	ID             int64   `json:"id"`
	Code           string  `json:"code"`
	Name           string  `json:"name"`
	RetailPriceCNY float64 `json:"retail_price_cny"`
	AnchorPriceCNY float64 `json:"anchor_price_cny"`
	MonthLimitUSD  float64 `json:"month_limit_usd"`
	ValidDays      int     `json:"valid_days"`
	DiscountLabel  string  `json:"discount_label"`
	Badge          string  `json:"badge"`
	IsRecommended  bool    `json:"is_recommended"`
	Sort           int     `json:"sort"`
}

// toBuyerPlanOut 把代理视角套餐视图映射为买家卡片（零售价取视图 RetailPrice）。
func toBuyerPlanOut(v tokenplan.TenantPlanView) buyerPlanOut {
	return buyerPlanOut{
		ID:             v.Plan.ID,
		Code:           v.Plan.Code,
		Name:           v.Plan.Name,
		RetailPriceCNY: v.RetailPrice,
		AnchorPriceCNY: v.Plan.AnchorPrice,
		MonthLimitUSD:  v.Plan.MonthLimitUSD,
		ValidDays:      v.Plan.ValidDays,
		DiscountLabel:  v.Plan.DiscountLabel,
		Badge:          v.Plan.Badge,
		IsRecommended:  v.Plan.IsRecommended,
		Sort:           v.Plan.Sort,
	}
}

// buyerSubOut 是买家「我的订阅」条目。status 透传领域枚举（active/exhausted/expired/refunded）。
type buyerSubOut struct {
	PlanCode    string  `json:"plan_code"`
	PlanName    string  `json:"plan_name"`
	Status      string  `json:"status"`
	UsedUSD     float64 `json:"used_usd"`
	LimitUSD    float64 `json:"limit_usd"`
	UsagePct    float64 `json:"usage_pct"`
	PeriodStart string  `json:"period_start"`
	PeriodEnd   string  `json:"period_end"`
	CreatedAt   string  `json:"created_at"`
}

// adminSubOut 是管理端「订阅监控」条目（当前租户维度，含满额预警）。
type adminSubOut struct {
	UserID     int64   `json:"user_id"`
	Username   string  `json:"username"`
	PlanCode   string  `json:"plan_code"`
	Status     string  `json:"status"`
	UsedUSD    float64 `json:"used_usd"`
	LimitUSD   float64 `json:"limit_usd"`
	UsagePct   float64 `json:"usage_pct"`
	AlertLevel string  `json:"alert_level"`
	PeriodEnd  string  `json:"period_end"`
}

// HandleAdminListSubscriptions GET /api/admin/subscriptions —— 当前租户全部订阅 + 用量 + 满额预警。
// 需 AdminAuth；租户取自 Host（TenantMiddleware）。只读：不改额度/状态（惰性过期落库由仓储完成）。
func (a *App) HandleAdminListSubscriptions(c *gin.Context) {
	t := tenantFrom(c)
	if t == nil {
		respondErr(c, tenant.ErrTenantNotFound)
		return
	}
	ctx := reqCtx(c)
	subs, err := a.TokenPlanRepo.ListSubscriptionsByTenant(ctx, t.ID, time.Now())
	if err != nil {
		respondErr(c, err)
		return
	}
	plans, err := a.planByID(ctx)
	if err != nil {
		respondErr(c, err)
		return
	}
	names := a.usernamesByIDs(ctx, subUserIDs(subs))
	out := make([]adminSubOut, 0, len(subs))
	for _, s := range subs {
		pct := usagePct(s.UsedUSD, s.MonthLimitUSD)
		out = append(out, adminSubOut{
			UserID:     s.UserID,
			Username:   names[s.UserID], // 取不到给空
			PlanCode:   plans[s.PlanID].Code,
			Status:     string(s.Status),
			UsedUSD:    s.UsedUSD,
			LimitUSD:   s.MonthLimitUSD,
			UsagePct:   pct,
			AlertLevel: alertLevel(pct, s.Status),
			PeriodEnd:  isoUTC(s.ExpireAt),
		})
	}
	respondOK(c, out)
}

// ---- 共享映射辅助（订阅监控/我的订阅复用）----

// planByID 取全部套餐并建 id→Plan 映射，供订阅条目补 plan_code/plan_name。
func (a *App) planByID(ctx context.Context) (map[int64]tokenplan.Plan, error) {
	plans, err := a.Catalog.List(ctx)
	if err != nil {
		return nil, err
	}
	m := make(map[int64]tokenplan.Plan, len(plans))
	for _, p := range plans {
		m[p.ID] = p
	}
	return m, nil
}

// subUserIDs 提取订阅去重后的 user_id 集（供批量回查用户名）。
func subUserIDs(subs []tokenplan.Subscription) []int64 {
	seen := make(map[int64]struct{}, len(subs))
	ids := make([]int64, 0, len(subs))
	for _, s := range subs {
		if _, ok := seen[s.UserID]; ok {
			continue
		}
		seen[s.UserID] = struct{}{}
		ids = append(ids, s.UserID)
	}
	return ids
}

// usernamesByIDs 批量回查 new-api users 表的 id→username（只读、单次 IN 查询，避免 N+1）。
// 直接走共享 *gorm.DB 原始查询，不引入 new-api model 包；查询失败/缺失一律给空（用户名非关键字段）。
func (a *App) usernamesByIDs(ctx context.Context, ids []int64) map[int64]string {
	out := make(map[int64]string, len(ids))
	if len(ids) == 0 {
		return out
	}
	var rows []struct {
		ID       int64
		Username string
	}
	if err := a.DB.WithContext(ctx).
		Table("users").Select("id, username").
		Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return out
	}
	for _, r := range rows {
		out[r.ID] = r.Username
	}
	return out
}

// usagePct 计算用量占比百分比 = used/limit*100（保留两位）；limit<=0 时记 0（防除零，对齐任务口径）。
func usagePct(used, limit float64) float64 {
	if limit > 0 {
		return round2(used / limit * 100)
	}
	return 0
}

// alertLevel 按用量占比分级满额预警：>=100 或已置 exhausted→exhausted；>=95→critical；>=80→warn；否则 none。
// exhausted 状态优先判定：Meter 整笔拒绝时 used 可能略低于 limit，但订阅已终态满额。
func alertLevel(pct float64, status tokenplan.SubStatus) string {
	if status == tokenplan.SubExhausted || pct >= 100 {
		return "exhausted"
	}
	switch {
	case pct >= 95:
		return "critical"
	case pct >= 80:
		return "warn"
	default:
		return "none"
	}
}

// round2 四舍五入到两位小数（用量占比展示用）。
func round2(v float64) float64 { return math.Round(v*100) / 100 }

// isoUTC 把时间格式化为 ISO-8601 UTC（doc/api-contract.md §1）；零值返回空串。
func isoUTC(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
