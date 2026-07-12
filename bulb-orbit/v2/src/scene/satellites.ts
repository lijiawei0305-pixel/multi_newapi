import * as THREE from 'three'
import { BULB_SCALE, CAMERA_Z, ORBITS } from './config'
import { satelliteAngle, evenAngles } from './orbit-math'
import { orbitModels, type Model } from '../data/models'
import { type OrbitRig, ORBIT_ORDER, ORBIT_PHASE, SATS_PER_ORBIT } from './orbits'

// ==========================================
// 卫星 —— 移植自 yun src/main.js 的 createSatellite3D() + animate() 里卫星每帧逻辑。
// 移植范围：品牌色光晕 Sprite 背衬、细金属环、淡玻璃圆盘、品牌 PNG 图标 billboard；朝向摄像机的
// billboard 抵消父级（轨道组）世界旋转；近大远小的深度缩放/透明度。
// 简化（已批准，见 Task 简报）：跳过 yun 的全息点云光环层（逐 logo 采样点云 bin，我们没有全部 24
// 个）——改用品牌色光晕 Sprite 背衬本身充当"卫星大气层"，24 个 logo 统一走这条路径。
// 不含：悬停/点击 raycast、HUD 联动——那些不属于本模块契约（留给之后引入交互的任务）。
// ==========================================

export interface Satellite {
  group: THREE.Group
  modelId: string
}

// 每颗卫星运行期状态，用强类型 WeakMap（而不是 three.js 的 any 类型 userData 包）承载，updateSatellites()
// 只从这里读数据，避免任何 any 泄漏。group.userData 仍会写入 modelId/baseAngle/speed（见下方
// createSatelliteGroup），供外部调试/未来的 raycast 命中代码读取——但那是"仅写不读"的旁路，不参与
// 本模块自身的每帧计算。
interface SatelliteRuntime {
  baseAngle: number
  speed: number
  orbitRadius: number
  glowMaterial: THREE.SpriteMaterial
  rimMaterial: THREE.MeshStandardMaterial
  iconMaterial: THREE.MeshBasicMaterial | null
}

const runtimeState = new WeakMap<THREE.Group, SatelliteRuntime>()

// ---- 尺寸/透明度常量（逐字取自 yun createSatellite3D，世界空间绝对尺寸按 bulb.ts 已确立的惯例
// 乘 BULB_SCALE；透明度是无量纲 [0,1]，不缩放）----

const GLOW_SCALE = 0.55 * BULB_SCALE
const GLOW_Z_OFFSET = -0.06 * BULB_SCALE
const GLOW_BASE_OPACITY = 0.16

const RIM_OUTER_RADIUS = 0.128 * BULB_SCALE
const RIM_TUBE_RADIUS = 0.006 * BULB_SCALE
const RIM_BASE_OPACITY = 0.4

const DISC_RADIUS = 0.128 * BULB_SCALE
const DISC_HEIGHT = 0.012 * BULB_SCALE
const DISC_OPACITY = 0.06

const ICON_SIZE = 0.22 * BULB_SCALE
const ICON_Z_OFFSET = 0.02 * BULB_SCALE

const BOB_AMPLITUDE = 0.03 * BULB_SCALE // 轻微上下浮动的幅度

// 近大远小 / 深度渐隐的距离映射区间——与 orbits.ts 的 DEPTH_NEAR/DEPTH_FAR 同源推导（相机距离 ∓
// 覆盖两条轨道的较宽半径 domestic.radius），只是这里用的是真实欧氏距离（camera.position.distanceTo）
// 而不是 view-space z，两者在轨道大致朝向摄像机时数值接近，yun 原版也是各自独立标定出接近的一组数字。
const NEAR_DIST = CAMERA_Z - ORBITS.domestic.radius
const FAR_DIST = CAMERA_Z + ORBITS.domestic.radius

// ---- 品牌色光晕贴图：与 bulb.ts 的 createGlowSpriteTexture() 同一惯例——中性白色贴图，运行时颜色
// 一律走 SpriteMaterial.color 做 GPU 端 tint。这里 24 颗卫星共用同一张贴图（懒加载单例），而不是像
// yun 那样每个品牌色烘焙一张单独 canvas 贴图——同一约定，且更省内存/避免创建 24 张纹理。
// （bulb.ts 未导出该函数，故本文件保留一份小型独立实现；两处都只有一份 <20 行的 canvas 绘制逻辑，
// 重复成本可以接受。）
let sharedGlowTexture: THREE.CanvasTexture | null = null
function getGlowTexture(): THREE.CanvasTexture {
  if (sharedGlowTexture) return sharedGlowTexture

  const canvas = document.createElement('canvas')
  canvas.width = 64
  canvas.height = 64
  const ctx = canvas.getContext('2d')
  if (!ctx) throw new Error('2D canvas context unavailable')

  const grad = ctx.createRadialGradient(32, 32, 0, 32, 32, 32)
  grad.addColorStop(0, '#ffffff')
  grad.addColorStop(0.2, '#ffffff')
  grad.addColorStop(1, 'rgba(255, 255, 255, 0)')

  ctx.fillStyle = grad
  ctx.fillRect(0, 0, 64, 64)

  sharedGlowTexture = new THREE.CanvasTexture(canvas)
  return sharedGlowTexture
}

// ---- 单颗卫星三层视觉：品牌色光晕背衬 + 细金属环 + 淡玻璃圆盘 + 品牌 PNG 图标（可读性主层）----

function createSatelliteGroup(model: Model, texture: THREE.Texture | null, baseAngle: number, speed: number, orbitRadius: number): Satellite {
  const group = new THREE.Group()
  const brand = new THREE.Color(model.brandColor)

  // 1. 品牌色光晕 Sprite 背衬（替代 yun 的全息点云光环——见文件头简化说明）
  const glowMaterial = new THREE.SpriteMaterial({
    map: getGlowTexture(),
    color: brand,
    transparent: true,
    opacity: GLOW_BASE_OPACITY,
    blending: THREE.AdditiveBlending,
    depthWrite: false,
  })
  const glowBacking = new THREE.Sprite(glowMaterial)
  glowBacking.scale.set(GLOW_SCALE, GLOW_SCALE, 1.0)
  glowBacking.position.z = GLOW_Z_OFFSET
  glowBacking.raycast = () => {} // atmosphere only — never a hover/click target
  group.add(glowBacking)

  // 2a. 细金属环（弱化：氛围点缀，不是 UI 按钮边框）
  const rimMaterial = new THREE.MeshStandardMaterial({
    color: brand,
    metalness: 0.9,
    roughness: 0.1,
    transparent: true,
    opacity: RIM_BASE_OPACITY,
  })
  const rimMesh = new THREE.Mesh(new THREE.TorusGeometry(RIM_OUTER_RADIUS, RIM_TUBE_RADIUS, 8, 32), rimMaterial)
  group.add(rimMesh)

  // 2b. 淡玻璃圆盘底座
  const discMaterial = new THREE.MeshPhongMaterial({
    color: 0x90d0ff,
    transparent: true,
    opacity: DISC_OPACITY,
    shininess: 120,
    specular: 0xffffff,
    side: THREE.DoubleSide,
    depthWrite: false,
  })
  const discMesh = new THREE.Mesh(new THREE.CylinderGeometry(DISC_RADIUS, DISC_RADIUS, DISC_HEIGHT, 32), discMaterial)
  discMesh.rotation.x = Math.PI / 2
  group.add(discMesh)

  // 3. 品牌 PNG 图标平面——可读性主层，toneMapped:false 保留原始品牌色
  let iconMaterial: THREE.MeshBasicMaterial | null = null
  if (texture) {
    iconMaterial = new THREE.MeshBasicMaterial({
      map: texture,
      transparent: true,
      toneMapped: false,
      depthWrite: false,
    })
    const iconMesh = new THREE.Mesh(new THREE.PlaneGeometry(ICON_SIZE, ICON_SIZE), iconMaterial)
    iconMesh.position.z = ICON_Z_OFFSET
    group.add(iconMesh)
  }

  // 仅写入，供外部调试/未来 raycast 命中代码读取；本模块自身的每帧更新一律走 runtimeState。
  group.userData.modelId = model.id
  group.userData.baseAngle = baseAngle
  group.userData.speed = speed

  runtimeState.set(group, { baseAngle, speed, orbitRadius, glowMaterial, rimMaterial, iconMaterial })

  return { group, modelId: model.id }
}

// ---- 对外契约 ----

// 按 orbitModels('intl')/('domestic') 各 12 家，用 evenAngles(SATS_PER_ORBIT, ORBIT_PHASE[orbit])
// 均布到 rig.groups[0]/rig.groups[1]（ORBIT_ORDER 固定该映射关系，与 orbits.ts 的彗尾计算共用同一份
// 相位常量，见 orbits.ts 顶部注释）。品牌 PNG 纹理并行加载（Promise.all），单张缺失不阻塞其余卫星
// （降级为无图标，只保留光晕/环/盘三层）。
export async function createSatellites(rig: OrbitRig): Promise<Satellite[]> {
  const allModels = ORBIT_ORDER.flatMap((orbit) => orbitModels(orbit))
  const loader = new THREE.TextureLoader()

  const textures = await Promise.all(
    allModels.map(async (model) => {
      try {
        const tex = await loader.loadAsync(`/logos/${model.logo}`)
        tex.colorSpace = THREE.SRGBColorSpace
        return tex
      } catch (err) {
        console.warn(`Logo texture missing: /logos/${model.logo}`, err)
        return null
      }
    }),
  )

  const satellites: Satellite[] = []
  let cursor = 0
  ORBIT_ORDER.forEach((orbit, orbitIndex) => {
    const group = rig.groups[orbitIndex]
    const cfg = ORBITS[orbit]
    const models = orbitModels(orbit)
    const baseAngles = evenAngles(SATS_PER_ORBIT, ORBIT_PHASE[orbit])

    models.forEach((model, i) => {
      const texture = textures[cursor]
      cursor += 1
      const sat = createSatelliteGroup(model, texture, baseAngles[i], cfg.satSpeed, cfg.radius)
      group.add(sat.group)
      satellites.push(sat)
    })
  })

  return satellites
}

// 每帧驱动：卫星沿轨位置、朝向摄像机的 billboard、近大远小的深度缩放/透明度。main.ts 的 animate()
// 循环需要在调用本函数【之前】先调用 rig.update(t, camera)——billboard 靠抵消父级（轨道组）当帧
// 世界旋转来保持朝向摄像机，若父级本帧的进动 rotation.y 还没写入，贴图朝向会滞后一帧、随进动缓慢
// 歪斜。
const scratchParentQuat = new THREE.Quaternion()
const scratchWorldPos = new THREE.Vector3()

export function updateSatellites(satellites: Satellite[], t: number, camera: THREE.Camera): void {
  for (const sat of satellites) {
    const runtime = runtimeState.get(sat.group)
    if (!runtime) continue // 不会发生：satellites 数组里的 group 全部来自 createSatelliteGroup

    const angle = satelliteAngle(runtime.baseAngle, runtime.speed, t)
    sat.group.position.set(
      runtime.orbitRadius * Math.cos(angle),
      Math.sin(t * 1.5 + runtime.baseAngle) * BOB_AMPLITUDE,
      runtime.orbitRadius * Math.sin(angle),
    )

    // 真正的朝向摄像机 billboard：父级轨道组带静态倾角 + 动态进动，需先抵消父级世界旋转
    // （local = parentWorldQuat⁻¹ · cameraQuat），卫星贴图才会始终正对摄像机、不随轨道进动歪斜。
    const parent = sat.group.parent
    if (parent) {
      parent.getWorldQuaternion(scratchParentQuat)
      sat.group.quaternion.copy(scratchParentQuat.invert()).multiply(camera.quaternion)
    }

    // 视深度距离 → 近大远小 + 近亮远暗
    sat.group.getWorldPosition(scratchWorldPos)
    const dist = camera.position.distanceTo(scratchWorldPos)
    const distFactor = THREE.MathUtils.clamp(THREE.MathUtils.mapLinear(dist, NEAR_DIST, FAR_DIST, 1.0, 0.5), 0.5, 1.0)

    sat.group.scale.setScalar(distFactor)

    if (runtime.iconMaterial) {
      runtime.iconMaterial.opacity = 0.35 + 0.65 * distFactor
    }
    runtime.glowMaterial.opacity = GLOW_BASE_OPACITY * distFactor
    runtime.rimMaterial.opacity = RIM_BASE_OPACITY * distFactor
  }
}
