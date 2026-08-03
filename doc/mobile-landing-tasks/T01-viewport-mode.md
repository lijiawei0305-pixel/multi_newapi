# T01 — `viewport-mode` 视口断点与指针能力（纯核）

> **Wave 1** ｜ 依赖：**无** ｜ 被依赖：T02（仅类型）、T03（常量）、T05
> 设计契约：[`detailed-design.md`](../mobile-landing-detailed-design.md) §2.1、§4.1

---

## 目标

建立系统内**关于「屏幕尺寸档位」与「指针能力」的唯一真源**。全模块零依赖、零 DOM、零 `window`，可直接单测。

## 交付物

| 文件 | 说明 |
| --- | --- |
| `web/default/src/features/landing-react/viewport-mode.ts` | 🆕 约 60 行 |
| `web/default/src/features/landing-react/viewport-mode.test.ts` | 🆕 约 55 行 |

## 最小可执行任务（MET）

- [x] **M1** 定义并导出 `BREAKPOINTS = { mobile: 768, narrow: 400, shortViewport: 700 } as const`
  - 注释必须写明：`mobile` 与 `hooks/use-mobile.ts` 的私有常量 `MOBILE_BREAKPOINT` 同值，**两处不互相 import**（全站 hook 不得反向依赖单个 feature）
- [x] **M2** 定义并导出 `type PointerMode = 'hover' | 'touch'`、`type PointerModeInput = { hoverCapable?: boolean; maxTouchPoints?: number }`
- [x] **M3** 实现纯函数 `resolvePointerMode(input?: PointerModeInput): PointerMode`，按下表：

  | `hoverCapable` | `maxTouchPoints` | 返回 |
  | --- | --- | --- |
  | `true` | 任意 | `'hover'` |
  | `false` | 任意 | `'touch'` |
  | `undefined` | `> 0` | `'touch'` |
  | `undefined` | `0` / `undefined` | `'hover'`（保守回落 = 保持改造前行为） |

- [x] **M4** 实现纯函数 `pointerModeFromEvent(pointerType: string): PointerMode | null`

  | `pointerType` | 返回 | 理由 |
  | --- | --- | --- |
  | `'mouse'` | `'hover'` | 真实鼠标 |
  | `'touch'` | `'touch'` | 手指 |
  | `'pen'` | `'touch'` | 触控笔不保证抬笔触发 `mouseleave`，取保守值 |
  | 其他 / 空串 | `null` | 不足以判定，调用方维持当前模式 |

- [x] **M5** 实现 `detectPointerMode(): PointerMode` —— 采样 `matchMedia('(hover: hover)')` 与 `navigator.maxTouchPoints` 后转调 `resolvePointerMode`
  - 这是本模块**唯一**允许接触 `window` 的函数；形式对齐既有 `scene3d-quality.ts` 的 `pickRenderQuality`(纯) / `detectRenderQuality`(采样) 范式
  - `matchMedia` 不存在时（老环境）传 `hoverCapable: undefined`
- [x] **M6** 写 `viewport-mode.test.ts`，覆盖下方 ✅ 全部 9 条

## ✅ 验收标准

| # | 用例 | 断言 |
| --- | --- | --- |
| 1 | 有悬停能力 | `resolvePointerMode({hoverCapable:true}) === 'hover'` |
| 2 | 无悬停能力 | `resolvePointerMode({hoverCapable:false}) === 'touch'` |
| 3 | 特性缺失 + 有触点 | `resolvePointerMode({maxTouchPoints:5}) === 'touch'` |
| 4 | 特性缺失 + 无触点 | `resolvePointerMode({}) === 'hover'` |
| 5 | 鼠标校正 | `pointerModeFromEvent('mouse') === 'hover'` |
| 6 ★ | **手指校正** | `pointerModeFromEvent('touch') === 'touch'` —— 触屏笔电缺陷回归护栏 |
| 7 | 触控笔校正 | `pointerModeFromEvent('pen') === 'touch'` |
| 8 | 未知类型不校正 | `pointerModeFromEvent('') === null` |
| 9 | 主断点锁定 | `BREAKPOINTS.mobile === 768` |

四道门全绿：`bun run lint` / `bun run typecheck` / `bun run test` / `bun run source-size:check`

## 🚫 禁止

- 模块内出现 `document` / `window`（`detectPointerMode` 内除外）
- 导出 `isMobile()` 之类的宽度布尔函数——宽度判定归 CSS 媒体查询，JS 持有宽度状态会引入 hydration 闪烁与重复真源
- `import` 任何兄弟模块或 `@/hooks/*`
