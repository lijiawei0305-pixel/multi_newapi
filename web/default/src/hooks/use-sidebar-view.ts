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
import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useLocation } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { agentContextQueryOptions } from '@/lib/agent-context'
import { ROLE } from '@/lib/roles'
import { resolveSidebarView } from '@/components/layout/lib/sidebar-view-registry'
import type { NavGroup, ResolvedSidebarView } from '@/components/layout/types'
import { useSidebarConfig } from './use-sidebar-config'
import { useSidebarData } from './use-sidebar-data'

/** Sentinel key used for the root navigation in animation `key=` props */
const ROOT_VIEW_KEY = '__root'

/**
 * Resolve the active sidebar view for the current location.
 *
 * - Returns the matching nested {@link SidebarView} (with its nav
 *   groups) when the URL belongs to a registered drill-in workspace.
 * - Otherwise returns the root navigation, narrowed by:
 *     · admin-only group visibility (role-based);
 *     · agent-owner-only visibility (the "Agent Self-Service" group and the
 *       "My Earnings" item appear only when the current user owns a tenant
 *       AND the current Host IS that tenant's own site — `is_agent_owner &&
 *       on_own_site`, see `agentContextQueryOptions`. 代理控制台只在自己的
 *       代理站出现，不泄漏到主站/别家站；L0 无自己的站 → 永不显示，其界面是
 *       主站钱包「邀请返现」面板);
 *     · `useSidebarConfig` (admin × user `sidebar_modules` overlay).
 *
 * Nested views are intentionally NOT passed through `useSidebarConfig`
 * — those filters target known dashboard URLs only, and gating is
 * already enforced at the route level (`beforeLoad` redirects).
 */
export function useSidebarView(): ResolvedSidebarView {
  const { t } = useTranslation()
  const pathname = useLocation({ select: (l) => l.pathname })
  const userRole = useAuthStore((s) => s.auth.user?.role)
  const rootSidebarData = useSidebarData()
  const configFilteredRoot = useSidebarConfig(rootSidebarData.navGroups)
  // Fail closed: while the gate is loading (data === undefined) treat the user
  // as a non-owner / level 0 so the agent menus never flash for normal users.
  const agentCtx = useQuery(agentContextQueryOptions).data
  // 代理控制台可见 = 拥有代理租户 && 当前 Host 是自己的代理站（on_own_site）。
  // 只看 is_agent_owner 会把代理菜单泄漏到主站/别家站（2026-07-07 bug）。
  const agentConsole =
    (agentCtx?.is_agent_owner ?? false) && (agentCtx?.on_own_site ?? false)
  const agentLevel = agentCtx?.level ?? 0

  const rootNavGroups = useMemo<NavGroup[]>(() => {
    const role = userRole ?? ROLE.GUEST
    const isAdmin = role >= ROLE.ADMIN
    return configFilteredRoot
      .filter((group) => (group.id === 'admin' ? isAdmin : true))
      .filter((group) => !group.agentOwnerOnly || agentConsole)
      .map((group) => {
        const items = group.items.filter(
          (item) =>
            (item.requiredRole === undefined || role >= item.requiredRole) &&
            (!item.agentOwnerOnly || agentConsole) &&
            (!item.agentLevelMin || agentLevel >= item.agentLevelMin)
        )
        return items.length === group.items.length ? group : { ...group, items }
      })
  }, [configFilteredRoot, userRole, agentConsole, agentLevel])

  const view = resolveSidebarView(pathname)

  if (view) {
    return {
      key: view.id,
      view,
      navGroups: view.getNavGroups(t),
    }
  }

  return {
    key: ROOT_VIEW_KEY,
    view: null,
    navGroups: rootNavGroups,
  }
}
