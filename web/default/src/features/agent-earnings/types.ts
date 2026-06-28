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
// Agent self-service earnings & withdrawals. Backed by:
//   GET  /api/tenant/earnings      — summary + earning detail rows
//   GET  /api/tenant/withdrawals   — this agent's withdrawal history
//   POST /api/tenant/withdrawals   { amount_cny }
// The backend scopes every response to the calling owner. All ¥ amounts.
// ============================================================================

/** One earning detail row. */
export interface Earning {
  source_type: string
  amount_cny: number
  reference?: string
  created_at?: number | string
}

/** Wallet headline figures shown in the top cards (¥). */
export interface EarningsSummary {
  withdrawable_cny: number
  frozen_cny: number
  total_earned_cny: number
}

/** One of the agent's own withdrawal requests. */
export interface MyWithdrawal {
  id: number
  amount_cny: number
  status: string
  created_at?: number | string
  reviewed_at?: number | string
}

/** Unified new-api control-plane envelope: `{ success, message, data }`. */
export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  code?: string
  data?: T
}
