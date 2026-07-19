/* WeDream 落地页「三屏」—— 移植自 bulb-orbit/index.html：
     StudioSection   ← 原版 #sec-studio   （708–781）
     ServicesSection ← 原版 #sec-services （783–793 + 脚本1 的 renderServices/SVC_CARDS/SVC_FEATURES 1143–1177）
     JoinSection     ← 原版 #sec-join     （795–848）

   文案全部走 New API 官方 i18n：下方数据数组里存的是 **英文 key**（i18next 自然语言 key 约定），
   译文在 src/i18n/locales/{zh,en}.json；ja/ru/fr/vi 未填的 key 由 fallbackLng:'en' 回落英文。
   原版 index.html 的 data-en 属性替换是「单文件无 i18n 运行时」的无奈之举，此处不复刻。

   与原版的差异（有意为之）：卡片由 innerHTML 模板字符串改为 React 数据驱动 map、SVG 图标改 JSX；
   入场动画由 useReveal（IntersectionObserver + ref）驱动，卸载即 disconnect。 */
import { useTranslation } from 'react-i18next'

import { useReveal } from './use-reveal'

/* ---------------- 图标（原版 SVC_ICONS / SVC_FEATURES / 各 CTA；描边样式由 CSS 统一给） ---------------- */
const IcModels = () => (
  <svg viewBox='0 0 24 24'>
    <path d='M12 3 3 8l9 5 9-5-9-5Z' />
    <path d='M3 12l9 5 9-5' />
    <path d='M3 16l9 5 9-5' />
  </svg>
)
const IcAigc = () => (
  <svg viewBox='0 0 24 24'>
    <path d='M12 3l1.6 4.4L18 9l-4.4 1.6L12 15l-1.6-4.4L6 9l4.4-1.6L12 3Z' />
    <path d='M18.5 14.5l.7 1.9 1.9.7-1.9.7-.7 1.9-.7-1.9-1.9-.7 1.9-.7Z' />
  </svg>
)
const IcResell = () => (
  <svg viewBox='0 0 24 24'>
    <circle cx='6' cy='12' r='2.4' />
    <circle cx='17.5' cy='6' r='2.4' />
    <circle cx='17.5' cy='18' r='2.4' />
    <path d='M8.2 10.9 15.3 7.2M8.2 13.1l7.1 3.7' />
  </svg>
)
const IcOem = () => (
  <svg viewBox='0 0 24 24'>
    <circle cx='12' cy='12' r='8.5' />
    <path d='M8.6 12.2 11 14.7l4.4-5' />
  </svg>
)
const IcPartner = () => (
  <svg viewBox='0 0 24 24'>
    <circle cx='9' cy='8' r='3' />
    <path d='M3.7 20a5.3 5.3 0 0 1 10.6 0' />
    <path d='M16 5.3a3 3 0 0 1 0 5.4' />
    <path d='M15.2 14.4a5.3 5.3 0 0 1 5.1 5.6' />
  </svg>
)
const IcArrow = () => (
  <svg viewBox='0 0 24 24'>
    <path d='M5 12h14M13 6l6 6-6 6' />
  </svg>
)

/* ---------------- ① AI 工坊 / #sec-studio ---------------- */
const CAPS = [
  {
    cls: 'svc-cap-chat',
    icon: (
      <svg viewBox='0 0 24 24'>
        <path d='M5 4h14a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H10l-4 3v-3H5a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2Z' />
        <path d='M8 9h8M8 12.5h5' />
      </svg>
    ),
    title: 'Smart Chat',
    desc: 'Writing, Q&A, analysis, coding — pick a model, start chatting, and think it all through.',
  },
  {
    cls: 'svc-cap-image',
    icon: (
      <svg viewBox='0 0 24 24'>
        <rect x='3' y='4' width='18' height='16' rx='2' />
        <circle cx='8.5' cy='9.5' r='1.6' />
        <path d='m3.5 17 4.5-4 3.5 3 3.5-3.5L21 17' />
      </svg>
    ),
    title: 'Image Creation',
    desc: 'Describe it in one sentence and generate images instantly — turn ideas into visuals fast.',
  },
  {
    cls: 'svc-cap-video',
    icon: (
      <svg viewBox='0 0 24 24'>
        <rect x='3' y='5' width='14' height='14' rx='2' />
        <path d='m17 10 4-2v8l-4-2' />
        <path d='m9 9 4 3-4 3V9Z' />
      </svg>
    ),
    title: 'Video Generation',
    desc: 'Generate dynamic videos from text or images — bring creativity to the screen fast.',
  },
]

export function StudioSection() {
  const { t } = useTranslation()
  const secRef = useReveal<HTMLElement>()
  const headRef = useReveal<HTMLDivElement>()

  return (
    <section id='sec-studio' ref={secRef}>
      <svg
        className='svc-fx'
        viewBox='0 0 1440 900'
        preserveAspectRatio='xMidYMid slice'
        aria-hidden='true'
      >
        <defs>
          <linearGradient id='svcStudioArcg' x1='0' y1='0' x2='1' y2='0'>
            <stop offset='0' stopColor='#35e0f0' stopOpacity='0' />
            <stop offset='.18' stopColor='#35e0f0' stopOpacity='.85' />
            <stop offset='.5' stopColor='#5ab4ff' stopOpacity='.5' />
            <stop offset='.85' stopColor='#7c8cf8' stopOpacity='.25' />
            <stop offset='1' stopColor='#7c8cf8' stopOpacity='0' />
          </linearGradient>
          <radialGradient id='svcStudioBloom'>
            <stop offset='0' stopColor='#35e0f0' stopOpacity='.5' />
            <stop offset='1' stopColor='#35e0f0' stopOpacity='0' />
          </radialGradient>
          <radialGradient id='svcStudioDotg'>
            <stop offset='0' stopColor='#a7e6ff' stopOpacity='.7' />
            <stop offset='1' stopColor='#a7e6ff' stopOpacity='0' />
          </radialGradient>
          <filter
            id='svcStudioGlow'
            x='-50%'
            y='-50%'
            width='200%'
            height='200%'
          >
            <feGaussianBlur stdDeviation='6' />
          </filter>
        </defs>
        <g className='svc-glowgrp'>
          <circle
            cx='360'
            cy='175'
            r='230'
            fill='url(#svcStudioBloom)'
            filter='url(#svcStudioGlow)'
          />
          <path
            d='M -60 300 C 220 150, 560 120, 900 165 S 1440 300, 1520 360'
            fill='none'
            stroke='url(#svcStudioArcg)'
            strokeWidth='3'
            filter='url(#svcStudioGlow)'
            opacity='.55'
          />
          <path
            d='M -60 300 C 220 150, 560 120, 900 165 S 1440 300, 1520 360'
            fill='none'
            stroke='url(#svcStudioArcg)'
            strokeWidth='1.3'
            opacity='.6'
          />
          <path
            d='M -40 388 C 260 250, 620 218, 980 258 S 1420 372, 1500 424'
            fill='none'
            stroke='url(#svcStudioArcg)'
            strokeWidth='1'
            filter='url(#svcStudioGlow)'
            opacity='.28'
          />
        </g>
        <g stroke='#3ecfff' strokeWidth='1' opacity='.12'>
          <line x1='820' y1='800' x2='1520' y2='470' />
          <line x1='980' y1='805' x2='1560' y2='560' />
          <line x1='1140' y1='810' x2='1580' y2='645' />
        </g>
        <g fill='url(#svcStudioDotg)'>
          <circle cx='300' cy='470' r='7' />
          <circle cx='1180' cy='300' r='8' />
          <circle cx='760' cy='655' r='6' />
          <circle cx='1330' cy='188' r='7' />
        </g>
        <g fill='#bfefff'>
          <circle className='svc-tw' cx='300' cy='470' r='2' opacity='.8' />
          <circle cx='1180' cy='300' r='2.4' opacity='.7' />
          <circle
            className='svc-tw'
            cx='760'
            cy='655'
            r='1.6'
            opacity='.8'
            style={{ animationDelay: '2s' }}
          />
          <circle cx='1330' cy='188' r='2' opacity='.6' />
          <circle
            className='svc-tw'
            cx='172'
            cy='250'
            r='1.6'
            opacity='.8'
            style={{ animationDelay: '3.5s' }}
          />
          <circle cx='632' cy='360' r='1.4' opacity='.55' />
        </g>
      </svg>

      <div className='svc-wrap'>
        <div className='svc-stu-head reveal' ref={headRef}>
          <p className='svc-eyebrow'>{t('AI STUDIO')}</p>
          <h2 className='svc-stu-title'>
            {t('One chat room, ')}
            <span className='svc-accent'>{t('Chat · Image · Video')}</span>
          </h2>
          <p className='svc-stu-sub'>
            {t(
              'Pick a model in the AI Studio and start chatting — smart chat, image generation and video generation, all from one entrance.'
            )}
          </p>
          <a className='svc-cta' href='/playground'>
            <span>{t('Enter AI Studio')}</span>
            <IcArrow />
          </a>
        </div>

        <div className='svc-caps'>
          {CAPS.map((c) => (
            <article className={`svc-cap ${c.cls}`} key={c.cls}>
              <span className='svc-cap-ic' aria-hidden='true'>
                {c.icon}
              </span>
              <h3>{t(c.title)}</h3>
              <p>{t(c.desc)}</p>
            </article>
          ))}
        </div>
      </div>
    </section>
  )
}

/* ---------------- ② 核心能力 / #sec-services ---------------- */
const CARDS = [
  {
    cls: 'svc-flagship',
    icon: <IcModels />,
    marker: 'MODELS',
    title: 'Unified Multi-Model Access',
    desc: 'One OpenAI-compatible interface aggregates GPT, Claude, Gemini, DeepSeek and more — switch models by changing a single parameter, with zero changes to existing code.',
    // 品牌名无需译文，i18next 缺 key 时回落 key 自身；仅 Doubao 在 zh.json 有「豆包」
    chips: ['GPT', 'Claude', 'Gemini', 'DeepSeek', 'Grok', 'Qwen', 'Doubao'],
  },
  {
    cls: '',
    icon: <IcAigc />,
    marker: 'AIGC',
    title: 'AIGC Capability Matrix',
    desc: 'Chat, image, video and voice in one integration — an out-of-the-box creative experience for end users.',
    tags: ['Conversation', 'Drawing', 'Video', 'Voice'],
  },
  {
    cls: '',
    icon: <IcResell />,
    marker: 'RESELL',
    title: 'Tiered Reseller Program',
    desc: 'Volume-tiered discounts plus three-level referral commissions, with training and marketing assets included — start reselling right away.',
    tags: ['Tiered discounts', '3-level commission', 'Marketing assets'],
  },
  {
    cls: 'svc-wide',
    icon: <IcOem />,
    marker: 'OEM',
    title: 'White-Label OEM',
    desc: 'Bind your own domain and branded console, set your own pricing and billing rules, with joint official support — launch your own AI platform fast.',
    tags: ['Own domain', 'Branded console', 'Own pricing', 'Joint support'],
  },
  {
    cls: 'svc-wide',
    icon: <IcPartner />,
    marker: 'PARTNER',
    title: 'Compute Partnership',
    desc: 'Strategic partners share equity and compute dividends, secure exclusive regional operations, and get priority in key decisions and future funding rounds.',
    tags: [
      'Equity dividends',
      'Regional exclusivity',
      'Co-building',
      'Funding priority',
    ],
  },
]

const FEATS = [
  {
    icon: (
      <svg viewBox='0 0 24 24'>
        <path d='M4 15a8 8 0 0 1 16 0' />
        <path d='M12 15l4-3' />
        <circle cx='12' cy='15' r='1' />
      </svg>
    ),
    title: 'High Availability',
    desc: '99.9% SLA, smart load balancing and auto failover',
  },
  {
    icon: (
      <svg viewBox='0 0 24 24'>
        <path d='M6 3h12v18l-2.5-1.5L13 21l-2.5-1.5L8 21l-2-1.5V3Z' />
        <path d='M9 8h6M9 12h5' />
      </svg>
    ),
    title: 'Transparent Billing',
    desc: 'Per-token billing with real-time usage and invoices',
  },
  {
    icon: (
      <svg viewBox='0 0 24 24'>
        <path d='M12 3l7 3v5c0 4.6-3 7.7-7 9-4-1.3-7-4.4-7-9V6l7-3Z' />
        <path d='M9.3 12l2 2 3.4-4' />
      </svg>
    ),
    title: 'Enterprise Security',
    desc: 'Fine-grained permissions, encryption and tenant isolation',
  },
  {
    icon: (
      <svg viewBox='0 0 24 24'>
        <path d='M13 3 5 13h5l-1 8 8-10h-5l1-8Z' />
      </svg>
    ),
    title: 'Drop-in Integration',
    desc: 'OpenAI-compatible — go live in a few lines of code',
  },
]

export function ServicesSection() {
  const { t } = useTranslation()
  const secRef = useReveal<HTMLElement>()
  const headRef = useReveal<HTMLElement>()

  return (
    <section id='sec-services' ref={secRef}>
      <div className='svc-wrap'>
        <header className='svc-head reveal' ref={headRef}>
          <p className='svc-eyebrow'>{t('CORE CAPABILITIES')}</p>
          <h2 className='svc-title'>
            {t('From connecting a single model')}
            <br />
            {t('to running an AI business')}
          </h2>
          <p className='svc-lede'>
            {t(
              'Multi-model access, white-label OEM, tiered reseller programs and compute partnerships — a complete chain covering both B2B and B2C.'
            )}
          </p>
        </header>

        <div className='svc-bento'>
          {CARDS.map((c) => (
            <article className={`svc-card ${c.cls}`} key={c.marker}>
              <div className='svc-card-top'>
                <span className='svc-ic' aria-hidden='true'>
                  {c.icon}
                </span>
                <span className='svc-marker'>{c.marker}</span>
              </div>
              <h3 className='svc-card-title'>{t(c.title)}</h3>
              <p className='svc-card-desc'>{t(c.desc)}</p>
              {c.chips ? (
                <div className='svc-constellation'>
                  <svg
                    className='svc-net'
                    viewBox='0 0 600 60'
                    preserveAspectRatio='none'
                    aria-hidden='true'
                  >
                    <defs>
                      <linearGradient id='svcLn' x1='0' y1='0' x2='1' y2='0'>
                        <stop offset='0' stopColor='#3ecfff' />
                        <stop offset='1' stopColor='#8f7bff' />
                      </linearGradient>
                    </defs>
                    <path
                      d='M20 44 C120 8,220 52,320 24 S520 8,584 40'
                      stroke='url(#svcLn)'
                      strokeWidth='1'
                      fill='none'
                    />
                    <path
                      d='M40 20 C160 48,260 12,360 40 S540 48,580 18'
                      stroke='url(#svcLn)'
                      strokeWidth='1'
                      fill='none'
                      opacity='.6'
                    />
                  </svg>
                  <div className='svc-chips'>
                    {c.chips.map((m, i) => (
                      <span
                        className={`svc-chip${i % 2 ? ' alt' : ''}`}
                        key={m}
                      >
                        {t(m)}
                      </span>
                    ))}
                  </div>
                </div>
              ) : (
                <div className='svc-tags'>
                  {(c.tags ?? []).map((tag) => (
                    <span className='svc-tag' key={tag}>
                      {t(tag)}
                    </span>
                  ))}
                </div>
              )}
            </article>
          ))}
        </div>

        <div className='svc-strip'>
          {FEATS.map((f) => (
            <div className='svc-feat' key={f.title}>
              <span className='svc-fic' aria-hidden='true'>
                {f.icon}
              </span>
              <div>
                <b>{t(f.title)}</b>
                <span>{t(f.desc)}</span>
              </div>
            </div>
          ))}
        </div>
      </div>
    </section>
  )
}

/* ---------------- ③ 页尾 CTA / #sec-join ---------------- */
const CONSTELLATION = 'M 100 170 L 390 410 L 720 285 L 1050 410 L 1340 170'

export function JoinSection() {
  const { t } = useTranslation()
  const contentRef = useReveal<HTMLDivElement>()

  return (
    <section id='sec-join'>
      <svg
        className='join-constellation'
        viewBox='0 0 1440 480'
        preserveAspectRatio='xMidYMid slice'
        aria-hidden='true'
      >
        <defs>
          <linearGradient
            id='joinConstellationGrad'
            x1='0'
            y1='0'
            x2='1'
            y2='0'
          >
            <stop offset='0' stopColor='#35dff0' stopOpacity='.04' />
            <stop offset='.32' stopColor='#3ecfff' stopOpacity='.52' />
            <stop offset='.68' stopColor='#5d9cff' stopOpacity='.46' />
            <stop offset='1' stopColor='#8f7bff' stopOpacity='.04' />
          </linearGradient>
          <radialGradient id='joinNodeGlow'>
            <stop offset='0' stopColor='#59dcff' stopOpacity='.72' />
            <stop offset='1' stopColor='#59dcff' stopOpacity='0' />
          </radialGradient>
          <filter
            id='joinConstellationBlur'
            x='-60%'
            y='-60%'
            width='220%'
            height='220%'
          >
            <feGaussianBlur stdDeviation='4.5' />
          </filter>
        </defs>
        <path className='join-constellation-glow' d={CONSTELLATION} />
        <path className='join-constellation-line' d={CONSTELLATION} />
        <g className='join-constellation-branch'>
          <path d='M390 410 250 300 325 145' />
          <path d='M720 285 600 145' />
          <path d='M720 285 840 130' />
          <path d='M1050 410 1190 300 1125 145' />
        </g>
        <path
          className='join-constellation-pulse'
          pathLength={1000}
          d={CONSTELLATION}
        />
        <g>
          <circle className='join-node-halo' cx='100' cy='170' r='34' />
          <circle className='join-node-core' cx='100' cy='170' r='2.2' />
          <circle
            className='join-node-halo'
            cx='390'
            cy='410'
            r='40'
            style={{ animationDelay: '1s' }}
          />
          <circle className='join-node-core alt' cx='390' cy='410' r='2.6' />
          <circle
            className='join-node-halo'
            cx='720'
            cy='285'
            r='52'
            style={{ animationDelay: '2s' }}
          />
          <circle className='join-node-core' cx='720' cy='285' r='3.2' />
          <circle
            className='join-node-halo'
            cx='1050'
            cy='410'
            r='40'
            style={{ animationDelay: '3s' }}
          />
          <circle className='join-node-core alt' cx='1050' cy='410' r='2.6' />
          <circle
            className='join-node-halo'
            cx='1340'
            cy='170'
            r='34'
            style={{ animationDelay: '4s' }}
          />
          <circle className='join-node-core' cx='1340' cy='170' r='2.2' />
        </g>
        <g>
          <circle className='join-spark' cx='250' cy='300' r='1.7' />
          <circle
            className='join-spark'
            cx='325'
            cy='145'
            r='2'
            style={{ animationDelay: '1.1s' }}
          />
          <circle
            className='join-spark'
            cx='600'
            cy='145'
            r='1.8'
            style={{ animationDelay: '2.3s' }}
          />
          <circle
            className='join-spark'
            cx='840'
            cy='130'
            r='2.1'
            style={{ animationDelay: '3.2s' }}
          />
          <circle
            className='join-spark'
            cx='1190'
            cy='300'
            r='1.7'
            style={{ animationDelay: '1.8s' }}
          />
          <circle
            className='join-spark'
            cx='1125'
            cy='145'
            r='2'
            style={{ animationDelay: '3.8s' }}
          />
        </g>
      </svg>

      <div className='join-content reveal' ref={contentRef}>
        <h2 className='join-title'>
          <span>{t('Turn fleeting inspiration')}</span>
          <span className='join-accent'>
            {t('into finished works with WeDream AI')}
          </span>
        </h2>
        <p className='join-copy'>
          {t(
            'Freely switch between top models — talk your ideas through, illustrate them, and turn them into videos. From a spark of inspiration to a finished work, all from one entrance.'
          )}
        </p>
        <div className='join-actions'>
          <a
            className='join-btn join-btn-primary'
            href='/playground'
            rel='noopener'
          >
            <span>{t('Start Creating')}</span>
            <IcArrow />
          </a>
          <a className='join-btn' href='/docs' rel='noopener'>
            <svg viewBox='0 0 24 24' aria-hidden='true'>
              <path d='M12 3l1.5 4.2L18 9l-4.5 1.8L12 15l-1.5-4.2L6 9l4.5-1.8L12 3Z' />
              <path d='M18.5 15l.7 1.8 1.8.7-1.8.7-.7 1.8-.7-1.8-1.8-.7 1.8-.7.7-1.8Z' />
            </svg>
            <span>{t('Learn about WeDream AI')}</span>
          </a>
        </div>
      </div>
    </section>
  )
}
