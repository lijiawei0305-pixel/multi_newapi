package mtwire

// 钩子实现：注册归因 / 消耗分润 / 租户解析 / tokenplan 收益适配器 —— 原生 relay/billing/注册侧旁路调用。
// 由 InstallHooks 注入 agenthook 包级变量；tokenplanEarningAdapter 由 wire 注入 tokenplan.EarningSink。

import (
	"context"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/agent"
	"github.com/QuantumNous/new-api/internal/platform/agenthook"
	"github.com/QuantumNous/new-api/internal/promotion"
	"github.com/QuantumNous/new-api/internal/tokenplan"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// ============================================================================
// 钩子装配：把真实实现注入 agenthook 包级变量（原生 service/controller 旁路调用）
// ============================================================================

// InstallHooks 注入「消耗分润」「注册归属」两个旁路钩子，并装配 2D 倍率钩子（层级×模型分组，§2.15）。
// 由 SetMtRouter 在所有节点调用一次。所有钩子都装：原生 service/controller/计费侧旁路调用，nil 即未装配回退。
func (a *App) InstallHooks() {
	agenthook.ConsumeCommission = a.creditConsumeCommission
	agenthook.AttributeRegistration = a.attributeRegistration
	a.InstallModelGroup2DHook()
	agenthook.ScanUserInput = a.scanUserInputHook // 6e 违禁词：/v1 转发前扫描用户输入
	agenthook.CheckCall = a.checkCallHook         // 7c 风控：/v1 转发前 RPM 限流 + 租户状态
}

// attributeRegistration 是 agenthook.AttributeRegistration 实现：把新用户归属到对应代理（租户）。
// 优先级：渠道码 > 注册 Host > 主站根域（均不命中则 tenant_id 保持 0）。
//   - 有渠道码且命中渠道 → UPDATE users SET tenant_id+promotion_channel_id、registered_count+1、落归属记录；
//   - 无码 / 未知码 → 回落按注册 Host 解析租户（仅 UPDATE tenant_id，promotion_channel_id 保持 0）；
//   - 主站根域 / 未知 Host → 不归属。
//
// best-effort：任何失败仅记日志，绝不影响注册主流程。
func (a *App) attributeRegistration(ctx context.Context, host, channelCode string, userID int64) {
	defer func() {
		if r := recover(); r != nil {
			common.SysError("mtwire: attributeRegistration panic recovered")
		}
	}()
	if userID <= 0 {
		return
	}
	// 渠道码优先：命中即归属到渠道所属租户+渠道，不再回落 Host。
	if code := strings.TrimSpace(channelCode); code != "" {
		if a.attributeByChannel(ctx, code, userID) {
			return
		}
		// 未知渠道码：让位给 Host 兜底（不静默丢归属）。
	}
	a.attributeByHost(ctx, host, userID)
}

// attributeByChannel 按渠道码归属：查渠道→UPDATE users(tenant_id,promotion_channel_id)→
// 落归属记录(幂等 by user_id)→registered_count 原子 +1。命中渠道返回 true（调用方据此不再回落 Host）。
// 渠道码未知 / 渠道无效租户 / 渠道已作废均返回 false（让位 Host 兜底）。任一写失败仅记日志（best-effort）。
func (a *App) attributeByChannel(ctx context.Context, code string, userID int64) bool {
	if a.PromotionRepo == nil {
		return false
	}
	ch, err := a.PromotionRepo.GetChannelByCode(ctx, code)
	if err != nil || ch == nil || ch.TenantID <= 0 {
		return false // 未知渠道码 / 无效渠道：回落 Host
	}
	if ch.Voided {
		// 渠道已作废（代理升级为独立档时自动作废，见 HandleAdminUpdateAgent/VoidChannelsByTenant）：
		// 不再向*新*注册归属，回落 Host/none；已经归属该渠道的历史用户不受影响（此处不触碰 users 表）。
		return false
	}
	// 归属：tenant_id + promotion_channel_id 一次写入（经渠道码注册的权威归属）。
	if err := a.DB.WithContext(ctx).Table("users").
		Where("id = ?", userID).
		Updates(map[string]interface{}{"tenant_id": ch.TenantID, "promotion_channel_id": ch.ID}).Error; err != nil {
		common.SysError("mtwire: attribute user to channel failed: " + err.Error())
		return true // 渠道码已识别：不回落 Host（避免双重归属到不同租户）
	}
	// 归属记录按 user_id 幂等；registered_count 原子 +1。注册天然一次，计数不重复。
	if err := a.PromotionRepo.CreateAttribution(ctx, &promotion.Attribution{
		UserID:      userID,
		TenantID:    ch.TenantID,
		ChannelID:   ch.ID,
		ChannelCode: ch.ChannelCode,
	}); err != nil {
		common.SysError("mtwire: create promotion attribution failed: " + err.Error())
	}
	if err := a.PromotionRepo.IncrRegisteredCount(ctx, ch.ID); err != nil {
		common.SysError("mtwire: incr registered_count failed: " + err.Error())
	}
	return true
}

// attributeByHost 按注册 Host 解析租户并 UPDATE users SET tenant_id（promotion_channel_id 保持 0）。
// 主站根域 / 未知 Host 解析不到则不归属（tenant_id 保持 0）。best-effort。
func (a *App) attributeByHost(ctx context.Context, host string, userID int64) {
	t, err := a.TenantResolver.ResolveByHost(ctx, host)
	if err != nil || t == nil || t.ID <= 0 {
		return // 主站根域 / 未知 Host：归属主站（tenant_id 保持 0）
	}
	if err := a.DB.WithContext(ctx).Table("users").
		Where("id = ?", userID).Update("tenant_id", t.ID).Error; err != nil {
		common.SysError("mtwire: attribute user to tenant failed: " + err.Error())
	}
}

// creditConsumeCommission 是 agenthook.ConsumeCommission 实现：userId→users.tenant_id→agent
// level→按档二选一入账（spec agent-tiering §9.9）：level==0 → L0 提成（creditL0Commission）；
// level≥1 → L1 差价（creditRatioMarkup，Task 13）。幂等键=requestID。best-effort：失败不阻断扣费。
//
// 套餐(订阅桶)消耗一律不产生代理分润（doc/finance-model-report-v3.md §一.A）：套餐的钱在购买时已
// 一次性分完（tokenplan_spread，见 internal/tokenplan/subscription.go ActivateFromPayment），后续
// 消耗套餐额度不再给代理二次分成——不区分 L0/L1，故这个短路挡在按档分支之前统一生效：
//   - L0 曾经会在此场景改发 tokenplan_commission（现已删除，见下方 creditL0Commission）；
//   - L1 的 creditRatioMarkup 自身不感知/不判断 billingSource，若不在此拦截，命中卖价覆盖的 L1
//     租户即便是套餐桶消耗也会误发 ratio_markup——这里统一堵死，两个档位都不再有例外。
func (a *App) creditConsumeCommission(userID int64, quotaUnits int64, requestID, billingSource, usingGroup string, chargedGroupRatio float64) {
	defer func() {
		if r := recover(); r != nil {
			common.SysError("mtwire: creditConsumeCommission panic recovered")
		}
	}()
	if userID <= 0 || quotaUnits <= 0 || requestID == "" {
		return
	}
	if billingSource == "subscription" {
		return // 套餐桶消耗：不入账（无论 L0/L1），套餐收益只在购买时结一次（tokenplan_spread）
	}
	ctx := context.Background()
	tenantID := a.userTenantID(ctx, userID)
	if tenantID <= 0 {
		return // 主站用户 / 未归属：无代理分润
	}
	// 钱包桶消耗台账（display-only；不产生收益、不动额度，与下方按档分润完全正交）：上方已短路套餐桶
	// 消耗（billingSource=="subscription" 直接 return），故此处必为钱包桶，quotaUnits 即本次全额走钱包桶
	// 的消耗额度（单事件资金来源单一，见 service/billing_session.go）。tenant 复用刚解析的 userTenantID，
	// 与消耗透镜 users.tenant_id 同口径，保证财务报表主站/代理站拆分不变。写在 GetAgentType 之前——
	// 平台租户/无 agent_profile 的租户也要记其钱包消耗（否则「主站钱包消耗」永远为 0）。best-effort 幂等。
	a.recordWalletConsume(ctx, tenantID, userID, quotaUnits, requestID)
	params, found, err := a.AgentRepo.GetAgentType(ctx, tenantID)
	if err != nil || !found {
		return // 该租户未设代理
	}
	// 按档二选一（spec §9.9）：level==0 → L0 提成通路；level≥1 → L1 差价通路。NEVER both——两条路径
	// 在此分支互斥，绝不重叠调用。
	if params.Level == 0 {
		a.creditL0Commission(ctx, tenantID, userID, quotaUnits, requestID, billingSource, params.CommissionRatio)
		return
	}
	a.creditRatioMarkup(ctx, tenantID, userID, quotaUnits, usingGroup, requestID, billingSource, chargedGroupRatio, params.DiscountRatio, params.BottomPriceRatio)
}

// creditL0Commission 是 L0（普通档）计费通路：官方原价提成（commission_ratio × quotaUnits，公式不变，
// spec §9.5——v2 不再有邀请 9 折，纯提成）。从 creditConsumeCommission 抽出以保持按档分支清晰；
// panic 由调用方的 defer 统一兜底。
//
// 只有钱包桶消耗会走到这里——creditConsumeCommission 已在套餐(订阅)桶消耗时提前返回（见上方），
// 故 source 恒为 consume_commission；tokenplan_commission 不再从此处（或任何地方）发出
// （doc/finance-model-report-v3.md §一.A：套餐消耗不再二次分成）。billingSource 仅保留用于备注。
func (a *App) creditL0Commission(ctx context.Context, tenantID, userID, quotaUnits int64, requestID, billingSource string, commissionRatio float64) {
	if commissionRatio <= 0 {
		return
	}
	cny := consumeCommissionCNY(quotaUnits, commissionRatio, operation_setting.USDExchangeRate)
	if cny <= 0 {
		return
	}
	if err := a.AgentEarnings.AddEarning(ctx, agent.EarningEntry{
		TenantID:   tenantID,
		UserID:     userID,
		SourceType: agent.SourceConsumeCommission,
		SourceID:   requestID,
		Amount:     cny,
		Remark:     "consume:" + billingSource,
	}); err != nil {
		common.SysError("mtwire: credit consume commission failed: " + err.Error())
	}
}

// consumeCommissionCNY 计算消耗分润（¥）= 消耗USD × ratio × usdRate，其中 USD = quotaUnits / QuotaPerUnit。
// 任一参数非正返回 0（旁路安全）。提为纯函数便于单测币种换算口径。
func consumeCommissionCNY(quotaUnits int64, ratio, usdRate float64) float64 {
	if quotaUnits <= 0 || ratio <= 0 || usdRate <= 0 {
		return 0
	}
	usd := float64(quotaUnits) / common.QuotaPerUnit
	return usd * ratio * usdRate
}

// userTenantIDStrict 直读 users.tenant_id，区分「用户 tenant_id=0（合法平台用户）」与「查询失败」：
// 失败返回 error，供建单等需要严格归属的场景拒绝，而非静默落为平台工单（tenant_id=0）。
func (a *App) userTenantIDStrict(ctx context.Context, userID int64) (int64, error) {
	var row struct{ TenantID int64 }
	if err := a.DB.WithContext(ctx).Table("users").
		Select("tenant_id").Where("id = ?", userID).Take(&row).Error; err != nil {
		return 0, err
	}
	return row.TenantID, nil
}

// userTenantID 轻量直读 users.tenant_id（不经 new-api model.User）；列缺失/查询失败一律给 0（旁路安全，非严格场景用）。
func (a *App) userTenantID(ctx context.Context, userID int64) int64 {
	tid, _ := a.userTenantIDStrict(ctx, userID)
	return tid
}

// moderationTenantID 解析「内容审核归属租户」：普通用户按自身 tenant_id；
// 站长(代理 owner)自身 tenant_id 多为 0/主租户，但其违规应归到「拥有的代理租户」——
// 这样代理后台「我的违规日志」可见、且套用该租户词库。仅在 tenant_id==0 时回查 owner_user_id（省热路径一次查询）。
func (a *App) moderationTenantID(ctx context.Context, userID int64) int64 {
	tid := a.userTenantID(ctx, userID)
	if tid != 0 {
		return tid
	}
	var owned struct{ ID int64 }
	if err := a.DB.WithContext(ctx).Table("tenants").
		Select("id").Where("owner_user_id = ?", userID).Take(&owned).Error; err == nil && owned.ID > 0 {
		return owned.ID
	}
	return 0
}

// ============================================================================
// tokenplan 差价收益 → agent 钱包 适配器（替换 wire.go 的 noopEarnings）
// ============================================================================

// tokenplanEarningAdapter 实现 tokenplan.EarningSink，把套餐差价收益转写为 agent.EarningEntry 落账。
// 套餐激活事务内（subscription.go ActivateFromPayment）按 source_order_id 幂等触发，注入即生效。
type tokenplanEarningAdapter struct{ sink agent.EarningSink }

func newTokenplanEarningAdapter(sink agent.EarningSink) *tokenplanEarningAdapter {
	return &tokenplanEarningAdapter{sink: sink}
}

var _ tokenplan.EarningSink = (*tokenplanEarningAdapter)(nil)

func (ad *tokenplanEarningAdapter) AddEarning(ctx context.Context, e tokenplan.EarningEntry) error {
	return ad.sink.AddEarning(ctx, agent.EarningEntry{
		TenantID:   e.TenantID,
		UserID:     e.UserID,
		SourceType: mapTokenplanSource(e.SourceType),
		SourceID:   e.SourceID,
		Amount:     e.Amount,
		Remark:     e.Reference,
	})
}

// mapTokenplanSource 把 tokenplan 收益来源映射到 agent 收益来源（当前同值字符串，显式映射防枚举漂移）。
func mapTokenplanSource(s tokenplan.EarningSource) agent.EarningSource {
	switch s {
	case tokenplan.EarningTokenplanSpread:
		return agent.SourceTokenplanSpread
	default:
		return agent.EarningSource(string(s))
	}
}
