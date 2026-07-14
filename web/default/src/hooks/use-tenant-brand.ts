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
import { applySiteBranding } from '@/lib/dom-utils'
import { resolveTenant } from '@/lib/tenant'
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

  // Shares the ['tenant-resolution'] query with the root-level site gate, so the
  // Host is classified with a single GET /api/tenant/current per session.
  const { data: resolution } = useQuery({
    queryKey: ['tenant-resolution'],
    queryFn: resolveTenant,
    staleTime: 5 * 60 * 1000,
    retry: false,
  })
  const data = resolution?.kind === 'tenant' ? resolution.tenant : undefined

  // Brand name + logo override (applied after /api/status populated the store).
  useEffect(() => {
    if (statusLoading) return
    if (!data?.brand_hidden) return
    const override: {
      systemName?: string
      logo?: string
      footerHtml?: string
    } = {}
    if (data.site_name) override.systemName = data.site_name
    if (data.logo_url) override.logo = data.logo_url

    // 页脚：代理配了就用他自己的；**没配也绝不能留空** —— <Footer /> 在 footerHtml 为空时
    // 会回落到 New API 的默认文档链接大列，那是比显示主站页脚更严重的品牌泄漏。
    // 故未配置时自动生成「© 年份 站名」。
    const ownFooter = data.footer?.trim()
    override.footerHtml =
      ownFooter ||
      `© ${new Date().getFullYear()} ${data.site_name || ''}`.trim()

    setConfig(override)

    // 同步覆写 document.title / favicon —— 它们由 main.tsx 的启动脚本按**平台** /api/status
    // 设置（React 之前），store 的覆盖管不到 DOM。不改这里，代理站的标签页会一直显示主站
    // 的名字和图标（brand_hidden 的品牌泄漏）。{tenant:true} 会上锁，防止 main.tsx 的后台
    // getStatus() 回来后把它盖回主站品牌（见 lib/dom-utils.ts 的注释）。
    applySiteBranding(data.site_name, data.logo_url, { tenant: true })
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
