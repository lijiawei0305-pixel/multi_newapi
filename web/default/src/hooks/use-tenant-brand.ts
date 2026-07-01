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
import { getCookie } from '@/lib/cookies'
import { getTenantCurrent } from '@/lib/tenant'
import {
  THEME_COOKIE_KEYS,
  THEME_PRESET_VALUES,
  type ThemePreset,
} from '@/lib/theme-customization'
import { useThemeCustomization } from '@/context/theme-customization-provider'
import { useSystemConfigStore } from '@/stores/system-config-store'

/**
 * Per-tenant brand + default theme for custom / tenant domains.
 *
 * Fetches GET /api/tenant/current and, for the resolved tenant:
 *  - when `brand_hidden`, overrides systemName/logo in the system-config store
 *    (every branding surface reads from it, so all flip with no per-component
 *    changes);
 *  - applies the agent's chosen default `theme_preset` for users who have not
 *    picked their own (no preset cookie). Cookies are host-only, so an agent's
 *    default never leaks to the main site or other tenants; users keep the
 *    ability to change the style from the top-right theme menu.
 *
 * MUST be rendered inside ThemeCustomizationProvider (it calls setPreset).
 */
export function useTenantBrand() {
  const setConfig = useSystemConfigStore((s) => s.setConfig)
  const statusLoading = useSystemConfigStore((s) => s.loading)
  const { setPreset } = useThemeCustomization()

  const { data } = useQuery({
    queryKey: ['tenant-current-brand'],
    queryFn: getTenantCurrent,
    staleTime: 5 * 60 * 1000,
    retry: false,
  })

  // Brand name + logo override (applied after /api/status populated the store).
  useEffect(() => {
    if (statusLoading) return
    if (!data?.brand_hidden) return
    const override: { systemName?: string; logo?: string } = {}
    if (data.site_name) override.systemName = data.site_name
    if (data.logo_url) override.logo = data.logo_url
    if (Object.keys(override).length > 0) setConfig(override)
  }, [statusLoading, data, setConfig])

  // Default theme preset for this tenant's site.
  useEffect(() => {
    const preset = data?.theme_preset
    if (!preset || preset === 'default') return
    if (!THEME_PRESET_VALUES.has(preset as ThemePreset)) return
    if (getCookie(THEME_COOKIE_KEYS.preset)) return // user already chose their own
    setPreset(preset as ThemePreset)
  }, [data, setPreset])
}
