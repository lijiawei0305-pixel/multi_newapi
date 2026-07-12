// Task 11 视觉门实测标定（screenshot + 像素测量，见 task-11-report.md）：BULB_SCALE=1.6 时灯泡
// 亮核占屏高约 22–31%（阈值不同测法有差异，均未达「≥55%」目标）。经两轮实测调参收敛到 3.1：
// 像素/世界单位换算比例 ≈290.8px/unit（@1920×1080, CAMERA_Z=4.6）。
export const BULB_SCALE = 2.95         // 占屏高约 56–57%（灯泡+底座，目标 ≥55%；3.1 实测到手
                                        // 59.35%，有余量往下收，换取轨道半径可以同步收紧）
export const CAMERA_Z = 4.6            // 沿用 yun 相机距离；放大靠缩放灯泡组，不靠拉近相机——
                                        // 拉近相机会把灯泡与轨道的屏幕占比按同一比例一起放大，
                                        // 无法单独把灯泡放大同时把轨道收紧（两者之比是相机距离
                                        // 无关的纯世界单位比值），所以只调 BULB_SCALE。
export const VIEW_OFFSET_X_RATIO = 0.15 // 灯泡视觉中心右移到 x≈62-65%（符号见 scene.ts 里的取反
                                        // 说明）——第三轮 0.13 实测华为卫星图标仍贴着"WeDream AI"
                                        // 标题最后一个字符，右侧 HUD 那一侧还有余量，继续加大到
                                        // 0.15 换取左侧多一点净空。

export interface OrbitCfg {
  radius: number
  tiltX: number
  tiltZ: number
  satSpeed: number       // 卫星沿轨角速度 rad/s（沿用 yun）
  precessPeriod: number  // 轨道整体进动周期 s
  precessDir: 1 | -1     // 进动方向
}

// 轨道半径【故意不再乘 BULB_SCALE】——Task 11 视觉门发现：若半径继续等比跟灯泡放大（旧写法
// `1.0 * BULB_SCALE`），灯泡放大到占屏 55%+ 后，外轨半径会撑到视口宽度的 30%+，两条轨道的椭圆
// 直接横扫过左栏文案与右侧 HUD 卡片。灯泡与轨道是否等比缩放没有强制关系（灯泡是视觉主体，轨道
// 只需要「贴着灯泡外沿留出可见空隙」），因此这里把半径拆成独立字面量，不再跟随 BULB_SCALE 联动。
//
// 半径数值经三轮实测调整：
//   第一轮 1.5/1.75 太小——轨道是倾斜椭圆，近景透视下有一段弧线投影到比"平面半径"小得多的屏幕
//   距离，实测卫星图标直接压在灯泡粒子云上；不能只按平面半径减灯泡半宽估净空，必须留出明显更大
//   的余量。
//   第二轮 2.2/2.4（配 BULB_SCALE=3.1）净空足够、不再压穿灯泡，但外轨伸进了左侧文案与右侧 HUD
//   卡片的可视范围（尤其贴着"WeDream AI"标题背后穿过）。
//   第三轮 配合 BULB_SCALE 3.1→2.95 的小幅回收，半径整体收紧到 1.9/2.05（保持与灯泡半宽相近
//   的净空比例，不重新引入压穿问题），并把 VIEW_OFFSET_X_RATIO 提到 0.13——灯泡不再压穿、右侧
//   HUD 干净，但左侧华为卫星图标仍贴着"WeDream AI"标题最后一个字符。
//   第四轮（当前）：半径再收紧到 1.8/1.9（净空比例略降但仍在灯泡放大前 yun 原比例的安全区间内），
//   配合 VIEW_OFFSET_X_RATIO 0.13→0.15，两者共同把外轨左沿从标题文字背后彻底推开。
//   灯泡半宽 = 0.36(穹顶半径) × BULB_SCALE(2.95) ≈ 1.062 世界单位
//   国际轨半径 1.8 ≈ 1.69× 灯泡半宽
//   国产轨半径 1.9（≈1.06× 国际轨）——把 yun 原始 1.28× 的内外轨差距收紧到 1.06×，两条轨道靠
//   倾角(tiltX/tiltZ)本身的差异区分层次，半径不再进一步拉大外轨、减少它伸进两侧留白的幅度。
export const ORBITS: { intl: OrbitCfg; domestic: OrbitCfg } = {
  intl:     { radius: 1.8, tiltX: 0.5,  tiltZ: 0.12,  satSpeed: 0.10,  precessPeriod: 45, precessDir: 1 },
  domestic: { radius: 1.9, tiltX: 0.72, tiltZ: -0.38, satSpeed: -0.075, precessPeriod: 70, precessDir: -1 },
}
