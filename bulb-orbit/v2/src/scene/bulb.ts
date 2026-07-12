import * as THREE from 'three'
import { BULB_SCALE } from './config'

// ==========================================
// 粒子灯泡 —— 移植自 yun src/main.js
// 移植范围：vertex/fragment shader（粒子鼠标排斥）、setupGlassCore()（玻璃罩/底座/辉光/灯丝）、
// 粒子 ShaderMaterial + uniforms、animate() 里灯泡相关的每帧逻辑（uTime、鼠标流体排斥、缓慢自转）。
// 不含：轨道卫星、HUD、点击/悬停 raycast——那些不属于本模块契约。
// ==========================================

const BIN_URL = '/models/dengpao_points_smooth.bin'
const POINT_COUNT = 40000

const DEFAULT_COLOR = '#00f0ff'

// yun 原始调参（未放大灯泡时）：粒子点大小、鼠标排斥场半径/强度，均是「相对灯泡自身世界坐标尺寸」
// 的绝对值。灯泡整体经 group.scale.setScalar(BULB_SCALE) 放大后，粒子间的世界空间间距也按
// BULB_SCALE 等比变大，但下面这三个值不会随 modelMatrix 自动缩放（uSize 是脱离 modelMatrix 的独立
// 屏幕空间系数；uRepelRadius/uRepelPower 虽在着色器里用于世界空间比较，但本身是纯 JS 端常量）。
// 若不补偿，放大后粒子会显得更稀疏、排斥环相对灯泡也显得更小——因此都乘上 BULB_SCALE 等比放大，
// 让「点覆盖率」「排斥环占灯泡表面的比例」与放大前保持一致的观感。
const PARTICLE_BASE_SIZE = 0.045
const REPEL_RADIUS_BASE = 0.26
const REPEL_POWER_BASE = 0.16

// ---- 1. Shaders（逐字移植自 yun，含鼠标排斥场）----

const vertexShader = `
  uniform float uTime;
  uniform float uSize;
  uniform vec3 uMouseOrigin;   // repulsion field ray origin (camera position)
  uniform vec3 uMouseDir;      // smoothed, normalized cursor ray direction
  uniform float uRepelStrength; // eased 0..1 (fades in on enter, out on leave)
  uniform float uRepelRadius;   // field radius around the cursor ray, world units
  uniform float uRepelPower;    // max outward displacement, world units

  attribute vec3 aNormal;
  attribute float aRandom;

  varying vec3 vColor;

  void main() {
    // 1. Gentle breathing displacement along normals (very micro, 0.4% max)
    float breathingOffset = sin(uTime * 1.1 + aRandom * 6.28318) * 0.006;

    // 2. Tiny natural noise jitter (0.25% max, for organic cell feeling)
    vec3 noiseOffset = vec3(
      sin(uTime * 0.5 + aRandom * 25.0),
      cos(uTime * 0.7 + aRandom * 30.0),
      sin(uTime * 0.9 + aRandom * 35.0)
    ) * 0.002;

    vec3 finalPos = position + aNormal * breathingOffset + noiseOffset;

    // 3. Global breathing scale pulse
    float breath = 1.0 + sin(uTime * 0.7) * 0.012;
    finalPos *= breath;

    // 4. Fluid mouse repulsion: displacement points away from the cursor ray with a
    // gaussian falloff — strongest at the cursor, so cleared particles pile up into
    // a soft ring around it. Strength/direction are eased in JS, so particles
    // disperse fluidly on approach and converge back home when the cursor leaves.
    vec3 worldPos = (modelMatrix * vec4(finalPos, 1.0)).xyz;
    vec3 toParticle = worldPos - uMouseOrigin;
    vec3 closest = uMouseOrigin + uMouseDir * dot(toParticle, uMouseDir);
    vec3 away = worldPos - closest;
    float rayDist = length(away);
    float force = uRepelStrength * exp(-(rayDist * rayDist) / (uRepelRadius * uRepelRadius));
    vec3 pushDir = rayDist > 0.0001 ? away / rayDist : aNormal;
    // per-particle stagger keeps the ring edge organic instead of mechanical
    worldPos += pushDir * force * uRepelPower * (0.75 + 0.5 * aRandom);

    // 5. Color gradient: core bright white-blue, edges deep cyan-blue;
    // displaced particles get a subtle excitation tint
    float radialDist = length(position.xz);
    vec3 coreColor = vec3(0.85, 0.94, 1.0); // Bright core
    vec3 edgeColor = vec3(0.0, 0.65, 1.0); // Cyan-blue edge
    vColor = mix(coreColor, edgeColor, smoothstep(0.06, 0.35, radialDist));
    vColor = mix(vColor, vec3(0.75, 0.95, 1.0), force * 0.6);

    // Position transform (world space, since repulsion happens there)
    vec4 mvPosition = viewMatrix * vec4(worldPos, 1.0);
    gl_Position = projectionMatrix * mvPosition;

    // 6. Individual twinkling sizing
    float twinkle = 0.7 + 0.3 * sin(uTime * (1.6 + aRandom * 2.2) + aRandom * 6.28318);
    gl_PointSize = uSize * (300.0 / -mvPosition.z) * twinkle;
  }
`

const fragmentShader = `
  varying vec3 vColor;

  void main() {
    float dist = length(gl_PointCoord - vec2(0.5));
    if (dist > 0.5) discard;

    // Soft radial edge
    float alpha = smoothstep(0.5, 0.06, dist);

    // Bright hot center
    float core = smoothstep(0.12, 0.0, dist) * 0.6;

    vec3 finalColor = mix(vColor, vec3(1.0), core * 0.4);
    gl_FragColor = vec4(finalColor, (alpha * 0.45 + core * 0.15) * 0.75);
  }
`

interface ParticleUniforms {
  // 索引签名满足 THREE.ShaderMaterialParameters['uniforms'] 的 { [uniform: string]: IUniform } 形状，
  // 下面逐个具名属性仍保留各自的强类型（number / Vector3），访问时不会退化成 any。
  [uniform: string]: THREE.IUniform
  uTime: THREE.IUniform<number>
  uSize: THREE.IUniform<number>
  uMouseOrigin: THREE.IUniform<THREE.Vector3>
  uMouseDir: THREE.IUniform<THREE.Vector3>
  uRepelStrength: THREE.IUniform<number>
  uRepelRadius: THREE.IUniform<number>
  uRepelPower: THREE.IUniform<number>
}

// ---- 2. 玻璃罩 + 底座 + 辉光 Sprite + 灯丝核心 ----

interface GlassCore {
  group: THREE.Group
  pointLight: THREE.PointLight
  glowMaterial: THREE.SpriteMaterial
  coreMesh: THREE.Mesh
  coreMaterial: THREE.MeshBasicMaterial
}

// 柔和径向辉光贴图。与 yun 不同：yun 把颜色直接烘进这张 canvas（每个模型换色都要重新画布+重新
// 上传纹理）；这里改成中性白色贴图，运行时改色一律走 SpriteMaterial.color 做 GPU 端 tint——
// setCoreColor 需要支持任意 hex 动态换色，避免每次 hover 换色都新建 canvas/纹理造成 GPU 资源泄漏。
function createGlowSpriteTexture(): THREE.CanvasTexture {
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

  return new THREE.CanvasTexture(canvas)
}

// 玻璃罩/颈/底座金属 + 辉光 Sprite + 灯丝核心，尺寸与位置逐字沿用 yun 的绝对数值——放大交给外层
// group.scale.setScalar(BULB_SCALE)，整组一起缩放，无需在这里手动乘 BULB_SCALE。
// pointLight 在 yun 里是场景级独立灯（未挂在 bulbGroup 下），这里必须内置在灯泡自己的返回组里：
// createBulb() 不接收外部 scene/light 引用，而 setCoreColor 契约要求能直接改这颗点光源的颜色。
function setupGlassCore(): GlassCore {
  const group = new THREE.Group()

  const glassMat = new THREE.MeshPhongMaterial({
    color: 0x8be5ff,
    transparent: true,
    opacity: 0.05,
    shininess: 120,
    specular: 0xffffff,
    side: THREE.DoubleSide,
    depthWrite: false,
  })

  const dome = new THREE.Mesh(new THREE.SphereGeometry(0.36, 40, 40), glassMat)
  dome.position.y = 0.14
  group.add(dome)

  const neck = new THREE.Mesh(new THREE.CylinderGeometry(0.36, 0.2, 0.32, 32, 1, true), glassMat)
  neck.position.y = -0.16
  group.add(neck)

  const baseMat = new THREE.MeshStandardMaterial({
    color: 0x1a2b42,
    metalness: 0.9,
    roughness: 0.2,
    transparent: true,
    opacity: 0.65,
  })
  const cap = new THREE.Mesh(new THREE.CylinderGeometry(0.18, 0.18, 0.18, 32), baseMat)
  cap.position.y = -0.41
  group.add(cap)

  const pointLight = new THREE.PointLight(DEFAULT_COLOR, 1.5, 8)
  pointLight.position.set(0, 0.14, 1.2)
  group.add(pointLight)

  const glowMaterial = new THREE.SpriteMaterial({
    map: createGlowSpriteTexture(),
    color: new THREE.Color(DEFAULT_COLOR),
    transparent: true,
    opacity: 0.28,
    blending: THREE.AdditiveBlending,
  })
  const glowSprite = new THREE.Sprite(glowMaterial)
  glowSprite.scale.set(0.65, 0.65, 1.0)
  glowSprite.position.y = 0.14
  group.add(glowSprite)

  // 灯丝底色沿用 yun：常态是白炽色，只有触发 setCoreColor/surge 才会被染成品牌色
  const coreMaterial = new THREE.MeshBasicMaterial({ color: 0xffffff, transparent: true, opacity: 0.65 })
  const coreMesh = new THREE.Mesh(new THREE.CylinderGeometry(0.008, 0.008, 0.12, 16), coreMaterial)
  coreMesh.position.y = 0.14
  group.add(coreMesh)

  return { group, pointLight, glowMaterial, coreMesh, coreMaterial }
}

// ---- 3. 加载平滑点云 bin（Task 6 产物：40000 点 × 6 float = 位置 xyz + 单位法线 xyz）----

async function loadParticleGeometry(): Promise<THREE.BufferGeometry> {
  const response = await fetch(BIN_URL)
  if (!response.ok) throw new Error(`Failed to load ${BIN_URL}`)
  const arrayBuffer = await response.arrayBuffer()
  const floatArray = new Float32Array(arrayBuffer)

  const positions = new Float32Array(POINT_COUNT * 3)
  const normals = new Float32Array(POINT_COUNT * 3)
  const randoms = new Float32Array(POINT_COUNT)

  for (let i = 0; i < POINT_COUNT; i++) {
    positions[i * 3] = floatArray[i * 6]
    positions[i * 3 + 1] = floatArray[i * 6 + 1]
    positions[i * 3 + 2] = floatArray[i * 6 + 2]

    normals[i * 3] = floatArray[i * 6 + 3]
    normals[i * 3 + 1] = floatArray[i * 6 + 4]
    normals[i * 3 + 2] = floatArray[i * 6 + 5]

    randoms[i] = Math.random()
  }

  const geometry = new THREE.BufferGeometry()
  geometry.setAttribute('position', new THREE.BufferAttribute(positions, 3))
  geometry.setAttribute('aNormal', new THREE.BufferAttribute(normals, 3))
  geometry.setAttribute('aRandom', new THREE.BufferAttribute(randoms, 1))
  return geometry
}

// ---- 4. surge() 迸发包络：移植自 yun triggerCoreSurge 的 gsap fromTo/yoyo 数值（0.35s 升 + 0.35s
// 落，power2.out 缓动），改成纯函数：由 update() 传入的 t 驱动，不引入 gsap 这个独立的第二时钟。----

const SURGE_RISE = 0.35
const SURGE_FALL = 0.35
const BASE_GLOW_OPACITY = 0.28
const SURGE_GLOW_OPACITY = 0.55
const BASE_CORE_SCALE = 1.0
const SURGE_CORE_SCALE = 1.8

const easeOutQuad = (x: number): number => 1 - (1 - x) * (1 - x)

// elapsed < 0（尚未触发）或 elapsed 超过总时长 → 0（回到 baseline）
function surgeEnvelope(elapsed: number): number {
  if (elapsed < 0 || elapsed >= SURGE_RISE + SURGE_FALL) return 0
  if (elapsed < SURGE_RISE) return easeOutQuad(elapsed / SURGE_RISE)
  return 1 - easeOutQuad((elapsed - SURGE_RISE) / SURGE_FALL)
}

// ---- 5. 对外契约 ----

export interface Bulb {
  group: THREE.Group
  setCoreColor(hex: string): void
  surge(hex: string): void
  update(t: number, mouse: { x: number; y: number }, hasPointer: boolean, camera: THREE.Camera): void
}

export async function createBulb(): Promise<Bulb> {
  const geometry = await loadParticleGeometry()

  const uniforms: ParticleUniforms = {
    uTime: { value: 0 },
    uSize: { value: PARTICLE_BASE_SIZE * BULB_SCALE },
    uMouseOrigin: { value: new THREE.Vector3(0, 0, 99) },
    uMouseDir: { value: new THREE.Vector3(0, 0, -1) },
    uRepelStrength: { value: 0 },
    uRepelRadius: { value: REPEL_RADIUS_BASE * BULB_SCALE },
    uRepelPower: { value: REPEL_POWER_BASE * BULB_SCALE },
  }

  const particleMaterial = new THREE.ShaderMaterial({
    vertexShader,
    fragmentShader,
    uniforms,
    transparent: true,
    depthWrite: false,
    blending: THREE.AdditiveBlending,
  })

  const particleSystem = new THREE.Points(geometry, particleMaterial)

  const { group: glassGroup, pointLight, glowMaterial, coreMesh, coreMaterial } = setupGlassCore()

  const group = new THREE.Group()
  group.add(particleSystem)
  group.add(glassGroup)
  group.scale.setScalar(BULB_SCALE)

  let lastT = 0
  let surgeStartT: number | null = null
  const cursorRayDir = new THREE.Vector3(0, 0, -1)

  function setCoreColor(hex: string): void {
    pointLight.color.set(hex)
    glowMaterial.color.set(hex)
    coreMaterial.color.set(hex)
  }

  function surge(hex: string): void {
    setCoreColor(hex)
    surgeStartT = lastT
  }

  function update(t: number, mouse: { x: number; y: number }, hasPointer: boolean, camera: THREE.Camera): void {
    const dt = THREE.MathUtils.clamp(t - lastT, 0.001, 0.05)
    lastT = t

    uniforms.uTime.value = t

    // Fluid mouse repulsion field: the field's ray direction eases toward the live
    // cursor (elastic lag = fluid feel) and its strength fades in fast / out slow,
    // so particles disperse on approach and gently flow home when the cursor leaves.
    if (hasPointer) {
      cursorRayDir.set(mouse.x, mouse.y, 0.5).unproject(camera).sub(camera.position).normalize()
    }
    uniforms.uMouseOrigin.value.copy(camera.position)
    uniforms.uMouseDir.value.lerp(cursorRayDir, 1 - Math.exp(-9 * dt)).normalize()
    const strengthTarget = hasPointer ? 1 : 0
    const ease = strengthTarget > uniforms.uRepelStrength.value ? 5 : 2.2 // engage fast, release gently
    uniforms.uRepelStrength.value += (strengthTarget - uniforms.uRepelStrength.value) * (1 - Math.exp(-ease * dt))

    // Gentle slow core spin (particles + glass core share one group, so one rotation
    // drives both — equivalent to yun rotating particleSystem/bulbGroup in lockstep)
    group.rotation.y = t * 0.02

    if (surgeStartT !== null) {
      const elapsed = t - surgeStartT
      const envelope = surgeEnvelope(elapsed)
      coreMesh.scale.setScalar(BASE_CORE_SCALE + envelope * (SURGE_CORE_SCALE - BASE_CORE_SCALE))
      glowMaterial.opacity = BASE_GLOW_OPACITY + envelope * (SURGE_GLOW_OPACITY - BASE_GLOW_OPACITY)
      if (elapsed >= SURGE_RISE + SURGE_FALL) surgeStartT = null
    }
  }

  return { group, setCoreColor, surge, update }
}
