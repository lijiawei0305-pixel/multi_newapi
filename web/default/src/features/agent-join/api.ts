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
import { api } from '@/lib/api'

/**
 * A publicly-visible plan card, served by the no-auth endpoint
 * `GET /api/tenant/token-plans/public` (main-site enabled plans, official base
 * price). Mirrors the backend `publicPlanOut` DTO — display fields only, no
 * internal cost/floor prices.
 */
export interface PublicTokenPlan {
  code: string
  name: string
  base_price_cny: number
  anchor_price_cny: number
  discount_label: string
  badge: string
  is_recommended: boolean
  month_limit_usd: number
  valid_days: number
  sort: number
}

interface ApiEnvelope<T> {
  success: boolean
  message?: string
  data?: T
}

/**
 * Fetch the main-site's on-sale plans for the public landing page.
 * No auth required. Returns [] on any envelope shape it doesn't recognise so
 * the caller can transparently fall back to static copy.
 */
export async function getPublicTokenPlans(): Promise<PublicTokenPlan[]> {
  const res = await api.get<ApiEnvelope<PublicTokenPlan[]>>(
    '/api/tenant/token-plans/public'
  )
  return res.data?.data ?? []
}
