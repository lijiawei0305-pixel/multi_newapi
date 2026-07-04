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
 * A publicly-visible **agent** plan (代理套餐), served by the no-auth endpoint
 * `GET /api/agent-plans/public`. These are the tiers a user buys to become an
 * agent (一次性 + 有效期) — distinct from user token-usage plans. Mirrors the
 * backend `publicAgentPlanOut` DTO: display fields + capability flags only, no
 * internal cost/discount-ratio.
 */
export interface PublicAgentPlan {
  code: string
  name: string
  description: string
  price_cny: number
  anchor_price_cny: number
  discount_label: string
  badge: string
  is_recommended: boolean
  valid_days: number
  grant_level: number
  grant_can_api: boolean
  sort: number
}

interface ApiEnvelope<T> {
  success: boolean
  message?: string
  data?: T
}

/**
 * Fetch the main-site's on-sale agent tiers for the public landing page.
 * No auth required. Returns [] on any envelope shape it doesn't recognise so
 * the caller can transparently fall back to static copy.
 */
export async function getPublicAgentPlans(): Promise<PublicAgentPlan[]> {
  const res = await api.get<ApiEnvelope<PublicAgentPlan[]>>(
    '/api/agent-plans/public'
  )
  return res.data?.data ?? []
}
