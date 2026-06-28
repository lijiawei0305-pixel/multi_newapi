// Package agenthook 提供「new-api 原生流程 → 代理增量」的旁路挂钩点（包级函数变量）。
//
// 设计意图：new-api 原生包（service/quota.go、controller/user.go、controller/oauth.go）不应 import
// 多租户增量装配层 mtwire（会形成 import 环、且污染原生包依赖）。改用「注册式钩子」：原生包只调用
// 本包的函数变量（nil 即未装配，直接跳过），mtwire 在启动时把真实实现注入这些变量。
//
// 所有钩子均为**旁路**（best-effort）：实现内部自行兜底 panic/error，绝不可阻断原生扣费/注册主流程。
package agenthook

import "context"

// ConsumeCommission 在一次成功的 PostConsume 之后被调用，按所属代理 commission_ratio 计佣入账。
// 参数：userID=消费用户；quotaUnits=本次消费的 new-api 内部额度单位（$1=common.QuotaPerUnit，可正可负，
// 实现侧只对正向消费计佣）；requestID=幂等键来源；billingSource="wallet"|"subscription"（区分钱包桶/套餐桶）。
// nil = 未装配。实现必须自身幂等且 best-effort（失败仅记日志，不返回错误）。
var ConsumeCommission func(userID int64, quotaUnits int64, requestID, billingSource string)

// AttributeRegistration 在新用户创建后被调用，把用户归属到对应代理（租户）。
// 归属优先级：渠道码 channelCode（经代理推广链接 /sign-up?channel=<code> 注册）> 注册 Host >
// 主站根域（均解析不到则 tenant_id 保持 0）。channelCode 命中时还会写 promotion_channel_id、
// 令该渠道 registered_count+1 并落一条归属记录；channelCode 为空或未知则回落 Host 归属。
// nil = 未装配。best-effort（实现内部兜底 panic/error，绝不阻断注册主流程）。
var AttributeRegistration func(ctx context.Context, host, channelCode string, userID int64)
