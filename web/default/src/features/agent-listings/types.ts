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
// Agent self-service: plan listings & pricing.
//   GET /api/tenant/token-plans/listings
//   PUT /api/tenant/token-plans/listings/:planId { retail_price_cny, enabled }
// The backend scopes the response to the calling owner (agent) and rejects a
// retail price below the plan's protection floor (`min_price_cny`).
// ============================================================================

/** One plan the agent may list and re-price. */
export interface AgentListing {
  plan_id: number
  code: string
  name: string
  /** 售价 — main-site base selling price (¥). */
  base_price_cny: number
  /** 保护线 — lowest retail price the agent may set (¥). */
  min_price_cny: number
  /** 原价 — strikethrough marketing anchor (¥); not billed. */
  anchor_price_cny: number
  /** 月限额 — monthly usage cap (USD). */
  month_limit_usd: number
  /** The agent's own retail price (¥); must be >= min_price_cny. */
  retail_price_cny: number
  /** Whether the agent currently lists this plan. */
  enabled: boolean
}

export interface UpdateListingPayload {
  retail_price_cny: number
  enabled: boolean
}

/** Unified new-api control-plane envelope: `{ success, message, data }`. */
export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  code?: string
  data?: T
}
