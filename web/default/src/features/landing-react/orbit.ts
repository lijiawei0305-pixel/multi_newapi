// @ts-nocheck
/* WeDream 伪3D 轨道系统 —— 移植自 bulb-orbit/index.html 脚本1（852–1141），参数原样
   （X0=22°/SPIN_PERIOD=300s/RY=0.33/ORBIT_SCALE=0.42）。改动：logo 路径→/lp-assets/；
   增加 React 卸载清理（cancel rAF + 移除动态节点 + 解绑 resize）；服务区渲染与 reveal 观察器不搬（在 sections.tsx）。
   导出 initOrbit() → cleanup。灯泡染色经 window.__bulb3d.setCoreColor（bulb3d.ts 提供）。

   i18n：本模块在 React 之外命令式运行，拿不到 useTranslation()，故直接引 i18next 单例。
   MODELS 里存的是**英文 key**，renderCard() 在「点击卫星那一刻」用 i18n.t() 取译文 ——
   因此语言切换后再点，取到的就是新语言（无需重挂载轨道）。 */
import i18n from '@/i18n/config'

export function initOrbit(): () => void {
  const TAU = Math.PI * 2
  const DEG = Math.PI / 180
  const added: Element[] = []

  const stage = document.querySelector('.hero-visual') as HTMLElement
  const heroRight = document.querySelector('.hero-right') as HTMLElement
  if (!stage || !heroRight) return () => {}
  /* --cx/--cy/--b3d/--chip 的兜底值声明在 .wd-landing-root（landing-css.ts）上。layout() 必须把动态值
     内联写到「同一个元素」——写到更外层的 <html> 会被 .wd-landing-root 自己的声明遮蔽掉，动态值永不生效。
     （原版 index.html 兜底在 :root，故写 documentElement 是对的；此处作用域收敛后必须同步。） */
  const varsEl = (stage.closest('.wd-landing-root') as HTMLElement) ?? document.documentElement
  let revealT0 = performance.now() / 1000
  const clamp01 = (x: number) => (x < 0 ? 0 : x > 1 ? 1 : x)
  const easeOut3 = (x: number) => 1 - Math.pow(1 - x, 3)

  const LOGOS: Record<string, string> = {
    openai: `<svg viewBox="0 0 24 24"><path fill="#ECF3FF" d="M22.2819 9.8211a5.9847 5.9847 0 0 0-.5157-4.9108 6.0462 6.0462 0 0 0-6.5098-2.9A6.0651 6.0651 0 0 0 4.9807 4.1818a5.9847 5.9847 0 0 0-3.9977 2.9 6.0462 6.0462 0 0 0 .7427 7.0966 5.98 5.98 0 0 0 .511 4.9107 6.051 6.051 0 0 0 6.5146 2.9001A5.9847 5.9847 0 0 0 13.2599 24a6.0557 6.0557 0 0 0 5.7718-4.2058 5.9894 5.9894 0 0 0 3.9977-2.9001 6.0557 6.0557 0 0 0-.7475-7.073zm-9.022 12.6081a4.4755 4.4755 0 0 1-2.8764-1.0408l.1419-.0804 4.7783-2.7582a.7948.7948 0 0 0 .3927-.6813v-6.7369l2.02 1.1686a.071.071 0 0 1 .038.052v5.5826a4.504 4.504 0 0 1-4.4945 4.4944zm-9.6607-4.1254a4.4708 4.4708 0 0 1-.5346-3.0137l.142.0852 4.783 2.7582a.7712.7712 0 0 0 .7806 0l5.8428-3.3685v2.3324a.0804.0804 0 0 1-.0332.0615L9.74 19.9502a4.4992 4.4992 0 0 1-6.1408-1.6464zM2.3408 7.8956a4.485 4.485 0 0 1 2.3655-1.9728V11.6a.7664.7664 0 0 0 .3879.6765l5.8144 3.3543-2.0201 1.1685a.0757.0757 0 0 1-.071 0l-4.8303-2.7865A4.504 4.504 0 0 1 2.3408 7.8956zm16.5963 3.8558L13.1038 8.364 15.1192 7.2a.0757.0757 0 0 1 .071 0l4.8303 2.7913a4.4944 4.4944 0 0 1-.6765 8.1042v-5.6772a.79.79 0 0 0-.407-.667zm2.0107-3.0231l-.142-.0852-4.7735-2.7818a.7759.7759 0 0 0-.7854 0L9.409 9.2297V6.8974a.0662.0662 0 0 1 .0284-.0615l4.8303-2.7866a4.4992 4.4992 0 0 1 6.6802 4.66zM8.3065 12.863l-2.02-1.1638a.0804.0804 0 0 1-.038-.0567V6.0742a4.4992 4.4992 0 0 1 7.3757-3.4537l-.142.0805L8.704 5.459a.7948.7948 0 0 0-.3927.6813zm1.0976-2.3654l2.602-1.4998 2.6069 1.4998v2.9994l-2.5974 1.4997-2.6067-1.4997z"/></svg>`,
    anthropic: `<svg viewBox="0 0 24 24"><path fill="#D97757" fill-rule="evenodd" d="M17.3041 3.541h-3.6718l6.696 16.918H24Zm-10.6082 0L0 20.459h3.7442l1.3693-3.5527h7.0052l1.3693 3.5527h3.7442L10.5363 3.541Zm-.3712 10.2232 2.2914-5.9456 2.2914 5.9456Z"/></svg>`,
    gemini: `<svg viewBox="0 0 24 24"><defs><linearGradient id="__id__g" x1="1" y1="4" x2="22" y2="21" gradientUnits="userSpaceOnUse"><stop offset="0" stop-color="#559BFA"/><stop offset=".55" stop-color="#8A7CF8"/><stop offset="1" stop-color="#C96BF0"/></linearGradient></defs><path fill="url(#__id__g)" d="M12 0c.53 6.9 5.1 11.47 12 12-6.9.53-11.47 5.1-12 12-.53-6.9-5.1-11.47-12-12C6.9 11.47 11.47 6.9 12 0Z"/></svg>`,
    deepseek: `<svg viewBox="0 0 24 24"><path fill="#5B78FF" d="M23.748 4.482c-.254-.124-.364.113-.512.234-.051.039-.094.09-.137.136-.372.397-.806.657-1.373.626-.829-.046-1.537.214-2.163.848-.133-.782-.575-1.248-1.247-1.548-.352-.156-.708-.311-.955-.65-.172-.241-.219-.51-.305-.774-.055-.16-.11-.323-.293-.35-.2-.031-.278.136-.356.276-.313.572-.434 1.202-.422 1.84.027 1.436.633 2.58 1.838 3.393.137.093.172.187.129.323-.082.28-.18.552-.266.833-.055.179-.137.217-.329.14a5.526 5.526 0 0 1-1.736-1.18c-.857-.828-1.631-1.742-2.597-2.458a11.365 11.365 0 0 0-.689-.471c-.985-.957.13-1.743.388-1.836.27-.098.093-.432-.779-.428-.872.004-1.67.295-2.687.684a3.055 3.055 0 0 1-.465.137 9.597 9.597 0 0 0-2.883-.102c-1.885.21-3.39 1.102-4.497 2.623C.082 8.606-.231 10.684.152 12.85c.403 2.284 1.569 4.175 3.36 5.653 1.858 1.533 3.997 2.284 6.438 2.14 1.482-.085 3.133-.284 4.994-1.86.47.234.962.327 1.78.397.63.059 1.236-.03 1.705-.128.735-.156.684-.837.419-.961-2.155-1.004-1.682-.595-2.113-.926 1.096-1.296 2.746-2.642 3.392-7.003.05-.347.007-.565 0-.845-.004-.17.035-.237.23-.256a4.173 4.173 0 0 0 1.545-.475c1.396-.763 1.96-2.015 2.093-3.517.02-.23-.004-.467-.247-.588zM11.581 18c-2.089-1.642-3.102-2.183-3.52-2.16-.392.024-.321.471-.235.763.09.288.207.486.371.739.114.167.192.416-.113.603-.673.416-1.842-.14-1.897-.167-1.361-.802-2.5-1.86-3.301-3.307-.774-1.393-1.224-2.887-1.298-4.482-.02-.386.093-.522.477-.592a4.696 4.696 0 0 1 1.529-.039c2.132.312 3.946 1.265 5.468 2.774.868.86 1.525 1.887 2.202 2.891.72 1.066 1.494 2.082 2.48 2.914.348.292.625.514.891.677-.802.09-2.14.11-3.054-.614zm1-6.44a.306.306 0 0 1 .415-.287.302.302 0 0 1 .2.288.306.306 0 0 1-.31.307.303.303 0 0 1-.304-.308zm3.11 1.596c-.2.081-.399.151-.59.16a1.245 1.245 0 0 1-.798-.254c-.274-.23-.47-.358-.552-.758a1.73 1.73 0 0 1 .016-.588c.07-.327-.008-.537-.239-.727-.187-.156-.426-.199-.688-.199a.559.559 0 0 1-.254-.078c-.11-.054-.2-.19-.114-.358.028-.054.16-.186.192-.21.356-.202.767-.136 1.146.016.353.144.62.409 1.004.781.393.45.462.576.685.914.176.265.336.537.445.848.067.195-.019.354-.253.453z"/></svg>`,
    xai: `<svg viewBox="0 0 24 24"><path fill="#F2F6FC" d="m3.005 8.858 8.783 12.544h3.904L6.908 8.858zM6.905 15.825 3 21.402h3.907l1.951-2.788zM16.585 2l-6.75 9.64 1.953 2.79L20.492 2zM17.292 7.965v13.437h3.2V3.395z"/></svg>`,
    qwen: `<img src="/lp-assets/logos/qwen.png" alt="" draggable="false">`,
    doubao: `<img src="/lp-assets/logos/doubao.png" alt="" draggable="false">`,
    kimi: `<img src="/lp-assets/logos/kimi.png" alt="" draggable="false">`,
    glm_chatglm: `<img src="/lp-assets/logos/glm_chatglm.png" alt="" draggable="false">`,
    minimax: `<img src="/lp-assets/logos/minimax.png" alt="" draggable="false">`,
  }

  /* 值 = i18n 英文 key（译文见 src/i18n/locales/{zh,en}.json）；纯英文的厂商名未建 key，
     由 i18next fallback 回落 key 自身。原 DEFAULT_MODEL 是死代码（从未被引用，HUD 默认态
     在 index.tsx 的 JSX 里），已随本次 i18n 改造一并删除。 */
  const MODELS: Record<string, any> = {
    openai: { name: 'GPT Series', provider: 'OPENAI', desc: 'Multimodal flagship — top-tier general intelligence and tool use, with the most mature ecosystem.', scene: 'General chat, agents, multimodal understanding and generation.', telemetry: 'Multimodal pipeline online', color: '#10d075' },
    anthropic: { name: 'Claude Series', provider: 'ANTHROPIC', desc: 'The benchmark for coding and complex reasoning; reliable long context and excellent safety alignment.', scene: 'Coding agents, long-document analysis, serious writing.', telemetry: '200K context active', color: '#d97757' },
    gemini: { name: 'Gemini Series', provider: 'GOOGLE DEEPMIND', desc: 'Natively multimodal engine with million-token context and strong video/audio understanding.', scene: 'Multimodal analysis, Q&A over very large corpora.', telemetry: '1M token pipeline', color: '#9020f0' },
    deepseek: { name: 'DeepSeek Series', provider: 'DEEPSEEK', desc: 'Best value in reasoning and code; open and strong at mathematics.', scene: 'Cost-effective reasoning, code generation, math problem solving.', telemetry: 'Reasoning chain active', color: '#4fa3ff' },
    xai: { name: 'Grok Series', provider: 'XAI', desc: 'Plugged into real-time information, with a distinct style and fast-improving reasoning.', scene: 'Real-time information Q&A, trend analysis.', telemetry: 'Real-time retrieval in sync', color: '#00a0ff' },
    qwen: { name: 'Qwen Series', provider: 'Alibaba Cloud', desc: 'Domestic open-source flagship — strong at code and multilingual tasks, with the broadest size lineup.', scene: 'Chinese conversation, code generation, structured output.', telemetry: 'Full size lineup online', color: '#6b6dff' },
    minimax: { name: 'MiniMax Series', provider: 'MINIMAX', desc: 'Long context and multimodality in step, with outstanding speech synthesis.', scene: 'Long-form processing, voice applications, character dialogue.', telemetry: 'Million-scale context', color: '#b987ff' },
    doubao: { name: 'Doubao Large Model', provider: 'ByteDance', desc: 'High concurrency at low cost, great everyday Chinese conversation, proven at scale.', scene: 'Large-scale consumer apps, smart customer service, translation.', telemetry: 'Volcano Engine pipeline', color: '#39c5ff' },
    kimi: { name: 'Kimi Series', provider: 'Moonshot AI', desc: 'A pioneer of ultra-long context — great for web and document digests, with open-source reasoning models.', scene: 'Long-document reading, research digests, deep reasoning.', telemetry: 'Ultra-long context active', color: '#6c7cff' },
    glm_chatglm: { name: 'GLM Series', provider: 'Zhipu AI', desc: 'Tsinghua-rooted technology with strong agent and coding ability; open and accessible.', scene: 'Agent development, coding assistance, academic research.', telemetry: 'Agent pipeline active', color: '#6f7bff' },
  }
  const hudEls = {
    box: document.getElementById('hud'),
    prov: document.getElementById('hud-prov'), name: document.getElementById('hud-name'),
    desc: document.getElementById('hud-desc'), scene: document.getElementById('hud-scene'),
    tele: document.getElementById('hud-tele'), dot: document.querySelector('#hud .hud-dot') as HTMLElement,
  }
  function renderCard(m: any) {
    if (!hudEls.box) return
    // 在「点击那一刻」取译文 → 语言切换后再点即为新语言
    hudEls.prov!.textContent = i18n.t(m.provider)
    hudEls.name!.textContent = i18n.t(m.name)
    hudEls.desc!.textContent = i18n.t(m.desc)
    hudEls.scene!.textContent = m.scene ? i18n.t(m.scene) : ''
    hudEls.tele!.textContent = i18n.t(m.telemetry)
    ;(hudEls.box as HTMLElement).style.borderLeftColor = m.color
    if (hudEls.dot) hudEls.dot.style.background = m.color
  }

  const ORDER_A = ['openai', 'anthropic', 'gemini', 'deepseek', 'xai']
  const ORDER_B = ['qwen', 'minimax', 'doubao', 'kimi', 'glm_chatglm']
  const bands: any[] = [
    { incl: 0.62, period: 40, dir: +1, precessPeriod: 45, precessDir: +1, baseTilt: 34 * DEG, spinDir: +1, spinPeriod: 55, order: ORDER_A, phase0: 0, g: document.getElementById('bandA') },
    { incl: 0.92, period: 52, dir: -1, precessPeriod: 70, precessDir: -1, baseTilt: -34 * DEG, spinDir: -1, spinPeriod: 55, order: ORDER_B, phase0: TAU / 24, g: document.getElementById('bandB') },
  ]

  function ringPsi(band: any, t: number) { return band.precessDir * TAU * (t / band.precessPeriod) }
  function projectRing(band: any, phi: number, psi: number) {
    const cf = Math.cos(phi), sf = Math.sin(phi)
    const ci = Math.cos(band.incl), si = Math.sin(band.incl)
    const cp = Math.cos(psi), sp = Math.sin(psi)
    return { x: cf * cp + sf * ci * sp, y: -sf * si, z: -cf * sp + sf * ci * cp }
  }
  const Q = new URLSearchParams(location.search)
  const FLAT = !/3d/i.test(Q.get('mode') || '')
  const X0 = (parseFloat(Q.get('x0')!) || 22) * DEG
  const SPIN_PERIOD = parseFloat(Q.get('period')!) || 300
  const RY = parseFloat(Q.get('ry')!) || 0.33
  const ORBIT_SCALE = parseFloat(Q.get('rs')!) || 0.42
  function projectRing2D(band: any, phi: number, t: number) {
    const theta = REDUCED ? X0 + 0.9 : X0 + TAU * (t / SPIN_PERIOD)
    const ang = band.spinDir * theta
    const ex = Math.cos(phi), ey = RY * Math.sin(phi)
    const ca = Math.cos(ang), sa = Math.sin(ang)
    const y = ex * sa + ey * ca
    return { x: ex * ca - ey * sa, y, z: y }
  }
  function projFor(t: number) {
    if (FLAT) return (B: any, phi: number) => projectRing2D(B, phi, t)
    for (const B of bands) B.psi = ringPsi(B, REDUCED ? 6.0 : t)
    return (B: any, phi: number) => projectRing(B, phi, B.psi)
  }

  const SVG_NS = 'http://www.w3.org/2000/svg'
  const SEG = 64
  for (const band of bands) {
    band.layers = {}
    for (const key of ['halo', 'mid', 'cglow', 'core']) {
      const g = document.createElementNS(SVG_NS, 'g')
      const segs: any[] = []
      for (let i = 0; i < SEG; i++) {
        const l = document.createElementNS(SVG_NS, 'line')
        l.setAttribute('stroke-linecap', 'round')
        g.appendChild(l); segs.push(l)
      }
      band.g.appendChild(g)
      band.layers[key] = segs
    }
  }

  const chips: any[] = []
  bands.forEach((band, b) => {
    band.order.forEach((name: string, i: number) => {
      const el = document.createElement('div')
      el.className = 'chip ' + (b === 0 ? 'main' : 'alt')
      el.dataset.mid = name
      const spin = 12 + ((b * 5 + i * 7) % 9)
      const dir = (i + b) % 2 ? 'reverse' : 'normal'
      el.innerHTML = `<span class="shell"><span class="disc" style="--spin:${spin}s;--dir:${dir}">` +
        LOGOS[name].replaceAll('__id__', 'u' + b + '_' + i) + '</span></span>'
      stage.appendChild(el); added.push(el)
      chips.push({ el, band, phase: band.phase0 + i * TAU / band.order.length, z: -1, idx: chips.length })
    })
  })

  function selectModel(mid: string | null) {
    if (mid) { renderCard(MODELS[mid]); hudEls.box?.classList.add('show'); heroRight.classList.add('card-open') }
    else { hudEls.box?.classList.remove('show'); heroRight.classList.remove('card-open') }
    if ((window as any).__bulb3d) (window as any).__bulb3d.setCoreColor(mid ? MODELS[mid].color : null)
  }
  for (const c of chips) {
    c.el.style.pointerEvents = 'auto'
    c.el.style.cursor = 'pointer'
    c.el.addEventListener('click', (e: Event) => { e.stopPropagation(); selectModel(c.el.dataset.mid) })
  }
  const onStageClick = () => selectModel(null)
  stage.addEventListener('click', onStageClick)

  const flows: any[] = []
  bands.forEach((band, b) => {
    for (let j = 0; j < 7; j++) {
      const el = document.createElement('div')
      el.className = 'fp'
      stage.appendChild(el); added.push(el)
      flows.push({ el, band, phase: j * TAU / 7 + b * 0.45 + j * 0.37, period: b ? 13 : 9 })
    }
  })

  /* 原版 index.html 有 5 圈 .ring 雷达环装饰（半径 [0.34,0.61,0.88,1.15,1.42]）；
     用户要求 React 版去掉（视觉太杂），此处为与原版的「刻意差异」，非移植遗漏。 */

  for (let i = 0; i < 36; i++) {
    const p = document.createElement('div')
    p.className = 'p'
    const s = (0.8 + Math.random() * 1.4).toFixed(1)
    p.style.cssText =
      `left:${(2 + Math.random() * 96).toFixed(1)}%;top:${(2 + Math.random() * 96).toFixed(1)}%;` +
      `width:${s}px;height:${s}px;background:rgba(235,245,255,.9);` +
      `--dx:${(Math.random() * 24 - 12).toFixed(0)}px;--dy:${(Math.random() * 24 - 12).toFixed(0)}px;` +
      `--dur:${(20 + Math.random() * 20).toFixed(1)}s;--tdur:${(2.5 + Math.random() * 3.5).toFixed(1)}s;` +
      `--delay:-${(Math.random() * 30).toFixed(1)}s;` +
      `--o1:${(0.05 + Math.random() * 0.10).toFixed(2)};--o2:${(0.35 + Math.random() * 0.35).toFixed(2)}`
    stage.appendChild(p); added.push(p)
  }
  for (let i = 0; i < 14; i++) {
    const p = document.createElement('div')
    p.className = 'p'
    const s = (2 + Math.random() * 1.6).toFixed(1)
    p.style.cssText =
      `left:${(4 + Math.random() * 92).toFixed(1)}%;top:${(4 + Math.random() * 92).toFixed(1)}%;` +
      `width:${s}px;height:${s}px;background:rgba(160,215,255,.9);` +
      `--dx:${(Math.random() * 120 - 60).toFixed(0)}px;--dy:${(Math.random() * 120 - 60).toFixed(0)}px;` +
      `--dur:${(16 + Math.random() * 24).toFixed(1)}s;--tdur:${(3 + Math.random() * 4).toFixed(1)}s;` +
      `--delay:-${(Math.random() * 30).toFixed(1)}s;` +
      `--o1:${(0.08 + Math.random() * 0.12).toFixed(2)};--o2:${(0.3 + Math.random() * 0.25).toFixed(2)}`
    stage.appendChild(p); added.push(p)
  }
  for (let i = 0; i < 9; i++) {
    const ray = document.createElement('div')
    ray.className = 'ray'
    const len = 30 + ((i * 29) % 18)
    const ang = i * 40 + (((i * 53) % 17) - 8)
    ray.style.cssText =
      `height:${len}vmin;margin-top:${-len}vmin;transform:rotate(${ang}deg);` +
      `--o1:${(0.30 + (i % 3) * 0.08).toFixed(2)};--o2:${(0.65 + (i % 4) * 0.07).toFixed(2)};` +
      `--pdur:${5 + ((i * 13) % 5)}s;--pdelay:-${(i * 1.7).toFixed(1)}s`
    stage.appendChild(ray); added.push(ray)
  }

  let CX = 0, CY = 0, RS = 0
  function layout() {
    const rect = heroRight.getBoundingClientRect()
    const rw = Math.max(1, rect.width), rh = Math.max(1, rect.height)
    const b3d = Math.round(Math.min(rw * 0.98, rh * 0.94))
    CX = rw * 0.36; CY = rh * 0.48
    RS = b3d * ORBIT_SCALE
    const root = varsEl.style
    root.setProperty('--cx', CX.toFixed(1) + 'px')
    root.setProperty('--cy', CY.toFixed(1) + 'px')
    root.setProperty('--b3d', b3d + 'px')
    root.setProperty('--chip', Math.round(Math.min(50, Math.max(30, b3d * 0.07))) + 'px')
  }
  layout()
  addEventListener('resize', layout)

  const REDUCED = matchMedia('(prefers-reduced-motion: reduce)').matches
  function rebuildBand(B: any, proj: any) {
    for (let i = 0; i < SEG; i++) {
      const a1 = i / SEG * TAU, a2 = (i + 1) / SEG * TAU, am = (a1 + a2) / 2
      const p1 = proj(B, a1), p2 = proj(B, a2), pm = proj(B, am)
      const x1 = CX + p1.x * RS, y1 = CY + p1.y * RS
      const x2 = CX + p2.x * RS, y2 = CY + p2.y * RS
      const d = (pm.z + 1) / 2
      const pe = 0.25 + 0.75 * d
      const put = (seg: any, wdt: number, stroke: string) => {
        seg.setAttribute('x1', x1.toFixed(1)); seg.setAttribute('y1', y1.toFixed(1))
        seg.setAttribute('x2', x2.toFixed(1)); seg.setAttribute('y2', y2.toFixed(1))
        seg.setAttribute('stroke-width', wdt.toFixed(2)); seg.setAttribute('stroke', stroke)
      }
      put(B.layers.halo[i], (7 + 5 * pe) * 2.2, `rgba(80,160,255,${((0.05 + 0.13 * pe) * 0.34).toFixed(3)})`)
      put(B.layers.mid[i], 1.6 + 1.2 * pe,
        `rgba(${Math.round(77 + 48 * pe)},${Math.round(166 + 18 * pe)},255,${(0.14 + 0.72 * pe).toFixed(3)})`)
      put(B.layers.cglow[i], 3.2, `rgba(150,215,255,${(0.04 + 0.24 * pe).toFixed(3)})`)
      put(B.layers.core[i], 1, `rgba(207,232,255,${(0.10 + 0.8 * pe).toFixed(3)})`)
    }
  }
  let rafId = 0
  function frame(now: number) {
    const t = now / 1000
    const tr = t - revealT0
    const proj = projFor(t)
    for (const B of bands) rebuildBand(B, proj)
    for (const c of chips) {
      const B = c.band
      const phi = c.phase + B.dir * TAU * (t / B.period)
      const p = proj(B, phi)
      const d = (p.z + 1) / 2
      const cr = easeOut3(clamp01((tr - c.idx * 0.055) / 0.75))
      c.el.style.transform =
        `translate3d(${(p.x * RS).toFixed(2)}px,${(p.y * RS).toFixed(2)}px,0) scale(${((0.62 + d * 0.5) * (0.55 + 0.45 * cr)).toFixed(3)})`
      c.el.style.opacity = ((0.5 + 0.5 * d) * cr).toFixed(3)
      const z = 2 + Math.round(d * 8)
      if (z !== c.z) { c.z = z; c.el.style.zIndex = z }
    }
    for (const fl of flows) {
      const B = fl.band
      const phi = fl.phase + B.dir * TAU * (t / fl.period)
      const p = proj(B, phi)
      const d = (p.z + 1) / 2
      const fr = easeOut3(clamp01((tr - 0.35) / 1.0))
      fl.el.style.transform =
        `translate3d(${(p.x * RS).toFixed(2)}px,${(p.y * RS).toFixed(2)}px,0) scale(${(0.7 + d * 0.5).toFixed(3)})`
      fl.el.style.opacity = ((0.25 + 0.75 * d) * fr).toFixed(3)
    }
    rafId = requestAnimationFrame(frame)
  }
  rafId = requestAnimationFrame(frame)

  return () => {
    cancelAnimationFrame(rafId)
    removeEventListener('resize', layout)
    stage.removeEventListener('click', onStageClick)
    added.forEach((e) => e.remove())
    for (const id of ['bandA', 'bandB']) { const g = document.getElementById(id); if (g) g.innerHTML = '' }
    heroRight.classList.remove('card-open')
  }
}
