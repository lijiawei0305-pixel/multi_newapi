package mtwire

// L1（独立档）差价入账实现（spec agent-tiering §9.4/§9.7/§9.9）。独立文件，紧邻 distribution.go 的
// consumeFloorRatio（Task 12：写路径的卖价地板）——本文件的 creditRatioMarkup 直接复用它，两处必须
// 同一口径（否则会出现「卖价被地板挡住却在入账时按 0 底价整单算成代理利润」的记账错误）。

import (
	"context"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/agent"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// ratioMarkupQuotaUnits 是差价入账的纯函数核心（spec §9.4 **v3 修订**：markup=rawUnits×(chargedGroupRatio−底价)
// = chargedQuota − rawUnits×底价）：
//
//	rawUnits = chargedQuota / chargedGroupRatio
//	markup   = chargedQuota − rawUnits × bottomRatio
//
// chargedGroupRatio 必须是「结算时实际生效的组合倍率」（用户层级优惠×分组倍率，来自
// relayInfo.PriceData.GroupRatioInfo.GroupRatio；层级优惠现在可能是代理自设的 vip 力度覆盖，spec
// §9.6.1）——用实际生效值反推 rawUnits，而不是重新查一遍「当前」的层级/分组倍率，从而与代理事后
// 改卖价/vip 力度的竞态解耦。
//
// **v2→v3 关键变化**：公式不再单独接收 sellRatio 参数，直接用 chargedGroupRatio（已经把层级优惠乘
// 进去）与 bottomRatio 的差值展开——代理自设的 vip 折扣因而**直接体现**在 markup 里（层级优惠越深、
// markup 越小），不再是 v2 时代"层级优惠已经在 chargedGroupRatio 里被除掉、与 markup 完全无关"的
// tier-不变量（spec §9.6 曾标注为已知差距，现由 §9.6.1 解决）。
//
// chargedGroupRatio<=bottomRatio（未加价；或管理员事后把底价调到卖价之上——配置漂移防御；或代理
// 自设/调深 vip 力度导致这一单的实际生效价跌破底价——同一类漂移，同一处兜底，不必为此单开分支）或
// 任一参数非正 → 0：结构上绝不产生负值（MANDATORY safety：无论 chargedGroupRatio 因为哪个原因被
// 拉低，markup 只会趋近 0，不会为负；这同时是 spec §9.6.1"卖价×vip 力度 ≥ 底价"floor 承诺的结算侧
// 兜底）。
func ratioMarkupQuotaUnits(chargedQuota int64, chargedGroupRatio, bottomRatio float64) int64 {
	if chargedQuota <= 0 || chargedGroupRatio <= 0 || chargedGroupRatio <= bottomRatio {
		return 0
	}
	rawUnits := float64(chargedQuota) / chargedGroupRatio
	markup := float64(chargedQuota) - rawUnits*bottomRatio
	if markup <= 0 { // 代数上该分支在上面的 guard 后不可达；保留作 belt-and-suspenders（浮点边界防御）。
		return 0
	}
	return int64(markup)
}

// creditRatioMarkup 是 L1（level≥1）差价入账实现（**v3 修订**，spec §9.4/§9.6.1）：先确认该用户所属
// 租户在 usingGroup 上是否设了卖价覆盖（tenant_groups；无覆盖则不入账——未设卖价的模型分组，用户按
// 平台直客价付费，不视为隐式底价加价；命中与否才是"是否入账"的判据，覆盖的具体数值本身 v3 起不再
// 参与 markup 计算，下方详述）× consumeFloorRatio(bottomPriceRatio, usingGroup)（Task 12 同口径地板）
// 算出 markup（直接用 chargedGroupRatio——已经把代理自设 vip 力度乘进去的实际生效倍率，§9.6.1——
// 而不是裸卖价），按 requestID 幂等入账到 L1 自己的钱包（source=ratio_markup）。best-effort：失败
// 不阻断调用方（由 creditConsumeCommission 的 panic 兜底覆盖，Task 14）。
//
// **v2→v3**：旧版本读取 sellRatio（卖价覆盖的具体数值）传入 markup 公式；新公式改用 chargedGroupRatio
// （已含层级优惠）直接对 bottomRatio 求差，sellRatio 的返回值不再需要——但**查询本身仍必须保留**，
// 因为它是"这个模型分组是否有卖价覆盖"这个入账资格判据的唯一来源（无覆盖=用户按平台价付费=不产生
// 差价，即便 chargedGroupRatio 本身合法非零）。
func (a *App) creditRatioMarkup(ctx context.Context, tenantID, userID, quotaUnits int64, usingGroup, requestID, billingSource string, chargedGroupRatio, bottomPriceRatio float64) {
	if tenantID <= 0 || quotaUnits <= 0 || requestID == "" || usingGroup == "" || chargedGroupRatio <= 0 {
		return
	}
	if a.ModelGroupRepo == nil || !a.ModelGroupRepo.IsModelGroup(usingGroup) {
		return // 非模型分组（层级名等）：无「卖价」概念，不产生差价
	}
	if _, hit := a.resolveTenantGroupRatio(ctx, userID, usingGroup); !hit {
		return // 未设卖价覆盖：用户按平台直客价付费，不视为隐式底价加价
	}
	bottom := consumeFloorRatio(bottomPriceRatio, usingGroup) // 与 HandleAgentSetGroupRatio 同口径（Task 12）
	markupQuota := ratioMarkupQuotaUnits(quotaUnits, chargedGroupRatio, bottom)
	if markupQuota <= 0 {
		return
	}
	// markup 已是「计费额」口径（quota 单位），直接按 ratio=1 换算 CNY（不再乘任何分润比例）。
	cny := consumeCommissionCNY(markupQuota, 1, operation_setting.USDExchangeRate)
	if cny <= 0 {
		return
	}
	if err := a.AgentEarnings.AddEarning(ctx, agent.EarningEntry{
		TenantID:   tenantID,
		UserID:     userID,
		SourceType: agent.SourceRatioMarkup,
		SourceID:   requestID,
		Amount:     cny,
		Remark:     "ratio_markup:" + usingGroup + ":" + billingSource,
	}); err != nil {
		common.SysError("mtwire: credit ratio markup failed: " + err.Error())
	}
}
