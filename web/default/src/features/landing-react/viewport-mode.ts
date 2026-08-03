/* 落地页视口断点与指针能力（纯核）。
   断点单一真源 + 指针能力开局判定 / 运行时校正；
   detectPointerMode 为本模块唯一允许采样 window 的入口（对齐 scene3d-quality 范式）。 */

/** 断点单一真源。CSS 媒体查询与 JS 判定都从这里取值，杜绝两边漂移。
 *  mobile 与 hooks/use-mobile.ts 的私有常量 MOBILE_BREAKPOINT 同值，
 *  两处不互相 import（全站 hook 不得反向依赖单个 feature）。 */
export const BREAKPOINTS = {
  /** 手机端主断点。与 hooks/use-mobile.ts 的 MOBILE_BREAKPOINT 同值。 */
  mobile: 768,
  /** 窄屏微调断点，覆盖 360–400px。 */
  narrow: 400,
  /** 矮屏断点（如 360×640），用于压缩 3D 区高度占比。 */
  shortViewport: 700,
} as const

/** 指针能力档位。'hover' = 有真实悬停能力（鼠标/触控板）；'touch' = 仅触摸。 */
export type PointerMode = 'hover' | 'touch'

export type PointerModeInput = {
  /** matchMedia('(hover: hover)').matches；老浏览器不支持该特性时为 undefined */
  hoverCapable?: boolean
  /** navigator.maxTouchPoints，仅在 hoverCapable 缺失时作为回落依据 */
  maxTouchPoints?: number
}

/**
 * 纯判定：开局用的初值。零 I/O，可直接单测。
 * - hoverCapable true → hover（含触屏笔电，靠运行时校正纠正）
 * - hoverCapable false → touch
 * - hoverCapable 缺失 + maxTouchPoints > 0 → touch
 * - 其余不确定情况 → hover（保守回落 = 保持改造前行为）
 */
export function resolvePointerMode(input: PointerModeInput = {}): PointerMode {
  if (input.hoverCapable === true) return 'hover'
  if (input.hoverCapable === false) return 'touch'
  // hoverCapable 缺失：靠 maxTouchPoints 回落
  if (typeof input.maxTouchPoints === 'number' && input.maxTouchPoints > 0) {
    return 'touch'
  }
  return 'hover'
}

/**
 * 纯校正：从一次真实交互的 PointerEvent.pointerType 反推指针模式。
 * 返回 null 表示「该事件不足以判定，维持当前模式」。
 * - mouse → hover
 * - touch / pen → touch（触控笔不保证抬笔触发 mouseleave，取保守值）
 * - 其他 / 空串 → null
 */
export function pointerModeFromEvent(pointerType: string): PointerMode | null {
  if (pointerType === 'mouse') return 'hover'
  if (pointerType === 'touch' || pointerType === 'pen') return 'touch'
  return null
}

/**
 * 从当前运行环境采样 matchMedia('(hover: hover)') 与 navigator.maxTouchPoints，
 * 再转调 resolvePointerMode。本模块唯一接触 window 的函数。
 * matchMedia 不存在时 hoverCapable 传 undefined。
 */
export function detectPointerMode(): PointerMode {
  let hoverCapable: boolean | undefined
  if (
    typeof window !== 'undefined' &&
    typeof window.matchMedia === 'function'
  ) {
    hoverCapable = window.matchMedia('(hover: hover)').matches
  }

  let maxTouchPoints: number | undefined
  if (typeof navigator !== 'undefined') {
    maxTouchPoints = navigator.maxTouchPoints
  }

  return resolvePointerMode({ hoverCapable, maxTouchPoints })
}
