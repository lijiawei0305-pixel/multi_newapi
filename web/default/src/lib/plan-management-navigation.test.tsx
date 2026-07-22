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
import type { AnchorHTMLAttributes } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { WaffoPancakeSettingsSection } from '@/features/system-settings/integrations/waffo-pancake-settings-section'
import { useSidebarConfig } from '@/hooks/use-sidebar-config'
import { useSidebarData } from '@/hooks/use-sidebar-data'
import { ROLE } from '@/lib/roles'
import { Route as AdvancedSubscriptionsRoute } from '@/routes/_authenticated/advanced-subscription-plans'
import { Route as LegacySubscriptionsRoute } from '@/routes/_authenticated/subscriptions'
import { useAuthStore } from '@/stores/auth-store'

const statusState = vi.hoisted(() => ({ sidebarModulesAdmin: '' }))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    Link: (props: AnchorHTMLAttributes<HTMLAnchorElement> & { to: string }) => {
      const { to, ...anchorProps } = props
      return <a href={to} {...anchorProps} />
    },
  }
})

vi.mock('@/hooks/use-status', () => ({
  useStatus: () => ({
    status: { SidebarModulesAdmin: statusState.sidebarModulesAdmin },
    loading: false,
    error: null,
  }),
}))

type RedirectResponse = Response & {
  options: { to?: string; replace?: boolean }
}

function captureLegacyRouteRedirect(): RedirectResponse {
  try {
    LegacySubscriptionsRoute.options.beforeLoad?.({} as never)
  } catch (error) {
    return error as RedirectResponse
  }
  throw new Error('Expected the legacy subscriptions route to redirect')
}

function captureAdvancedRouteRedirect(): RedirectResponse {
  try {
    AdvancedSubscriptionsRoute.options.beforeLoad?.({} as never)
  } catch (error) {
    return error as RedirectResponse
  }
  throw new Error('Expected the advanced subscription plans route to redirect')
}

function SidebarLinks() {
  const sidebar = useSidebarData()
  const groups = useSidebarConfig(sidebar.navGroups)

  return groups.flatMap((group) =>
    group.items.map((item) => {
      if (!('url' in item) || !item.url) return null
      return <a key={item.url} href={item.url} />
    })
  )
}

function setAdminUser() {
  useAuthStore.getState().auth.setUser({
    id: 1,
    username: 'admin',
    role: ROLE.ADMIN,
    permissions: { sidebar_settings: false },
  })
}

describe('plan management navigation', () => {
  afterEach(() => {
    useAuthStore.getState().auth.reset()
    statusState.sidebarModulesAdmin = ''
  })

  it('redirects an administrator from the legacy route to token plans', () => {
    setAdminUser()

    const redirect = captureLegacyRouteRedirect()

    expect(redirect.status).toBe(307)
    expect(redirect.options).toMatchObject({
      to: '/token-plans',
      replace: true,
    })
  })

  it('keeps the legacy route restricted to administrators', () => {
    useAuthStore.getState().auth.setUser({
      id: 2,
      username: 'user',
      role: ROLE.ADMIN - 1,
    })

    const redirect = captureLegacyRouteRedirect()

    expect(redirect.status).toBe(307)
    expect(redirect.options.to).toBe('/403')
  })

  it('keeps advanced native plan configuration available to administrators', () => {
    setAdminUser()

    expect(() =>
      AdvancedSubscriptionsRoute.options.beforeLoad?.({} as never)
    ).not.toThrow()
    expect(AdvancedSubscriptionsRoute.options.component).toBeDefined()
  })

  it('keeps advanced native plan configuration restricted to administrators', () => {
    useAuthStore.getState().auth.setUser({
      id: 2,
      username: 'user',
      role: ROLE.ADMIN - 1,
    })

    const redirect = captureAdvancedRouteRedirect()

    expect(redirect.status).toBe(307)
    expect(redirect.options.to).toBe('/403')
  })

  it('renders only the token plans entry for plan administration', () => {
    setAdminUser()
    statusState.sidebarModulesAdmin = JSON.stringify({
      admin: { enabled: true, subscription: true },
    })

    const markup = renderToStaticMarkup(<SidebarLinks />)

    expect(markup.match(/href="\/token-plans"/g)).toHaveLength(1)
    expect(markup).not.toContain('href="/subscriptions"')
  })

  it('keeps token plans governed by the existing subscription setting', () => {
    setAdminUser()
    statusState.sidebarModulesAdmin = JSON.stringify({
      admin: { enabled: true, subscription: false },
    })

    const markup = renderToStaticMarkup(<SidebarLinks />)

    expect(markup).not.toContain('href="/token-plans"')
  })

  it('links Waffo Pancake plan products to advanced native plan configuration', () => {
    const values = {
      WaffoPancakeMerchantID: '',
      WaffoPancakePrivateKey: '',
      WaffoPancakeReturnURL: '',
    }
    const markup = renderToStaticMarkup(
      <WaffoPancakeSettingsSection
        defaultValues={values}
        values={values}
        onValueChange={() => undefined}
        selectedBinding={{ storeID: '', productID: '' }}
        savedBinding={{ storeID: '', productID: '' }}
        onSelectedBindingChange={() => undefined}
      />
    )

    expect(markup).toContain('href="/advanced-subscription-plans"')
    expect(markup).toContain('Open Advanced Native Subscription Plans')
  })
})
