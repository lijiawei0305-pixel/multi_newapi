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
import { api, getApiErrorCode } from '@/lib/api'

// Public per-Host tenant brand (GET /api/tenant/current). No auth required; the
// backend resolves the tenant from the request Host. Returns null on the main
// site / unknown Host (no tenant), so callers can no-op there.
export interface TenantCurrent {
  id: number
  slug: string
  site_name: string
  logo_url: string
  /** Default theme preset key the agent set for this site (empty = default). */
  theme_preset: string
  theme_color: string
  /** OEM: when true the agent has opted to hide main-site branding. */
  brand_hidden: boolean
  status: string
  tokenplan_enabled: boolean
}

/**
 * Three-way classification of the current Host, resolved from
 * GET /api/tenant/current:
 *  - `tenant`        → a registered agent site (carries the brand payload);
 *  - `main`          → the main site (www / apex / localhost / direct access);
 *                      backend answers 404 `TENANT_NOT_FOUND`;
 *  - `not-activated` → an unregistered `*.wedreamhub.com` subdomain; backend
 *                      answers 404 `SITE_NOT_ACTIVATED` so we render a dedicated
 *                      "站点未开通" page instead of the main site.
 */
export type TenantResolution =
  | { kind: 'tenant'; tenant: TenantCurrent }
  | { kind: 'main' }
  | { kind: 'not-activated' }

export async function resolveTenant(): Promise<TenantResolution> {
  // skip handlers: main site legitimately 404s TENANT_NOT_FOUND — never toast.
  try {
    const res = await api.get('/api/tenant/current', {
      skipBusinessError: true,
      skipErrorHandler: true,
    })
    const body = res.data as { success?: boolean; data?: TenantCurrent | null }
    if (body?.success && body.data) return { kind: 'tenant', tenant: body.data }
    return { kind: 'main' }
  } catch (err) {
    // 404s reject through the api interceptor; distinguish "未开通" by stable code.
    if (getApiErrorCode(err) === 'SITE_NOT_ACTIVATED') {
      return { kind: 'not-activated' }
    }
    return { kind: 'main' }
  }
}

// Backward-compatible helper: null on the main site / unknown Host (no tenant).
export async function getTenantCurrent(): Promise<TenantCurrent | null> {
  const r = await resolveTenant()
  return r.kind === 'tenant' ? r.tenant : null
}
