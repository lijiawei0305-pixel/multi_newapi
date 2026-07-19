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
  AdminTokenPlan,
  ApiResponse,
  TokenPlanPayload,
  TokenPlanStatus,
} from './types'

// ============================================================================
// Admin tokenplan CRUD — api-contract §2.4
//   GET/POST/PATCH /api/admin/token-plans[/:id]
// Auth is carried by new-api's shared axios instance (session cookie +
// New-Api-User header), identical to every other admin page.
// ============================================================================

export async function getAdminTokenPlans(): Promise<
  ApiResponse<AdminTokenPlan[]>
> {
  const res = await api.get('/api/admin/token-plans')
  return res.data
}

export async function createTokenPlan(
  data: TokenPlanPayload
): Promise<ApiResponse<AdminTokenPlan>> {
  const res = await api.post('/api/admin/token-plans', data)
  return res.data
}

export async function updateTokenPlan(
  id: number,
  data: TokenPlanPayload
): Promise<ApiResponse<AdminTokenPlan>> {
  const res = await api.patch(`/api/admin/token-plans/${id}`, data)
  return res.data
}

export async function patchTokenPlanStatus(
  id: number,
  status: TokenPlanStatus
): Promise<ApiResponse> {
  const res = await api.patch(`/api/admin/token-plans/${id}`, { status })
  return res.data
}
