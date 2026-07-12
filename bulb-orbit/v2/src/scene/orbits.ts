import * as THREE from 'three'
import { BULB_SCALE, CAMERA_Z, ORBITS, type OrbitCfg } from './config'
import { precessionAngle, satelliteAngle, evenAngles } from './orbit-math'
import type { Orbit } from '../data/models'

// ==========================================
// 轨道能量线 —— 移植自 yun src/main.js 的 orbitVertexShader/orbitFragmentShader + setupOrbits()
// 移植范围：两层 TubeGeometry（细核心线 + 4x 宽柔光晕）、深度渐隐、灯泡剪影遮罩、彗尾（comet wake）、
// 沿线能量流动。新增（yun 没有）：轨道整体进动——update() 里对每条轨道 group 的 rotation.y 写入
// precessionAngle(...)，实现「轨道本身也绕灯泡转，不只是卫星在轨道上跑」。
// 不含：卫星本体（satellites.ts）、HUD、点击/悬停——那些不属于本模块契约。
// ==========================================

// 每条轨道固定 12 颗卫星（对应 orbitModels('intl')/('domestic') 各 12 家），与 GLSL 定长数组
// uSatAngles[12] 及下方 satellites.ts 的 evenAngles(SATS_PER_ORBIT, ...) 共用同一个常量，避免两处
// 数量漂移导致彗尾与卫星实际位置对不上。
export const SATS_PER_ORBIT = 12

// 两条轨道均布卫星的起始相位——两者数量相同（12/12），仅用不同相位错开，纯美观选择（yun 原版两条
// 轨道数量不同 5/6 天然错开，无需相位区分）。satellites.ts 必须使用同一份常量摆放卫星，否则彗尾
// （由 update() 用相同公式独立算出）会跟卫星实际位置对不上。
export const ORBIT_PHASE: Record<Orbit, number> = {
  intl: 0,
  domestic: Math.PI / 12,
}

// groups[0]/groups[1] 的固定顺序——satellites.ts 按同一顺序把卫星分别挂到 rig.groups[0]/[1]。
export const ORBIT_ORDER: readonly Orbit[] = ['intl', 'domestic']

const TAU = Math.PI * 2

// 每条轨道的纯视觉参数（线色/透明度/线宽/彗尾强度与衰减）——不属于跨任务共享的 OrbitCfg
// （config.ts 里的 OrbitCfg 只放半径/倾角/公转速度/进动这些多任务都要用的字段），逐字取自 yun：
// intl  = yun 主轨（前层，半径 1.0，更亮更细，wakeStrength 0.9/falloff 5.0）
// domestic = yun 副轨（后层，半径 1.28，更暗更粗管径不同，wakeStrength 0.6/falloff 6.0）
// tubeRadius 是世界空间绝对线宽，按 bulb.ts 已确立的惯例乘 BULB_SCALE，让放大后的灯泡场景里线的
// 视觉粗细比例保持不变。
interface OrbitVisual {
  color: string
  opacity: number
  tubeRadius: number
  wakeStrength: number
  wakeFalloff: number
}

const ORBIT_VISUALS: Record<Orbit, OrbitVisual> = {
  intl: {
    color: '#8fd8ff',
    opacity: 0.55,
    tubeRadius: 0.0028 * BULB_SCALE,
    wakeStrength: 0.9,
    wakeFalloff: 5.0,
  },
  domestic: {
    color: '#5f7cff',
    opacity: 0.3,
    tubeRadius: 0.0018 * BULB_SCALE,
    wakeStrength: 0.6,
    wakeFalloff: 6.0,
  },
}

// 灯泡剪影遮罩半径（view-space，玻璃罩椭圆的半宽/半高）——yun 原始标定值 (0.48, 0.7) 是对应它未放大
// 的灯泡几何（dome 半径 0.36 等）算出来的；bulb.ts 把整颗灯泡用 group.scale.setScalar(BULB_SCALE)
// 等比放大，所以这里必须同比例放大，否则放大后的灯泡会比遮罩椭圆大，导致远侧轨道弧线从灯泡边缘露出
// （视觉门判据⑤要求「远侧弧线不横穿灯泡」）。
const MASK_RADIUS_X = 0.48 * BULB_SCALE
const MASK_RADIUS_Y = 0.7 * BULB_SCALE

// 深度渐隐阈值（view-space z，负值=在摄像机前方）——yun 原始 (-3.4, -5.9) 是按它的相机距离 4.6 与
// 副轨半径 1.28 算出来的（4.6∓1.28 ≈ 3.32/5.88）。本项目相机距离不变（CAMERA_Z 沿用 4.6，放大只作用
// 于轨道半径），所以改为用同样的关系式现算，而不是照抄 yun 的数字乘 BULB_SCALE——直接乘会把「相机
// 距离」这个没有缩放的量也错误地放大。用较宽的副轨（domestic）半径推导，覆盖两条轨道各自的近/远范围。
const DEPTH_NEAR = -(CAMERA_Z - ORBITS.domestic.radius)
const DEPTH_FAR = -(CAMERA_Z + ORBITS.domestic.radius)

// 保证是形如 "-2.552000" 的合法 GLSL float 字面量（必须带小数点，否则整数字面量在某些实现下会被当
// 成 int 解析失败）。
const glslFloat = (n: number): string => n.toFixed(6)

// ---- 1. Shaders（逐字移植自 yun，仅 uSatAngles 定长数组大小与深度阈值改为按上面常量注入）----

const orbitVertexShader = `
  varying vec3 vViewPos;
  varying vec2 vUv;
  void main() {
    vUv = uv;
    vec4 mvPosition = modelViewMatrix * vec4(position, 1.0);
    vViewPos = mvPosition.xyz;
    gl_Position = projectionMatrix * mvPosition;
  }
`

const orbitFragmentShader = `
  uniform vec3 uColor;
  uniform float uOpacity;
  uniform float uTime;
  uniform float uDir;          // orbit rotation direction: +1 or -1
  uniform float uSatCount;
  uniform float uSatAngles[${SATS_PER_ORBIT}]; // current angular positions of this orbit's satellites
  uniform float uWakeStrength;
  uniform float uWakeFalloff;
  uniform vec3 uBulbView;      // bulb center in view space
  uniform vec2 uMaskRadius;    // bulb silhouette half-extents at bulb depth (view units)
  varying vec3 vViewPos;
  varying vec2 vUv;

  const float TWO_PI = 6.28318530718;

  void main() {
    // Depth fade: near arcs full strength, far arcs dimmed to ~30%
    float depthFactor = smoothstep(${glslFloat(DEPTH_FAR)}, ${glslFloat(DEPTH_NEAR)}, vViewPos.z);
    float alpha = uOpacity * mix(0.3, 1.0, depthFactor);

    // Bulb mask: project this fragment onto the bulb's depth plane; fragments that
    // sit behind the bulb AND inside its silhouette ellipse fade out smoothly
    vec2 atBulbDepth = vViewPos.xy * (uBulbView.z / vViewPos.z);
    float m = length((atBulbDepth - uBulbView.xy) / uMaskRadius);
    float behind = smoothstep(0.3, -0.3, vViewPos.z - uBulbView.z);
    alpha *= mix(1.0, smoothstep(0.7, 1.12, m), behind);

    // Comet wakes: the line glows where a satellite just passed and the energy decays
    // with angular distance behind it — sharp at the satellite, trailing off after.
    // (uDir flips "behind" for the counter-rotating orbit; wrap keeps it seam-safe.)
    float theta = vUv.x * TWO_PI;
    float wake = 0.0;
    for (int i = 0; i < ${SATS_PER_ORBIT}; i++) {
      if (float(i) >= uSatCount) break;
      float d = (uSatAngles[i] - theta) * uDir;
      d -= TWO_PI * floor(d / TWO_PI);
      wake += exp(-d * uWakeFalloff);
    }
    wake *= uWakeStrength;

    // Subtle energy flow along the line, moving WITH the orbit's rotation direction
    // (3 wavelengths, seamless on the closed loop)
    float flow = 1.0 + 0.15 * sin(vUv.x * 18.849556 - uTime * 0.7 * uDir);

    vec3 color = uColor * (0.85 + 0.45 * depthFactor) * flow * (1.0 + wake * 1.2);
    gl_FragColor = vec4(color, min(alpha * (1.0 + wake * 1.5), 1.0));
  }
`

interface OrbitUniforms {
  // 索引签名满足 ShaderMaterialParameters['uniforms'] 的形状，具名属性保留强类型。
  [uniform: string]: THREE.IUniform
  uColor: THREE.IUniform<THREE.Color>
  uOpacity: THREE.IUniform<number>
  uTime: THREE.IUniform<number>
  uDir: THREE.IUniform<number>
  uSatCount: THREE.IUniform<number>
  uSatAngles: THREE.IUniform<number[]>
  uWakeStrength: THREE.IUniform<number>
  uWakeFalloff: THREE.IUniform<number>
  uBulbView: THREE.IUniform<THREE.Vector3>
  uMaskRadius: THREE.IUniform<THREE.Vector2>
}

// ---- 2. 轨道曲线 + TubeGeometry 双层（移植自 yun OrbitCurve + makeLayer）----

class OrbitCurve extends THREE.Curve<THREE.Vector3> {
  private readonly radius: number

  constructor(radius: number) {
    super()
    this.radius = radius
  }

  override getPoint(t: number, target: THREE.Vector3 = new THREE.Vector3()): THREE.Vector3 {
    const theta = t * Math.PI * 2
    return target.set(this.radius * Math.cos(theta), 0, this.radius * Math.sin(theta))
  }
}

interface OrbitLayer {
  mesh: THREE.Mesh
  uniforms: OrbitUniforms
}

// satAngles 数组由调用方（createOrbits）按轨道创建一次并在两层间共享引用：update() 每帧只算一次
// 当前卫星角度、写入这一个数组，两层材质自动同步，避免 yun 原版「两层各自独立、逐帧重复计算同一批
// 角度」的冗余。
function createLayer(cfg: OrbitCfg, visual: OrbitVisual, radiusScale: number, opacity: number, satAngles: number[]): OrbitLayer {
  const geometry = new THREE.TubeGeometry(new OrbitCurve(cfg.radius), 256, visual.tubeRadius * radiusScale, 6, true)

  const uniforms: OrbitUniforms = {
    uColor: { value: new THREE.Color(visual.color) },
    uOpacity: { value: opacity },
    uTime: { value: 0 },
    uDir: { value: Math.sign(cfg.satSpeed) || 1 },
    uSatCount: { value: SATS_PER_ORBIT },
    uSatAngles: { value: satAngles },
    uWakeStrength: { value: visual.wakeStrength },
    uWakeFalloff: { value: visual.wakeFalloff },
    uBulbView: { value: new THREE.Vector3() },
    uMaskRadius: { value: new THREE.Vector2(MASK_RADIUS_X, MASK_RADIUS_Y) },
  }

  const material = new THREE.ShaderMaterial({
    vertexShader: orbitVertexShader,
    fragmentShader: orbitFragmentShader,
    uniforms,
    transparent: true,
    depthWrite: false,
    blending: THREE.AdditiveBlending,
  })

  const mesh = new THREE.Mesh(geometry, material)
  mesh.raycast = () => {} // scenery, never a hover/click target
  return { mesh, uniforms }
}

// ---- 3. 对外契约 ----

export interface OrbitRig {
  groups: THREE.Group[]
  update(t: number, camera: THREE.Camera): void
}

interface OrbitEntry {
  cfg: OrbitCfg
  group: THREE.Group
  baseAngles: number[]
  satAngles: number[]
  layers: OrbitLayer[]
}

export function createOrbits(): OrbitRig {
  const bulbView = new THREE.Vector3()
  const entries: OrbitEntry[] = ORBIT_ORDER.map((orbit) => {
    const cfg = ORBITS[orbit]
    const visual = ORBIT_VISUALS[orbit]

    const group = new THREE.Group()
    // 静态倾角只在创建时设一次；进动只改 rotation.y，X/Z 分量此后不再触碰。
    group.rotation.set(cfg.tiltX, 0, cfg.tiltZ)

    const baseAngles = evenAngles(SATS_PER_ORBIT, ORBIT_PHASE[orbit])
    const satAngles = new Array<number>(SATS_PER_ORBIT).fill(0)

    const core = createLayer(cfg, visual, 1.0, visual.opacity, satAngles) // 细核心线
    const halo = createLayer(cfg, visual, 4.0, visual.opacity * 0.22, satAngles) // 4x 宽柔光晕
    group.add(core.mesh, halo.mesh)

    return { cfg, group, baseAngles, satAngles, layers: [core, halo] }
  })

  const groups = entries.map((e) => e.group)

  function update(t: number, camera: THREE.Camera): void {
    camera.updateMatrixWorld()
    bulbView.set(0, 0, 0).applyMatrix4(camera.matrixWorldInverse)

    for (const entry of entries) {
      // 新增：轨道整体进动——整条椭圆（线+挂在其下的卫星）绕 Y 轴缓慢转动。
      entry.group.rotation.y = precessionAngle(entry.cfg.precessDir, entry.cfg.precessPeriod, t)

      // 与 satellites.ts 的 updateSatellites() 使用完全相同的 satelliteAngle 公式与 baseAngles，
      // 保证彗尾（wake）严格跟随卫星实际位置。% TAU 只是给上传到 GLSL float(单精度)的数值定期折
      // 回 [0, 2π) 附近，避免长时间运行后数值变大精度下降；着色器内部对差值 d 自己做了正确的模运算，
      // 因此这里符号是否为负并不影响正确性。
      for (let i = 0; i < entry.baseAngles.length; i++) {
        entry.satAngles[i] = satelliteAngle(entry.baseAngles[i], entry.cfg.satSpeed, t) % TAU
      }

      for (const layer of entry.layers) {
        layer.uniforms.uTime.value = t
        layer.uniforms.uBulbView.value.copy(bulbView)
      }
    }
  }

  return { groups, update }
}
