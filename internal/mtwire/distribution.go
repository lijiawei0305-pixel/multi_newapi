package mtwire

// 代理自助分销端点（Phase 2 · P1-UI-04）：套餐上架改价 / 推广渠道 / 我的用户 / 兑换码（原生 quota 口径）
// / 我的用户组倍率。除「用户兑换」外，全部经 AgentOwnerAuth（owner 维度）+ 强制 scopeByTenant：
// 租户一律取自 AgentOwnerAuth 校验过的 ctx（agentTenantID），绝不接受客户端传 tenant_id。
//
// 货币口径：人民币字段后缀 *_cny、美元额度后缀 *_usd；额度单位换算 $1 = common.QuotaPerUnit。

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/pricing"
	"github.com/QuantumNous/new-api/internal/promotion"
	"github.com/QuantumNous/new-api/internal/tenant"
	"github.com/QuantumNous/new-api/internal/tokenplan"
	"github.com/QuantumNous/new-api/internal/wallet"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// maxRedemptionBatch 单次建码张数上限（防刷 / 限制批量插入规模）。
const maxRedemptionBatch = 1000

// usdToQuotaUnits 把美元额折算为 new-api 内部 quota 单位（$1 = common.QuotaPerUnit）。
// 建码预扣与兑换入账共用同一换算，保证额度严格守恒。
func usdToQuotaUnits(usd float64) int64 { return int64(usd * common.QuotaPerUnit) }

// ============================================================================
// 1) 套餐上架 / 改价（复用 App.Retail，经成本保护线）
// ============================================================================

// listingOut 是代理视角的套餐上架行（本租户全部可上架套餐 + 上架态/零售价）。
type listingOut struct {
	PlanID         int64   `json:"plan_id"`
	Code           string  `json:"code"`
	Name           string  `json:"name"`
	BasePriceCNY   float64 `json:"base_price_cny"`
	MinPriceCNY    float64 `json:"min_price_cny"`
	AnchorPriceCNY float64 `json:"anchor_price_cny"`
	MonthLimitUSD  float64 `json:"month_limit_usd"`
	RetailPriceCNY float64 `json:"retail_price_cny"`
	Enabled        bool    `json:"enabled"`
}

// HandleAgentListPlanListings GET /api/tenant/token-plans/listings —— 本租户全部套餐 + 上架态/零售价。
func (a *App) HandleAgentListPlanListings(c *gin.Context) {
	tenantID := agentTenantID(c)
	if tenantID <= 0 {
		respondErr(c, errAgentForbidden)
		return
	}
	views, err := a.Retail.ListForTenant(reqCtx(c), tenantID)
	if err != nil {
		respondErr(c, err)
		return
	}
	out := make([]listingOut, 0, len(views))
	for _, v := range views {
		out = append(out, listingOut{
			PlanID:         v.Plan.ID,
			Code:           v.Plan.Code,
			Name:           v.Plan.Name,
			BasePriceCNY:   v.Plan.BasePrice,
			MinPriceCNY:    v.Plan.MinPrice,
			AnchorPriceCNY: v.Plan.AnchorPrice,
			MonthLimitUSD:  v.Plan.MonthLimitUSD,
			RetailPriceCNY: v.RetailPrice,
			Enabled:        v.Enabled,
		})
	}
	respondOK(c, out)
}

// HandleAgentSetPlanListing PUT /api/tenant/token-plans/listings/:planId —— 上架/退出并设零售价。
// 低于主站保护线（min_price）返回 RETAIL_BELOW_MIN（由 Retail.SetListing 经 PricingGuard 给出）。
func (a *App) HandleAgentSetPlanListing(c *gin.Context) {
	tenantID := agentTenantID(c)
	if tenantID <= 0 {
		respondErr(c, errAgentForbidden)
		return
	}
	planID, err := strconv.ParseInt(c.Param("planId"), 10, 64)
	if err != nil {
		respondErr(c, tokenplan.ErrPlanNotFound)
		return
	}
	var body struct {
		RetailPriceCNY float64 `json:"retail_price_cny"`
		Enabled        bool    `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		respondErr(c, errAgentInputInvalid)
		return
	}
	if err := a.Retail.SetListing(reqCtx(c), tenantID, planID, body.Enabled, body.RetailPriceCNY); err != nil {
		respondErr(c, err) // PLAN_NOT_FOUND / PLAN_DISABLED / RETAIL_BELOW_MIN
		return
	}
	respondOK(c, gin.H{"plan_id": planID, "retail_price_cny": body.RetailPriceCNY, "enabled": body.Enabled})
}

// ============================================================================
// 2) 推广渠道（复用 promotion 领域服务建码；列表 scopeByTenant）
// ============================================================================

// channelOut 是推广渠道行。
type channelOut struct {
	ID              int64  `json:"id"`
	Code            string `json:"code"`
	Name            string `json:"name"`
	RegisteredCount int64  `json:"registered_count"`
}

func toChannelOut(c promotion.Channel) channelOut {
	return channelOut{ID: c.ID, Code: c.ChannelCode, Name: c.Name, RegisteredCount: c.RegisteredCount}
}

// HandleAgentListChannels GET /api/tenant/promotion/channels —— 本租户的推广渠道（scopeByTenant）。
func (a *App) HandleAgentListChannels(c *gin.Context) {
	tenantID := agentTenantID(c)
	if tenantID <= 0 {
		respondErr(c, errAgentForbidden)
		return
	}
	rows, err := a.PromotionRepo.ListChannelsByTenant(reqCtx(c), tenantID)
	if err != nil {
		respondErr(c, err)
		return
	}
	out := make([]channelOut, 0, len(rows))
	for _, ch := range rows {
		out = append(out, toChannelOut(ch))
	}
	respondOK(c, out)
}

// HandleAgentCreateChannel POST /api/tenant/promotion/channels —— 建渠道。入参 {name}。
// 渠道码由服务端生成 <prefix>_<rand>（prefix 取服务端随机段，不向客户端暴露），全局唯一。
func (a *App) HandleAgentCreateChannel(c *gin.Context) {
	tenantID := agentTenantID(c)
	if tenantID <= 0 {
		respondErr(c, errAgentForbidden)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		respondErr(c, errAgentInputInvalid)
		return
	}
	// API 只收 name；prefix 为服务端随机短码（满足 [a-z0-9-] 校验），渠道码 = <prefix>_<rand>。
	ch, err := a.Promotion.CreateChannel(reqCtx(c), tenantID, body.Name, randPrefix())
	if err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, toChannelOut(*ch))
}

// ============================================================================
// 3) 我的用户（原生 users.tenant_id 维度；轻量 Table 查询）
// ============================================================================

// tenantUserOut 是下级用户行。quota/used_quota 为原生额度单位（前端按 QuotaPerUnit 折算 USD）。
type tenantUserOut struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Quota       int64  `json:"quota"`
	UsedQuota   int64  `json:"used_quota"`
	Status      int    `json:"status"`
	CreatedAt   string `json:"created_at"`
}

// HandleAgentListUsers GET /api/tenant/users —— 本代理名下用户（WHERE tenant_id=ctx），返裸数组。
// 列表天然受限于本代理下级规模（按 tenant_id 索引）；如后续规模增长需服务端分页可在此叠加（前端契约：裸数组）。
func (a *App) HandleAgentListUsers(c *gin.Context) {
	tenantID := agentTenantID(c)
	if tenantID <= 0 {
		respondErr(c, errAgentForbidden)
		return
	}
	var rows []struct {
		ID          int64
		Username    string
		DisplayName string
		Quota       int64
		UsedQuota   int64
		Status      int
		CreatedAt   int64
	}
	// 排除软删除用户（raw Table 查询不会自动套用 gorm 软删除 scope）。
	if err := a.DB.WithContext(reqCtx(c)).Table("users").
		Where("tenant_id = ? AND deleted_at IS NULL", tenantID).
		Select("id, username, display_name, quota, used_quota, status, created_at").
		Order("id desc").Find(&rows).Error; err != nil {
		respondErr(c, err)
		return
	}
	out := make([]tenantUserOut, 0, len(rows))
	for _, r := range rows {
		out = append(out, tenantUserOut{
			ID:          r.ID,
			Username:    r.Username,
			DisplayName: r.DisplayName,
			Quota:       r.Quota,
			UsedQuota:   r.UsedQuota,
			Status:      r.Status,
			CreatedAt:   isoUTC(unixToTime(r.CreatedAt)),
		})
	}
	respondOK(c, out)
}

// ============================================================================
// 4) 兑换码（原生 quota 口径）：建码从代理 owner users.quota 预扣；兑换 IncreaseUserQuota
// ============================================================================

// redemptionOut 是兑换码行。
type redemptionOut struct {
	ID           int64   `json:"id"`
	Code         string  `json:"code"`
	AmountUSD    float64 `json:"amount_usd"`
	Status       string  `json:"status"`
	UsedByUserID int64   `json:"used_by_user_id"`
	CreatedAt    string  `json:"created_at"`
}

// HandleAgentCreateRedemptions POST /api/tenant/redemptions —— 建 count 张面额 amount_usd 的兑换码。
// AgentOwnerAuth：事务内从代理 owner（= 当前用户）原生 users.quota 条件扣减
// count×amount_usd×QuotaPerUnit，不足拒 INSUFFICIENT_QUOTA。
func (a *App) HandleAgentCreateRedemptions(c *gin.Context) {
	tenantID := agentTenantID(c)
	if tenantID <= 0 {
		respondErr(c, errAgentForbidden)
		return
	}
	ownerUserID := int64(c.GetInt("id")) // AgentOwnerAuth 已校验 owner == 当前用户
	var body struct {
		AmountUSD float64 `json:"amount_usd"`
		Count     int     `json:"count"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		respondErr(c, errAgentInputInvalid)
		return
	}
	perCode := usdToQuotaUnits(body.AmountUSD)
	if body.AmountUSD <= 0 || perCode <= 0 || body.Count < 1 || body.Count > maxRedemptionBatch {
		respondErr(c, errAgentInputInvalid)
		return
	}
	total := perCode * int64(body.Count)

	codes := make([]*wallet.RedemptionCode, 0, body.Count)
	seen := make(map[string]struct{}, body.Count)
	for len(codes) < body.Count {
		code := genRedeemCode()
		if _, dup := seen[code]; dup { // 批内去重（极罕见），避免唯一键冲突整批回滚
			continue
		}
		seen[code] = struct{}{}
		codes = append(codes, &wallet.RedemptionCode{TenantID: tenantID, Code: code, AmountUSD: body.AmountUSD})
	}

	if err := a.RedemptionRepo.CreateCodesWithDeduction(reqCtx(c), tenantID, ownerUserID, total, codes); err != nil {
		respondErr(c, err) // INSUFFICIENT_QUOTA / WALLET_AMOUNT_INVALID
		return
	}
	// 预扣已落 DB；失效 owner 用户缓存，使后续读到扣后额度（best-effort，不影响主流程）。
	_ = model.InvalidateUserCache(int(ownerUserID))

	// 前端契约：返回新建兑换码的裸数组（ApiResponse<Redemption[]>）；扣减额度 = count×amount_usd 前端自算。
	out := make([]redemptionOut, 0, len(codes))
	for _, rc := range codes {
		out = append(out, redemptionOut{
			ID: rc.ID, Code: rc.Code, AmountUSD: rc.AmountUSD,
			Status: string(rc.Status), CreatedAt: isoUTC(rc.CreatedAt),
		})
	}
	respondOK(c, out)
}

// HandleAgentListRedemptions GET /api/tenant/redemptions —— 本租户的兑换码（scopeByTenant）。
func (a *App) HandleAgentListRedemptions(c *gin.Context) {
	tenantID := agentTenantID(c)
	if tenantID <= 0 {
		respondErr(c, errAgentForbidden)
		return
	}
	rows, err := a.RedemptionRepo.ListCodesByTenant(reqCtx(c), tenantID)
	if err != nil {
		respondErr(c, err)
		return
	}
	out := make([]redemptionOut, 0, len(rows))
	for _, rc := range rows {
		out = append(out, redemptionOut{
			ID: rc.ID, Code: rc.Code, AmountUSD: rc.AmountUSD,
			Status: string(rc.Status), UsedByUserID: rc.UsedByUserID, CreatedAt: isoUTC(rc.CreatedAt),
		})
	}
	respondOK(c, out)
}

// HandleRedeem POST /api/tenant/redeem —— 用户兑换码（UserAuth，不强制 owner）。
// 租户取自 Host；单赢家 CAS 翻 used 后调原生 IncreaseUserQuota 入账（amount_usd × QuotaPerUnit）。
func (a *App) HandleRedeem(c *gin.Context) {
	t := tenantFrom(c)
	if t == nil {
		respondErr(c, tenant.ErrTenantNotFound)
		return
	}
	userID := int64(c.GetInt("id"))
	if userID <= 0 {
		respondErr(c, errAgentForbidden)
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.Code) == "" {
		respondErr(c, errAgentInputInvalid)
		return
	}
	code := strings.TrimSpace(body.Code)
	amountUSD, err := a.RedemptionRepo.RedeemCode(reqCtx(c), t.ID, code, userID, time.Now())
	if err != nil {
		respondErr(c, err) // REDEEM_CODE_INVALID / REDEEM_CODE_USED
		return
	}
	credit := usdToQuotaUnits(amountUSD)
	if err := model.IncreaseUserQuota(int(userID), int(credit), true); err != nil {
		// 罕见：CAS 已翻 used 但入账失败（DB 错误）。loudly 记日志，码已消费但额度未到账，需人工核对。
		common.SysError("mtwire: redeem credited CAS but IncreaseUserQuota failed for user " +
			strconv.FormatInt(userID, 10) + " code " + code + ": " + err.Error())
		respondErr(c, err)
		return
	}
	respondOK(c, gin.H{"amount_usd": amountUSD, "credited_quota": credit})
}

// ============================================================================
// 5) 我的用户组倍率（存储 + 校验 + 展示；计费接线留作后续，见文件头与报告 TODO）
// ============================================================================

// groupOut 是用户组倍率行：ratio=代理设定倍率；floor=主站保护下限（GetGroupRatio，缺则 1.0）。
type groupOut struct {
	GroupName string  `json:"group_name"`
	Ratio     float64 `json:"ratio"`
	Floor     float64 `json:"floor"`
}

// HandleAgentListGroups GET /api/tenant/groups —— 本租户已配置的用户组倍率 + 各自保护下限。
func (a *App) HandleAgentListGroups(c *gin.Context) {
	tenantID := agentTenantID(c)
	if tenantID <= 0 {
		respondErr(c, errAgentForbidden)
		return
	}
	groups, err := a.TenantRepo.ListGroups(reqCtx(c), tenantID)
	if err != nil {
		respondErr(c, err)
		return
	}
	out := make([]groupOut, 0, len(groups))
	for _, g := range groups {
		out = append(out, groupOut{GroupName: g.GroupName, Ratio: g.Ratio, Floor: groupFloor(g.GroupName)})
	}
	respondOK(c, out)
}

// HandleAgentSetGroupRatio PUT /api/tenant/groups/:group —— 设某用户组倍率。
// 经 pricing.Guard.ValidateGroupRatio(ratio, floor) 校验（floor=主站 GetGroupRatio(group)，缺则 1.0），
// 低于保护下限返回 RATIO_BELOW_FLOOR。
func (a *App) HandleAgentSetGroupRatio(c *gin.Context) {
	tenantID := agentTenantID(c)
	if tenantID <= 0 {
		respondErr(c, errAgentForbidden)
		return
	}
	group := strings.TrimSpace(c.Param("group"))
	if group == "" {
		respondErr(c, errAgentInputInvalid)
		return
	}
	var body struct {
		Ratio float64 `json:"ratio"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		respondErr(c, errAgentInputInvalid)
		return
	}
	floor := groupFloor(group)
	if err := pricing.NewGuard().ValidateGroupRatio(body.Ratio, floor); err != nil {
		respondErr(c, err) // RATIO_BELOW_FLOOR
		return
	}
	if err := a.TenantRepo.UpsertGroup(reqCtx(c), tenantID, group, body.Ratio); err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, groupOut{GroupName: group, Ratio: body.Ratio, Floor: floor})
}

// ============================================================================
// 辅助
// ============================================================================

// groupFloor 取某用户组的主站保护下限：GetGroupRatio 命中返回其值，未命中返回 1.0（其内部缺失即返回 1）。
func groupFloor(group string) float64 {
	floor := ratio_setting.GetGroupRatio(group)
	if floor <= 0 {
		return 1.0
	}
	return floor
}

// randPrefix 返回服务端随机短前缀（8 hex 字符，满足 promotion 前缀 [a-z0-9-] 校验）。
func randPrefix() string { return randToken(4) }

// genRedeemCode 生成 16 位大写十六进制兑换码（8 字节熵，租户内唯一足矣）。
func genRedeemCode() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return strings.ToUpper(randToken(8)) // 熵源故障回退
	}
	return strings.ToUpper(hex.EncodeToString(b))
}

// unixToTime 把 new-api users.created_at（unix 秒）转 time.Time；<=0 给零值（isoUTC 输出空串）。
func unixToTime(sec int64) time.Time {
	if sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}
