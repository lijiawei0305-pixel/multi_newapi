package mtwire

// 租户用户组倍率覆盖的查找（Phase 2：并入 2D 的 modelFactor）。
//
// 代理在 tenant_groups 表为某模型分组设定的 per-tenant 倍率覆盖，由 2D 解析器
// （resolveModelGroup2D，见 modelgroup.go）在计算 modelFactor 时查询；命中即用作折扣系数（代理加价）。
// 本文件提供该查询：userID → users.tenant_id → tenant_groups[tenant_id, group, enabled]。
// 安全第一：任何 miss / 错误 / 未装配一律回退 (0,false)，调用方改用平台基准倍率，绝不破坏计费。
//
// 历史：Phase 1 曾把它作为独立旁路钩子 grouphook.TenantGroupRatioResolver 注入计费单点
// （命中即返、对任意 group 生效）。Phase 2 改为「按组合（层级 × 模型分组）下限」语义后，租户覆盖
// 只对模型分组生效并与层级相乘，故并入 2D 的 modelFactor、不再单设钩子。

import (
	"context"

	"github.com/QuantumNous/new-api/common"
)

// resolveTenantGroupRatio 读「某用户所属租户对某用户组（usingGroup）设定的 enabled 倍率覆盖」：
// userID → users.tenant_id（轻量 Table 主键点查，复用 userTenantID）→ tenant_groups[tenant_id, group, enabled]。
// 命中即返 (ratio, true)；无租户 / 无覆盖 / 禁用 / 任何错误 → (0, false)，调用方回退平台基准倍率。
//
// 由 resolveModelGroup2D 在计算 modelFactor 时调用（仅对模型分组）。自带 panic 兜底；绝不阻断或破坏计费。
// 每次解析至多 2 次索引点查（users 主键 + tenant_groups 唯一键），无 N+1。正确性优先暂不加缓存——
// 代理改倍率需即时生效，缓存陈旧会多扣/少扣。
func (a *App) resolveTenantGroupRatio(ctx context.Context, userID int64, group string) (ratio float64, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			common.SysError("mtwire: resolveTenantGroupRatio panic recovered")
			ratio, ok = 0, false
		}
	}()
	if userID <= 0 || group == "" {
		return 0, false
	}
	tenantID := a.userTenantID(ctx, userID)
	if tenantID <= 0 {
		return 0, false // 主站用户 / 未归属：用平台基准倍率
	}
	r, found, err := a.TenantRepo.LookupEnabledGroupRatio(ctx, tenantID, group)
	if err != nil || !found {
		return 0, false // 无覆盖 / 禁用 / 出错：回退平台基准
	}
	return r, true
}

// resolveTierRatio 是层级轴的倍率解析（Change 1，spec agent-tiering §9.6.1）：
//   - userGroup 不是「可代理覆盖层级」(agentOverridableTier；今仅 vip，default 恒排除) → 平台全局
//     groupRatioOf(userGroup)（既有行为，不变，不查库）。
//   - 是可代理覆盖层级，但用户不归属任何 L1（独立档）代理（主站直客 / L0 代理下级 / 未归属）→
//     平台全局 groupRatioOf(userGroup)（范围限定为仅 level>=1——L0 代理没有 HandleAgentSetTierRatio
//     的调用权限，因此 L0 下级用户的层级折扣行为与 Change 1 上线前完全一致；这是一处需用户确认的
//     判断，见 Task 16 顶部"范围判断"）。
//   - 是可代理覆盖层级 且 归属 L1 代理：该代理为此层级设了 enabled 覆盖 → 用覆盖值（代理自担，
//     §9.4）；未设置 → 1（"Default=1，no discount"——不回退平台全局，"No overlap"）。
//
// 自带 panic 兜底由调用方 resolveModelGroup2D 的 defer/recover 统一覆盖，此处不重复包一层。
func (a *App) resolveTierRatio(ctx context.Context, userID int64, userGroup string) float64 {
	baseline := groupRatioOf(userGroup)
	if !agentOverridableTier(userGroup) {
		return baseline
	}
	tenantID := a.userTenantID(ctx, userID)
	if tenantID <= 0 {
		return baseline // 主站直客：平台全局，既有行为不变
	}
	if a.AgentService == nil {
		return baseline // 未装配：安全回退（不查库、不改变现状）
	}
	lvl, err := a.AgentService.AgentLevel(ctx, tenantID)
	if err != nil || lvl < 1 {
		return baseline // L0 / 非代理 / 查询失败：无自设覆盖能力，沿用平台全局（对 L0 零行为变化）
	}
	if a.TenantRepo == nil {
		return baseline
	}
	if override, found, err := a.TenantRepo.LookupEnabledGroupRatio(ctx, tenantID, tierGroupKey(userGroup)); err == nil && found {
		return override
	}
	return 1 // L1 且未配置覆盖：Default=1，不回退平台全局（No overlap）
}
