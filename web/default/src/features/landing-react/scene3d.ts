// @ts-nocheck
/* WeDream 落地页统一 3D 场景 —— 灯泡 + 两条 3D 轨道 + 全息卫星,同一 WebGL 画布。
   替代旧 orbit.ts(SVG)并吸收 bulb3d.ts(灯泡)。本文件随实现计划分任务扩建:
   T3 灯泡骨架 → T4 轨道 → T5 卫星 → T6 方向 → T7 进动 → T8 缓停/锁定 → T9 选中副作用。

   与旧 bulb3d.ts 的关键差异:
   - 相机不再"贴着灯泡"取景,改用 yun 取景(fov 45 / z 4.6),给轨道留出空间(灯泡因此变小、留白待轨道填)。
   - 画布不再是 --b3d 定尺方块,而是铺满 .hero-visual;渲染缓冲固定高 940(保 bloom 归一化一致,见旧注释)
     宽随盒子宽高比,ResizeObserver 适配。透明叠加(AlphaFromLuma)保留,让黑底/极光透出。 */
import {
  ACESFilmicToneMapping, AdditiveBlending, AmbientLight, BufferAttribute, BufferGeometry,
  CanvasTexture, Color, Curve, CylinderGeometry, DoubleSide, Group, MathUtils, Mesh,
  MeshBasicMaterial, MeshPhongMaterial, MeshStandardMaterial, PerspectiveCamera, PlaneGeometry,
  PointLight, Points, PointsMaterial, Quaternion, Raycaster, Scene, ShaderMaterial, SphereGeometry,
  SRGBColorSpace, Sprite, SpriteMaterial, TorusGeometry, TubeGeometry, Vector2, Vector3, WebGLRenderer,
} from 'three'
import { EffectComposer } from 'three/examples/jsm/postprocessing/EffectComposer.js'
import { OutputPass } from 'three/examples/jsm/postprocessing/OutputPass.js'
import { RenderPass } from 'three/examples/jsm/postprocessing/RenderPass.js'
import { ShaderPass } from 'three/examples/jsm/postprocessing/ShaderPass.js'
import { UnrealBloomPass } from 'three/examples/jsm/postprocessing/UnrealBloomPass.js'

import i18n from '@/i18n/config'

import { approach, drawLogoCanvas, makeGlowTexture, sampleAlphaToPoints } from './scene3d-assets'
import { LOGOS, MODELS, ORBITS } from './scene3d-config'
import { nextSelection } from './scene3d-interaction'

const DATA_URL = '/lp-assets/dengpao_points.bin'
const RENDER_H = 940 // 渲染缓冲高度固定(bloom 归一化一致);宽 = 高 × 盒子宽高比
const BULB_SCALE = 1.5 // 灯泡整体放大(与轨道解耦:轨道半径在 config 里单独收小 → 大灯泡 + 小轨道)
const SHIFT_X = -0.28 // 灯泡+轨道整体左移(用户觉得太靠右);同步用于灯泡剪影遮罩中心

export async function initScene3d(canvas: HTMLCanvasElement): Promise<() => void> {
  const PRM = matchMedia('(prefers-reduced-motion: reduce)').matches
  const FROZEN_T = 12.0

  // ---- 灯泡粒子着色器(与 bulb3d.ts 原样一致,保观感不变)----
  const bulbVertexShader = `
    uniform float uTime; uniform float uSize; uniform vec3 uEdgeColor;
    uniform vec3 uMouseOrigin; uniform vec3 uMouseDir;
    uniform float uRepelStrength; uniform float uRepelRadius; uniform float uRepelPower;
    attribute vec3 aNormal; attribute float aRandom; varying vec3 vColor; varying float vBaseFade;
    void main() {
      // 底座/下半致密粒子渐进调暗:越靠底越暗,灯泡主体(上半)不动。
      vBaseFade = smoothstep(-0.55, 0.05, position.y);
      float breathingOffset = sin(uTime * 1.1 + aRandom * 6.28318) * 0.006;
      vec3 noiseOffset = vec3(
        sin(uTime * 0.5 + aRandom * 25.0), cos(uTime * 0.7 + aRandom * 30.0), sin(uTime * 0.9 + aRandom * 35.0)
      ) * 0.002;
      vec3 finalPos = position + aNormal * breathingOffset + noiseOffset;
      float breath = 1.0 + sin(uTime * 0.7) * 0.012; finalPos *= breath;
      vec3 worldPos = (modelMatrix * vec4(finalPos, 1.0)).xyz;
      vec3 toParticle = worldPos - uMouseOrigin;
      vec3 closest = uMouseOrigin + uMouseDir * dot(toParticle, uMouseDir);
      vec3 away = worldPos - closest; float rayDist = length(away);
      float force = uRepelStrength * exp(-(rayDist * rayDist) / (uRepelRadius * uRepelRadius));
      vec3 pushDir = rayDist > 0.0001 ? away / rayDist : aNormal;
      worldPos += pushDir * force * uRepelPower * (0.75 + 0.5 * aRandom);
      float radialDist = length(position.xz);
      vec3 coreColor = vec3(0.85, 0.94, 1.0);
      vColor = mix(coreColor, uEdgeColor, smoothstep(0.06, 0.35, radialDist));
      vColor = mix(vColor, vec3(0.75, 0.95, 1.0), force * 0.6);
      vec4 mvPosition = viewMatrix * vec4(worldPos, 1.0);
      gl_Position = projectionMatrix * mvPosition;
      float twinkle = 0.7 + 0.3 * sin(uTime * (1.6 + aRandom * 2.2) + aRandom * 6.28318);
      gl_PointSize = uSize * (300.0 / -mvPosition.z) * twinkle;
    }`
  const bulbFragmentShader = `
    varying vec3 vColor; varying float vBaseFade;
    void main() {
      float dist = length(gl_PointCoord - vec2(0.5));
      if (dist > 0.5) discard;
      float alpha = smoothstep(0.5, 0.06, dist);
      float core = smoothstep(0.12, 0.0, dist) * 0.6;
      vec3 finalColor = mix(vColor, vec3(1.0), core * 0.4);
      gl_FragColor = vec4(finalColor, (alpha * 0.45 + core * 0.15) * 0.75 * mix(0.15, 1.0, vBaseFade));
    }`
  // luma→alpha:让透明画布正确叠加在页面(黑底/极光)之上
  const AlphaFromLumaShader = {
    uniforms: { tDiffuse: { value: null } },
    vertexShader: `varying vec2 vUv;
      void main() { vUv = uv; gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0); }`,
    fragmentShader: `uniform sampler2D tDiffuse; varying vec2 vUv;
      void main() { vec4 c = texture2D(tDiffuse, vUv); gl_FragColor = vec4(c.rgb, clamp(max(c.r, max(c.g, c.b)), 0.0, 1.0)); }`,
  }

  // ---- 轨道着色器(移植 yun main.js:97-156)----
  // 深度淡化(近亮远暗)+ 灯泡剪影遮罩(绕到灯泡背后的弧被淡出)+ 彗尾光迹(卫星身后拖尾)+ 能量流。
  const orbitVertexShader = `
    varying vec3 vViewPos; varying vec2 vUv;
    void main() {
      vUv = uv;
      vec4 mvPosition = modelViewMatrix * vec4(position, 1.0);
      vViewPos = mvPosition.xyz;
      gl_Position = projectionMatrix * mvPosition;
    }`
  const orbitFragmentShader = `
    uniform vec3 uColor; uniform float uOpacity; uniform float uTime; uniform float uDir;
    uniform float uSatCount; uniform float uSatAngles[8];
    uniform float uWakeStrength; uniform float uWakeFalloff;
    uniform vec3 uBulbView; uniform vec2 uMaskRadius;
    varying vec3 vViewPos; varying vec2 vUv;
    const float TWO_PI = 6.28318530718;
    void main() {
      float depthFactor = smoothstep(-5.9, -3.4, vViewPos.z);
      float alpha = uOpacity * mix(0.3, 1.0, depthFactor);
      vec2 atBulbDepth = vViewPos.xy * (uBulbView.z / vViewPos.z);
      float m = length((atBulbDepth - uBulbView.xy) / uMaskRadius);
      float behind = smoothstep(0.3, -0.3, vViewPos.z - uBulbView.z);
      alpha *= mix(1.0, smoothstep(0.7, 1.12, m), behind);
      float theta = vUv.x * TWO_PI;
      float wake = 0.0;
      for (int i = 0; i < 8; i++) {
        if (float(i) >= uSatCount) break;
        float d = (uSatAngles[i] - theta) * uDir;
        d -= TWO_PI * floor(d / TWO_PI);
        wake += exp(-d * uWakeFalloff);
      }
      wake *= uWakeStrength;
      float flow = 1.0 + 0.15 * sin(vUv.x * 18.849556 - uTime * 0.7 * uDir);
      vec3 color = uColor * (0.85 + 0.45 * depthFactor) * flow * (1.0 + wake * 1.2);
      gl_FragColor = vec4(color, min(alpha * (1.0 + wake * 1.5), 1.0));
    }`

  // ---- 点云数据 ----
  const buf = await fetch(DATA_URL).then((r) => r.arrayBuffer())
  const f = new Float32Array(buf)
  if (f.length !== 240000) throw new Error('dengpao 数据长度异常: ' + f.length)

  // ---- 渲染器 / 场景 / 相机 ----
  const renderer = new WebGLRenderer({ canvas, antialias: true, alpha: true })
  renderer.setPixelRatio(Math.min(devicePixelRatio, 2))
  renderer.toneMapping = ACESFilmicToneMapping
  renderer.toneMappingExposure = 1.0
  renderer.setClearColor(0x000000, 0)

  const scene = new Scene()
  // fov 越小 = 灯泡+轨道整体越大(向原项目的大灯泡靠拢);外环 radius 1.32 在 fov 33 半高 ~1.36 内仍不裁切。
  const camera = new PerspectiveCamera(33, 1, 0.1, 100)
  camera.position.set(0, 0, 4.6)

  scene.add(new AmbientLight('#040d20', 1.5))
  const pointLight = new PointLight('#00f0ff', 1.5, 8)
  pointLight.position.set(0, 0.14, 1.2)
  scene.add(pointLight)

  const root = new Group()
  scene.add(root)

  // ---- 灯泡点云 ----
  const positions = new Float32Array(40000 * 3)
  const normals = new Float32Array(40000 * 3)
  const randoms = new Float32Array(40000)
  let minY = Infinity, maxY = -Infinity
  for (let i = 0; i < 40000; i++) {
    const y = f[i * 6 + 1]
    positions[i * 3] = f[i * 6]; positions[i * 3 + 1] = y; positions[i * 3 + 2] = f[i * 6 + 2]
    normals[i * 3] = f[i * 6 + 3]; normals[i * 3 + 1] = f[i * 6 + 4]; normals[i * 3 + 2] = f[i * 6 + 5]
    randoms[i] = Math.random()
    if (y < minY) minY = y
    if (y > maxY) maxY = y
  }
  const geo = new BufferGeometry()
  geo.setAttribute('position', new BufferAttribute(positions, 3))
  geo.setAttribute('aNormal', new BufferAttribute(normals, 3))
  geo.setAttribute('aRandom', new BufferAttribute(randoms, 1))
  const particleMaterial = new ShaderMaterial({
    vertexShader: bulbVertexShader, fragmentShader: bulbFragmentShader,
    uniforms: {
      uTime: { value: 0.0 }, uSize: { value: 0.055 }, uEdgeColor: { value: new Vector3(0.0, 0.65, 1.0) },
      uMouseOrigin: { value: new Vector3(0, 0, 99) }, uMouseDir: { value: new Vector3(0, 0, -1) },
      uRepelStrength: { value: 0.0 }, uRepelRadius: { value: 0.26 }, uRepelPower: { value: 0.16 },
    },
    transparent: true, depthWrite: false, blending: AdditiveBlending,
  })
  const bulbPoints = new Points(geo, particleMaterial)
  root.add(bulbPoints)

  // ---- 玻璃壳 + 辉光 ----
  const glassMat = new MeshPhongMaterial({
    color: 0x8be5ff, transparent: true, opacity: 0.05, shininess: 120, specular: 0xffffff, side: DoubleSide, depthWrite: false,
  })
  const dome = new Mesh(new SphereGeometry(0.36, 40, 40), glassMat); dome.position.y = 0.14; root.add(dome)
  const neck = new Mesh(new CylinderGeometry(0.36, 0.2, 0.32, 32, 1, true), glassMat); neck.position.y = -0.16; root.add(neck)
  const glowSprite = new Sprite(new SpriteMaterial({
    map: makeGlowTexture('#00f0ff'), transparent: true, opacity: 0.4, blending: AdditiveBlending, depthWrite: false,
  }))
  glowSprite.scale.set(0.58, 0.58, 1.0); glowSprite.position.y = 0.14; root.add(glowSprite)

  // 灯泡垂直居中(其视觉中线 ≈ y0,轨道将绕此展开)。相机用 yun 取景(fov 45)给轨道留空间。
  const top = Math.max(maxY, 0.5), bottom = Math.min(minY, -0.5)
  root.scale.setScalar(BULB_SCALE)
  root.position.set(SHIFT_X, (-(top + bottom) / 2) * BULB_SCALE, 0)

  // ---- 两条 3D 管环轨道(灯泡居中于世界原点,轨道绕原点展开)----
  const orbitMaterials: any[] = []
  const orbitGroups: any[] = []
  const flows: any[] = [] // 沿轨道飞驰的光点(第一版 .fp 的 3D 版)
  class OrbitCurve extends Curve<Vector3> {
    radius: number
    constructor(radius: number) { super(); this.radius = radius }
    getPoint(t: number, target = new Vector3()) {
      const theta = t * Math.PI * 2
      return target.set(this.radius * Math.cos(theta), 0, this.radius * Math.sin(theta))
    }
  }
  function createOrbit(cfg: any) {
    const group = new Group()
    group.rotation.set(cfg.tilt[0], cfg.tilt[1], cfg.tilt[2])
    group.position.x = SHIFT_X // 与灯泡同步左移
    scene.add(group)
    const makeLayer = (radiusScale: number, opacity: number) => {
      const geometry = new TubeGeometry(new OrbitCurve(cfg.radius), 256, cfg.tubeRadius * radiusScale, 6, true)
      const material = new ShaderMaterial({
        vertexShader: orbitVertexShader, fragmentShader: orbitFragmentShader,
        uniforms: {
          uColor: { value: new Color(cfg.color) }, uOpacity: { value: opacity },
          uTime: { value: 0 }, uDir: { value: Math.sign(cfg.speed) || 1 },
          uSatCount: { value: cfg.keys.length }, uSatAngles: { value: new Array(8).fill(0) },
          uWakeStrength: { value: cfg.wakeStrength }, uWakeFalloff: { value: cfg.wakeFalloff },
          uBulbView: { value: new Vector3() }, uMaskRadius: { value: new Vector2(0.48 * BULB_SCALE, 0.7 * BULB_SCALE) },
        },
        transparent: true, depthWrite: false, blending: AdditiveBlending,
      })
      orbitMaterials.push({ material, cfg })
      const tube = new Mesh(geometry, material)
      tube.raycast = () => {} // 布景,永不作为命中目标
      group.add(tube)
    }
    makeLayer(1.0, cfg.opacity) // 细核心线
    makeLayer(3.2, cfg.opacity * 0.22) // 细柔光
    // 沿轨道不断飞驰的光点(抄第一版:细线里运动的亮点),比图标快数倍;作为 group 子对象继承倾斜/进动/左移
    // 颜色取该环颜色(而非白色,否则很突兀)
    const flowTex = makeGlowTexture(cfg.color)
    const FLOW_N = cfg.ring === 'inner' ? 5 : 7
    for (let j = 0; j < FLOW_N; j++) {
      const s = new Sprite(new SpriteMaterial({ map: flowTex, transparent: true, opacity: 0.9, blending: AdditiveBlending, depthWrite: false }))
      s.scale.set(0.02, 0.02, 1); s.raycast = () => {}
      group.add(s)
      flows.push({ s, radius: cfg.radius, phase: (j / FLOW_N) * Math.PI * 2, speed: cfg.speed * 3.4 })
    }
    return group
  }
  for (const cfg of ORBITS) orbitGroups.push({ group: createOrbit(cfg), cfg })
  const _bulbView = new Vector3()

  // ---- 卫星 = DOM 芯片(用户原版样式:深色圆盘 + 发光边框 + 慢速自转)。放弃全息点云(用户不满意)。
  //      位置由 3D 轨道投影驱动;前后遮挡靠 z-index 穿过透明画布(近=z7 压画布 / 远=z5 被灯泡挡)。----
  const stage = canvas.parentElement as HTMLElement // .hero-visual
  const chips: any[] = []
  const addedChips: HTMLElement[] = []
  let hoverKey: string | null = null
  orbitGroups.forEach(({ group, cfg }: any, b: number) => {
    const step = (Math.PI * 2) / cfg.keys.length
    cfg.keys.forEach((key: string, i: number) => {
      const el = document.createElement('div')
      el.className = 'chip ' + (b === 0 ? 'main' : 'alt')
      el.dataset.mid = key
      const spin = 12 + ((b * 5 + i * 7) % 9)
      const dir = (i + b) % 2 ? 'reverse' : 'normal'
      el.innerHTML = `<span class="shell"><span class="disc" style="--spin:${spin}s;--dir:${dir}">`
        + LOGOS[key].replaceAll('__id__', 'u' + b + '_' + i) + '</span></span>'
      el.style.left = '0'; el.style.top = '0'; el.style.margin = '0'; el.style.pointerEvents = 'auto'
      el.addEventListener('mouseenter', () => { hoverKey = key })
      el.addEventListener('mouseleave', () => { hoverKey = null })
      stage.appendChild(el); addedChips.push(el)
      chips.push({ el, group, baseAngle: i * step, speed: cfg.speed, radius: cfg.radius, zi: -1 })
    })
  })
  const _cw = new Vector3()

  // ---- 后处理 ----
  const composer = new EffectComposer(renderer)
  composer.addPass(new RenderPass(scene, camera))
  const bloom = new UnrealBloomPass(new Vector2(RENDER_H, RENDER_H), 0.45, 0.42, 0.9)
  composer.addPass(bloom)
  composer.addPass(new OutputPass())
  composer.addPass(new ShaderPass(AlphaFromLumaShader))

  document.body.classList.add('webgl3d')

  // ---- 尺寸:缓冲高固定 RENDER_H,宽随盒子宽高比;CSS 用 100% 拉伸(不失真因宽高比一致)----
  function layoutSize() {
    const boxEl = canvas.parentElement as HTMLElement | null
    const bw = Math.max(1, boxEl?.clientWidth || canvas.clientWidth || 1)
    const bh = Math.max(1, boxEl?.clientHeight || canvas.clientHeight || 1)
    const aspect = Math.min(2.2, Math.max(0.6, bw / bh))
    const H = RENDER_H, W = Math.round(H * aspect)
    renderer.setPixelRatio(Math.min(devicePixelRatio, 2))
    renderer.setSize(W, H, false)
    composer.setSize(W, H)
    camera.aspect = aspect
    camera.updateProjectionMatrix()
    if (PRM) renderTick(FROZEN_T, 0.016)
  }
  let ro: ResizeObserver | null = null
  if ('ResizeObserver' in window && canvas.parentElement) {
    ro = new ResizeObserver(() => layoutSize()); ro.observe(canvas.parentElement)
  }
  addEventListener('resize', layoutSize)

  // ---- 交互:鼠标斥力 + 点击浪涌 ----
  const mouseNDC = new Vector2(0, 0)
  let pointerInCanvas = false
  const cursorRayDir = new Vector3(0, 0, -1)
  let surgeT0 = -1e9
  const coreCur = { r: 0.0, g: 0.65, b: 1.0 }
  const coreTarget = { r: 0.0, g: 0.65, b: 1.0 }
  const onMouseMove = (e: MouseEvent) => {
    const r = canvas.getBoundingClientRect()
    mouseNDC.x = ((e.clientX - r.left) / r.width) * 2 - 1
    mouseNDC.y = -((e.clientY - r.top) / r.height) * 2 + 1
    pointerInCanvas = Math.abs(mouseNDC.x) < 1.05 && Math.abs(mouseNDC.y) < 1.05
  }
  const onMouseLeave = () => { pointerInCanvas = false }
  const onPointerDown = (e: PointerEvent) => {
    if (!(window as any).__scene3dActive) return
    const r = canvas.getBoundingClientRect()
    const nx = ((e.clientX - r.left) / r.width) * 2 - 1
    const ny = -((e.clientY - r.top) / r.height) * 2 + 1
    if (nx * nx + ny * ny < 0.5 * 0.5) surge()
  }
  function surge() { if (!PRM && (window as any).__scene3dActive) surgeT0 = performance.now() / 1000 }
  addEventListener('mousemove', onMouseMove)
  document.addEventListener('mouseleave', onMouseLeave)
  addEventListener('pointerdown', onPointerDown, true)

  function hexToRgb(hex: string) {
    const n = parseInt(hex.slice(1), 16)
    return { r: ((n >> 16) & 255) / 255, g: ((n >> 8) & 255) / 255, b: (n & 255) / 255 }
  }
  function setCoreColor(hex: string | null) {
    const c = hex ? hexToRgb(hex) : { r: 0.0, g: 0.65, b: 1.0 }
    coreTarget.r = c.r; coreTarget.g = c.g; coreTarget.b = c.b
    surge()
  }

  // ---- 悬停锁定 + 缓停 + HUD/染色/让位 ----
  const varsEl = (canvas.closest('.wd-landing-root') as HTMLElement) || document.documentElement
  const raycaster = new Raycaster()
  raycaster.params.Points.threshold = 0.02
  let orbitPhase = 0, speedFactor = 1
  let selected: string | null = null

  // HUD 文案走现有中文 i18n(key=英文原句;W5)。DOM 结构在 index.tsx 的 #hud。
  function setHud(m: any) {
    const set = (id: string, val: string) => { const el = document.getElementById(id); if (el) el.textContent = val }
    set('hud-prov', i18n.t(m.provider)); set('hud-name', i18n.t(m.name)); set('hud-desc', i18n.t(m.desc))
    set('hud-scene', m.scene ? i18n.t(m.scene) : ''); set('hud-tele', i18n.t(m.telemetry))
    const box = document.getElementById('hud'); if (box) (box as HTMLElement).style.borderLeftColor = m.color
    const dot = document.querySelector('#hud .hud-dot') as HTMLElement | null; if (dot) dot.style.background = m.color
  }
  function onSelectChange(key: string | null) {
    const hudBox = document.getElementById('hud')
    const heroRight = document.querySelector('.hero-right')
    if (key) {
      setHud(MODELS[key])
      hudBox?.classList.add('show'); heroRight?.classList.add('card-open')
      setCoreColor(MODELS[key].color)
      varsEl.style.setProperty('--wd-brand', MODELS[key].color)
      document.body.style.cursor = 'pointer'
    } else {
      hudBox?.classList.remove('show'); heroRight?.classList.remove('card-open')
      setCoreColor(null)
      varsEl.style.removeProperty('--wd-brand')
      document.body.style.cursor = ''
    }
  }

  // ---- 动画 ----
  let last = 0, heroVisible = true, rafId = 0
  const stageEl = document.querySelector('.hero-stage')
  let io: IntersectionObserver | null = null
  if (stageEl && 'IntersectionObserver' in window) {
    io = new IntersectionObserver((es) => { heroVisible = es[0].isIntersecting }, { threshold: 0 })
    io.observe(stageEl)
  }
  function renderTick(t: number, dt: number) {
    const u = particleMaterial.uniforms
    u.uTime.value = t
    if (!PRM) {
      if (pointerInCanvas) cursorRayDir.set(mouseNDC.x, mouseNDC.y, 0.5).unproject(camera).sub(camera.position).normalize()
      u.uMouseOrigin.value.copy(camera.position)
      u.uMouseDir.value.lerp(cursorRayDir, 1 - Math.exp(-9 * dt)).normalize()
      const target = pointerInCanvas ? 1 : 0
      const ease = target > u.uRepelStrength.value ? 5 : 2.2
      u.uRepelStrength.value += (target - u.uRepelStrength.value) * (1 - Math.exp(-ease * dt))
    }
    const kc = 1 - Math.exp(-6 * dt)
    coreCur.r += (coreTarget.r - coreCur.r) * kc
    coreCur.g += (coreTarget.g - coreCur.g) * kc
    coreCur.b += (coreTarget.b - coreCur.b) * kc
    u.uEdgeColor.value.set(coreCur.r, coreCur.g, coreCur.b)
    pointLight.color.setRGB(coreCur.r, coreCur.g, coreCur.b)
    root.rotation.y = t * 0.02
    const k = (t - surgeT0) / 0.7
    const env = (k >= 0 && k <= 1) ? Math.sin(Math.PI * k) : 0
    glowSprite.material.opacity = 0.4 + 0.18 * env
    pointLight.intensity = 1.5 + 1.3 * env
    // 轨道相位累积时钟:speedFactor 缓动 1↔0 → 悬停时整轨平滑冻结、移开平滑恢复,不跳帧。
    orbitPhase += dt * speedFactor
    // 进动摆:两环反相小幅左右摆(内 ±4° / 外 ±6°,周期 18s);随 orbitPhase 一起冻结(停轨时也停摆)。
    for (const { group, cfg } of orbitGroups) {
      const wob = (cfg.wobbleDeg * Math.PI / 180) * Math.sin((2 * Math.PI * orbitPhase) / cfg.wobblePeriod + cfg.wobblePhase)
      group.rotation.set(cfg.tilt[0], cfg.tilt[1] + wob, cfg.tilt[2])
    }
    // 轨道着色器:能量流 + 灯泡视空间中心(剪影遮罩)+ 卫星角度(彗尾)。角度都用 orbitPhase,与卫星循环一致。
    camera.updateMatrixWorld()
    _bulbView.set(SHIFT_X, 0, 0).applyMatrix4(camera.matrixWorldInverse)
    for (const { material, cfg } of orbitMaterials) {
      material.uniforms.uTime.value = orbitPhase
      material.uniforms.uBulbView.value.copy(_bulbView)
      const step = (Math.PI * 2) / cfg.keys.length
      const arr = material.uniforms.uSatAngles.value
      for (let i = 0; i < cfg.keys.length; i++) arr[i] = (i * step + cfg.speed * orbitPhase) % (Math.PI * 2)
    }
    // 光点沿轨道飞驰 + 远近淡化/缩放(随 orbitPhase,停轨时也停)
    for (const fl of flows) {
      const a = fl.phase + fl.speed * orbitPhase
      fl.s.position.set(fl.radius * Math.cos(a), 0, fl.radius * Math.sin(a))
      fl.s.getWorldPosition(_cw)
      const d = Math.min(1, Math.max(0, (_cw.z + fl.radius) / (2 * fl.radius)))
      fl.s.material.opacity = 0.25 + 0.75 * d
      const sc = 0.011 + 0.013 * d // 小而清脆的光点(参考原版,别做成大光斑)
      fl.s.scale.set(sc, sc, 1)
    }
    composer.render()

    // 芯片定位:投影 3D 轨道位置到屏幕 + z-index 前后遮挡(渲染后 matrixWorld 含本帧进动摆)
    const cw = canvas.clientWidth || 1, ch = canvas.clientHeight || 1
    for (const c of chips) {
      const angle = c.baseAngle + c.speed * orbitPhase
      _cw.set(c.radius * Math.cos(angle), Math.sin(orbitPhase * 1.5 + c.baseAngle) * 0.03, c.radius * Math.sin(angle))
      c.group.localToWorld(_cw)
      const worldZ = _cw.z
      _cw.project(camera)
      const px = (_cw.x * 0.5 + 0.5) * cw, py = (-_cw.y * 0.5 + 0.5) * ch
      const d = Math.min(1, Math.max(0, (worldZ + c.radius) / (2 * c.radius)))
      c.el.style.transform = `translate(${px.toFixed(1)}px,${py.toFixed(1)}px) translate(-50%,-50%) scale(${(0.62 + 0.5 * d).toFixed(3)})`
      c.el.style.opacity = (0.35 + 0.65 * d).toFixed(3)
      const zi = worldZ > 0 ? 7 : 5
      if (zi !== c.zi) { c.zi = zi; c.el.style.zIndex = String(zi) }
    }

    // 悬停(DOM 芯片 mouseenter 设 hoverKey)→ 吸附锁定 → 缓停/恢复
    const prevSel = selected
    selected = nextSelection(selected, hoverKey, pointerInCanvas)
    if (selected !== prevSel) onSelectChange(selected)
    speedFactor = approach(speedFactor, selected ? 0 : 1, 3, dt)
  }
  function loop(nowMs: number) {
    rafId = requestAnimationFrame(loop)
    if (!heroVisible) return
    const t = nowMs / 1000
    renderTick(t, Math.min(Math.max(t - last, 0.001), 0.05))
    last = t
  }

  ;(window as any).__scene3dActive = true
  ;(window as any).__scene3d = {
    surge, setCoreColor,
    get points() { return bulbPoints.geometry.attributes.position.count },
    get selected() { return selected },
  }

  layoutSize()
  if (PRM) renderTick(FROZEN_T, 0.016)
  else rafId = requestAnimationFrame(loop)

  return () => {
    cancelAnimationFrame(rafId)
    removeEventListener('mousemove', onMouseMove)
    document.removeEventListener('mouseleave', onMouseLeave)
    removeEventListener('pointerdown', onPointerDown, true)
    removeEventListener('resize', layoutSize)
    ro?.disconnect(); io?.disconnect()
    addedChips.forEach((e) => e.remove())
    try { renderer.dispose() } catch { /* noop */ }
    document.body.classList.remove('webgl3d')
    delete (window as any).__scene3d
    ;(window as any).__scene3dActive = false
  }
}
