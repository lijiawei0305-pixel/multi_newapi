export const BULB_SCALE = 1.6          // 相对 yun 放大，占屏高约 55–60%
export const CAMERA_Z = 4.6            // 沿用 yun 相机距离；放大靠缩放灯泡组与外扩轨道
export const VIEW_OFFSET_X_RATIO = 0.10 // 灯泡视觉中心右移到 x≈60%

export interface OrbitCfg {
  radius: number
  tiltX: number
  tiltZ: number
  satSpeed: number       // 卫星沿轨角速度 rad/s（沿用 yun）
  precessPeriod: number  // 轨道整体进动周期 s
  precessDir: 1 | -1     // 进动方向
}

// 半径 = yun 原值 × BULB_SCALE，保持「约 2.8×/3.6× 灯泡宽」的比例
export const ORBITS: { intl: OrbitCfg; domestic: OrbitCfg } = {
  intl:     { radius: 1.0 * BULB_SCALE, tiltX: 0.5,  tiltZ: 0.12,  satSpeed: 0.10,  precessPeriod: 45, precessDir: 1 },
  domestic: { radius: 1.28 * BULB_SCALE, tiltX: 0.72, tiltZ: -0.38, satSpeed: -0.075, precessPeriod: 70, precessDir: -1 },
}
