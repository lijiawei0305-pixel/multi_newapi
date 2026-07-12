import * as THREE from 'three'
import type { Satellite } from '../scene/satellites'

// ==========================================
// 卫星命中测试 —— 移植自 yun src/main.js 的 raycaster 设置 + checkSatelliteHover()/click 里的
// "从命中的子网格向上找到卫星顶层 group" 逻辑。
// 不含：HUD 联动、灯泡换色——那些是调用方（scene.ts）的接线职责，不属于本模块契约。
// ==========================================

// yun 原始注释：Default Points threshold is 1 WORLD unit — with badge-scale satellite groups that
// would make every satellite's hit zone far larger than its visible icon. Keep it near icon scale.
const POINTS_HOVER_THRESHOLD = 0.02

export interface SatelliteRaycaster {
  hover(mouse: THREE.Vector2): string | null
  click(mouse: THREE.Vector2): string | null
}

export function makeRaycaster(camera: THREE.Camera, satellites: Satellite[]): SatelliteRaycaster {
  const raycaster = new THREE.Raycaster()
  raycaster.params.Points.threshold = POINTS_HOVER_THRESHOLD

  // 命中测试用卫星顶层 group 数组：intersectObjects(groups, true) 递归命中每个 group 下的子网格
  // （金属环/图标平面——光晕 Sprite 与轨道线均已在各自模块把 raycast 设为空操作，不会被命中），
  // 命中的是子网格本身，需要向上walk 找到这份数组里的顶层 group 才能读到 userData.modelId。
  // 类型显式标为 Object3D[]（而非推断出的 Group[]）：下面 while 循环里 hit 的类型是 Object3D |
  // null，Array.prototype.includes 要求实参可赋值给数组元素类型，Object3D 无法赋值给更窄的 Group
  // （Group 多一个 isGroup 字面量属性）——两处都用 Object3D 这个共同的宽类型即可各自类型检查通过，
  // intersectObjects 本身接收的也正是 Object3D[]。
  const groups: THREE.Object3D[] = satellites.map((sat) => sat.group)

  function hitTest(mouse: THREE.Vector2): string | null {
    raycaster.setFromCamera(mouse, camera)
    const intersects = raycaster.intersectObjects(groups, true)
    if (intersects.length === 0) return null

    let hit: THREE.Object3D | null = intersects[0].object
    while (hit !== null && !groups.includes(hit)) {
      hit = hit.parent
    }
    if (hit === null) return null

    // Object3D.userData 的索引签名类型是 any；显式收窄成 unknown 再做 typeof narrowing，避免 any
    // 从三方库类型泄漏进本模块的类型化返回值。
    const modelId: unknown = hit.userData.modelId
    return typeof modelId === 'string' ? modelId : null
  }

  // hover 和 click 是同一份命中测试——调用方在不同时机（每帧悬停检测 / click 事件）各自调用。
  return { hover: hitTest, click: hitTest }
}
