package mtwire

// 租户用户组倍率 → /v1 计费接线（Phase 2 代理收尾）。
//
// 代理在 tenant_groups 表为某用户组设定的倍率覆盖，经本文件注入 new-api 计费的「单一解析点」
// relay/helper.HandleGroupRatio（预扣与结算共用）。命中所属租户的 enabled 覆盖即用租户倍率计费，
// 否则回退全局 GetGroupRatio。安全第一：任何 miss/错误/未装配一律回退全局倍率，绝不破坏计费。

import (
	"context"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/platform/grouphook"
)

// InstallGroupRatioHook 注入「租户用户组倍率覆盖」旁路钩子（grouphook.TenantGroupRatioResolver）。
// 由 InstallHooks 在 master/all 节点调用一次；未调用（如单测）时钩子为 nil，计费侧全程走全局倍率。
func (a *App) InstallGroupRatioHook() {
	grouphook.TenantGroupRatioResolver = a.resolveTenantGroupRatio
}

// resolveTenantGroupRatio 是 grouphook.TenantGroupRatioResolver 实现：
// userID → users.tenant_id（轻量 Table 主键点查，复用 userTenantID）→ tenant_groups[tenant_id, group, enabled]。
// 命中即返 (ratio, true)；无租户 / 无覆盖 / 禁用 / 任何错误 → (0, false)，由计费侧回退全局倍率。
//
// 旁路安全：自带 panic 兜底；绝不阻断或破坏计费。每次解析至多 2 次索引点查（users 主键 +
// tenant_groups 唯一键），无 N+1。正确性优先暂不加缓存——代理改倍率需即时生效，缓存陈旧会多扣/少扣。
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
		return 0, false // 主站用户 / 未归属：用全局倍率
	}
	r, found, err := a.TenantRepo.LookupEnabledGroupRatio(ctx, tenantID, group)
	if err != nil || !found {
		return 0, false // 无覆盖 / 禁用 / 出错：回退全局倍率
	}
	return r, true
}
