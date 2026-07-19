// Package agenthook 提供「new-api 原生流程 → 代理增量」的旁路挂钩点（包级函数变量）。
//
// 设计意图：new-api 原生包（service/quota.go、controller/user.go、controller/oauth.go）不应 import
// 多租户增量装配层 mtwire（会形成 import 环、且污染原生包依赖）。改用「注册式钩子」：原生包只调用
// 本包的函数变量（nil 即未装配，直接跳过），mtwire 在启动时把真实实现注入这些变量。
//
// 所有钩子均为**旁路**（best-effort）：实现内部自行兜底 panic/error，绝不可阻断原生扣费/注册主流程。
package agenthook

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
)

// ConsumeCommission 在一次成功的 PostConsume 之后被调用，按所属代理档位二选一计佣入账
// （level==0 → 提成 consume_commission；level≥1 → 差价 ratio_markup；见 mtwire.creditConsumeCommission，
// Task 14）。参数：userID=消费用户；quotaUnits=本次消费的 new-api 内部额度单位（$1=common.QuotaPerUnit，
// 可正可负，实现侧只对正向消费计佣）；requestID=幂等键来源；billingSource="wallet"|"subscription"
// （区分钱包桶/套餐桶）；usingGroup=本次计费实际使用的分组（relayInfo.UsingGroup）；chargedGroupRatio=
// 本次计费实际生效的组合倍率（用户层级优惠×分组倍率，relayInfo.PriceData.GroupRatioInfo.GroupRatio）——
// 后两者供 L1 差价入账精确反推 token×ModelRatio，spec agent-tiering §9.4。
// nil = 未装配。实现必须自身幂等；错误返回给 durable settlement dispatcher，事件保持 pending 重放。
var ConsumeCommission func(userID int64, quotaUnits int64, sourceID, billingSource, usingGroup string, chargedGroupRatio float64) error

// CommissionSnapshot freezes every economic input needed to persist wallet
// consumption and any payable earning. It is prepared before the billing
// settlement transaction and stored with that transaction, so delayed replay
// never reads changed agent pricing/profile configuration.
type CommissionSnapshot struct {
	SourceID          string
	OccurredAt        time.Time
	WalletTenantID    int64
	WalletUserID      int64
	WalletQuota       int64
	EarningApplicable bool
	EarningTenantID   int64
	EarningUserID     int64
	EarningSourceType string
	EarningAmount     float64
	EarningRemark     string
}

// CommissionPolicy freezes attribution and the formula inputs before
// upstream work. Final quota is deliberately absent and is supplied only when
// materializing the immutable payable snapshot.
type CommissionPolicy struct {
	OccurredAt        time.Time `json:"occurred_at"`
	WalletTenantID    int64     `json:"wallet_tenant_id"`
	WalletUserID      int64     `json:"wallet_user_id"`
	EarningMode       string    `json:"earning_mode"`
	EarningTenantID   int64     `json:"earning_tenant_id"`
	EarningUserID     int64     `json:"earning_user_id"`
	EarningSourceType string    `json:"earning_source_type"`
	EarningRemark     string    `json:"earning_remark"`
	DirectRate        float64   `json:"direct_rate"`
	MarkupFactor      float64   `json:"markup_factor"`
	QuotaCNYRate      float64   `json:"quota_cny_rate"`
}

var PrepareConsumeCommission func(userID int64, quotaUnits int64, sourceID, billingSource, usingGroup string, chargedGroupRatio float64) (CommissionSnapshot, error)
var PersistConsumeCommission func(snapshot CommissionSnapshot) error
var PrepareConsumeCommissionPolicy func(userID int64, billingSource, usingGroup string, chargedGroupRatio float64) (CommissionPolicy, error)
var MaterializeConsumeCommissionPolicy func(policy CommissionPolicy, quotaUnits int64, sourceID string) (CommissionSnapshot, error)

// AttributeRegistration 在新用户创建后被调用，把用户归属到对应代理（租户）。
// 归属优先级：渠道码 channelCode（经代理推广链接 /sign-up?channel=<code> 注册）> 注册 Host >
// 主站根域（均解析不到则 tenant_id 保持 0）。channelCode 命中时还会写 promotion_channel_id、
// 令该渠道 registered_count+1 并落一条归属记录；channelCode 为空或未知则回落 Host 归属。
// nil = 未装配。best-effort（实现内部兜底 panic/error，绝不阻断注册主流程）。
var AttributeRegistration func(ctx context.Context, host, channelCode string, userID int64)

// ScanUserInput 在 /v1 转发前被调用，扫描用户输入消息的违禁词（6e，§2.14）。
// 命中 block 级 → 返回非 nil 错误（原生 relay 据此拦截返回）；remind 级 → 返回 nil（实现侧已记录违规）。
// 实现内部自身兜底 panic/error，扫描/记录失败绝不阻断请求。nil = 未装配。
var ScanUserInput func(ctx context.Context, userID, tokenID int64, model string, request dto.Request) *types.NewAPIError

// CheckCall 在 /v1 转发前被调用，做「调用前风控」（7c，§2.13）：RPM 限流 / IP allowlist / 主体状态。
// 命中 → 返回非 nil（原生 relay 据此拦截，429 限流 / 403 IP·状态）；放行 → 返回 nil。
// clientIP/requestID 由 relay 从 gin 上下文取出后传入（本包不 import gin，保持原生依赖洁净）。
// best-effort：实现内部兜底 panic/error，绝不误杀正常请求。nil = 未装配。
var CheckCall func(ctx context.Context, userID, tokenID int64, model, clientIP, requestID string) *types.NewAPIError
