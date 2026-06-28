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
// Agent self-service: model-group multipliers.
//   GET /api/tenant/groups
//   PUT /api/tenant/groups/:group { ratio }
// Each row is a model group. The agent's `ratio` may only be set at or above
// the main-site baseline (`platform_ratio` = `floor`) — i.e. mark up only, to
// earn the spread. The backend rejects values below the floor.
// ============================================================================

export interface TenantGroup {
  group_name: string
  /** Tenant's current effective multiplier (the override, or the baseline). */
  ratio: number
  /** Main-site baseline multiplier; equals `floor`. */
  platform_ratio: number
  /** Lowest multiplier the agent may set for this group (= platform_ratio). */
  floor: number
  /** Whether this tenant has set an override above the baseline. */
  has_override: boolean
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
