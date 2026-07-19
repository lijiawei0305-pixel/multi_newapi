/* WeDream 落地页（移植自 bulb-orbit/index.html）—— 本文件是页面外壳：
   顶栏 + Hero（灯泡/轨道/HUD）+ 三屏（见 sections.tsx）+ 客服弹窗。
   要点：DOM 用 JSX；灯泡/轨道逻辑以命令式模块在 useEffect 里挂到 DOM（three 用 npm 包）；
   StrictMode 双调用用「模块级单例 + 引用计数 + 延迟卸载」化解（第二次 boot 只 refs++）。
   文案走 New API 官方 i18n（key = 英文原句，译文在 src/i18n/locales/*.json），勿在组件里手写译文。

   站点信息：**站名**读后台「系统设置 → 站点与品牌」的 systemName（读不到才回落 'WeDream AI'）；
   **logo 保持落地页专用的方形版**（用户明确要求，后台那张是圆形、与此处的圆角方块视觉不符）。
   页脚（自定义 HTML + 隐私政策/用户协议）由原生 <Footer /> 渲染，挂在 features/home 里、
   本组件之外（见那边注释：落地页的无 layer reset 会清掉 Footer 的 Tailwind 内边距）。
   注意：文案里句子内嵌的 "WeDream AI"（如「在 WeDream AI 里成为作品」）属营销文案、在 locale
   文件中，不随 systemName 变。 */
import { type CSSProperties, useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { LanguageSwitcher } from '@/components/language-switcher'
import { useSystemConfig } from '@/hooks/use-system-config'

import { KefuModal } from './kefu-modal'
import { LANDING_CSS } from './landing-css'
import { initScene3d } from './scene3d'
import { JoinSection, ServicesSection, StudioSection } from './sections'
import { SECTIONS_CSS } from './sections-css'

/* ---- 模块级单例：整页单实例；StrictMode mount→unmount→mount 只保留一个活实例 ---- */
let instance: { cleanup: () => void; refs: number } | null = null
type LandingCSSProperties = CSSProperties &
  Record<`--${string}`, string | number>

function boot() {
  if (instance) {
    instance.refs++
    return
  }
  const inst = { cleanup: () => {}, refs: 1 }
  instance = inst
  document.body.classList.add('lit')
  let cleanup = () => {}
  let cancelled = false
  const canvas = document.querySelector('#scene3d') as HTMLCanvasElement | null
  if (canvas) {
    initScene3d(canvas)
      .then((c) => {
        if (cancelled) c()
        else cleanup = c
      })
      .catch((_error) => {
        document.body.classList.remove('webgl3d')
      })
  }
  inst.cleanup = () => {
    cancelled = true
    cleanup()
    document.body.classList.remove('lit')
  }
}
function unboot() {
  if (!instance) return
  instance.refs--
  if (instance.refs <= 0) {
    const c = instance.cleanup
    instance = null
    c()
  }
}

export function LandingReact() {
  const { t } = useTranslation()
  const [kefuOpen, setKefuOpen] = useState(false)
  const closeKefu = useCallback(() => setKefuOpen(false), [])
  const { systemName } = useSystemConfig()
  // 主站落地页品牌 = WeDream AI;New API 是底层软件默认名(未品牌化),视为空 → 用我们的品牌。
  // 自定义品牌(代理/后台设了非默认 systemName)仍按其显示。
  const siteName =
    systemName && systemName !== 'New API' ? systemName : 'WeDream AI'

  /* 文案全部走 New API 官方 i18n（key = 英文原句，译文在 src/i18n/locales/*.json）。
     zh/en 已填；ja/ru/fr/vi 未填的 key 由 i18next 的 fallbackLng:'en' 回落到英文。 */
  const T = {
    badge: t('Next-Gen AI Platform'),
    hl2: t('Set Your Imagination Free'),
    sub: t("Move freely among the world's top AI models"),
    f1n: t('Multi-Model Access'),
    f1d: t("Access the world's top AI models"),
    f2n: t('Secure & Stable'),
    f2d: t('Enterprise-grade security, stable and dependable'),
    cta1: t('Try It Now'),
    cta2: t('Learn more'),
    support: t('Customer Support'),
    start: t('Get Started'),
  }

  useEffect(() => {
    boot()
    return () => {
      setTimeout(unboot, 0)
    }
  }, [])

  return (
    <div className='wd-landing-root'>
      {/* 挂载时注入样式、卸载即移除（不永久污染全站） */}
      <style>{LANDING_CSS + SECTIONS_CSS}</style>

      <div id='bg' />
      <div id='aurora' />
      <div id='grid' />
      <div id='stardust' />
      <div id='stardust2' />

      <main className='landing-scene'>
        <header className='lp-topbar'>
          <div className='lp-logo'>
            <img
              className='lp-mark-img'
              src='/lp-assets/wedream-logo.png'
              alt={siteName}
            />
            <span>{siteName}</span>
          </div>
          <div className='lp-actions'>
            <button
              className='lp-kefu-btn'
              type='button'
              onClick={() => setKefuOpen(true)}
            >
              <svg viewBox='0 0 24 24'>
                <path d='M4 14v-2a8 8 0 0 1 16 0v2' />
                <path d='M4 14a2 2 0 0 1 2-2h1v5H6a2 2 0 0 1-2-2v-1ZM20 14a2 2 0 0 0-2-2h-1v5h1a2 2 0 0 0 2-2v-1Z' />
                <path d='M18 17v1a3 3 0 0 1-3 3h-3' />
              </svg>
              <span>{T.support}</span>
            </button>
            <a className='lp-start' href='/playground'>
              <span>{T.start}</span>{' '}
              <svg viewBox='0 0 24 24'>
                <path d='M5 12h14M13 6l6 6-6 6' />
              </svg>
            </a>
            {/* New API 原生语言选择器(下拉,含全部界面语言;登录态会持久化到后端) */}
            <LanguageSwitcher />
          </div>
        </header>
        <button
          className='lp-fab'
          type='button'
          aria-label={t('Live Support')}
          onClick={() => setKefuOpen(true)}
        >
          <svg viewBox='0 0 24 24'>
            <path d='M4 14v-2a8 8 0 0 1 16 0v2' />
            <path d='M4 14a2 2 0 0 1 2-2h1v5H6a2 2 0 0 1-2-2v-1ZM20 14a2 2 0 0 0-2-2h-1v5h1a2 2 0 0 0 2-2v-1Z' />
            <path d='M18 17v1a3 3 0 0 1-3 3h-3' />
          </svg>
        </button>

        <div className='hero-stage'>
          <div className='hero-left'>
            <header id='hero'>
              <span
                id='badge'
                className='rise'
                style={{ '--rd': '.15s' } as LandingCSSProperties}
              >
                <svg viewBox='0 0 24 24' fill='currentColor' aria-hidden='true'>
                  <path d='M12 1.6c.5 6 4.4 9.9 10.4 10.4-6 .5-9.9 4.4-10.4 10.4-.5-6-4.4-9.9-10.4-10.4 6-.5 9.9-4.4 10.4-10.4Z' />
                </svg>
                <span>{T.badge}</span>
              </span>
              <h1>
                <span
                  className='hl1 rise'
                  style={{ '--rd': '.3s' } as LandingCSSProperties}
                >
                  {siteName}
                </span>
                <span
                  className='hl2 rise'
                  style={{ '--rd': '.45s' } as LandingCSSProperties}
                >
                  {T.hl2}
                </span>
              </h1>
              <p
                id='sub'
                className='rise'
                style={{ '--rd': '.62s' } as LandingCSSProperties}
              >
                {T.sub}
              </p>
            </header>
            <div
              className='features rise'
              style={{ '--rd': '.72s' } as LandingCSSProperties}
            >
              <div className='feature'>
                <span className='feature-ico'>
                  <svg
                    viewBox='0 0 24 24'
                    fill='none'
                    stroke='#7cc4ff'
                    strokeWidth='1.7'
                    strokeLinecap='round'
                    strokeLinejoin='round'
                  >
                    <circle cx='12' cy='5' r='2.3' />
                    <circle cx='5' cy='18' r='2.3' />
                    <circle cx='19' cy='18' r='2.3' />
                    <path d='M12 7.3v3.4M11 12.4l-4.4 3.4M13 12.4l4.4 3.4' />
                  </svg>
                </span>
                <div>
                  <div className='feature-name'>{T.f1n}</div>
                  <div className='feature-desc'>{T.f1d}</div>
                </div>
              </div>
              <div className='feature'>
                <span className='feature-ico'>
                  <svg
                    viewBox='0 0 24 24'
                    fill='none'
                    stroke='#3ee0b0'
                    strokeWidth='1.7'
                    strokeLinecap='round'
                    strokeLinejoin='round'
                  >
                    <path d='M12 3l7 3v5c0 4.4-3 7.6-7 9-4-1.4-7-4.6-7-9V6z' />
                    <path d='M9 12l2 2 4-4' />
                  </svg>
                </span>
                <div>
                  <div className='feature-name'>{T.f2n}</div>
                  <div className='feature-desc'>{T.f2d}</div>
                </div>
              </div>
            </div>
            <div
              className='cta rise'
              style={{ '--rd': '.82s' } as LandingCSSProperties}
            >
              <a className='btn btn-primary' href='/dashboard/overview'>
                {T.cta1}
              </a>
              <a className='btn btn-ghost' href='/docs'>
                {T.cta2}
              </a>
            </div>
          </div>

          <div className='hero-right'>
            <div className='hero-visual'>
              {/* 非-WebGL 兜底(body 无 webgl3d 时显示);成功则被 scene3d 隐藏 */}
              <div id='halo' />
              <img
                id='bulb'
                src='/lp-assets/bulb.png'
                alt=''
                draggable='false'
              />
              {/* 统一 3D 场景:灯泡 + 轨道 + 卫星,铺满可视区 */}
              <canvas id='scene3d' />
            </div>
            {/* HUD 默认态；点击卫星后由 orbit.ts 用 i18n.t() 覆写为该模型的信息。 */}
            <aside id='hud'>
              <div className='hud-prov'>
                <span className='hud-dot' />
                <span id='hud-prov'>{t('System Running')}</span>
              </div>
              <div className='hud-name' id='hud-name'>
                {t('WeDream Core')}
              </div>
              <div className='hud-desc' id='hud-desc'>
                {t(
                  'Central intelligence core. The orbiting satellites are the connected models — click any satellite for details.'
                )}
              </div>
              <div className='hud-scene' id='hud-scene' />
              <div className='hud-tele' id='hud-tele'>
                {t('Core load 100%')}
              </div>
            </aside>
          </div>
        </div>
        <div id='vignette' />

        {/* 以下三屏顺序对齐原版 index.html（708 / 783 / 795） */}
        <StudioSection />
        <ServicesSection />
        <JoinSection />
      </main>

      <KefuModal open={kefuOpen} onClose={closeKefu} />
    </div>
  )
}

export default LandingReact
