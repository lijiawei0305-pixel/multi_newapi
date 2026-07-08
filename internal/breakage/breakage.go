/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

// Package breakage 是「breakage 监控」（P2-BRK-01）的领域契约层：只定义类型 + 接口 + 错误码，
// 不含实现（实现落 internal/breakage/service.go 与 internal/breakage/gormrepo/）。
//
// 什么是 breakage：用户用不满导致的额度沉淀。整个定价模型建立在「用户只用约 1% 月限额」
// （proposal §2.4），故 breakage 可观测是二期风控财务的核心缺口。本模块把它拆成 4 个可聚合指标
// （见 Overview）+ 订阅维明细（见 DetailRow）+ 历史快照趋势（见 Snapshot）。
//
// 硬约束（下游实现必须遵守，见 .ccg/tasks/breakage-monitor/requirements.md）：
//
//	【门 #4 租户隔离】聚合方法一律带 tenantID *int64 作用域参数：
//	    tenantID == nil → 主站/管理端跨租户，统一 tenant_id <> 0（排除主站未归属 tenant_id=0）；
//	    tenantID != nil → 代理自助单租户，tenant_id = *tenantID。
//	  绝不从 query/body 读 tenant_id——handler 层的 tenantID 只取自鉴权中间件解析出的租户
//	  （tenantFrom(c)），与 internal/mtwire/http.go HandleAdminListSubscriptions 完全一致。
//	【升级安全】实现走 raw db.Table()/Joins() 的 SUM/GROUP BY，绝不 import 兄弟模块的 model 结构
//	  （参考 internal/report/reportrepo/reportrepo.go 范式）。
//	【惰性过期口径】判「到期未使用」一律按 expire_at < now，不能只信持久化的 status=expired。
//	【金额】一律 float64，币种由字段名后缀表达（_usd）；handler 边界 round2；计数用 int 精确不舍入。
//
// 表：tokenplan_subscriptions（订阅台账）、subscription_usage_logs（套餐内计量）、
// user_balances（钱包余额，balance_usd 已是 USD）、breakage_snapshots（本模块新表）。
package breakage

import (
	"context"
	"net/http"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

// ============================================================================
// 告警级别（与 web/default/src/features/subscription-monitor/types.ts 的
// alertLevelValues 语义对齐：warn < critical < exhausted；健康行给空串 AlertNone）
// ============================================================================

// AlertLevel 是订阅额度用量的告警级别（服务端计算，前端只渲染）。语义完全对齐
// internal/mtwire/http.go alertLevel()：>=100 或 status=exhausted → exhausted；
// >=95 → critical；>=80 → warn；否则 none（健康）。字符串值直接进 snake_case DTO 的 alert_level。
type AlertLevel string

const (
	// AlertNone 健康（用量 < 80%）。DTO 里序列化为空串（前端「空/缺省即健康」）。
	AlertNone AlertLevel = ""
	// AlertWarn 预警（80% <= 用量 < 95%）。
	AlertWarn AlertLevel = "warn"
	// AlertCritical 严重（95% <= 用量 < 100%）。
	AlertCritical AlertLevel = "critical"
	// AlertExhausted 已满额（用量 >= 100% 或订阅 status=exhausted）。
	AlertExhausted AlertLevel = "exhausted"
)

// AlertLevelForPct 按用量占比（0..100+）+ 订阅是否已终态满额，映射告警级别。
// 与 internal/mtwire/http.go alertLevel() 同口径，供 Repo/Service 计算 DetailRow.AlertLevel 复用，
// 保证 breakage 页与订阅监控页对同一条订阅给出一致级别。exhausted 优先：Meter 整笔拒绝时 used 可能
// 略低于 limit 但订阅已终态满额，故传入 statusExhausted=true 时直接判 exhausted。
func AlertLevelForPct(usagePct float64, statusExhausted bool) AlertLevel {
	if statusExhausted || usagePct >= 100 {
		return AlertExhausted
	}
	switch {
	case usagePct >= 95:
		return AlertCritical
	case usagePct >= 80:
		return AlertWarn
	default:
		return AlertNone
	}
}

// ============================================================================
// 域类型（金额 float64 _usd；计数 int；这些是包内领域对象，非 HTTP DTO——
// snake_case 序列化在 internal/mtwire/breakage.go 层完成）
// ============================================================================

// Overview 是 breakage 总览的 4 个核心指标（GET /api/admin/breakage/overview 的数据源）。
// 全部按调用方传入的租户作用域聚合（主站 nil → 全平台 tenant_id<>0；代理 → 本租户）。
//
// 4 指标口径（实现必须照此，见 requirements 验收判据①）：
//
//	ActiveRemainingUSD 活跃套餐剩余 = Σ(month_limit_usd - used_usd)
//	    FROM tokenplan_subscriptions WHERE status='active' AND expire_at > now AND tenant_id 作用域内。
//	    （惰性过期：status='active' 且 expire_at>now 双条件，不单信持久化 status。）
//	ExpiredUnusedUSD  到期未使用 = Σ(month_limit_usd - used_usd)
//	    FROM tokenplan_subscriptions WHERE expire_at < now AND (month_limit_usd - used_usd) > 0 AND tenant_id 作用域内。
//	    （按 expire_at<now 判到期，不看 status；这是惰性过期口径的核心。）
//	WalletUnusedUSD   钱包未消耗 = Σ(balance_usd)
//	    FROM user_balances WHERE tenant_id 作用域内。balance_usd 已是 USD（decimal(20,8)），直接求和不换算。
//	AnomalyCount      系统异常卡单数（引用对账）= COUNT payment_orders WHERE status IN ('created','paid')
//	    AND created_at < now-5min（卡在已下单未入账的中间态；沿用 reconcile_loop.go reconcileMinAge=5min 口径）。
//	    数据源即支付概览的 created/paid 两态（payment_overview.go）；管理端跨租户不加 tenant 过滤，
//	    代理作用域按 tenant_id=*tenantID 过滤。整数，精确不换算。
type Overview struct {
	// ActiveRemainingUSD 活跃套餐剩余额度合计（USD）：仍在有效期内的订阅未用完的部分。
	ActiveRemainingUSD float64
	// ExpiredUnusedUSD 到期未使用余额合计（USD）：已过期订阅里没用完、就此沉淀的部分（breakage 主体）。
	ExpiredUnusedUSD float64
	// WalletUnusedUSD 钱包未消耗余额合计（USD）：用户钱包里充值了但还没消费的部分。
	WalletUnusedUSD float64
	// AnomalyCount 系统异常笔数：已付未激活 / 卡单等中间态订单数（引用对账口径）。整数。
	AnomalyCount int
}

// DetailRow 是 breakage 明细表的一行（订阅维；GET /api/admin/breakage/detail 的数据源）。
// 一行 = 一个 tokenplan_subscriptions 实例，附带回填的租户名/用户名/套餐 code + 计算派生字段。
type DetailRow struct {
	TenantID   int64      // 归属租户；主站跨租户视图每行标注，代理视图恒为本租户。
	TenantName string     // 回填自 tenants.name（取不到给空，非关键字段）。
	UserID     int64      // 订阅所属用户。
	Username   string     // 回填自 users.username（取不到给空）。
	PlanCode   string     // 回填自 token_plans.code（订阅对应套餐的自然键）。
	Status     string     // 订阅状态字符串：active/exhausted/expired/refunded（惰性过期后落库值）。
	LimitUSD   float64    // 本期额度上限（= month_limit_usd）。
	UsedUSD    float64    // 本期已消耗（= used_usd）。
	UnusedUSD  float64    // 未使用 = LimitUSD - UsedUSD（<0 归零；breakage 关注量）。
	UsagePct   float64    // 用量占比 = UsedUSD/LimitUSD*100（limit<=0 记 0，防除零；对齐 usagePct()）。
	AlertLevel AlertLevel // 告警级别（AlertLevelForPct 计算）。
	// PeriodEnd 本期到期时间（epoch 秒）。惰性过期判定与展示都用它；handler 层转 ISO-8601 UTC。
	PeriodEnd int64
}

// Snapshot 是 breakage_snapshots 表的一行快照（GET /api/admin/breakage/snapshots 的数据源）。
// 快照 job（master-only StartBreakageSnapshotLoop）按 period_end 把每个订阅的 breakage 定格落表，
// 提供历史趋势 + 避免事后全表重算。唯一键 = (tenant_id, user_id, sub_id, period_end) 保证 Upsert 幂等。
//
// 注意：Snapshot 是「订阅维快照行」（一订阅一期一行），趋势展示时由 Repo.SnapshotTrend 按 period_end
// 分桶聚合成时间序列（见 SnapshotPoint）。回填历史（BackfillSnapshots）与 job 都 Upsert 这张表。
type Snapshot struct {
	TenantID  int64   // 归属租户（<>0）。
	UserID    int64   // 订阅所属用户。
	SubID     int64   // tokenplan_subscriptions.id。
	PlanCode  string  // 快照时的套餐 code（冗余存，避免趋势查询再 JOIN）。
	LimitUSD  float64 // 快照时的额度上限（month_limit_usd）。
	UsedUSD   float64 // 快照时已消耗（used_usd）。
	UnusedUSD float64 // 快照时未使用（= LimitUSD - UsedUSD，<0 归零）。
	Status    string  // 快照时订阅状态。
	// PeriodEnd 本期到期时间（epoch 秒）= 快照的时间锚点，也是唯一键的一部分。
	PeriodEnd int64
	// SnapshotAt 落快照的时刻（epoch 秒）：同一 period_end 可被 job/回填多次 Upsert，取最后一次。
	SnapshotAt int64
}

// SnapshotPoint 是快照趋势的一个时间桶（按 period_end 聚合）：GET /api/admin/breakage/snapshots 的
// 每个数据点。把同一 period_end（同一「期」）内、作用域内所有订阅的 breakage 汇总成一条。
type SnapshotPoint struct {
	// PeriodEnd 该桶的期末时间（epoch 秒）= 桶锚点，前端按东八区/UTC 格式化为横轴标签。
	PeriodEnd int64
	// ExpiredUnusedUSD 该期到期未使用合计（USD）：趋势主指标（breakage 沉淀随时间的走势）。
	ExpiredUnusedUSD float64
	// ActiveRemainingUSD 该期活跃剩余合计（USD，快照当时口径）。
	ActiveRemainingUSD float64
	// SubscriptionCount 该期纳入快照的订阅条数（整数）。
	SubscriptionCount int
}

// Filter 是明细/快照查询的筛选条件（handler 从 query 解析后传入，tenant_id 除外）。
type Filter struct {
	// TenantID 租户作用域：nil = 管理端跨租户（tenant_id<>0）；非 nil = 单租户。
	// 【安全】只由 handler 从鉴权中间件（tenantFrom(c)）注入，绝不从 query/body 读。
	TenantID *int64
	// PlanCode 按套餐 code 过滤（空 = 不过滤）。
	PlanCode string
	// Start / End 期末时间区间（epoch 秒；按 period_end / expire_at 过滤）。<=0 视为不设该端。
	Start int64
	End   int64
	// AlertLevel 按告警级别过滤（空 = 不过滤；否则取 warn/critical/exhausted）。
	AlertLevel AlertLevel
	// Page / PageSize 分页（page<=0→1，page_size<=0→默认，实现侧兜底归一化）。
	Page     int
	PageSize int
}

// ============================================================================
// 仓储契约（实现：internal/breakage/gormrepo，raw db.Table 聚合 + 快照读写）
// ============================================================================

// Repo 是 breakage 聚合仓储契约。全部方法带 ctx；聚合方法带 tenantID *int64 作用域参数
// （nil=跨租户 tenant_id<>0；非 nil=单租户）。实现走 raw db.Table()/Joins() SUM/GROUP BY，
// 不 import 兄弟模块 model 结构（升级安全）。
type Repo interface {
	// Overview 聚合 4 指标（活跃剩余/到期未用/钱包未消耗/系统异常），按 tenantID 作用域 + now 判到期。
	// now 由调用方传入（可测；实现按 expire_at 与 now 比较判惰性过期）。
	Overview(ctx context.Context, tenantID *int64, now int64) (Overview, error)

	// Detail 分页返回订阅维明细（含派生 UnusedUSD/UsagePct/AlertLevel），按 f 筛选。
	// now 用于惰性过期口径 + 计算派生字段。返回 (行, 匹配总数, error)。
	Detail(ctx context.Context, f Filter, now int64) ([]DetailRow, int64, error)

	// UpsertSnapshots 幂等写入一批快照行（唯一键 (tenant_id,user_id,sub_id,period_end)，
	// 冲突则更新 limit/used/unused/status/snapshot_at）。快照 job 与回填共用，可重跑不重复。
	UpsertSnapshots(ctx context.Context, rows []Snapshot) error

	// SnapshotTrend 读快照趋势：按 period_end 分桶聚合 SnapshotPoint，tenantID 作用域 + [start,end] 期末区间。
	// 返回按 period_end 升序的时间序列。
	SnapshotTrend(ctx context.Context, tenantID *int64, start, end int64) ([]SnapshotPoint, error)

	// CollectSnapshots 从活/历史订阅装配「当下应落库」的快照行（供 job 与回填共用）。
	// tenantID=nil 跨租户全量；now 判到期口径。实现只读订阅台账，不改额度/状态。
	// 供 UpsertSnapshots 的入参来源；分离「采集」与「落库」便于回填按游标分批。
	CollectSnapshots(ctx context.Context, tenantID *int64, now int64) ([]Snapshot, error)
}

// ============================================================================
// 服务门面（实现：internal/breakage/service.go；handler 只调 Service，不直接碰 Repo）
// ============================================================================

// Service 是 handler（internal/mtwire/breakage.go）调用的门面。薄封装 Repo：透传租户作用域、
// 组装派生指标、驱动快照 job。所有金额仍 float64（round2 在 handler 边界做，Service 不舍入）。
type Service interface {
	// Overview 返回 4 指标总览（透传 tenantID 作用域 + now）。
	Overview(ctx context.Context, tenantID *int64, now int64) (Overview, error)

	// Detail 分页明细（透传 Filter + now）。返回 (行, 总数, error)。
	Detail(ctx context.Context, f Filter, now int64) ([]DetailRow, int64, error)

	// Snapshots 读快照趋势（透传 tenantID 作用域 + 期末区间）。
	Snapshots(ctx context.Context, tenantID *int64, start, end int64) ([]SnapshotPoint, error)

	// RunSnapshot 采集当下全平台快照并幂等 Upsert（master-only job 与一次性回填共用）。
	// tenantID=nil 跨租户全量。返回落库的快照条数（best-effort，供 job 记日志）。
	RunSnapshot(ctx context.Context, tenantID *int64, now int64) (int, error)
}

// ============================================================================
// 错误码（模块前缀 BREAKAGE_；由入口中间件统一转 HTTP + JSON，见 apperr）
// ============================================================================

var (
	// ErrRangeInvalid 期末时间区间非法（缺失/start>end/跨度超上限）。快照趋势端点用。
	ErrRangeInvalid = apperr.New("BREAKAGE_RANGE_INVALID", "时间区间非法", http.StatusBadRequest)
	// ErrAlertLevelInvalid alert_level 过滤参数非法（仅 warn/critical/exhausted）。
	ErrAlertLevelInvalid = apperr.New("BREAKAGE_ALERT_LEVEL_INVALID", "告警级别非法（仅 warn/critical/exhausted）", http.StatusBadRequest)
	// ErrFormatInvalid 导出格式非法（仅 csv）。detail 端点 ?format 用。
	ErrFormatInvalid = apperr.New("BREAKAGE_FORMAT_INVALID", "导出格式非法（仅 csv）", http.StatusBadRequest)
	// ErrExportFailed 导出渲染/写出失败。
	ErrExportFailed = apperr.New("BREAKAGE_EXPORT_FAILED", "导出失败", http.StatusInternalServerError)
)
