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

import type { ApiResponse, CustomDomain } from './types'

// Auth is carried by new-api's shared axios instance (session cookie +
// New-Api-User header); the backend authorizes via AgentOwnerAuth and scopes
// every operation to the owner's tenant (never a client-supplied tenant_id).

export async function getCustomDomain(): Promise<ApiResponse<CustomDomain>> {
  const res = await api.get('/api/tenant/custom-domain', {
    disableDuplicate: true,
  })
  return res.data
}

// bind/verify pass skipErrorHandler so the page can map the stable error code
// (DOMAIN_RESERVED / DOMAIN_TAKEN / DOMAIN_LIMIT / DOMAIN_INVALID /
// DNS_VERIFY_FAILED) to a localized message instead of the raw backend toast.
export async function bindCustomDomain(
  domain: string
): Promise<ApiResponse<CustomDomain>> {
  const res = await api.post(
    '/api/tenant/custom-domain',
    { domain },
    { skipErrorHandler: true }
  )
  return res.data
}

export async function verifyCustomDomain(): Promise<ApiResponse<CustomDomain>> {
  const res = await api.post(
    '/api/tenant/custom-domain/verify',
    {},
    { skipErrorHandler: true }
  )
  return res.data
}

export async function unbindCustomDomain(): Promise<
  ApiResponse<{ unbound: boolean; domain: string }>
> {
  const res = await api.delete('/api/tenant/custom-domain')
  return res.data
}
