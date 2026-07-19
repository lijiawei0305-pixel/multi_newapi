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
// tokenplan (套餐) — Admin types
//
// Mirrors doc/api-contract.md §2.4 / §3 (Plan object) plus the admin-only
// fields (cost / protection price) listed in §2.4. The backend handler at
// `/api/admin/token-plans` is delivered by the parallel 5a worker; this
// module is contract-first and aligns with that endpoint during integration.
// ============================================================================

/** Plan availability. The doc lists "状态" (status) as an admin column/field. */
export const tokenPlanStatusValues = ['enabled', 'disabled'] as const
export type TokenPlanStatus = (typeof tokenPlanStatusValues)[number]

/**
 * Admin view of a tokenplan. Superset of the buyer-facing Plan (§3):
 * adds the admin-only `cost_price_cny` (成本价) and `min_price_cny`
 * (保护线 — the floor an agent's retail price may not undercut) and `status`.
 *
 * Currency convention (§1): `*_cny` are CNY ¥ (price/earnings), `*_usd` are
 * USD quota/metering. Never mix the two.
 */
export interface AdminTokenPlan {
  id: number
  code: string
  name: string
  /** 售价 — site base selling price (¥). */
  base_price_cny: number
  /** 原价 — strikethrough marketing anchor (¥); not billed. */
  anchor_price_cny: number
  /** Current site retail price (¥); agents may override within [min, …]. */
  retail_price_cny?: number
  /** 成本价 — main-site cost basis (¥), admin only. */
  cost_price_cny: number
  /** 保护线 — lowest retail price agents may set (¥), admin only. */
  min_price_cny: number
  /** 月限额 — monthly usage cap (USD). */
  month_limit_usd: number
  /** Billing multiplier applied to plan-bucket usage. */
  multiplier: number
  /** Subscription validity window in days (e.g. 30). */
  valid_days: number
  /** 折扣文案 — e.g. "-91%". */
  discount_label?: string
  /** Marketing badge text (e.g. "热销"). */
  badge?: string
  /** 推荐 — highlight on the purchase page. */
  is_recommended: boolean
  /** 排序 — ascending display order. */
  sort: number
  status: TokenPlanStatus
}

/** Unified new-api control-plane envelope: `{ success, message, data }`. */
export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  /** Error code per api-contract §1.1 (e.g. PLAN_NOT_FOUND); optional. */
  code?: string
  data?: T
}

export type TokenPlanPayload = Partial<
  Omit<AdminTokenPlan, 'id' | 'retail_price_cny'>
>

export type TokenPlansDialogType = 'create' | 'update' | 'toggle-status'
