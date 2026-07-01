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

// Public per-Host tenant brand (GET /api/tenant/current). No auth required; the
// backend resolves the tenant from the request Host. Returns null on the main
// site / unknown Host (no tenant), so callers can no-op there.
export interface TenantCurrent {
  id: number
  slug: string
  site_name: string
  logo_url: string
  theme_color: string
  /** OEM: when true the agent has opted to hide main-site branding. */
  brand_hidden: boolean
  status: string
  tokenplan_enabled: boolean
}

export async function getTenantCurrent(): Promise<TenantCurrent | null> {
  // skip handlers: the main site legitimately returns TENANT_NOT_FOUND — never toast.
  const res = await api.get('/api/tenant/current', {
    skipBusinessError: true,
    skipErrorHandler: true,
  })
  const body = res.data as { success?: boolean; data?: TenantCurrent | null }
  return body?.success && body.data ? body.data : null
}
