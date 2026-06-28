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
// Agent self-service: user-group multipliers.
//   GET /api/tenant/groups
//   PUT /api/tenant/groups/:group { ratio }
// `ratio` is the billing multiplier for the group; the backend rejects a value
// below the group's `floor`. Wiring the multiplier into billing is a follow-up.
// ============================================================================

export interface TenantGroup {
  group_name: string
  ratio: number
  /** Lowest ratio the agent may set for this group. */
  floor: number
}

export interface UpdateGroupPayload {
  ratio: number
}

/** Unified new-api control-plane envelope: `{ success, message, data }`. */
export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  code?: string
  data?: T
}
