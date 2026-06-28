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
import type {
  ApiResponse,
  PurchaseResult,
  TenantPlan,
  TenantSubscription,
} from './types'

// ============================================================================
// Buyer-facing tenant tokenplan endpoints.
// Auth is carried by new-api's shared axios instance (session cookie +
// New-Api-User header), identical to every other authenticated page.
// ============================================================================

/** List the plans on sale for the current tenant (purchase card grid). */
export async function getTenantTokenPlans(): Promise<
  ApiResponse<TenantPlan[]>
> {
  const res = await api.get('/api/tenant/token-plans')
  return res.data
}

/**
 * Purchase a plan. `idOrCode` is the plan's numeric id when available,
 * otherwise its code — both are accepted by the `:id` path segment.
 * `provider` (default wxpay) selects the auth-service mock pay page returned as
 * `pay.wxpay_qr` (QR) or `pay.alipay_url` (redirect) — same shape as recharge.
 */
export async function purchaseTokenPlan(
  idOrCode: number | string,
  provider: 'wxpay' | 'alipay' = 'wxpay'
): Promise<ApiResponse<PurchaseResult>> {
  const res = await api.post(`/api/tenant/token-plans/${idOrCode}/purchase`, {
    provider,
  })
  return res.data
}

/** The current buyer's own subscriptions (the "我的订阅" block). */
export async function getTenantSubscriptions(): Promise<
  ApiResponse<TenantSubscription[]>
> {
  const res = await api.get('/api/tenant/subscriptions')
  return res.data
}
