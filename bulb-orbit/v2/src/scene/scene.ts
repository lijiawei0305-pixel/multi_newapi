import * as THREE from 'three'
import { OrbitControls } from 'three/examples/jsm/controls/OrbitControls.js'
import { EffectComposer } from 'three/examples/jsm/postprocessing/EffectComposer.js'
import { RenderPass } from 'three/examples/jsm/postprocessing/RenderPass.js'
import { UnrealBloomPass } from 'three/examples/jsm/postprocessing/UnrealBloomPass.js'
import { OutputPass } from 'three/examples/jsm/postprocessing/OutputPass.js'

import { createBulb, DEFAULT_PARTICLE_COUNT } from './bulb'
import { createOrbits } from './orbits'
import { createSatellites, updateSatellites } from './satellites'
import { CAMERA_Z, VIEW_OFFSET_X_RATIO } from './config'
import { viewOffset } from './orbit-math'
import { makeRaycaster } from '../interactions/raycast'
import { resolveHover, coreColorFor } from '../interactions/logic'
import { renderHud, hudModelFor, type HudEls } from '../ui/hud'

// ==========================================
// 场景装配 —— 移植自 yun src/main.js 的 init()/setupPostProcessing()/setupInteractions()/
// checkSatelliteHover()/animate()/onWindowResize()，用 Task 9/10 的 bulb/orbits/satellites 模块
// 与 Task 4/5 的 hud/logic 模块替换 yun 的内联逻辑。这是把各模块接成最终可演示页面的组装任务，
// 本身不新增粒子/轨道/HUD 渲染逻辑——只负责 scene/camera/renderer/composer/controls 的搭建、
// 相机取景偏移、动画循环调度顺序，以及 hover/click 的事件接线。
// ==========================================

export interface SceneHandle {
  onResize(): void
}

// Task 12：移动端性能降级——两个字段都可选，省略时保持 Task 11 桌面行为不变（40000 粒子、bloom
// 开）。opts 整体也可选（mountScene(canvas) 单参调用与 Task 11 时期签名保持源码兼容）。
export interface SceneOptions {
  particleCount?: number
  bloomEnabled?: boolean
}

export async function mountScene(canvas: HTMLCanvasElement, opts: SceneOptions = {}): Promise<SceneHandle> {
  const { particleCount = DEFAULT_PARTICLE_COUNT, bloomEnabled = true } = opts
  const hudEls = getHudEls()

  const scene = new THREE.Scene()
  // 与 yun 一致：远处稀薄雾效，雾色=页面背景色（--bg #01040a），让远景标准材质（玻璃罩/金属环/
  // 图标平面）随深度自然融入深空背景——自定义 ShaderMaterial（粒子灯泡/轨道线）不读场景雾，
  // 它们各自的深度渐隐已经在着色器里手写实现（见 orbits.ts 顶部注释），两者互不冲突、覆盖不同图层。
  scene.fog = new THREE.FogExp2('#01040a', 0.22)

  const camera = new THREE.PerspectiveCamera(45, window.innerWidth / window.innerHeight, 0.1, 100)
  camera.position.set(0, 0, CAMERA_Z)

  const renderer = new THREE.WebGLRenderer({ canvas, antialias: true, alpha: true })
  renderer.setSize(window.innerWidth, window.innerHeight)
  renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2))
  // ACES 色调映射是硬性要求：Task 7 的 24 张品牌 logo PNG 是按「最终经 ACES + OutputPass」这条管线
  // 反向预补偿过的（inverse-ACES bake），少了这一步品牌色会渲染错误。
  renderer.toneMapping = THREE.ACESFilmicToneMapping
  renderer.toneMappingExposure = 1.0

  const controls = new OrbitControls(camera, renderer.domElement)
  controls.enableDamping = true
  controls.dampingFactor = 0.05
  controls.enableZoom = false
  controls.minPolarAngle = Math.PI / 2.3
  controls.maxPolarAngle = Math.PI / 1.7

  // 灯泡自身的 point light 内置在 bulb.group 里（供 setCoreColor 调色）；这里只补一盏与 yun 一致的
  // 暗蓝环境光做基础补光。
  const ambientLight = new THREE.AmbientLight('#040d20', 1.5)
  scene.add(ambientLight)

  const bulb = await createBulb(particleCount)
  scene.add(bulb.group)

  const rig = createOrbits()
  scene.add(...rig.groups)
  const satellites = await createSatellites(rig)

  // composer 管线固定是 RenderPass → [UnrealBloomPass] → OutputPass：OutputPass 是应用 ACES 色调
  // 映射的那一步，Task 7 的 24 张品牌 logo PNG 是按「最终经 ACES + OutputPass」这条管线反向预补偿过
  // 的（inverse-ACES bake）。移动端（bloomEnabled=false）只跳过中间的 UnrealBloomPass 这一条 pass
  // 省 GPU，RenderPass/OutputPass 与下面 animate() 里的 composer.render() 调用完全不变——不能整体
  // 退回 renderer.render()、也不能连带丢掉 OutputPass，否则 24 个品牌色会因缺一次 ACES 映射而渲染
  // 错误（相当于被反向补偿了却没有正向映射抵消回来）。
  const composer = new EffectComposer(renderer)
  composer.addPass(new RenderPass(scene, camera))
  if (bloomEnabled) {
    composer.addPass(new UnrealBloomPass(new THREE.Vector2(window.innerWidth, window.innerHeight), 0.6, 0.45, 0.85))
  }
  composer.addPass(new OutputPass())

  // 相机取景偏移：把灯泡视觉中心从屏幕正中推到 x≈60%（落在 index.html 的 hero-stage 中栏），
  // 给左侧 40% 的品牌文案留白。setViewOffset 内部已经会自己设 aspect + 调 updateProjectionMatrix，
  // 调完不用再手动 set aspect。
  // 符号经截图实测标定：width=fullW 的子矩形从 [offsetX, offsetX+fullW] 取景，正的 offsetX 会把
  // 世界原点（灯泡中心）推向屏幕左侧而非右侧——实测 offsetX=+10%·width 时灯泡中心落在 x≈39.7%
  // （比正中 50% 还偏左约 10 个百分点，与「正偏移量」符号相反）。取负号后灯泡中心落在 x≈60.3%，
  // 与目标一致，故此处对 viewOffset() 的结果取负。
  function applyViewOffset(): void {
    const fullW = window.innerWidth
    const fullH = window.innerHeight
    const offsetX = -viewOffset(fullW, VIEW_OFFSET_X_RATIO)
    camera.setViewOffset(fullW, fullH, offsetX, 0, fullW, fullH)
  }
  applyViewOffset()

  // ---- 交互接线（移植自 yun setupInteractions/checkSatelliteHover/triggerCoreSurge）----

  const mouse = new THREE.Vector2()
  let hasPointer = false
  const ray = makeRaycaster(camera, satellites)
  let prevHoverId: string | null = null

  window.addEventListener('mousemove', (event: MouseEvent) => {
    mouse.x = (event.clientX / window.innerWidth) * 2 - 1
    mouse.y = -(event.clientY / window.innerHeight) * 2 + 1
    hasPointer = true
  })

  // 光标离开整个页面：停掉逐帧悬停检测（避免用陈旧坐标继续判定命中），并把 HUD 立即收回默认态——
  // 否则一旦最后一次悬停在某颗卫星上时光标离页，HUD 会永远停在那张卡片上，直到光标重新进入页面。
  document.addEventListener('mouseleave', () => {
    hasPointer = false
    if (prevHoverId !== null) {
      renderHud(hudEls, hudModelFor(null))
      prevHoverId = null
    }
  })

  // 直接绑定在 canvas 自身而非 window：index.html 里 .hero 整体 pointer-events:none，只有
  // .hero-left/.hero-right 子树重新打开 pointer-events:auto，所以落在文案/HUD 卡片上的点击根本不会
  // 传到 canvas；无需再像 yun 那样额外判断 event.target.id === 'webgl-canvas'。
  canvas.addEventListener('click', () => {
    const id = ray.click(mouse)
    const color = coreColorFor(id)
    bulb.surge(color)
    bulb.setCoreColor(color)
    document.documentElement.style.setProperty('--cyan', color)
  })

  // ---- 动画循环 ----

  const clock = new THREE.Clock()

  function animate(): void {
    requestAnimationFrame(animate)
    const t = clock.getElapsedTime()

    // controls.update() 先跑（阻尼可能改变本帧相机位姿），随即显式刷新 matrixWorld——bulb.update
    // 里的鼠标射线 unproject 与 rig.update 里的 uBulbView 都要读本帧最新的相机世界矩阵，不能拖到
    // composer.render() 内部才被动更新（那样会晚一帧）。
    controls.update()
    camera.updateMatrixWorld()

    bulb.update(t, mouse, hasPointer, camera)
    // rig.update 写入本帧轨道进动 rotation.y；updateSatellites 的 billboard 用 getWorldQuaternion
    // 读父级（轨道组）世界旋转来抵消，必须在 rig.update 之后调用，否则贴图朝向滞后一帧（见 Task 10
    // report）。
    rig.update(t, camera)
    updateSatellites(satellites, t, camera)

    composer.render()

    // 卫星持续沿轨运动，即使鼠标静止也要每帧重新命中测试；放在 render 之后，保证 raycast 用的是
    // 本帧刚更新过的世界矩阵。未进入页面前(hasPointer=false)跳过——否则 mouse 默认值 NDC (0,0)
    // 会被当成"鼠标停在屏幕正中"参与命中测试。
    if (hasPointer) {
      const hitId = ray.hover(mouse)
      const { modelId, changed } = resolveHover(prevHoverId, hitId)
      if (changed) {
        renderHud(hudEls, hudModelFor(modelId))
        prevHoverId = modelId
      }
    }
  }
  animate()

  function onResize(): void {
    const fullW = window.innerWidth
    const fullH = window.innerHeight
    renderer.setSize(fullW, fullH)
    renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2))
    composer.setSize(fullW, fullH)
    applyViewOffset()
  }

  return { onResize }
}

function getHudEls(): HudEls {
  const name = document.getElementById('hud-name')
  const provider = document.getElementById('hud-provider')
  const desc = document.getElementById('hud-desc')
  const telemetry = document.getElementById('hud-telemetry')
  const panel = document.getElementById('hud-panel')
  if (!name || !provider || !desc || !telemetry || !panel) {
    throw new Error('HUD DOM elements not found (#hud-name/#hud-provider/#hud-desc/#hud-telemetry/#hud-panel)')
  }
  return { name, provider, desc, telemetry, panel }
}
