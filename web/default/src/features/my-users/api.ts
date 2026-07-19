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
  SetUserTierPayload,
  SetUserTierResult,
  TenantUser,
} from './types'

// Auth is carried by new-api's shared axios instance (session cookie +
// New-Api-User header); the backend scopes every response to the caller.

export async function getTenantUsers(): Promise<ApiResponse<TenantUser[]>> {
  const res = await api.get('/api/tenant/users')
  return res.data
}

/**
 * PUT /api/tenant/users/:id/tier — set a downstream user's membership tier.
 * `skipErrorHandler` lets us surface the stable error `code`
 * (AGENT_TIER_INVALID / AGENT_FORBIDDEN) as a localized toast instead of the
 * global interceptor's raw backend message.
 */
export async function setTenantUserTier(
  id: number,
  payload: SetUserTierPayload
): Promise<ApiResponse<SetUserTierResult>> {
  const res = await api.put(`/api/tenant/users/${id}/tier`, payload, {
    skipErrorHandler: true,
  })
  return res.data
}
