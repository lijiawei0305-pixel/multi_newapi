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
import { useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getTenantCurrent } from '@/lib/tenant'
import { useSystemConfigStore } from '@/stores/system-config-store'

/**
 * OEM brand override for custom / tenant domains.
 *
 * Fetches GET /api/tenant/current; when the current Host resolves to a tenant
 * that has opted into brand hiding (`brand_hidden`), it overrides the
 * system-config store's `systemName`/`logo` with the agent's brand. Because all
 * branding surfaces (header, sidebar, login page, footer) read from that store,
 * this single override flips them to the agent identity with no per-component
 * changes.
 *
 * No-op on the main site (no tenant → null) and on tenant hosts that have not
 * enabled brand hiding. Applied only after `/api/status` finished loading so the
 * agent brand wins over the just-populated main-site values.
 */
export function useTenantBrand() {
  const setConfig = useSystemConfigStore((s) => s.setConfig)
  const statusLoading = useSystemConfigStore((s) => s.loading)

  const { data } = useQuery({
    queryKey: ['tenant-current-brand'],
    queryFn: getTenantCurrent,
    staleTime: 5 * 60 * 1000,
    retry: false,
  })

  useEffect(() => {
    if (statusLoading) return
    if (!data?.brand_hidden) return
    const override: { systemName?: string; logo?: string } = {}
    if (data.site_name) override.systemName = data.site_name
    if (data.logo_url) override.logo = data.logo_url
    if (Object.keys(override).length > 0) setConfig(override)
  }, [statusLoading, data, setConfig])
}
