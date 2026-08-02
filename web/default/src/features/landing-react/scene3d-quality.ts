/* 落地页 WebGL 画质档位（「肉眼难辨」性能档）。
   只调像素比 / MSAA / Bloom 内部分辨率，不改粒子数、颜色、shader 与动画曲线。 */

export type RenderQuality = {
  /** renderer.setPixelRatio 上限后的实际比 */
  pixelRatio: number
  /** WebGL MSAA；粒子加算场景收益有限，中低档关闭 */
  antialias: boolean
  /** UnrealBloom 内部缓冲相对画布的比例（0.5 = half-res bloom） */
  bloomScale: number
  /** 档位名，便于调试 */
  tier: 'high' | 'mid' | 'low'
}

export type RenderQualityInput = {
  dpr?: number
  cores?: number
  /** NetworkInformation.saveData */
  saveData?: boolean
  /** navigator.deviceMemory（GB，Chrome） */
  deviceMemory?: number
}

/**
 * 按设备能力挑选渲染档位。
 * - high：桌面多核 / 非省流 → DPR≤2，保留 antialias，bloom 半分辨率
 * - mid：手机高 DPR 或 中等核数 → DPR≤1.5，关 antialias，bloom 半分辨率
 * - low：省流 / 低内存 / 双核及以下 → DPR≤1.25，关 antialias，bloom 半分辨率
 */
export function pickRenderQuality(input: RenderQualityInput = {}): RenderQuality {
  const dpr =
    input.dpr ??
    (typeof devicePixelRatio === 'number' && devicePixelRatio > 0
      ? devicePixelRatio
      : 1)
  const cores =
    input.cores ??
    (typeof navigator !== 'undefined' && navigator.hardwareConcurrency
      ? navigator.hardwareConcurrency
      : 4)
  const saveData =
    input.saveData ??
    (typeof navigator !== 'undefined' &&
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      (navigator as any).connection?.saveData === true)
  const deviceMemory =
    input.deviceMemory ??
    (typeof navigator !== 'undefined'
      ? // eslint-disable-next-line @typescript-eslint/no-explicit-any
        (navigator as any).deviceMemory
      : undefined)

  if (saveData || (typeof deviceMemory === 'number' && deviceMemory > 0 && deviceMemory <= 2) || cores <= 2) {
    return {
      tier: 'low',
      pixelRatio: Math.min(dpr, 1.25),
      antialias: false,
      bloomScale: 0.5,
    }
  }

  // 高 DPR 手机（≥2.5 或 核数 ≤4）走 mid，避免 3x 屏 × 全分辨率 bloom 过热
  if (cores <= 4 || dpr >= 2.5) {
    return {
      tier: 'mid',
      pixelRatio: Math.min(dpr, 1.5),
      antialias: false,
      bloomScale: 0.5,
    }
  }

  return {
    tier: 'high',
    pixelRatio: Math.min(dpr, 2),
    antialias: true,
    bloomScale: 0.5,
  }
}

/** 从当前运行环境采样（供 scene3d 启动时调用）。 */
export function detectRenderQuality(): RenderQuality {
  return pickRenderQuality({})
}
