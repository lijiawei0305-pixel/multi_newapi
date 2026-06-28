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
// Agent self-service: promotion channels (signup attribution).
//   GET  /api/tenant/promotion/channels
//   POST /api/tenant/promotion/channels { name }
// Each channel exposes a dedicated link `/sign-up?channel=<code>`; users who
// register through it are attributed to this agent's `registered_count`.
// ============================================================================

export interface PromotionChannel {
  id: number
  code: string
  name: string
  /** Number of users who signed up through this channel. */
  registered_count: number
  created_at?: number | string
}

/** Unified new-api control-plane envelope: `{ success, message, data }`. */
export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  code?: string
  data?: T
}
