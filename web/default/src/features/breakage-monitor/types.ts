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

// ============================================================================
// Breakage 监控 (P2-BRK-01) — FROZEN backend contract types.
// breakage = 用户用不满导致的额度沉淀。整个定价模型建立在「用户只用约 1% 月限额」
// (proposal §2.4)，breakage 可观测是二期风控财务的核心缺口。
//
// Wire rules (与 financial-report 契约同):
//   - 金额 = 普通 JSON number (float64)，币种由字段后缀表达（本 feature 全 `_usd`），
//     后端 handler 边界已 round2；前端只负责展示格式化。
//   - 计数 (anomaly_count / subscription_count) = 精确整数。
//   - 请求时间区间 = epoch 秒 int64（start_timestamp / end_timestamp）。
//   - overview/detail 的响应时间戳 = ISO-8601 UTC 字符串（period_end）。
//   - snapshots series 的 period_end = epoch 秒（bucket 锚点，非 ISO）。
//
// 租户作用域由后端从鉴权中间件解析（主站看全平台 / 代理隔离本租户）。
// 前端「绝不」传 tenant_id — 明细里的 tenant_id/tenant_name 仅在跨租户(主站)时出现，
// 代理作用域下被 omitempty 省略，故为可选。
// ============================================================================

/** 统一 New API 控制面信封 `{ success, message, data }`。 */
export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  code?: string
  data?: T
}

// ---------------------------------------------------------------------------
// 枚举
// ---------------------------------------------------------------------------

/**
 * 明细行的告警级别。空字符串 = 健康（无告警）。
 * warn < critical < exhausted，与后端 detailOut.alert_level 对齐。
 */
export const alertLevelValues = ['warn', 'critical', 'exhausted'] as const
export type AlertLevel = (typeof alertLevelValues)[number]

/** 明细筛选可选的告警级（发往后端 `alert_level` 查询参数）。 */
export type AlertLevelFilter = AlertLevel

/** 导出格式：`''`/缺省 → JSON；`csv` → 触发文件下载。 */
export type ExportFormat = 'csv'

// ---------------------------------------------------------------------------
// 1) GET /api/admin/breakage/overview — 4 指标卡
// ---------------------------------------------------------------------------

/** `data` of overview：4 个即时指标（now = 服务器时间，无时间区间）。 */
export interface BreakageOverview {
  /** 活跃订阅的剩余额度 Σ(month_limit − used)，status=active（USD）。 */
  active_remaining_usd: number
  /** 到期未使用余额 Σ(month_limit − used)，按 expire_at < now 判定（USD）。 */
  expired_unused_usd: number
  /** 钱包未消耗余额 Σ(user_balances)（USD）。 */
  wallet_unused_usd: number
  /** 系统异常条数（整数计数）。 */
  anomaly_count: number
}

// ---------------------------------------------------------------------------
// 2) GET /api/admin/breakage/detail — 明细表（筛选 + 分页 + ?format=csv）
// ---------------------------------------------------------------------------

/** 明细一行（订阅维）。tenant_id/tenant_name 仅跨租户(主站)出现。 */
export interface BreakageDetailRow {
  /** 归属租户（主站跨全平台时出现；代理作用域被省略）。 */
  tenant_id?: number
  tenant_name?: string
  user_id: number
  username: string
  plan_code: string
  status: string
  /** 本期额度上限（USD）。 */
  limit_usd: number
  /** 本期已消耗（USD）。 */
  used_usd: number
  /** 本期未使用 = limit − used（USD）。 */
  unused_usd: number
  /** 使用率百分比 [0..100]，后端已算。 */
  usage_pct: number
  /** 空字符串 = 健康；否则 warn < critical < exhausted。 */
  alert_level: AlertLevel | ''
  /** 计费周期结束时间 — ISO-8601 UTC。 */
  period_end: string
}

/** `data` of detail（分页嵌套在 data 内，镜像 report detailOut）。 */
export interface BreakageDetailResponse {
  items: BreakageDetailRow[]
  total: number
  page: number
  page_size: number
}

// ---------------------------------------------------------------------------
// 3) GET /api/admin/breakage/snapshots — 历史快照趋势（按期）
// ---------------------------------------------------------------------------

/** 一个快照桶（按 period_end 升序）。 */
export interface BreakageSnapshotPoint {
  /** 桶锚点 — epoch 秒（注意：与 overview/detail 的 ISO 不同）。 */
  period_end: number
  /** 该期到期未使用余额 Σ（USD）。 */
  expired_unused_usd: number
  /** 该期活跃剩余额度 Σ（USD）。 */
  active_remaining_usd: number
  /** 该期订阅条数（整数计数）。 */
  subscription_count: number
}

/** `data` of snapshots。 */
export interface BreakageSnapshotResponse {
  series: BreakageSnapshotPoint[]
}

// ---------------------------------------------------------------------------
// 请求参数（wire 上一律 snake_case，epoch 秒）
// ---------------------------------------------------------------------------

/** epoch 秒时间窗（snapshots 必填；detail 可选筛 period_end）。 */
export interface RangeParams {
  start_timestamp: number
  end_timestamp: number
}

/** 明细查询参数。 */
export interface BreakageDetailParams {
  page?: number
  page_size?: number
  /** 档位过滤（token_plans.plan_code）。 */
  plan_code?: string
  /** 按 period_end 过滤的 epoch 秒窗（可选）。 */
  start_timestamp?: number
  end_timestamp?: number
  /** 告警级过滤（warn|critical|exhausted）。 */
  alert_level?: AlertLevelFilter
  /** 存在 → 服务端流式返回 CSV 文件；缺省 → JSON。 */
  format?: ExportFormat
}
