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
// Admin subscription monitoring — backed by GET /api/admin/subscriptions.
// Read-only oversight of every buyer subscription, with usage + alerting.
// ============================================================================

/** Server-assigned severity for a subscription's quota usage. */
export const alertLevelValues = ['warn', 'critical', 'exhausted'] as const
export type AlertLevel = (typeof alertLevelValues)[number]

/** One row of the admin subscription monitor table. */
export interface MonitorSubscription {
  user_id: number
  username: string
  plan_code: string
  status: string
  /** Quota already consumed this period (USD). */
  used_usd: number
  /** Quota ceiling for this period (USD). */
  limit_usd: number
  /** Server-computed usage percentage [0..100]. */
  usage_pct: number
  /** Empty / absent means healthy; otherwise warn < critical < exhausted. */
  alert_level?: AlertLevel | '' | null
  /** Period end — unix seconds, unix ms, or ISO string (formatter is tolerant). */
  period_end?: number | string
}

/** Unified new-api control-plane envelope: `{ success, message, data }`. */
export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  code?: string
  data?: T
}
