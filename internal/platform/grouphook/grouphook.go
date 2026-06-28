// Package grouphook 提供「new-api 计费分组倍率解析 → 租户用户组倍率覆盖」的旁路挂钩点（包级函数变量）。
//
// 设计意图：new-api 计费路径（relay/helper.HandleGroupRatio）在解析「请求维度 groupRatio」时，
// 不应 import 多租户增量装配层 mtwire / internal/tenant（会形成 import 环、污染原生计费包依赖）。
// 改用「注册式钩子」（与 internal/platform/agenthook 同位、同模式）：原生计费包只调用本包的函数
// 变量（nil 即未装配，直接回退全局倍率），mtwire 在启动时把真实实现注入该变量。
//
// 安全第一：本钩子为**旁路覆盖**——任何 miss / 错误 / 未装配一律由调用方回退全局 GetGroupRatio，
// 绝不可阻断或破坏计费。实现侧必须自身兜底 panic/error，并在无覆盖时返回 (0, false)。
package grouphook

import "context"

// TenantGroupRatioResolver 解析「某用户所属租户对某用户组（usingGroup）设定的倍率覆盖」。
//
//   - 返回 (ratio, true)：命中所属租户 tenant_groups[tenant_id, group] 的 enabled 覆盖，
//     调用方用该 ratio 作为计费 groupRatio。
//   - 返回 (_, false)：无租户（主站用户）/ 无覆盖 / 禁用 / 任何错误——调用方回退全局 GetGroupRatio。
//   - nil（未装配，如单测 / 非多租户环境）：调用方一律回退全局倍率。
//
// 该解析点同时供「预扣」与「结算」复用（见 relay/helper.HandleGroupRatio），覆盖一处即两端一致。
var TenantGroupRatioResolver func(ctx context.Context, userID int64, group string) (float64, bool)
