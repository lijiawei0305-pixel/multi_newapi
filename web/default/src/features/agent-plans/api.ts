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

/** 公开代理套餐（一次性 + 有效期）；GET /api/agent-plans/public，无需登录。 */
export interface AgentPlan {
  id: number
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

/** 购买响应（与充值/套餐购买同形）：pay_url 与 pay.* 同值。 */
export interface AgentPlanPurchaseResult {
  order_no?: string
  pay_url?: string
  amount_cny?: number
  plan_id?: number
  pay?: { wxpay_qr?: string; alipay_url?: string }
}

interface ApiEnvelope<T> {
  success: boolean
  message?: string
  data?: T
}

/** 拉取主站在售代理套餐（enabled，按 sort 排序）。无需登录。 */
export async function getPublicAgentPlans(): Promise<AgentPlan[]> {
  const res = await api.get<ApiEnvelope<AgentPlan[]>>('/api/agent-plans/public')
  return res.data?.data ?? []
}

/**
 * 购买一档代理套餐（控制台，需登录）。provider 选官方微信/支付宝；slug/name 仅新开通代理时用于建站点
 * （已是代理则后端忽略、升级既有租户）。返回支付凭据。
 */
export async function purchaseAgentPlan(
  planId: number,
  args: { provider: 'wxpay' | 'alipay'; slug?: string; name?: string }
): Promise<ApiEnvelope<AgentPlanPurchaseResult>> {
  const res = await api.post<ApiEnvelope<AgentPlanPurchaseResult>>(
    `/api/tenant/agent-plans/${planId}/purchase`,
    args
  )
  return res.data
}
