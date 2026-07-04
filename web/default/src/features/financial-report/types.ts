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
import type { ApiResponse } from '@/features/agent-earnings/types'

// Re-export the shared control-plane envelope so this feature has a single
// canonical `ApiResponse<T>` import surface.
export type { ApiResponse }

// ============================================================================
// Financial Reporting — FROZEN CONTRACT types (doc/finance-report-contract.md).
//
// Wire rules (contract §3):
//   - Money = plain JSON number (float64); currency encoded by field suffix:
//       `*_cny` → CNY ¥ ,  `*_usd` → USD $ . Never mix _usd and _cny.
//   - Raw quota / token / call counts = exact integers.
//   - Request time range = epoch seconds int64 (`start_timestamp`/`end_timestamp`).
//   - Trend bucket = calendar `bucket` label + epoch-seconds `bucket_ts`.
//   - Response timestamps = ISO-8601 UTC strings (may also arrive as unix; the
//     `lib` formatters tolerate both).
// ============================================================================

// ---------------------------------------------------------------------------
// Enums (FROZEN — contract §1 common params)
// ---------------------------------------------------------------------------

/** Earning ledger source kinds (`internal/agent/model.go:68-81`). */
export type SourceType =
  | 'recharge_spread'
  | 'consume_commission'
  | 'ratio_markup'
  | 'tokenplan_spread'
  | 'tokenplan_commission'
  | 'manual_adjustment'

/** Withdrawal lifecycle (`model.go:152-159`). NO `withdrawn`/`frozen`/`paid`. */
export type WithdrawalStatus = 'pending' | 'approved' | 'rejected'

/** The four data lenses driving trend + detail. */
export type Lens = 'earnings' | 'recharge' | 'consumption' | 'withdrawals'

/** Trend bucket granularity. */
export type Granularity = 'day' | 'week' | 'month'

/** Sort direction for the admin ranking. */
export type SortOrder = 'asc' | 'desc'

/** Export file formats accepted by the `detail` endpoints. */
export type ExportFormat = 'csv' | 'pdf'

/** Sortable numeric columns of the admin agent ranking (contract §1.3). */
export type AgentSortBy =
  | 'total_earned_cny'
  | 'recharge_paid_cny'
  | 'subscription_paid_cny'
  | 'consumption_cost_cny'
  | 'consumption_used_quota'
  | 'withdrawn_cny'
  | 'pending_withdraw_cny'
  | 'withdrawable_cny'

// ---------------------------------------------------------------------------
// Summary (contract §1.1 / §1.5)
// ---------------------------------------------------------------------------

/** Echo of the requested epoch-seconds window. */
export interface FinanceRange {
  start_timestamp: number
  end_timestamp: number
}

/** One `{ source_type, amount_cny }` ledger bucket (`amount_cny` may be < 0). */
export interface EarningSourceSum {
  source_type: SourceType
  amount_cny: number
}

/** Aggregated wallet balances (admin = Σ across agents; tenant = single row). */
export interface WalletTotal {
  withdrawable_cny: number
  frozen_cny: number
  total_earned_cny: number
  api_balance_usd: number
}

export interface EarningsSummary {
  total_earned_cny: number
  by_source: EarningSourceSum[]
  wallet_total: WalletTotal
}

export interface RechargeSummary {
  recharge_paid_cny: number
  subscription_paid_cny: number
  subscription_cost_cny: number
  subscription_spread_cny: number
  recharge_spread_cny: number
}

export interface ConsumptionSummary {
  used_quota: number
  used_cost_usd: number
  used_cost_cny: number
  calls: number
  tokens: number
}

export interface WithdrawalsSummary {
  pending_cny: number
  frozen_cny: number
  withdrawn_cny: number
  rejected_cny: number
}

export interface ExchangeInfo {
  quota_per_unit: number
  usd_exchange_rate: number
}

// ---------------------------------------------------------------------------
// v3 Overview (doc/finance-model-report-v3.md §二) — scope-specific KPI totals
// surfaced at `data.overview`. Agent and admin shapes don't overlap field-for-
// field (mirrors backend internal/mtwire/report.go agentFinanceOverviewOut /
// adminFinanceOverviewOut, itself mirroring the `trendOut.Series any` /
// `TrendResponse<P>` precedent below), so `FinanceSummary` is generic over
// which one applies instead of one flattened supertype padded with fields the
// other scope doesn't have.
// ---------------------------------------------------------------------------

/** Agent self-service overview (4 fields, §二「代理报表」). */
export interface AgentFinanceOverview {
  /** 套餐收益(用户支付金额) — Σ 售价，购买时计（同 recharge.subscription_paid_cny）。 */
  tokenplan_revenue_cny: number
  /** 套餐可提现 — 代理套餐差价，购买时一次性入账、立即可提现（= Σ tokenplan_spread）。 */
  tokenplan_withdrawable_cny: number
  /**
   * apikey 消费收益 — 用户消耗的钱包余额。
   * ⚠️ 口径缺口（临时，见后端 agentFinanceOverviewOut 注释）：现有数据无法把钱包桶消耗和套餐桶
   * 消耗干净分开，本字段当前 = 两者合计（上界，非纯钱包值）。
   */
  apikey_consumption_cny: number
  /**
   * 消耗可提现 — 代理消耗差价，消耗时逐笔实时入账、立即可提现
   * （= Σ ratio_markup + consume_commission）。
   */
  consumption_withdrawable_cny: number
}

/** Admin cross-tenant overview (6 fields, §二「管理员报表」，主站/代理站分列). */
export interface AdminFinanceOverview {
  /** 主站套餐收益 — 平台直营（platform 租户）用户买套餐的支付总额。 */
  mainsite_tokenplan_revenue_cny: number
  /** 代理站套餐收益 — 各代理站用户买套餐的支付总额(合计)。 */
  agent_tokenplan_revenue_cny: number
  /** 给代理的套餐返现 — Σ 各代理 tokenplan_spread（不含主站）。 */
  tokenplan_rebate_cny: number
  /** 主站钱包消耗 — 同 AgentFinanceOverview.apikey_consumption_cny 口径缺口，当前为上界。 */
  mainsite_wallet_consumption_cny: number
  /** 代理站钱包消耗(合计) — 同上口径缺口，当前为上界。 */
  agent_wallet_consumption_cny: number
  /** 需返现代理的 api 消耗金额 — Σ 各代理 (ratio_markup + consume_commission)，不含主站。 */
  agent_api_rebate_cny: number
}

/** Either scope's overview shape (narrow via which fields are declared). */
export type FinanceOverview = AgentFinanceOverview | AdminFinanceOverview

/**
 * `data` of `GET /api/{admin,tenant}/finance/summary`. `O` pins `overview` to
 * the calling scope's shape (agent vs admin) — callers specify it explicitly
 * (see `api.ts`) rather than relying on the union default.
 */
export interface FinanceSummary<O extends FinanceOverview = FinanceOverview> {
  range: FinanceRange
  earnings: EarningsSummary
  recharge: RechargeSummary
  consumption: ConsumptionSummary
  withdrawals: WithdrawalsSummary
  exchange: ExchangeInfo
  overview: O
}

// ---------------------------------------------------------------------------
// Trend (contract §1.2 / §1.6) — per-lens series points
// ---------------------------------------------------------------------------

/** Fields every series point carries regardless of lens. */
export interface TrendPointBase {
  /** Calendar label: day `YYYY-MM-DD`, week `YYYY-MM-DD` (Mon), month `YYYY-MM`. */
  bucket: string
  /** Epoch seconds (UTC) of the bucket start, for the chart time axis. */
  bucket_ts: number
}

export interface EarningsTrendPoint extends TrendPointBase {
  amount_cny: number
  /** 套餐可提现（Σ tokenplan_spread），供代理「我的收益」3 线趋势图使用。 */
  tokenplan_withdrawable_cny: number
  /** apikey 消费可提现（Σ ratio_markup + consume_commission），同上。 */
  consumption_withdrawable_cny: number
}

export interface RechargeTrendPoint extends TrendPointBase {
  recharge_paid_cny: number
  subscription_paid_cny: number
  subscription_cost_cny: number
  subscription_spread_cny: number
}

export interface ConsumptionTrendPoint extends TrendPointBase {
  used_quota: number
  used_cost_cny: number
  calls: number
  tokens: number
}

export interface WithdrawalsTrendPoint extends TrendPointBase {
  pending_cny: number
  withdrawn_cny: number
  rejected_cny: number
}

/** Discriminate by the active `lens`; narrow with the matching point type. */
export type TrendPoint =
  | EarningsTrendPoint
  | RechargeTrendPoint
  | ConsumptionTrendPoint
  | WithdrawalsTrendPoint

/** `data` of `GET /api/{admin,tenant}/finance/trend`. */
export interface TrendResponse<P extends TrendPointBase = TrendPoint> {
  lens: Lens
  granularity: Granularity
  series: P[]
}

// ---------------------------------------------------------------------------
// Admin agent ranking (contract §1.3)
// ---------------------------------------------------------------------------

export interface AgentRankRow {
  tenant_id: number
  agent_name: string
  owner_user_id: number
  owner_username: string
  total_earned_cny: number
  consume_commission_cny: number
  ratio_markup_cny: number
  tokenplan_spread_cny: number
  manual_adjustment_cny: number
  recharge_paid_cny: number
  subscription_paid_cny: number
  subscription_cost_cny: number
  consumption_used_quota: number
  consumption_cost_cny: number
  withdrawn_cny: number
  pending_withdraw_cny: number
  withdrawable_cny: number
}

/** `data` of `GET /api/admin/finance/agents`. */
export interface AgentRankResponse {
  items: AgentRankRow[]
  total: number
  page: number
  page_size: number
  sort_by: AgentSortBy
  order: SortOrder
}

// ---------------------------------------------------------------------------
// Detail rows (contract §1.4 / §1.7) — per-lens unions.
// `tenant_id` / `agent_name` are present for ADMIN scope and omitted for the
// single-tenant agent scope, hence optional.
// ---------------------------------------------------------------------------

export interface EarningsDetailItem {
  tenant_id?: number
  agent_name?: string
  source_type: SourceType
  amount_cny: number
  reference: string
  created_at: string
}

export interface WithdrawalsDetailItem {
  id: number
  tenant_id?: number
  agent_name?: string
  amount_cny: number
  status: WithdrawalStatus
  created_at: string
  reviewed_at: string
}

export interface RechargeDetailItem {
  order_no: string
  tenant_id?: number
  kind: 'recharge' | 'subscription'
  provider: string
  amount_usd: number
  actual_paid_cny: number
  agent_cost_price_cny: number
  status: string
  created_at: string
}

export interface ConsumptionDetailItem {
  tenant_id?: number
  agent_name?: string
  model_name: string
  calls: number
  tokens: number
  used_quota: number
  used_cost_cny: number
}

/** Any detail row, discriminated by the active `lens`. */
export type DetailItem =
  | EarningsDetailItem
  | WithdrawalsDetailItem
  | RechargeDetailItem
  | ConsumptionDetailItem

/** `data` of any `detail` endpoint (pagination nested inside `data`, §7.2). */
export interface DetailResponse<T = DetailItem> {
  items: T[]
  total: number
  page: number
  page_size: number
}

// ---------------------------------------------------------------------------
// Request param shapes (snake_case on the wire, contract §1 common params)
// ---------------------------------------------------------------------------

/** Epoch-seconds window shared by every endpoint. */
export interface RangeParams {
  start_timestamp: number
  end_timestamp: number
}

export interface TrendParams extends RangeParams {
  granularity: Granularity
  lens: Lens
}

export interface AgentRankParams extends RangeParams {
  sort_by?: AgentSortBy
  order?: SortOrder
  page?: number
  page_size?: number
}

export interface DetailParams extends RangeParams {
  lens: Lens
  page?: number
  page_size?: number
  /** Present → server streams a file; absent → JSON. */
  format?: ExportFormat
}
