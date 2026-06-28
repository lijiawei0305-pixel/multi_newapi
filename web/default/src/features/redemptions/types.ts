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
// Agent self-service: redemption codes.
//   GET  /api/tenant/redemptions
//   POST /api/tenant/redemptions { amount_usd, count }
// Generating `count` codes pre-deducts `count × amount_usd` from the agent's
// own quota. The backend scopes the listing to the calling owner (agent).
// ============================================================================

export interface Redemption {
  id: number
  code: string
  /** Face value of the code (USD). */
  amount_usd: number
  status: string
  used_by_user_id?: number | null
  created_at?: number | string
}

export interface CreateRedemptionPayload {
  amount_usd: number
  count: number
}

/** Unified new-api control-plane envelope: `{ success, message, data }`. */
export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  code?: string
  data?: T
}
