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
import { useQuery } from '@tanstack/react-query'
import { lazy, Suspense } from 'react'
import { useTranslation } from 'react-i18next'

import { PublicLayout } from '@/components/layout'
import { Footer } from '@/components/layout/components/footer'
import { RichContent } from '@/components/rich-content'
import { isLikelyHtml } from '@/lib/content-format'
import { normalizeHttpNavigationUrl } from '@/lib/safe-navigation'
import { resolveTenant } from '@/lib/tenant'
import { useAuthStore } from '@/stores/auth-store'

import { CTA, Features, Hero, HowItWorks, Stats } from './components'
import { useHomePageContent } from './hooks'

const LandingReact = lazy(() =>
  import('@/features/landing-react').then((module) => ({
    default: module.LandingReact,
  }))
)

function MainSiteLanding({ loadingLabel }: { loadingLabel: string }) {
  // 不套 PublicLayout：落地页自带顶栏/客服/语言，且需要整屏深空背景。
  // 但仍挂原生 <Footer />（须在 .wd-landing-root 外 + .dark，见历史注释）。
  return (
    <>
      <Suspense
        fallback={
          <main className='flex min-h-screen items-center justify-center bg-[#061127] text-white'>
            {loadingLabel}
          </main>
        }
      >
        <LandingReact />
      </Suspense>
      <div className='dark relative z-20 bg-[#061127]'>
        <Footer className='border-transparent' />
      </div>
    </>
  )
}

export function Home() {
  const { t } = useTranslation()
  const { auth } = useAuthStore()
  const isAuthenticated = !!auth.user
  const { content, isLoaded, isUrl } = useHomePageContent()
  const contentUrl = isUrl ? normalizeHttpNavigationUrl(content) : null
  // 复用 root 的 ['tenant-resolution'] 查询（每会话一次 GET /api/tenant/current），按 Host 区分主站/代理站。
  const { data: resolution, isLoading: isTenantLoading } = useQuery({
    queryKey: ['tenant-resolution'],
    queryFn: resolveTenant,
    staleTime: 5 * 60 * 1000,
    retry: false,
  })

  // Phase 4：租户解析完成后，主站且无自定义首页内容时可立即挂落地页，
  // 不必再等 HomePageContent 网络往返（content 已同步读 localStorage；空=默认落地页）。
  if (isTenantLoading) {
    return (
      <main className='flex min-h-screen items-center justify-center bg-[#061127] text-white/80'>
        {t('Loading...')}
      </main>
    )
  }

  // 主站默认首页 = WeDream 落地页（React 组件，见 features/landing-react）。
  // 管理员显式配置的自定义首页(HomePageContent)仍优先；代理站(kind==='tenant')跳过此块。
  const isMainSite = resolution?.kind !== 'tenant'

  // 有缓存/已加载的自定义内容：优先渲染（不必等网络 isLoaded）
  if (content) {
    if (contentUrl) {
      return (
        <PublicLayout showMainContainer={false}>
          <iframe
            src={contentUrl}
            className='h-screen w-full border-none'
            title={t('Custom Home Page')}
            sandbox='allow-forms allow-popups allow-popups-to-escape-sandbox allow-scripts'
            referrerPolicy='no-referrer'
          />
        </PublicLayout>
      )
    }

    return (
      <PublicLayout>
        <div className='mx-auto max-w-6xl px-4 py-8'>
          <RichContent
            mode={isLikelyHtml(content) ? 'html' : 'markdown'}
            content={content}
            className='custom-home-content'
          />
        </div>
      </PublicLayout>
    )
  }

  // 主站且无自定义内容：立即落地页（不等 HomePageContent 网络）
  if (isMainSite) {
    return <MainSiteLanding loadingLabel={t('Loading...')} />
  }

  // 代理站：等首页配置拉取完成后再画原生段落，避免空内容闪一下
  if (!isLoaded) {
    return (
      <PublicLayout showMainContainer={false}>
        <main className='flex min-h-screen items-center justify-center'>
          <div className='text-muted-foreground'>{t('Loading...')}</div>
        </main>
      </PublicLayout>
    )
  }

  return (
    <PublicLayout showMainContainer={false}>
      <Hero isAuthenticated={isAuthenticated} />
      <Stats />
      <Features />
      <HowItWorks />
      <CTA isAuthenticated={isAuthenticated} />
      <Footer />
    </PublicLayout>
  )
}
