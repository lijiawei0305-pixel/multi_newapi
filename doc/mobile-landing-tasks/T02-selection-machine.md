# T02 — `selection-machine` 卫星选中状态机（纯核）

> **Wave 2** ｜ 依赖：T01（**仅类型** `PointerMode`） ｜ 被依赖：T05
> 设计契约：[`detailed-design.md`](../mobile-landing-detailed-design.md) §2.2、§4.2

---

## 目标

用一个纯 reducer 统一「桌面悬停选中」与「触屏点击选中」两条路径，**根治 R7**——现有实现在触屏上点开卫星信息卡后会永久卡死。

## 背景：要治的缺陷（必读）

```
现状（scene3d.ts:700–704，每帧执行）：
    selected = nextSelection(selected, hoverKey, pointerInCanvas)

触屏时序：
  tap  → 合成 mousemove  → pointerInCanvas = true
       → 合成 mouseenter → hoverKey = 'openai'
  抬指 → ❌ 无 mouseleave → hoverKey 恒为 'openai'
       → ❌ 无 mousemove  → pointerInCanvas 恒为 true
  ⇒ 每帧 nextSelection('openai','openai',true) === 'openai' → 永不释放
```

**关键判断**：这**不是** `nextSelection` 的 bug——它的迟滞语义对鼠标是正确的。问题是触屏根本不该走这条路径。因此按 `PointerMode` **分流**，**严禁**修改 hover 语义。

## 交付物

| 文件 | 说明 |
| --- | --- |
| `web/default/src/features/landing-react/selection-machine.ts` | 🆕 约 60 行 |
| `web/default/src/features/landing-react/selection-machine.test.ts` | 🆕 约 70 行 |

> ⚠️ **本任务不删除 `scene3d-interaction.ts`**。此刻 `scene3d.ts:49` 仍在 import 它，删了会断 typecheck。删除动作在 **T05** 接线完成后执行。本阶段两处测试并存（重复覆盖无害）。

## 最小可执行任务（MET）

- [ ] **M1** 定义类型：

```ts
import type { PointerMode } from './viewport-mode'

export type SelectionState = { readonly selected: string | null }

export type SelectionEvent =
  | { type: 'hover-tick'; hoverKey: string | null; pointerInside: boolean }
  | { type: 'tap-chip'; key: string }
  | { type: 'tap-outside' }
  | { type: 'close' }

export const INITIAL_SELECTION: SelectionState = { selected: null }
```

- [ ] **M2** 实现 `reduce(state, event, mode): SelectionState`，严格按下方**完备迁移表**
- [ ] **M3** 落实**恒等性保证**：结果 `selected` 与入参相同时，**返回同一个对象引用**（调用方靠 `next !== state` 零成本判变化）
- [ ] **M4** 把 `scene3d-interaction.ts` 的 `nextSelection` 逻辑**搬迁**为 `reduce` 的 hover 分支实现（可作为文件内私有 helper，不对外导出）
- [ ] **M5** 写 `selection-machine.test.ts`，覆盖 ✅ 全部 14 条（含从 `scene3d-interaction.test.ts` 等价迁移的 5 条）

## 完备迁移表（实现依据）

| mode | event | 条件 | 新 `selected` |
| --- | --- | --- | --- |
| `hover` | `hover-tick` | `pointerInside === false` | `null` |
| `hover` | `hover-tick` | `pointerInside && hoverKey !== null` | `hoverKey` |
| `hover` | `hover-tick` | `pointerInside && hoverKey === null` | **不变**（迟滞：命中空处不松手） |
| `hover` | `tap-chip` / `tap-outside` / `close` | — | **不变** |
| `touch` | `hover-tick` | — | **不变** ★ R7 根治点 |
| `touch` | `tap-chip` | — | `event.key` |
| `touch` | `tap-outside` | — | `null` |
| `touch` | `close` | — | `null` |

> **完备性是硬要求**：每个 `(mode, event)` 组合都必须有确定结果。宿主正是依赖这一点才敢**无条件双绑**监听器、才敢在运行时切换 `mode` 而不重绑（见 §2.2.4-bis）。

## ✅ 验收标准

| 组 | 用例 | 断言 |
| --- | --- | --- |
| hover（迁移） | 命中即锁定 | `hover-tick('openai', true)` → `'openai'` |
| hover（迁移） | 不同命中即切换 | `'openai'` → `'gemini'` |
| hover（迁移） | 命中空处保持锁定 | `hover-tick(null, true)` → 不变 |
| hover（迁移） | 离开画布即清空 | `hover-tick(null, false)` → `null` |
| hover（迁移） | 离开优先于命中 | `hover-tick('gemini', false)` → `null` |
| touch | tap 卫星即选中 | `tap-chip('openai')` → `'openai'` |
| touch | tap 另一卫星即切换 | → `'gemini'` |
| touch | tap 外部即关闭 | `tap-outside` → `null` |
| touch | close 即关闭 | `close` → `null` |
| **隔离** ★ | **touch 忽略 hover-tick** | `reduce(s, hover-tick(...), 'touch') === s`（**同引用**） |
| 隔离 | hover 忽略 tap 事件 | `reduce(s, tap-chip, 'hover') === s` |
| 恒等 | 无变化返回同引用 | `reduce(s,e,m) === s` |
| 恒等 | 有变化返回新引用 | `reduce(s,e,m) !== s` |
| 幂等 | 重复施加结果稳定 | `reduce(reduce(s,e,m),e,m) === reduce(s,e,m)` |

四道门全绿。

## 🚫 禁止

- 修改 hover 分支的既有语义（迟滞规则必须逐字保留，否则桌面端行为改变 → 违反 M1）
- 在 reduce 内出现任何副作用（DOM、`console`、时间、随机）
- 本任务内删除 `scene3d-interaction.ts`（见上方 ⚠️）
- `import` `viewport-mode` 的**运行时**成员（只允许 `import type`）
