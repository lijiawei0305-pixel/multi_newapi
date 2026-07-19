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
    // 但仍挂原生 <Footer />，以保留后台「系统设置 → 站点与品牌」配的页脚 HTML、
    // 隐私政策 / 用户协议链接（Footer 自己读 useSystemConfig()/useStatus()）。
    // 两个约束：
    //  1) Footer 必须放在 .wd-landing-root **外面** —— 落地页的 <style> 是无 layer 注入的，
    //     其中 `.wd-landing-root * { margin:0; padding:0 }` 会盖过 Tailwind 的 @layer utilities，
    //     放进去会把 Footer 的 px-6/py-5 全清零。
    //  2) 强制 .dark —— 落地页恒为深空黑底，而 Footer 用平台主题色；浅色主题下
    //     text-muted-foreground 是深灰，在黑底上看不见。z-20 是为了压过落地页的 #vignette(z:12)。
    return (
      <>
        <Suspense
          fallback={
            <main className='flex min-h-screen items-center justify-center bg-[#061127] text-white'>
              {t('Loading...')}
            </main>
          }
        >
          <LandingReact />
        </Suspense>
        {/* 页脚接在落地页深空渐变之后：给 wrapper 填渐变的收尾色 #061127，与上方场景同色无缝衔接，
            同时盖住浅色主题下透出的白色 body 背景（否则页面最底部会出现一条白带）。
            Footer 自带的 border-t 是一道微弱白线，在纯色底上会成接缝，故传 border-transparent 消除。 */}
        <div className='dark relative z-20 bg-[#061127]'>
          <Footer className='border-transparent' />
        </div>
      </>
    )
  }

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
