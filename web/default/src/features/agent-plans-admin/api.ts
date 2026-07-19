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

/** 管理端代理套餐条目（含授予能力等内部字段）。镜像后端 adminAgentPlanOut。 */
export interface AdminAgentPlan {
  id: number
  code: string
  name: string
  description: string
  price_cny: number
  anchor_price_cny: number
  discount_label: string
  grant_level: number
  grant_can_api: boolean
  grant_discount_ratio: number
  valid_days: number
  is_recommended: boolean
  badge: string
  sort: number
  status: string // enabled | disabled
}

/** 创建/更新入参（后端 adminAgentPlanIn，全部可选 → 部分更新）。 */
export type AdminAgentPlanInput = Partial<Omit<AdminAgentPlan, 'id'>>

interface ApiEnvelope<T> {
  success: boolean
  message?: string
  data?: T
}

/** 列出全部代理套餐（管理员）。 */
export async function getAdminAgentPlans(): Promise<AdminAgentPlan[]> {
  const res = await api.get<ApiEnvelope<AdminAgentPlan[]>>(
    '/api/admin/agent-plans'
  )
  return res.data?.data ?? []
}

/** 新建代理套餐。 */
export async function createAgentPlan(
  input: AdminAgentPlanInput
): Promise<ApiEnvelope<AdminAgentPlan>> {
  const res = await api.post<ApiEnvelope<AdminAgentPlan>>(
    '/api/admin/agent-plans',
    input
  )
  return res.data
}

/** 全量更新代理套餐（含上下架状态）。 */
export async function updateAgentPlan(
  id: number,
  input: AdminAgentPlanInput
): Promise<ApiEnvelope<{ id: number }>> {
  const res = await api.patch<ApiEnvelope<{ id: number }>>(
    `/api/admin/agent-plans/${id}`,
    input
  )
  return res.data
}
