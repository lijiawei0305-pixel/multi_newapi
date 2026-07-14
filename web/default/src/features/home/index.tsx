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
import { useTranslation } from 'react-i18next'

import { PublicLayout } from '@/components/layout'
import { Footer } from '@/components/layout/components/footer'
import { RichContent } from '@/components/rich-content'
import { LandingReact } from '@/features/landing-react'
import { isLikelyHtml } from '@/lib/content-format'
import { resolveTenant } from '@/lib/tenant'
import { useAuthStore } from '@/stores/auth-store'

import { CTA, Features, Hero, HowItWorks, Stats } from './components'
import { useHomePageContent } from './hooks'

export function Home() {
  const { t } = useTranslation()
  const { auth } = useAuthStore()
  const isAuthenticated = !!auth.user
  const { content, isLoaded, isUrl } = useHomePageContent()
  // 复用 root 的 ['tenant-resolution'] 查询（每会话一次 GET /api/tenant/current），按 Host 区分主站/代理站。
  const { data: resolution, isLoading: isTenantLoading } = useQuery({
    queryKey: ['tenant-resolution'],
    queryFn: resolveTenant,
    staleTime: 5 * 60 * 1000,
    retry: false,
  })

  if (!isLoaded || isTenantLoading) {
    return (
      <PublicLayout showMainContainer={false}>
        <main className='flex min-h-screen items-center justify-center'>
          <div className='text-muted-foreground'>{t('Loading...')}</div>
        </main>
      </PublicLayout>
    )
  }

  // 主站默认首页 = WeDream 落地页（React 组件，见 features/landing-react）。
  // 曾是 iframe 内嵌 nginx 静态的 /landing/index.html（2MB 单文件），2026-07-13 换成 React：
  // 文案接官方 i18n、与平台同一套构建、可走路由跳转。nginx 的 /landing/ 暂留作回滚兜底。
  // 管理员显式配置的自定义首页(HomePageContent)仍优先；代理站(kind==='tenant')跳过此块，
  // 走下方原生 React 段落，保留其原有首页。
  const isMainSite = resolution?.kind !== 'tenant'
  if (isMainSite && !content) {
    // 不套 PublicLayout：落地页自带顶栏/客服/语言，且需要整屏深空背景。
    return <LandingReact />
  }

  if (content) {
    if (isUrl) {
      return (
        <PublicLayout showMainContainer={false}>
          <iframe
            src={content}
            className='h-screen w-full border-none'
            title={t('Custom Home Page')}
            sandbox='allow-forms allow-popups allow-popups-to-escape-sandbox allow-scripts'
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
