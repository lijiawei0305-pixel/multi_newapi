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
// Agent self-service: my downstream users.
//   GET  /api/tenant/users               — list (scoped to the calling agent)
//   PUT  /api/tenant/users/:id/tier      — set a user's membership tier
// `quota` / `used_quota` are raw quota (500000 = $1); displayed in USD.
// ============================================================================

/** Membership tier an agent may assign to one of its downstream users. */
export type UserTier = 'default' | 'vip'

export interface TenantUser {
  id: number
  username: string
  display_name?: string
  quota: number
  used_quota: number
  status: number | string
  created_at?: number | string
  /**
   * Current tier (native user `group`). The list endpoint does not yet return
   * it; kept optional so the Tier column reflects it if/when the backend adds
   * it, while freshly-set tiers are tracked optimistically in the page.
   */
  group?: string
}

export interface SetUserTierPayload {
  tier: UserTier
}

/** PUT /api/tenant/users/:id/tier success payload. */
export interface SetUserTierResult {
  id: number
  tier: string
}

/** Unified new-api control-plane envelope: `{ success, message, data }`. */
export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  code?: string
  data?: T
}
