/* 卫星选中状态机（纯核）。
   按 PointerMode 分流：桌面走 hover-tick 迟滞锁定；触屏走 tap/close，
   彻底忽略合成鼠标事件（R7 根治）。零副作用；无变化时返回同引用。 */

import type { PointerMode } from './viewport-mode'

/** 状态极简：只有「当前选中哪颗卫星」。其余由宿主派生。 */
export type SelectionState = { readonly selected: string | null }

export type SelectionEvent =
  /** 桌面路径：每帧上报一次悬停采样 */
  | { type: 'hover-tick'; hoverKey: string | null; pointerInside: boolean }
  /** 触屏路径：点中某颗卫星 */
  | { type: 'tap-chip'; key: string }
  /** 触屏路径：点在抽屉与卫星之外 */
  | { type: 'tap-outside' }
  /** 触屏路径：点抽屉关闭按钮 */
  | { type: 'close' }

export const INITIAL_SELECTION: SelectionState = { selected: null }

/**
 * 悬停选中的吸附/锁定(hysteresis) —— 自 scene3d-interaction.nextSelection 原样搬迁。
 * 规则：离开视觉区即关闭；命中某图标即锁定（含切换）；命中空处保持当前锁定不松手。
 */
function nextSelection(
  current: string | null,
  hitKey: string | null,
  pointerInside: boolean
): string | null {
  if (!pointerInside) return null
  if (hitKey) return hitKey
  return current
}

/**
 * 纯迁移函数。
 * 恒等性：结果 selected 与入参相同时返回**同一个对象引用**，
 * 调用方可用 `next !== state` 零成本判断是否需要触发副作用。
 */
export function reduce(
  state: SelectionState,
  event: SelectionEvent,
  mode: PointerMode
): SelectionState {
  let nextSelected: string | null = state.selected

  if (mode === 'hover') {
    if (event.type === 'hover-tick') {
      nextSelected = nextSelection(
        state.selected,
        event.hoverKey,
        event.pointerInside
      )
    }
    // tap-chip / tap-outside / close → 不变（桌面防御性忽略）
  } else {
    // mode === 'touch'
    switch (event.type) {
      case 'hover-tick':
        // ★ R7：触屏合成的 mousemove/mouseenter 一律不影响选中
        break
      case 'tap-chip':
        nextSelected = event.key
        break
      case 'tap-outside':
      case 'close':
        nextSelected = null
        break
    }
  }

  if (nextSelected === state.selected) return state
  return { selected: nextSelected }
}
