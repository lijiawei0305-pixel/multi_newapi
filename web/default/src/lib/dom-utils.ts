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
export function applyFaviconToDom(url: string) {
  if (typeof document === 'undefined' || !url) return
  try {
    const next = new URL(url, window.location.href).href
    const existing =
      document.querySelectorAll<HTMLLinkElement>('link[rel~="icon"]')
    if (existing.length === 1 && existing[0].href === next) return
    const link = document.createElement('link')
    link.rel = 'icon'
    link.href = url
    existing.forEach((l) => l.remove())
    document.head.appendChild(link)
  } catch {
    // Ignore malformed URLs
  }
}

/* ─── 站点品牌（document.title + favicon）的唯一入口 ───────────────────────────
   两个调用方，且**顺序不确定**：
     · main.tsx 的 initSystemBranding()：React 之前跑，用**平台** /api/status 的
       system_name/logo 设置（缓存优先一次 + 后台 getStatus() 回来再一次）。
     · useTenantBrand()（__root 内）：代理站解析出租户后，用**租户**的 site_name/logo_url
       覆盖（仅当 brand_hidden）。
   若不加锁，main.tsx 后台刷新那次会把已生效的租户品牌覆盖回主站的 —— 慢网下必现，
   代理站标签页会显示主站名与图标（brand_hidden 的品牌泄漏）。
   规则：**租户品牌一旦应用即锁定，此后平台侧的调用一律忽略。** */
let tenantBrandLocked = false

export function applySiteBranding(
  name: string | null | undefined,
  logo: string | null | undefined,
  opts: { tenant?: boolean } = {}
) {
  if (typeof document === 'undefined') return
  if (tenantBrandLocked && !opts.tenant) return
  if (opts.tenant) tenantBrandLocked = true

  if (name) {
    document.title = name
    const metaTitle = document.querySelector<HTMLMetaElement>(
      'meta[name="title"]'
    )
    if (metaTitle) metaTitle.setAttribute('content', name)
  }
  if (logo) applyFaviconToDom(logo)
}
