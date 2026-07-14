// @ts-nocheck
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
import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { useSystemConfig } from '@/hooks/use-system-config'

import { initBulb3d } from './bulb3d'
import { KefuModal } from './kefu-modal'
import { LANDING_CSS } from './landing-css'
import { initOrbit } from './orbit'
import { JoinSection, ServicesSection, StudioSection } from './sections'
import { SECTIONS_CSS } from './sections-css'

/* ---- 模块级单例：整页单实例；StrictMode mount→unmount→mount 只保留一个活实例 ---- */
let instance: { cleanup: () => void; refs: number } | null = null
function boot() {
  if (instance) { instance.refs++; return }
  const inst = { cleanup: () => {}, refs: 1 }
  instance = inst
  document.body.classList.add('lit')
  const cleanupOrbit = initOrbit()
  let cleanupBulb = () => {}
  let cancelled = false
  const canvas = document.getElementById('bulb3d') as HTMLCanvasElement | null
  if (canvas) {
    initBulb3d(canvas)
      .then((c) => { if (cancelled) c(); else cleanupBulb = c })
      .catch((e) => { document.body.classList.remove('webgl3d'); console.warn('bulb3d 回退 PNG：', e?.message) })
  }
  inst.cleanup = () => { cancelled = true; cleanupOrbit(); cleanupBulb(); document.body.classList.remove('lit') }
}
function unboot() {
  if (!instance) return
  instance.refs--
  if (instance.refs <= 0) { const c = instance.cleanup; instance = null; c() }
}

export function LandingReact() {
  const { t, i18n } = useTranslation()
  const zh = (i18n.language || 'zh').startsWith('zh')
  const [kefuOpen, setKefuOpen] = useState(false)
  const closeKefu = useCallback(() => setKefuOpen(false), [])
  const { systemName } = useSystemConfig()
  const siteName = systemName || 'WeDream AI'

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
    return () => { setTimeout(unboot, 0) }
  }, [])

  const toggleLang = () => i18n.changeLanguage(zh ? 'en' : 'zh')

  return (
    <div className='wd-landing-root'>
      {/* 挂载时注入样式、卸载即移除（不永久污染全站） */}
      <style dangerouslySetInnerHTML={{ __html: LANDING_CSS + SECTIONS_CSS }} />

      <div id='bg' />
      <div id='aurora' />
      <div id='grid' />
      <div id='stardust' />
      <div id='stardust2' />

      <main className='landing-scene'>
        <header className='lp-topbar'>
          <div className='lp-logo'>
            <img className='lp-mark-img' src='/lp-assets/wedream-logo.png' alt={siteName} />
            <span>{siteName}</span>
          </div>
          <div className='lp-actions'>
            <button className='lp-kefu-btn' type='button' onClick={() => setKefuOpen(true)}>
              <svg viewBox='0 0 24 24'><path d='M4 14v-2a8 8 0 0 1 16 0v2' /><path d='M4 14a2 2 0 0 1 2-2h1v5H6a2 2 0 0 1-2-2v-1ZM20 14a2 2 0 0 0-2-2h-1v5h1a2 2 0 0 0 2-2v-1Z' /><path d='M18 17v1a3 3 0 0 1-3 3h-3' /></svg>
              <span>{T.support}</span>
            </button>
            <a className='lp-start' href='/playground'><span>{T.start}</span> <svg viewBox='0 0 24 24'><path d='M5 12h14M13 6l6 6-6 6' /></svg></a>
            <button className='lp-lang' type='button' title={t('Switch language')} onClick={toggleLang}>
              <svg viewBox='0 0 24 24'><path d='M4 5h11M9 3v2c0 4.5-2.2 8-6 9.5M6.5 10c0 3 2.5 5.5 7 6.5' /><path d='m12.5 20 4-9 4 9M14 17h5' /></svg>
              <span className='lp-lang-txt'>{zh ? 'EN' : '中'}</span>
            </button>
          </div>
        </header>
        <button className='lp-fab' type='button' aria-label={t('Live Support')} onClick={() => setKefuOpen(true)}>
          <svg viewBox='0 0 24 24'><path d='M4 14v-2a8 8 0 0 1 16 0v2' /><path d='M4 14a2 2 0 0 1 2-2h1v5H6a2 2 0 0 1-2-2v-1ZM20 14a2 2 0 0 0-2-2h-1v5h1a2 2 0 0 0 2-2v-1Z' /><path d='M18 17v1a3 3 0 0 1-3 3h-3' /></svg>
        </button>

        <div className='hero-stage'>
          <div className='hero-left'>
            <header id='hero'>
              <span id='badge' className='rise' style={{ '--rd': '.15s' } as any}>
                <svg viewBox='0 0 24 24' fill='currentColor' aria-hidden='true'><path d='M12 1.6c.5 6 4.4 9.9 10.4 10.4-6 .5-9.9 4.4-10.4 10.4-.5-6-4.4-9.9-10.4-10.4 6-.5 9.9-4.4 10.4-10.4Z' /></svg>
                <span>{T.badge}</span>
              </span>
              <h1>
                <span className='hl1 rise' style={{ '--rd': '.3s' } as any}>{siteName}</span>
                <span className='hl2 rise' style={{ '--rd': '.45s' } as any}>{T.hl2}</span>
              </h1>
              <p id='sub' className='rise' style={{ '--rd': '.62s' } as any}>{T.sub}</p>
            </header>
            <div className='features rise' style={{ '--rd': '.72s' } as any}>
              <div className='feature'>
                <span className='feature-ico'><svg viewBox='0 0 24 24' fill='none' stroke='#7cc4ff' strokeWidth='1.7' strokeLinecap='round' strokeLinejoin='round'><circle cx='12' cy='5' r='2.3' /><circle cx='5' cy='18' r='2.3' /><circle cx='19' cy='18' r='2.3' /><path d='M12 7.3v3.4M11 12.4l-4.4 3.4M13 12.4l4.4 3.4' /></svg></span>
                <div><div className='feature-name'>{T.f1n}</div><div className='feature-desc'>{T.f1d}</div></div>
              </div>
              <div className='feature'>
                <span className='feature-ico'><svg viewBox='0 0 24 24' fill='none' stroke='#3ee0b0' strokeWidth='1.7' strokeLinecap='round' strokeLinejoin='round'><path d='M12 3l7 3v5c0 4.4-3 7.6-7 9-4-1.4-7-4.6-7-9V6z' /><path d='M9 12l2 2 4-4' /></svg></span>
                <div><div className='feature-name'>{T.f2n}</div><div className='feature-desc'>{T.f2d}</div></div>
              </div>
            </div>
            <div className='cta rise' style={{ '--rd': '.82s' } as any}>
              <a className='btn btn-primary' href='/dashboard/overview'>{T.cta1}</a>
              <a className='btn btn-ghost' href='/docs'>{T.cta2}</a>
            </div>
          </div>

          <div className='hero-right'>
            <div className='hero-visual'>
              <svg id='orbits'>
                <g id='bandA' />
                <g id='bandB' />
              </svg>
              <div id='halo' />
              <img id='bulb' src='/lp-assets/bulb.png' alt='' draggable='false' />
              <canvas id='bulb3d' />
            </div>
            {/* HUD 默认态；点击卫星后由 orbit.ts 用 i18n.t() 覆写为该模型的信息。 */}
            <aside id='hud'>
              <div className='hud-prov'><span className='hud-dot' /><span id='hud-prov'>{t('System Running')}</span></div>
              <div className='hud-name' id='hud-name'>{t('WeDream Core')}</div>
              <div className='hud-desc' id='hud-desc'>{t('Central intelligence core. The orbiting satellites are the connected models — click any satellite for details.')}</div>
              <div className='hud-scene' id='hud-scene' />
              <div className='hud-tele' id='hud-tele'>{t('Core load 100%')}</div>
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
