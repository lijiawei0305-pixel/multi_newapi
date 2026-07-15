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
  CylinderGeometry, DoubleSide, Group, Mesh, MeshPhongMaterial, PerspectiveCamera,
  PointLight, Points, Scene, ShaderMaterial, SphereGeometry, Sprite, SpriteMaterial, Vector2,
  Vector3, WebGLRenderer,
} from 'three'
import { EffectComposer } from 'three/examples/jsm/postprocessing/EffectComposer.js'
import { OutputPass } from 'three/examples/jsm/postprocessing/OutputPass.js'
import { RenderPass } from 'three/examples/jsm/postprocessing/RenderPass.js'
import { ShaderPass } from 'three/examples/jsm/postprocessing/ShaderPass.js'
import { UnrealBloomPass } from 'three/examples/jsm/postprocessing/UnrealBloomPass.js'

import { makeGlowTexture } from './scene3d-assets'

const DATA_URL = '/lp-assets/dengpao_points.bin'
const RENDER_H = 940 // 渲染缓冲高度固定(bloom 归一化一致);宽 = 高 × 盒子宽高比

export async function initScene3d(canvas: HTMLCanvasElement): Promise<() => void> {
  const PRM = matchMedia('(prefers-reduced-motion: reduce)').matches
  const FROZEN_T = 12.0

  // ---- 灯泡粒子着色器(与 bulb3d.ts 原样一致,保观感不变)----
  const bulbVertexShader = `
    uniform float uTime; uniform float uSize; uniform vec3 uEdgeColor;
    uniform vec3 uMouseOrigin; uniform vec3 uMouseDir;
    uniform float uRepelStrength; uniform float uRepelRadius; uniform float uRepelPower;
    attribute vec3 aNormal; attribute float aRandom; varying vec3 vColor;
    void main() {
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
    varying vec3 vColor;
    void main() {
      float dist = length(gl_PointCoord - vec2(0.5));
      if (dist > 0.5) discard;
      float alpha = smoothstep(0.5, 0.06, dist);
      float core = smoothstep(0.12, 0.0, dist) * 0.6;
      vec3 finalColor = mix(vColor, vec3(1.0), core * 0.4);
      gl_FragColor = vec4(finalColor, (alpha * 0.45 + core * 0.15) * 0.75);
    }`
  // luma→alpha:让透明画布正确叠加在页面(黑底/极光)之上
  const AlphaFromLumaShader = {
    uniforms: { tDiffuse: { value: null } },
    vertexShader: `varying vec2 vUv;
      void main() { vUv = uv; gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0); }`,
    fragmentShader: `uniform sampler2D tDiffuse; varying vec2 vUv;
      void main() { vec4 c = texture2D(tDiffuse, vUv); gl_FragColor = vec4(c.rgb, clamp(max(c.r, max(c.g, c.b)), 0.0, 1.0)); }`,
  }

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
  const camera = new PerspectiveCamera(45, 1, 0.1, 100)
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
    map: makeGlowTexture('#00f0ff'), transparent: true, opacity: 0.36, blending: AdditiveBlending, depthWrite: false,
  }))
  glowSprite.scale.set(0.65, 0.65, 1.0); glowSprite.position.y = 0.14; root.add(glowSprite)

  // 灯泡垂直居中(其视觉中线 ≈ y0,轨道将绕此展开)。相机用 yun 取景(fov 45)给轨道留空间。
  const top = Math.max(maxY, 0.5), bottom = Math.min(minY, -0.5)
  root.position.y = -(top + bottom) / 2

  // ---- 后处理 ----
  const composer = new EffectComposer(renderer)
  composer.addPass(new RenderPass(scene, camera))
  const bloom = new UnrealBloomPass(new Vector2(RENDER_H, RENDER_H), 0.6, 0.45, 0.85)
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
    glowSprite.material.opacity = 0.36 + 0.27 * env
    pointLight.intensity = 1.5 + 1.3 * env
    composer.render()
  }
  function loop(nowMs: number) {
    rafId = requestAnimationFrame(loop)
    if (!heroVisible) return
    const t = nowMs / 1000
    renderTick(t, Math.min(Math.max(t - last, 0.001), 0.05))
    last = t
  }

  ;(window as any).__scene3dActive = true
  ;(window as any).__scene3d = { surge, setCoreColor, get points() { return bulbPoints.geometry.attributes.position.count } }

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
    try { renderer.dispose() } catch { /* noop */ }
    document.body.classList.remove('webgl3d')
    delete (window as any).__scene3d
    ;(window as any).__scene3dActive = false
  }
}
