// @ts-nocheck
/* WeDream 灯泡 3D 粒子 —— 移植自 bulb-orbit/index.html 的 BULB3D-CODE（源 yun/src/main.js，参数原样）。
   与静态版差异仅两点：three 改用 npm 包（named import 取代内联 vendor）；点云改 fetch 二进制（取代内联 base64）。
   导出 initBulb3d(canvas) → Promise<cleanup>，供 React useEffect 调用/卸载。 */
import {
  ACESFilmicToneMapping, AdditiveBlending, AmbientLight, BufferAttribute, BufferGeometry,
  CanvasTexture, CylinderGeometry, DoubleSide, Group, Mesh, MeshPhongMaterial, PerspectiveCamera,
  PointLight, Points, Scene, ShaderMaterial, SphereGeometry, Sprite, SpriteMaterial, Vector2,
  Vector3, WebGLRenderer,
} from 'three'
import { EffectComposer } from 'three/examples/jsm/postprocessing/EffectComposer.js'
import { OutputPass } from 'three/examples/jsm/postprocessing/OutputPass.js'
import { RenderPass } from 'three/examples/jsm/postprocessing/RenderPass.js'
import { ShaderPass } from 'three/examples/jsm/postprocessing/ShaderPass.js'
import { UnrealBloomPass } from 'three/examples/jsm/postprocessing/UnrealBloomPass.js'

const DATA_URL = '/lp-assets/dengpao_points.bin'

export async function initBulb3d(canvas3d: HTMLCanvasElement): Promise<() => void> {
  const PRM = matchMedia('(prefers-reduced-motion: reduce)').matches
  const FROZEN_T = 12.0

  /* 内部渲染分辨率固定，不随画布 CSS 尺寸(--b3d)变化 —— 显示大小交给 CSS 缩放。
     为什么：UnrealBloomPass 的高斯核以「每级 mip 的像素数」为单位(3/5/7/9/11 texel)，
     而 mip 尺寸随画布分辨率缩放，于是辉光的「归一化扩散范围 ∝ 1/分辨率」。窄窗口下
     --b3d 会缩到 ~486px（全屏约 940px），辉光扩散占比从 ~37% 暴涨到 ~72%，糊满整块画布
     并在画布矩形边界被硬切 → 灯泡周围出现过曝方块。（原版 index.html 同样有此问题。）
     固定分辨率后，任意窗口尺寸的渲染结果都与全屏时一致。940 取自全屏时的 --b3d 量级，
     保证既有观感零变化。 */
  const RENDER_PX = 940

  const bulbVertexShader = `
    uniform float uTime;
    uniform float uSize;
    uniform vec3 uEdgeColor;
    uniform vec3 uMouseOrigin;
    uniform vec3 uMouseDir;
    uniform float uRepelStrength;
    uniform float uRepelRadius;
    uniform float uRepelPower;
    attribute vec3 aNormal;
    attribute float aRandom;
    varying vec3 vColor;
    void main() {
      float breathingOffset = sin(uTime * 1.1 + aRandom * 6.28318) * 0.006;
      vec3 noiseOffset = vec3(
        sin(uTime * 0.5 + aRandom * 25.0),
        cos(uTime * 0.7 + aRandom * 30.0),
        sin(uTime * 0.9 + aRandom * 35.0)
      ) * 0.002;
      vec3 finalPos = position + aNormal * breathingOffset + noiseOffset;
      float breath = 1.0 + sin(uTime * 0.7) * 0.012;
      finalPos *= breath;
      vec3 worldPos = (modelMatrix * vec4(finalPos, 1.0)).xyz;
      vec3 toParticle = worldPos - uMouseOrigin;
      vec3 closest = uMouseOrigin + uMouseDir * dot(toParticle, uMouseDir);
      vec3 away = worldPos - closest;
      float rayDist = length(away);
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
    }
  `
  const bulbFragmentShader = `
    varying vec3 vColor;
    void main() {
      float dist = length(gl_PointCoord - vec2(0.5));
      if (dist > 0.5) discard;
      float alpha = smoothstep(0.5, 0.06, dist);
      float core = smoothstep(0.12, 0.0, dist) * 0.6;
      vec3 finalColor = mix(vColor, vec3(1.0), core * 0.4);
      gl_FragColor = vec4(finalColor, (alpha * 0.45 + core * 0.15) * 0.75);
    }
  `
  const AlphaFromLumaShader = {
    uniforms: { tDiffuse: { value: null } },
    vertexShader: `varying vec2 vUv;
      void main() { vUv = uv; gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0); }`,
    fragmentShader: `uniform sampler2D tDiffuse; varying vec2 vUv;
      void main() {
        vec4 c = texture2D(tDiffuse, vUv);
        gl_FragColor = vec4(c.rgb, clamp(max(c.r, max(c.g, c.b)), 0.0, 1.0));
      }`,
  }

  function createGlowTexture(colorHex: string) {
    const c = document.createElement('canvas')
    c.width = 64; c.height = 64
    const ctx = c.getContext('2d')!
    const grad = ctx.createRadialGradient(32, 32, 0, 32, 32, 32)
    grad.addColorStop(0, colorHex)
    grad.addColorStop(0.2, colorHex)
    grad.addColorStop(1, 'rgba(0, 0, 0, 0)')
    ctx.fillStyle = grad
    ctx.fillRect(0, 0, 64, 64)
    return new CanvasTexture(c)
  }

  // 点云：fetch 二进制（取代 decodeDengpao 的内联 base64）
  const buf = await fetch(DATA_URL).then((r) => r.arrayBuffer())
  const f = new Float32Array(buf)
  if (f.length !== 240000) throw new Error('dengpao 数据长度异常: ' + f.length)

  let renderer: any, scene: any, camera: any, composer: any, root: any
  let particleMaterial: any, glowSprite: any, pointLight: any, bulbPoints: any
  let surgeT0 = -1e9
  const coreCur = { r: 0.0, g: 0.65, b: 1.0 }
  const coreTarget = { r: 0.0, g: 0.65, b: 1.0 }
  const mouseNDC = new Vector2(0, 0)
  let pointerInCanvas = false
  const cursorRayDir = new Vector3(0, 0, -1)

  renderer = new WebGLRenderer({ canvas: canvas3d, antialias: true, alpha: true })
  renderer.setPixelRatio(Math.min(devicePixelRatio, 2))
  renderer.toneMapping = ACESFilmicToneMapping
  renderer.toneMappingExposure = 1.0
  renderer.setClearColor(0x000000, 0)

  scene = new Scene()
  camera = new PerspectiveCamera(45, 1, 0.1, 100)
  camera.position.set(0, 0, 4.6)

  scene.add(new AmbientLight('#040d20', 1.5))
  pointLight = new PointLight('#00f0ff', 1.5, 8)
  pointLight.position.set(0, 0.14, 1.2)
  scene.add(pointLight)

  root = new Group()
  scene.add(root)

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
  particleMaterial = new ShaderMaterial({
    vertexShader: bulbVertexShader,
    fragmentShader: bulbFragmentShader,
    uniforms: {
      uTime: { value: 0.0 },
      uSize: { value: 0.055 },
      uEdgeColor: { value: new Vector3(0.0, 0.65, 1.0) },
      uMouseOrigin: { value: new Vector3(0, 0, 99) },
      uMouseDir: { value: new Vector3(0, 0, -1) },
      uRepelStrength: { value: 0.0 },
      uRepelRadius: { value: 0.26 },
      uRepelPower: { value: 0.16 },
    },
    transparent: true,
    depthWrite: false,
    blending: AdditiveBlending,
  })
  bulbPoints = new Points(geo, particleMaterial)
  root.add(bulbPoints)

  const glassMat = new MeshPhongMaterial({
    color: 0x8be5ff, transparent: true, opacity: 0.05, shininess: 120,
    specular: 0xffffff, side: DoubleSide, depthWrite: false,
  })
  const dome = new Mesh(new SphereGeometry(0.36, 40, 40), glassMat)
  dome.position.y = 0.14
  root.add(dome)
  const neck = new Mesh(new CylinderGeometry(0.36, 0.20, 0.32, 32, 1, true), glassMat)
  neck.position.y = -0.16
  root.add(neck)
  glowSprite = new Sprite(new SpriteMaterial({
    map: createGlowTexture('#00f0ff'), transparent: true, opacity: 0.36, blending: AdditiveBlending, depthWrite: false,
  }))
  glowSprite.scale.set(0.65, 0.65, 1.0)
  glowSprite.position.y = 0.14
  root.add(glowSprite)

  const top = Math.max(maxY, 0.5), bottom = Math.min(minY, -0.5)
  root.position.y = -(top + bottom) / 2
  camera.fov = 2 * Math.atan((top - bottom) * 1.7 / 2 / 4.6) * 180 / Math.PI
  camera.updateProjectionMatrix()

  composer = new EffectComposer(renderer)
  composer.addPass(new RenderPass(scene, camera))
  composer.addPass(new UnrealBloomPass(new Vector2(RENDER_PX, RENDER_PX), 0.6, 0.45, 0.85))
  composer.addPass(new OutputPass())
  composer.addPass(new ShaderPass(AlphaFromLumaShader))

  document.body.classList.add('webgl3d')

  function onResize3D() {
    // 只在 dpr 变化（拖到不同 DPI 显示器）时才需要重设；尺寸恒为 RENDER_PX，见上方注释。
    renderer.setPixelRatio(Math.min(devicePixelRatio, 2))
    renderer.setSize(RENDER_PX, RENDER_PX, false) // updateStyle=false：不覆盖 CSS 的 --b3d
    composer.setSize(RENDER_PX, RENDER_PX)
    if (PRM) renderTick(FROZEN_T, 0.016)
  }

  let last3d = 0, heroVisible = true, rafId = 0
  const stageEl = document.querySelector('.hero-stage')
  let io: IntersectionObserver | null = null
  if (stageEl && 'IntersectionObserver' in window) {
    io = new IntersectionObserver((es) => { heroVisible = es[0].isIntersecting }, { threshold: 0 })
    io.observe(stageEl)
  }
  function loop3d(nowMs: number) {
    rafId = requestAnimationFrame(loop3d)
    if (!heroVisible) return
    const t = nowMs / 1000
    renderTick(t, Math.min(Math.max(t - last3d, 0.001), 0.05))
    last3d = t
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
    {
      const kc = 1 - Math.exp(-6 * dt)
      coreCur.r += (coreTarget.r - coreCur.r) * kc
      coreCur.g += (coreTarget.g - coreCur.g) * kc
      coreCur.b += (coreTarget.b - coreCur.b) * kc
      u.uEdgeColor.value.set(coreCur.r, coreCur.g, coreCur.b)
      pointLight.color.setRGB(coreCur.r, coreCur.g, coreCur.b)
    }
    root.rotation.y = t * 0.02
    const k = (t - surgeT0) / 0.7
    const env = (k >= 0 && k <= 1) ? Math.sin(Math.PI * k) : 0
    glowSprite.material.opacity = 0.36 + 0.27 * env
    pointLight.intensity = 1.5 + 1.3 * env
    composer.render()
  }

  const onMouseMove = (e: MouseEvent) => {
    const r = canvas3d.getBoundingClientRect()
    mouseNDC.x = ((e.clientX - r.left) / r.width) * 2 - 1
    mouseNDC.y = -((e.clientY - r.top) / r.height) * 2 + 1
    pointerInCanvas = Math.abs(mouseNDC.x) < 1.15 && Math.abs(mouseNDC.y) < 1.15
  }
  const onMouseLeave = () => { pointerInCanvas = false }
  const onPointerDown = (e: PointerEvent) => {
    if (!(window as any).__bulb3dActive) return
    const r = canvas3d.getBoundingClientRect()
    const nx = ((e.clientX - r.left) / r.width) * 2 - 1
    const ny = -((e.clientY - r.top) / r.height) * 2 + 1
    if (nx * nx + ny * ny < 0.55 * 0.55) surge()
  }
  function surge() { if (!PRM && (window as any).__bulb3dActive) surgeT0 = performance.now() / 1000 }
  addEventListener('mousemove', onMouseMove)
  document.addEventListener('mouseleave', onMouseLeave)
  addEventListener('pointerdown', onPointerDown, true)
  addEventListener('resize', onResize3D)

  function hexToRgb(hex: string) {
    const n = parseInt(hex.slice(1), 16)
    return { r: ((n >> 16) & 255) / 255, g: ((n >> 8) & 255) / 255, b: (n & 255) / 255 }
  }
  function setCoreColor(hex: string | null) {
    const c = hex ? hexToRgb(hex) : { r: 0.0, g: 0.65, b: 1.0 }
    coreTarget.r = c.r; coreTarget.g = c.g; coreTarget.b = c.b
    surge()
  }
  ;(window as any).__bulb3dActive = true
  ;(window as any).__bulb3d = { surge, setCoreColor, get points() { return bulbPoints ? bulbPoints.geometry.attributes.position.count : 0 } }

  onResize3D()
  if (PRM) renderTick(FROZEN_T, 0.016)
  else rafId = requestAnimationFrame(loop3d)

  return () => {
    cancelAnimationFrame(rafId)
    removeEventListener('mousemove', onMouseMove)
    document.removeEventListener('mouseleave', onMouseLeave)
    removeEventListener('pointerdown', onPointerDown, true)
    removeEventListener('resize', onResize3D)
    io?.disconnect()
    try { renderer.dispose() } catch { /* noop */ }
    document.body.classList.remove('webgl3d')
    delete (window as any).__bulb3d
    ;(window as any).__bulb3dActive = false
  }
}
