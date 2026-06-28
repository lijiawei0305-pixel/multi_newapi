package tenant

// UserGroup 是代理（租户）维度的用户组倍率配置（tenant_groups 表，UNIQUE(tenant_id, group_name)）。
//
// Ratio 为代理为该用户组设定的倍率（计费溢价系数）；Enabled 标记是否启用。
// 校验：Ratio 不得低于主站保护下限 floor（= ratio_setting.GetGroupRatio(group)，缺则 1.0），
// 经 pricing.Guard.ValidateGroupRatio 把关，低于返回 RATIO_BELOW_FLOOR。
//
// 注：本轮（P1-UI-04）仅落「存储 + 校验 + 展示」；倍率真正作用于 /v1 计费的接线
// （relay 按租户组倍率计费）留作后续——需改动计费路径，见交付报告「计费接线 TODO」。
type UserGroup struct {
	TenantID  int64
	GroupName string
	Ratio     float64
	Enabled   bool
}
