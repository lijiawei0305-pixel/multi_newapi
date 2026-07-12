package mtwire

// 主站管理 HTTP handler（AdminAuth；非租户维度）：设代理/列表/改代理/删代理/指标 + 提现审核。

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/agent"
	"github.com/QuantumNous/new-api/internal/pricing"
	"github.com/QuantumNous/new-api/internal/tenant"
)

type agentOut struct {
	ID               int64   `json:"id"` // = tenant_id
	OwnerUserID      int64   `json:"owner_user_id"`
	OwnerUsername    string  `json:"owner_username"`
	Slug             string  `json:"slug"`
	Name             string  `json:"name"`
	Level            int     `json:"level"`
	CanAPI           bool    `json:"can_api"`
	CostPriceCNY     float64 `json:"cost_price_cny"`
	PackageDiscount  float64 `json:"package_discount"`
	CommissionRatio  float64 `json:"commission_ratio"`
	BottomPriceRatio float64 `json:"bottom_price_ratio"`
	DiscountRatio    float64 `json:"discount_ratio"`
	Status           string  `json:"status"`
	Subdomain        string  `json:"subdomain"`
	WithdrawableCNY  float64 `json:"withdrawable_cny"`
	FrozenCNY        float64 `json:"frozen_cny"`
	TotalEarnedCNY   float64 `json:"total_earned_cny"`
}

// agentMetricsOut 是 GET /api/admin/agents/:id/metrics 响应：代理升档决策的只读指标
// （总充值 / 累计分润 / 下级用户数），复用 reportrepo 财务聚合 + 一条下级计数薄查询。
type agentMetricsOut struct {
	RechargeTotalCNY    float64 `json:"recharge_total_cny"`
	CommissionEarnedCNY float64 `json:"commission_earned_cny"`
	DownstreamUserCount int64   `json:"downstream_user_count"`
}

// agentCreateIn 是 POST /api/admin/agents 入参。
type agentCreateIn struct {
	Slug             string  `json:"slug"`
	Name             string  `json:"name"`
	OwnerUserID      int64   `json:"owner_user_id"`
	Level            int     `json:"level"`
	CostPriceCNY     float64 `json:"cost_price_cny"`
	PackageDiscount  float64 `json:"package_discount"`
	CommissionRatio  float64 `json:"commission_ratio"`
	DiscountFloor    float64 `json:"discount_floor"`
	BottomPriceRatio float64 `json:"bottom_price_ratio"`
	DiscountRatio    float64 `json:"discount_ratio"`
}

// agentPatchIn 是 PATCH /api/admin/agents/:id 入参（指针支持局部更新）。
type agentPatchIn struct {
	Name             *string  `json:"name"`
	Level            *int     `json:"level"`
	CostPriceCNY     *float64 `json:"cost_price_cny"`
	PackageDiscount  *float64 `json:"package_discount"`
	CommissionRatio  *float64 `json:"commission_ratio"`
	DiscountFloor    *float64 `json:"discount_floor"`
	BottomPriceRatio *float64 `json:"bottom_price_ratio"`
	DiscountRatio    *float64 `json:"discount_ratio"`
	Status           *string  `json:"status"`
}

// ============================================================================
// 主站管理：设代理 / 列表 / 改代理（AdminAuth；非租户维度）
// ============================================================================

// HandleAdminCreateAgent POST /api/admin/agents —— 设代理。需 AdminAuth。
//
// 流程：① 校验参数/折扣（pricing.Guard，失败即返回，不建租户）；② 校验 owner 用户存在且未占用
// （1:1）；③ 建租户（复用 tenant.Create，自动派生域名）；④ 写 owner_user_id + agent_profile + 钱包。
// 注：③④ 跨仓储非单一 DB 事务（已前置强校验把常见失败挡在建租户前，详见报告「风险/未决」）。
func (a *App) HandleAdminCreateAgent(c *gin.Context) {
	var in agentCreateIn
	if err := c.ShouldBindJSON(&in); err != nil {
		respondErr(c, errAgentInputInvalid)
		return
	}
	params := agent.AgentParams{
		UserID:           in.OwnerUserID,
		CostPrice:        in.CostPriceCNY,
		PackageDiscount:  in.PackageDiscount,
		CommissionRatio:  in.CommissionRatio,
		Level:            in.Level,
		DiscountFloor:    in.DiscountFloor,
		BottomPriceRatio: in.BottomPriceRatio,
		DiscountRatio:    in.DiscountRatio,
	}
	// ① 前置强校验（纯函数，不写库）：参数 + 折扣保护线。
	if err := params.Validate(); err != nil {
		respondErr(c, err)
		return
	}
	if err := pricing.NewGuard().ValidateGroupRatio(params.PackageDiscount, params.DiscountFloor); err != nil {
		respondErr(c, err)
		return
	}
	ctx := reqCtx(c)
	// ② owner 用户存在性 + 1:1 占用校验。
	if in.OwnerUserID <= 0 || !a.userExists(ctx, in.OwnerUserID) {
		respondErr(c, errAgentUserNotFound)
		return
	}
	if a.ownerTaken(ctx, in.OwnerUserID, 0) {
		respondErr(c, errAgentOwnerTaken)
		return
	}
	// ③ 建租户（复用 tenant.Create：slug 校验 + 派生 <slug>.wedreamhub.com 域名）。
	t, err := a.TenantService.Create(ctx, tenant.CreateTenantInput{
		Slug:             in.Slug,
		Name:             in.Name,
		TokenplanEnabled: true,
		SkipSubdomain:    params.Level < 1, // L0 普通：不发子域名（spec §5.2.2）
	})
	if err != nil {
		respondErr(c, err) // SLUG_INVALID / SLUG_RESERVED / SLUG_DUPLICATE
		return
	}
	// ④ owner 归属 + agent_profile + 钱包。
	if err := a.TenantRepo.SetOwnerUserID(ctx, t.ID, in.OwnerUserID); err != nil {
		respondErr(c, err)
		return
	}
	// owner 自身落「主站基准」(tenant_id=0)：代理 owner 自用按进货价/平台基准，**不落任何代理店**
	// （他设的模型分组加价只对其名下用户生效；见 doc/detailed-design.md §2.15，用户确认 Option B）。
	// 否则 owner 若仍带注册时的 tenant_id（甚至别人的店），自用会错按那家的覆盖计费。
	if err := a.DB.WithContext(ctx).Table("users").Where("id = ?", in.OwnerUserID).Update("tenant_id", 0).Error; err != nil {
		respondErr(c, err)
		return
	}
	if err := a.AgentService.SetAgentType(ctx, t.ID, params); err != nil {
		respondErr(c, err)
		return
	}
	if err := a.AgentRepo.EnsureWallet(ctx, t.ID, in.OwnerUserID); err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, a.buildAgentOut(ctx, t.ID, in.OwnerUserID, t.Slug, t.Name, string(t.Status), params))
}

// HandleAdminListAgents GET /api/admin/agents —— 代理列表（含 owner 用户名 + 钱包）。需 AdminAuth。
func (a *App) HandleAdminListAgents(c *gin.Context) {
	ctx := reqCtx(c)
	profiles, err := a.AgentRepo.ListProfiles(ctx)
	if err != nil {
		respondErr(c, err)
		return
	}
	ids := make([]int64, 0, len(profiles))
	for _, p := range profiles {
		ids = append(ids, p.UserID)
	}
	names := a.usernamesByIDs(ctx, ids)
	out := make([]agentOut, 0, len(profiles))
	for _, p := range profiles {
		t, terr := a.TenantService.Get(ctx, p.TenantID)
		slug, name, status := "", "", ""
		if terr == nil && t != nil {
			slug, name, status = t.Slug, t.Name, string(t.Status)
		}
		if status == string(tenant.StatusDeleted) {
			continue // 已删除(归档)代理默认不在列表显示
		}
		w, _ := a.AgentService.GetWallet(ctx, p.TenantID)
		out = append(out, agentOut{
			ID:               p.TenantID,
			OwnerUserID:      p.UserID,
			OwnerUsername:    names[p.UserID],
			Slug:             slug,
			Name:             name,
			Level:            p.Level,
			CanAPI:           p.CanAPI,
			CostPriceCNY:     p.CostPriceCNY,
			PackageDiscount:  p.PackageDiscount,
			CommissionRatio:  p.CommissionRatio,
			BottomPriceRatio: p.BottomPriceRatio,
			DiscountRatio:    p.DiscountRatio,
			Status:           status,
			Subdomain:        a.TenantRepo.GetPrimaryDomain(ctx, p.TenantID),
			WithdrawableCNY:  walletField(w, func(x *agent.AgentWallet) float64 { return x.WithdrawableBalance }),
			FrozenCNY:        walletField(w, func(x *agent.AgentWallet) float64 { return x.FrozenWithdrawAmount }),
			TotalEarnedCNY:   walletField(w, func(x *agent.AgentWallet) float64 { return x.TotalEarned }),
		})
	}
	respondOK(c, out)
}

// HandleAdminSetAgentDomain PUT /api/admin/agents/:id/domain —— 管理员给代理设子域名（label）。需 AdminAuth。
// 入参 {label} → `<label>.wedreamhub.com`（校验格式/保留词 + 全局查重 + 替换该代理现有主子域名）→ 失效 Host 缓存。
// 通配 nginx 已反代到 app,故插一条 tenant_domains 即建站生效（doc/agent-subdomain-and-delete.md §一）。
func (a *App) HandleAdminSetAgentDomain(c *gin.Context) {
	tenantID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		respondErr(c, errAgentInputInvalid)
		return
	}
	ctx := reqCtx(c)
	if _, err := a.TenantService.Get(ctx, tenantID); err != nil {
		respondErr(c, err) // TENANT_NOT_FOUND
		return
	}
	var in struct {
		Label string `json:"label"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		respondErr(c, errAgentInputInvalid)
		return
	}
	domain, removed, err := a.TenantService.AddSubdomain(ctx, tenantID, strings.ToLower(strings.TrimSpace(in.Label)))
	if err != nil {
		respondErr(c, err) // SLUG_INVALID / SLUG_RESERVED / DOMAIN_TAKEN
		return
	}
	for _, h := range removed {
		a.tenantCache.Invalidate(ctx, h)
	}
	a.tenantCache.Invalidate(ctx, domain)
	respondOK(c, gin.H{"subdomain": domain})
}

// HandleAdminDeleteAgent DELETE /api/admin/agents/:id —— 删除代理（归档软删,doc/agent-subdomain-and-delete.md §二）。需 AdminAuth。
// 结算闸门(未提现/冻结收益>0 拒删) → status=deleted → 回收子域名+解绑自定义域名+失效缓存 → 终端用户迁回主站(tenant_id=0)。
// 数据留存归档,不硬删。
func (a *App) HandleAdminDeleteAgent(c *gin.Context) {
	tenantID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		respondErr(c, errAgentInputInvalid)
		return
	}
	ctx := reqCtx(c)
	if _, err := a.TenantService.Get(ctx, tenantID); err != nil {
		respondErr(c, err) // TENANT_NOT_FOUND
		return
	}
	// 结算闸门：钱包尚有未提现/冻结中收益 → 拒删（避免钱蒸发）。
	if w, werr := a.AgentService.GetWallet(ctx, tenantID); werr == nil && w != nil &&
		w.WithdrawableBalance+w.FrozenWithdrawAmount > 0 {
		respondErr(c, errAgentUnsettled)
		return
	}
	// 软删：active/suspended → deleted。
	if err := a.TenantService.SetStatus(ctx, tenantID, tenant.StatusDeleted); err != nil {
		respondErr(c, err) // STATUS_TRANSITION / TENANT_NOT_FOUND
		return
	}
	// 回收子域名 + 失效缓存（删了站点即不再解析，配合 getActiveTenant 的 status 过滤双保险）。
	if removed, derr := a.TenantRepo.DeleteDomainsByTenant(ctx, tenantID); derr == nil {
		for _, h := range removed {
			a.tenantCache.Invalidate(ctx, h)
		}
	}
	// 解绑自定义域名（若有）。
	if dom, uerr := a.CustomDomains.Unbind(ctx, tenantID); uerr == nil && dom != "" {
		a.tenantCache.Invalidate(ctx, dom)
	}
	// 终端用户迁回主站：tenant_id=0，账号/余额/历史留存、并入主站继续用。
	a.DB.WithContext(ctx).Exec("UPDATE users SET tenant_id = 0 WHERE tenant_id = ?", tenantID)
	respondOK(c, gin.H{"id": tenantID, "status": string(tenant.StatusDeleted)})
}

// HandleAdminUpdateAgent PATCH /api/admin/agents/:id —— 改代理（id = tenant_id）。需 AdminAuth。
// 支持局部更新类型/等级/成本/折扣/分润（重过保护线校验）与租户名/状态。
func (a *App) HandleAdminUpdateAgent(c *gin.Context) {
	tenantID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		respondErr(c, errAgentInputInvalid)
		return
	}
	ctx := reqCtx(c)
	t, err := a.TenantService.Get(ctx, tenantID)
	if err != nil {
		respondErr(c, err) // TENANT_NOT_FOUND
		return
	}
	curParams, _, err := a.AgentRepo.GetAgentType(ctx, tenantID)
	if err != nil {
		respondErr(c, err)
		return
	}
	var in agentPatchIn
	if err := c.ShouldBindJSON(&in); err != nil {
		respondErr(c, errAgentInputInvalid)
		return
	}
	if in.Level != nil {
		curParams.Level = *in.Level
	}
	if in.CostPriceCNY != nil {
		curParams.CostPrice = *in.CostPriceCNY
	}
	if in.PackageDiscount != nil {
		curParams.PackageDiscount = *in.PackageDiscount
	}
	if in.CommissionRatio != nil {
		curParams.CommissionRatio = *in.CommissionRatio
	}
	if in.DiscountFloor != nil {
		curParams.DiscountFloor = *in.DiscountFloor
	}
	if in.BottomPriceRatio != nil {
		curParams.BottomPriceRatio = *in.BottomPriceRatio
	}
	if in.DiscountRatio != nil {
		curParams.DiscountRatio = *in.DiscountRatio
	}
	// 升档 → 独立档：先幂等派生子域名 `<slug>.wedreamhub.com`（resolver 不缓存负结果，无需失效缓存），
	// 再自动作废该代理名下的全部推广渠道（邀请链接不再向*新*注册归属，见 attributeByChannel 的
	// Voided 跳过分支；已归属的历史用户不受影响，语义见 promotion.PromotionRepo.VoidChannelsByTenant），
	// 成功后才落 level（原子性：任一步失败绝不能让代理停在「level=1 但基建/清理未完成」——那会解锁
	// 独立档自助能力却留下不一致状态，见复盘 Fix 3）。两步都幂等，对已是 L1 的代理重复调用无副作用，
	// 故重排序对既有（已是 L1 / 不升档）流程安全。
	if in.Level != nil && *in.Level >= 1 {
		if err := a.TenantService.EnsureSubdomain(ctx, tenantID, t.Slug); err != nil {
			respondErr(c, err)
			return
		}
		if err := a.Promotion.VoidChannelsByTenant(ctx, tenantID); err != nil {
			respondErr(c, err)
			return
		}
	}
	// SetAgentType 内含参数 + 折扣保护线校验（非法即上浮，不落库）。
	if err := a.AgentService.SetAgentType(ctx, tenantID, curParams); err != nil {
		respondErr(c, err)
		return
	}
	// 可选：更新租户名 / 状态。
	if in.Name != nil && *in.Name != "" {
		if err := a.TenantRepo.UpdateName(ctx, tenantID, *in.Name); err != nil {
			respondErr(c, err)
			return
		}
		t.Name = *in.Name
	}
	if in.Status != nil {
		if err := a.TenantService.SetStatus(ctx, tenantID, tenant.TenantStatus(*in.Status)); err != nil {
			respondErr(c, err)
			return
		}
		t.Status = tenant.TenantStatus(*in.Status)
	}
	respondOK(c, a.buildAgentOut(ctx, tenantID, t.OwnerUserID, t.Slug, t.Name, string(t.Status), curParams))
}

// HandleAdminAgentMetrics GET /api/admin/agents/:id/metrics —— 代理升档决策指标（需 AdminAuth；id=tenant_id）。
// 只读复用 reportrepo：总充值=RechargePaid([1,now] 求和)；分润收益=WalletTotals.TotalEarnedCNY（累计）；
// 下级用户数=CountTenantUsers（薄查询）。绝不接受客户端传除 :id 外的任何口径。
func (a *App) HandleAdminAgentMetrics(c *gin.Context) {
	tenantID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || tenantID <= 0 {
		respondErr(c, errAgentInputInvalid)
		return
	}
	ctx := reqCtx(c)
	// 总充值：生命周期窗口 [1, now] 上复用 RechargePaid（单租户 map 至多一条，求和即总额）。
	rechargeMap, err := a.ReportRepo.RechargePaid(ctx, &tenantID, 1, time.Now().Unix())
	if err != nil {
		respondErr(c, err)
		return
	}
	var rechargeTotal float64
	for _, v := range rechargeMap {
		rechargeTotal += v
	}
	// 分润收益：钱包累计已赚（生命周期；缺行返回零值不报错）。
	wallet, err := a.ReportRepo.WalletTotals(ctx, &tenantID)
	if err != nil {
		respondErr(c, err)
		return
	}
	// 下级用户数：薄计数查询（tenant_id=? AND deleted_at IS NULL）。
	userCount, err := a.ReportRepo.CountTenantUsers(ctx, tenantID)
	if err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, agentMetricsOut{
		RechargeTotalCNY:    round2(rechargeTotal),
		CommissionEarnedCNY: round2(wallet.TotalEarnedCNY),
		DownstreamUserCount: userCount,
	})
}

// ============================================================================
// 主站管理：提现审核（AdminAuth）
// ============================================================================

// HandleAdminListWithdrawals GET /api/admin/withdrawals —— 全部提现单（可选 ?status= 过滤）。需 AdminAuth。
func (a *App) HandleAdminListWithdrawals(c *gin.Context) {
	ctx := reqCtx(c)
	rows, err := a.AgentRepo.ListWithdrawals(ctx, c.Query("status"))
	if err != nil {
		respondErr(c, err)
		return
	}
	out := make([]withdrawalOut, 0, len(rows))
	for _, w := range rows {
		out = append(out, a.toWithdrawalOut(ctx, w))
	}
	respondOK(c, out)
}

// HandleAdminApproveWithdrawal POST /api/admin/withdrawals/:id/approve —— 通过（扣冻结，线下打款）。需 AdminAuth。
func (a *App) HandleAdminApproveWithdrawal(c *gin.Context) {
	a.reviewWithdrawal(c, true)
}

// HandleAdminRejectWithdrawal POST /api/admin/withdrawals/:id/reject —— 拒绝（解冻退回）。需 AdminAuth。
//
// 驳回理由字段名标准化为 remark（提现闭环补强 #3：前端统一发 remark，与后端绑定字段对齐，
// 不再走会丢理由的 reason/remark 不匹配）；已持久化进 agent_withdrawals.remark，并经
// withdrawalOut.Remark 在代理自助列表 + 管理端列表中一并返回。
func (a *App) HandleAdminRejectWithdrawal(c *gin.Context) {
	a.reviewWithdrawal(c, false)
}

func (a *App) reviewWithdrawal(c *gin.Context, approve bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		respondErr(c, errAgentInputInvalid)
		return
	}
	var body struct {
		Remark string `json:"remark"`
	}
	_ = c.ShouldBindJSON(&body) // remark 可选
	if err := a.Withdrawals.Review(reqCtx(c), id, approve, body.Remark); err != nil {
		respondErr(c, err) // WITHDRAW_NOT_PENDING / WITHDRAW_NOT_FOUND
		return
	}
	respondOK(c, gin.H{"id": id})
}

// HandleAdminMarkPaidWithdrawal POST /api/admin/withdrawals/:id/mark-paid —— 标记已打款
// （提现闭环补强 #2：approved→paid，CAS(WHERE status='approved')；扣减冻结资金，真正出账；
// 记打款单号/凭证 + 打款时间）。payout_ref 必填。需 AdminAuth。
func (a *App) HandleAdminMarkPaidWithdrawal(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		respondErr(c, errAgentInputInvalid)
		return
	}
	var body struct {
		PayoutRef string `json:"payout_ref"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		respondErr(c, errAgentInputInvalid)
		return
	}
	if err := a.Withdrawals.MarkPaid(reqCtx(c), id, body.PayoutRef); err != nil {
		respondErr(c, err) // PAYOUT_REF_REQUIRED / WITHDRAW_NOT_APPROVED / WITHDRAW_NOT_FOUND
		return
	}
	respondOK(c, gin.H{"id": id})
}

// buildAgentOut 组装单个代理对象（含 owner 用户名 + 钱包）。
func (a *App) buildAgentOut(ctx context.Context, tenantID, ownerUserID int64, slug, name, status string, p agent.AgentParams) agentOut {
	w, _ := a.AgentService.GetWallet(ctx, tenantID)
	return agentOut{
		ID:               tenantID,
		OwnerUserID:      ownerUserID,
		OwnerUsername:    a.usernamesByIDs(ctx, []int64{ownerUserID})[ownerUserID],
		Slug:             slug,
		Name:             name,
		Level:            p.Level,
		CanAPI:           p.CanAPI,
		CostPriceCNY:     p.CostPrice,
		PackageDiscount:  p.PackageDiscount,
		CommissionRatio:  p.CommissionRatio,
		BottomPriceRatio: p.BottomPriceRatio,
		DiscountRatio:    p.DiscountRatio,
		Status:           status,
		Subdomain:        a.TenantRepo.GetPrimaryDomain(ctx, tenantID),
		WithdrawableCNY:  walletField(w, func(x *agent.AgentWallet) float64 { return x.WithdrawableBalance }),
		FrozenCNY:        walletField(w, func(x *agent.AgentWallet) float64 { return x.FrozenWithdrawAmount }),
		TotalEarnedCNY:   walletField(w, func(x *agent.AgentWallet) float64 { return x.TotalEarned }),
	}
}

// userExists 轻量校验 new-api 用户存在（不经 model.User）。
func (a *App) userExists(ctx context.Context, userID int64) bool {
	var n int64
	if err := a.DB.WithContext(ctx).Table("users").Where("id = ?", userID).Count(&n).Error; err != nil {
		return false
	}
	return n > 0
}

// ownerTaken 报告某 owner 用户是否已是其他租户的 owner（1:1 占用校验；excludeTenantID 排除自身）。
func (a *App) ownerTaken(ctx context.Context, ownerUserID, excludeTenantID int64) bool {
	var n int64
	q := a.DB.WithContext(ctx).Table("tenants").Where("owner_user_id = ?", ownerUserID)
	if excludeTenantID > 0 {
		q = q.Where("id <> ?", excludeTenantID)
	}
	if err := q.Count(&n).Error; err != nil {
		return false
	}
	return n > 0
}
