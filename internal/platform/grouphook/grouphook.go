// Package grouphook 提供「new-api 计费分组倍率解析 → 2D 倍率（层级 × 模型分组，含代理 per-tenant 覆盖）」
// 的旁路挂钩点（包级函数变量）。
//
// 设计意图：new-api 计费路径（relay/helper.HandleGroupRatio）在解析「请求维度 groupRatio」时，
// 不应 import 多租户增量装配层 mtwire / internal/tenant（会形成 import 环、污染原生计费包依赖）。
// 改用「注册式钩子」（与 internal/platform/agenthook 同位、同模式）：原生计费包只调用本包的函数
// 变量（nil 即未装配，直接回退全局倍率），mtwire 在启动时把真实实现注入该变量。
//
// 安全第一：本钩子为**旁路覆盖**——任何 miss / 错误 / 未装配一律由调用方回退全局 GetGroupRatio，
// 绝不可阻断或破坏计费。实现侧必须自身兜底 panic/error，并在无覆盖时返回 (0, false)。
package grouphook

// ModelGroup2DResolver 解析「2D 倍率（层级 × 模型分组，含代理 per-tenant 覆盖）」（见 doc/detailed-design.md §2.15）。
//
// 把计费倍率从一维升级为二维相乘：
//
//		最终 groupRatio = GroupRatio[userGroup(层级)] × modelFactor
//		modelFactor = usingGroup ∈ model_groups
//		              ? ( 该用户所属租户对此模型分组有 enabled 覆盖 ? 覆盖值 : GroupRatio[usingGroup] )
//		              : 1
//
//	  - userID：发起请求的用户 id（解析其 users.tenant_id → tenant_groups 覆盖；主站用户 / 0 → 无覆盖，用平台基准）；
//	  - userGroup：用户所属层级（User.Group，如 default/vip/svip），倍率在原生 GroupRatio；
//	  - usingGroup：本次请求所用 token 组；若它是「已登记的模型分组」，叠乘其折扣系数（代理租户可加价覆盖），否则系数=1（仅层级）；
//	  - 返回 (ratio, true)：用该 ratio 作计费 groupRatio（装配且无异常时恒命中）；
//	  - 返回 (_, false)：任何错误 / panic / 未装配——调用方回退原生 GetGroupGroupRatio/GetGroupRatio；
//	  - nil（未装配，如单测 / 平台未启用）：调用方一律回退原生倍率。
//
// 代理 per-tenant 覆盖只对模型分组生效（折扣系数侧），并受「组合下限」保护（覆盖值 ≥ 平台基准，只能加价）；
// 校验在写入端（代理「我的用户组」端点）完成。本解析点同时供「预扣」与「结算」复用（覆盖一处即两端一致）。
// 安全第一：实现侧自身兜底 panic，miss/错误一律回退，绝不阻断或破坏计费。
var ModelGroup2DResolver func(userID int64, userGroup, usingGroup string) (float64, bool)
