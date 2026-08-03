# 主站落地页移动端改造 — 详细设计文档

> **文档定位**：`doc/mobile-landing-proposal.md`（需求/方案）的**实现级详细设计**。定义模块边界、TypeScript 接口契约、状态机迁移、数据流时序与单测矩阵。
> **与其他文档的关系**：
> - 上游需求 → [`doc/mobile-landing-proposal.md`](mobile-landing-proposal.md)（现状实测、根因 R1–R12、决策 D1–D11、硬约束 M1–M5、完整 CSS 草稿）
> - 平台级设计 → [`doc/detailed-design.md`](detailed-design.md)（后端 15 个 Go 模块，**本文件不涉及**）
> - 前端总纲 → [`doc/frontend.md`](frontend.md)
> **范围**：仅 `web/default/src/features/landing-react/` 及其 2 个外围挂载点。**不涉及任何后端、接口、数据库。**
> **状态**：待确认。本轮**不动代码**。
> **日期**：2026-08-03

---

## 目录

1. [架构总览](#1-架构总览)
2. [模块详细设计](#2-模块详细设计)
3. [端到端关键数据流](#3-端到端关键数据流)
4. [模块独立性与测试矩阵](#4-模块独立性与测试矩阵)
5. [目录布局与文件级改动账](#5-目录布局与文件级改动账)
6. [横切实现细节](#6-横切实现细节)
7. [待确认项](#7-待确认项)

---

## 1. 架构总览

### 1.1 设计立意：Imperative Shell / Functional Core

落地页现状是一个 **753 行的命令式模块** `scene3d.ts`——three.js 渲染、DOM 芯片定位、事件监听、选中状态、HUD 文案填充全部混在一个闭包里。其中只有 3 处已被抽成纯函数（`pickRenderQuality` / `nextSelection` / config 常量），且都配了零 mock 单测——**这个先例就是本次设计要推广的模式**。

本设计把落地页切成两层：

| 层 | 特征 | 可测性 |
| --- | --- | --- |
| **纯核（Functional Core）** | 零 I/O、零 DOM、零 `window`、输入→输出确定 | **零 mock 直接单测** |
| **副作用宿主（Imperative Shell）** | 读 DOM / 派事件 / 调 three.js / 改 class | 不单测，靠 E2E 与人工验收 |

改造的核心动作是：**把新增的判断逻辑全部放进纯核，宿主只剩「采样 → 交给纯核 → 按结果改 DOM」三步**。

### 1.2 依赖方向图

```
                     ┌──────────────────────────────┐
                     │  index.tsx（React 外壳）       │
                     │  · 渲染 DOM 骨架               │
                     │  · 注入 3 张样式表             │
                     │  · 派发 wd-hud-close 事件      │
                     └───────┬──────────────────────┘
                             │ 注入（纯数据）
        ┌────────────────────┼────────────────────┐
        ▼                    ▼                    ▼
  landing-css.ts      sections-css.ts     landing-mobile-css.ts 🆕
   （桌面样式）         （三屏样式）          （移动端样式）
                                                  │ import 常量
                                                  ▼
                     ┌──────────────────────────────┐
                     │  viewport-mode.ts 🆕（纯核）   │◄──┐
                     │  BREAKPOINTS / PointerMode   │   │
                     └──────────────┬───────────────┘   │
                                    │ 仅类型              │
                                    ▼                    │
                     ┌──────────────────────────────┐   │
                     │  selection-machine.ts 🆕（纯核）│   │
                     │  reduce(state, event, mode)  │   │
                     └──────────────┬───────────────┘   │
                                    │                    │
                     ┌──────────────▼───────────────┐   │
                     │  scene3d.ts（副作用宿主）       ├───┘
                     │  · three.js 渲染               │
                     │  · DOM 芯片定位                │
                     │  · 事件采样 → 派给纯核          │
                     │  · 按 selected 改 DOM/CSS 变量  │
                     └──────────────┬───────────────┘
                                    │
        ┌───────────────────────────┼───────────────────────┐
        ▼                           ▼                       ▼
  scene3d-quality.ts        scene3d-config.ts        scene3d-assets.ts
   （纯核·既有）              （纯数据·既有）            （既有）
```

**依赖规则（硬性）**

1. 箭头**只向下**。纯核**永不** import 宿主。
2. 纯核之间**只允许类型依赖**（`import type`），不允许运行时依赖——唯一例外是 `landing-mobile-css.ts` import `BREAKPOINTS` 常量（纯数据，无副作用）。
3. 纯核**不得**出现 `window` / `document` / `matchMedia` / `navigator` 字样。能力采样一律由宿主完成后作为参数传入。
4. `scene3d-interaction.ts` **被 `selection-machine.ts` 吸收后删除**（详见 §2.2.5）。

### 1.3 继承自方案文档的约束

本设计不重新论证，直接继承 `mobile-landing-proposal.md` 的：

- **决策** D1–D11（§2）
- **硬约束** M1 桌面端零变化 / M2 不增内容元素 / M3 不引入新依赖 / M4 界面中文 / M5 单文件 ≤1500 行（§5.1）
- **根因** R1–R12（§4）——本设计对每个模块标注它治哪几条

其中 **M1 在本设计中从「人工守」升级为「机器守」**（§6.1）。

### 1.4 本设计相对方案文档的增量

| 方面 | 方案文档 §8 | 本设计 |
| --- | --- | --- |
| 文件数 | 6（新 1 / 改 5） | **13**（新 6 / 改 5 / 删 2） |
| 原因 | 按「最小侵入」估算，新逻辑内联在 `scene3d.ts` | 按「统一状态机 + 纯函数化」拆分，新增 3 个纯模块各配 1 个测试 |
| 单测覆盖 | 无新增 | 新增 3 个测试文件，**新逻辑 100% 零 mock 可测** |

> 这个差值是决策的直接后果，不是范围蔓延。若你希望回到 6 文件版，见 §7 待确认 #1。

---

## 2. 模块详细设计

### 2.1 `viewport-mode.ts` — 视口断点与指针能力（纯核）🆕

#### 2.1.1 职责

系统内**关于「屏幕尺寸档位」与「指针能力」的唯一真源**。两个消费者：`landing-mobile-css.ts`（生成媒体查询）与 `scene3d.ts`（决定交互路径）。

**治理根因**：R7（触屏误判）、以及「断点值散落在 CSS 各处、改一处漏一处」的隐患。

#### 2.1.2 接口契约

```ts
/** 断点单一真源。CSS 媒体查询与 JS 判定都从这里取值，杜绝两边漂移。 */
export const BREAKPOINTS = {
  /** 手机端主断点。与 hooks/use-mobile.ts 的私有常量 MOBILE_BREAKPOINT 同值（见 §7 #3）。 */
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

/** 纯判定：开局用的初值。零 I/O，可直接单测。 */
export function resolvePointerMode(input?: PointerModeInput): PointerMode

/** 从当前运行环境采样后调用 resolvePointerMode（本模块唯一接触 window 的函数，供宿主调用）。 */
export function detectPointerMode(): PointerMode

/**
 * 纯校正：从一次真实交互的 PointerEvent.pointerType 反推指针模式。
 * 返回 null 表示「该事件不足以判定，维持当前模式」。
 */
export function pointerModeFromEvent(pointerType: string): PointerMode | null
```

> `detectPointerMode` 是本模块**唯一**的非纯函数，其存在形式刻意对齐既有 `scene3d-quality.ts` 的 `pickRenderQuality`（纯）/ `detectRenderQuality`（采样）双函数范式。单测只测纯的那两个。

#### 2.1.3 开局判定规则（`resolvePointerMode`，完备表）

| `hoverCapable` | `maxTouchPoints` | 结果 | 说明 |
| --- | --- | --- | --- |
| `true` | 任意 | `'hover'` | 鼠标/触控板；**混合设备（触屏笔电）也落此档**——但会被 §2.1.4 的运行时校正纠正 |
| `false` | 任意 | `'touch'` | 手机/平板 |
| `undefined` | `> 0` | `'touch'` | 老浏览器回落 |
| `undefined` | `0` / `undefined` | `'hover'` | 老浏览器回落，保守取桌面路径（等于不改变现状） |

**回落方向的选择理由**：任何判定不确定的情况都落到 `'hover'`，即**保持改造前的行为**。这样 M1「桌面端零变化」在能力探测失败时依然成立。

#### 2.1.4 运行时校正规则（`pointerModeFromEvent`）★

**要解决的问题**：触屏笔电（Surface / 触屏 Win 本）上 `matchMedia('(hover: hover)')` 返回 `true`，开局判为 `'hover'`。但用户若用**手指**点卫星，浏览器仍会合成 `mouseenter` 且永不触发 `mouseleave` → 与手机端**同一个卡死缺陷**（R7 的第二个入口）。仅靠开局的媒体特性判定无法覆盖这类设备。

**解法**：开局值只是初值，**首次及后续每次真实交互时按 `PointerEvent.pointerType` 校正**。

| `pointerType` | 结果 | 理由 |
| --- | --- | --- |
| `'mouse'` | `'hover'` | 真实鼠标，悬停语义成立 |
| `'touch'` | `'touch'` | 手指，走 tap 路径 |
| `'pen'` | `'touch'` | 触控笔虽有悬停能力，但**不保证**抬笔时触发 `mouseleave`；取 `'touch'` 是保守选择——tap 路径永远有显式关闭手段，不可能卡死 |
| 其他 / 空串 | `null` | 不足以判定，维持当前模式 |

**效果**：同一台设备上，用鼠标就是 hover 体验、用手指就是 tap 体验，且**可来回切换**。

**为什么这不破坏纯核**：`mode` 始终是 `reduce()` 的**入参**而非内部状态。宿主每次调用时传入当前值即可，`selection-machine` 一行都不用改，仍是纯函数、仍零 mock 可测。运行时可变性完全被隔离在宿主的一个局部变量里。

#### 2.1.4 边界

- **不**导出 `isMobile()` 之类的布尔便利函数——宽度判定由 CSS 媒体查询承担，JS 侧一旦持有宽度状态就会引入 hydration 闪烁与重复真源。
- **不**依赖 `hooks/use-mobile.ts`（方向相反：全站 hook 不应被单个 feature 反向依赖）。两处各持 `768` 字面量，由注释 + 单测断言钉住一致性。

---

### 2.2 `selection-machine.ts` — 卫星选中状态机（纯核）🆕

#### 2.2.1 职责

统一桌面「悬停选中」与触屏「点击选中」两条路径，产出唯一的 `selected: string | null`。HUD 抽屉的开合、标题渐变换色、轨道缓停全部由它派生。

**治理根因**：**R7**——现有实现在触屏上会永久卡死。

#### 2.2.2 现状缺陷的机理（要治的东西）

```
现状（scene3d.ts:700–704，每帧执行）：
    selected = nextSelection(selected, hoverKey, pointerInCanvas)

其中 pointerInCanvas 只由 onMouseMove 写入（scene3d.ts:455–459），
     hoverKey    只由 .chip 的 mouseenter/mouseleave 写入（:381–386）。

触屏时序：
  tap  → 浏览器合成一次 mousemove → pointerInCanvas = true
       → 合成 mouseenter        → hoverKey = 'openai'
  抬指 → ❌ 不产生 mouseleave     → hoverKey 恒为 'openai'
       → ❌ 不产生 mousemove      → pointerInCanvas 恒为 true

  于是每帧 nextSelection('openai', 'openai', true) === 'openai' → 永不释放。
  即便手动置 hoverKey = null，nextSelection('openai', null, true) 仍返回 'openai'
  （「命中空处不松手」的迟滞规则），依然锁死。
```

**结论**：这不是 `nextSelection` 的 bug——它的迟滞语义对鼠标是正确的。问题是**触屏根本不该走这条路径**。因此设计上按 `PointerMode` 分流，而不是修改 hover 语义。

#### 2.2.3 接口契约

```ts
import type { PointerMode } from './viewport-mode'

/** 状态极简：只有「当前选中哪颗卫星」。其余（抽屉开合、配色、轨道缓停）全部由它派生。 */
export type SelectionState = { readonly selected: string | null }

export type SelectionEvent =
  /** 桌面路径：每帧上报一次悬停采样（保持与改造前逐帧重算的语义完全等价） */
  | { type: 'hover-tick'; hoverKey: string | null; pointerInside: boolean }
  /** 触屏路径：点中某颗卫星 */
  | { type: 'tap-chip'; key: string }
  /** 触屏路径：点在抽屉与卫星之外的任意处 */
  | { type: 'tap-outside' }
  /** 触屏路径：点抽屉右上角关闭按钮（来自 React 的 wd-hud-close 事件） */
  | { type: 'close' }

export const INITIAL_SELECTION: SelectionState = { selected: null }

/**
 * 纯迁移函数。
 * 恒等性保证：若结果与入参 state 的 selected 相同，返回**同一个对象引用**，
 * 调用方可用 `next !== state` 零成本判断是否需要触发副作用。
 */
export function reduce(
  state: SelectionState,
  event: SelectionEvent,
  mode: PointerMode
): SelectionState
```

#### 2.2.4 迁移表（完备）

| mode | event | 条件 | 新 `selected` | 说明 |
| --- | --- | --- | --- | --- |
| `hover` | `hover-tick` | `pointerInside === false` | `null` | 离开画布即清空 |
| `hover` | `hover-tick` | `pointerInside && hoverKey !== null` | `hoverKey` | 命中即锁定 / 切换 |
| `hover` | `hover-tick` | `pointerInside && hoverKey === null` | **不变** | 迟滞：命中空处不松手（缩小让位时不抽搐） |
| `hover` | `tap-chip` / `tap-outside` / `close` | — | **不变** | 桌面不派发这些事件；防御性忽略 |
| `touch` | `hover-tick` | — | **不变** | ★ **R7 根治点**：触屏合成的 mousemove/mouseenter 一律不影响选中 |
| `touch` | `tap-chip` | — | `event.key` | 点中即选中 / 直接切换 |
| `touch` | `tap-outside` | — | `null` | 点抽屉外关闭 |
| `touch` | `close` | — | `null` | 点 × 关闭 |

**不变式**（可作为单测断言）：

- I1：`reduce(s, e, 'hover')` 的结果**只**取决于 `hover-tick` 的载荷 → 桌面行为与改造前逐行等价。
- I2：`reduce(s, {type:'hover-tick', ...}, 'touch') === s`（同引用）→ 触屏对合成鼠标事件完全免疫。
- I3：任意 `mode` 下，`reduce(reduce(s, e, m), e, m) === reduce(s, e, m)`（幂等）。
- I4：`selected` 的取值域 ⊆ `{null} ∪ Object.keys(MODELS)`（由调用方保证 key 合法，reduce 不校验）。

#### 2.2.4-bis 迁移表的完备性 = 运行时可切换的前提 ★

上表对**每一个 (mode, event) 组合**都给出了确定结果，包括「本模式下不该出现的事件」（一律返回同引用）。这个完备性带来一个关键推论：

> **宿主无需按 mode 条件绑定监听器。** 两条路径的监听可以**无条件同时绑定**，由 `reduce` 按当前 `mode` 过滤——不该生效的事件自动被吞掉且零副作用（同引用 → 宿主快路径 return）。

因此 §2.1.4 的运行时模式切换**不需要重绑任何监听器**，只需改宿主的一个局部变量。这让动态切换的实现成本降到近乎为零，也消除了「重绑时机与事件竞态」这一整类 bug。

#### 2.2.5 对既有 `scene3d-interaction.ts` 的处理

现状：`nextSelection(current, hitKey, pointerInside)` 纯函数 + 3 个测试用例，**生产引用只有 `scene3d.ts:49` 一处**（已用 `grep` 核实）。

处理方式：**逻辑搬入 `selection-machine.ts` 作为 `hover` 分支实现，文件删除，3 个测试用例等价迁移**。

| 原用例（`scene3d-interaction.test.ts`） | 迁移后（`selection-machine.test.ts`） |
| --- | --- |
| `nextSelection(null,'openai',true) === 'openai'` | `reduce({selected:null},{type:'hover-tick',hoverKey:'openai',pointerInside:true},'hover').selected === 'openai'` |
| `nextSelection('openai','gemini',true) === 'gemini'` | 同构，断言切换 |
| `nextSelection('openai',null,true) === 'openai'` | 同构，断言迟滞不松手 |
| `nextSelection('openai',null,false) === null` | 同构，断言离开画布清空 |
| `nextSelection('openai','gemini',false) === null` | 同构，断言离开优先于命中 |

**零覆盖损失**：迁移后 hover 语义的 5 条断言全部保留，另加触屏与跨模式隔离的新用例。

---

### 2.3 `landing-mobile-css.ts` — 移动端样式表（纯数据）🆕

#### 2.3.1 职责

导出一个字符串常量 `MOBILE_CSS`，由 `index.tsx` 拼在 `LANDING_CSS + SECTIONS_CSS` **之后**注入。

**治理根因**：R1–R6、R8–R12（除 R7 外的全部排版类根因）。

#### 2.3.2 接口契约

```ts
import { BREAKPOINTS } from './viewport-mode'

/**
 * 移动端样式。全部规则包在媒体查询内，宽屏一行不生效（M1）。
 * 注入顺序排在 LANDING_CSS + SECTIONS_CSS 之后，靠**源码顺序**覆盖同权重规则，
 * 全文件不使用 !important（由 landing-mobile-css.test.ts 强制）。
 */
export const MOBILE_CSS = `…`
```

#### 2.3.3 内部结构（4 个媒体查询块）

| 块 | 条件 | 覆盖 |
| --- | --- | --- |
| B1 主块 | `(max-width: ${BREAKPOINTS.mobile}px)` | 顶栏 / Hero 竖排 / 文字 / 卡片 / CTA / HUD 抽屉 / FAB / 三屏收敛 / 滚动流畅度 |
| B2 窄屏 | `(max-width: ${BREAKPOINTS.narrow}px)` | 字号再收一档，保 360px 单行 |
| B3 矮屏 | `(max-width: ${mobile}px) and (max-height: ${shortViewport}px)` | 降低 3D 区高度占比 |
| B4 触屏 | `(hover: none)` | 中和粘滞 hover 态（R11） |

主体 CSS 见 `mobile-landing-proposal.md` §9，**本文件不重复抄录**。

#### 2.3.4 相对方案 §9 草稿的增补（2 条）

设计阶段新识别的两处冲突，需在 B1 块内补入：

```css
/* ① 抽屉升起时隐藏客服 FAB —— 两者都在屏底，会重叠且 FAB 紧邻 × 按钮易误点。
      .lp-fab 是 .hero-stage 的「前序兄弟」，纯 CSS 选不到 #hud.show，
      故由 onSelectChange 在 .wd-landing-root 上切 wd-hud-open 类（§2.4.2 #5）。 */
.wd-landing-root.wd-hud-open .lp-fab {
  opacity: 0;
  pointer-events: none;
  transform: translateY(8px);
}

/* ② 抽屉本体需要可交互（桌面端 #hud 是 pointer-events:none 的纯展示层） */
#hud { pointer-events: auto; }
```

> ① 特意**不用** `:has()`（`.landing-scene:has(#hud.show) .lp-fab`）。虽然 `:has()` 的浏览器支持面比页面已依赖的 `@property` 还宽，但 class 钩子方案零浏览器风险，且 `onSelectChange` 本就在做 class 与 CSS 变量的切换，多一行是同构的，不引入新范式。

#### 2.3.4 为什么样式是「模块」而不是散落的媒体查询

三个理由，都指向可验证性：

1. **M1 可机器化**：所有移动端规则集中在一个字符串里，契约测试才能断言「不存在媒体查询之外的规则」。散落到各文件后这个断言无从写起。
2. **回滚成本 = 一个 token**：从 `index.tsx` 的 `<style>` 里删掉 `+ MOBILE_CSS` 即完全回退。
3. **diff 可读**：review 时「哪些是本次移动端改动」一目了然，不必在 359 行的 `landing-css.ts` 里逐行辨认。

---

### 2.4 `scene3d.ts` — 副作用宿主（既有，职责收窄）✏️

#### 2.4.1 收窄后的职责

| 保留 | 移出 |
| --- | --- |
| three.js 场景构建 / 渲染循环 | ~~选中判定逻辑~~ → `selection-machine` |
| DOM 芯片创建与逐帧投影定位 | ~~设备能力判定~~ → `viewport-mode` |
| 事件监听与采样 → 派给纯核 | |
| 按 `selected` 变化改 DOM（class / CSS 变量 / HUD 文案） | |
| 监听器生命周期与 cleanup | |

#### 2.4.2 改动点（8 处，**仅新增 1 个监听器**）

| # | 位置 | 改动 | 治 |
| --- | --- | --- | --- |
| 1 | import 段（`:49`） | `nextSelection` → `reduce, INITIAL_SELECTION, type SelectionEvent`；新增 `detectPointerMode, pointerModeFromEvent` | — |
| 2 | 初始化段（`:512`） | `let pointerMode = detectPointerMode()`（**`let`**——运行时可校正，见 §2.1.4）；`let selection = INITIAL_SELECTION` 取代 `let selected: string \| null = null` | R7 |
| 3 | 芯片事件绑定（`:381–386`） | **无条件双绑**：保留原 `mouseenter`/`mouseleave`，**并增绑** `click` → `e.stopPropagation(); dispatch({type:'tap-chip', key})`。**无 `if` 分支**（依据 §2.2.4-bis） | R7 |
| 4 | `setHud`（`:526`） | `box.style.borderLeftColor = m.color` → `box.style.setProperty('--hud-accent', m.color)` | R8 |
| 5 | `onSelectChange`（`:530–553`） | 两个分支各加 1 行：`varsEl.classList.toggle('wd-hud-open', !!key)`（复用既有 `varsEl` 引用，`:505`） | FAB 碰撞 |
| 6 | 渲染循环（`:700–704`） | 改为 `dispatch({type:'hover-tick', hoverKey, pointerInside: pointerInCanvas})`；`speedFactor` 一行留在循环内（触屏选中同样应缓停轨道） | R7 |
| 7 | 既有 window capture `pointerdown`（`:464–478`） | **扩展**（非新增）：① 首先按 `e.pointerType` 校正 `pointerMode` ② 既有 surge 逻辑原样保留 ③ 末尾判 `tap-outside` | R7 |
| 8 | IntersectionObserver 回调（`:598–606`） | 既有回调里 `heroVisible` 转 `false` 时补一句 `dispatch({type:'close'})` | 滚出首屏残留 |
| 9 | 事件注册 / cleanup（`:478` / `:737`） | 新增并成对移除 `window` 级 `wd-hud-close` 监听——**本次唯一新增的监听器** | R7 |

> 相比初版设计，这一版**少了一个监听器、少了一处条件绑定**：`tap-outside` 复用既有的 window capture `pointerdown`，自动关闭复用既有的 IntersectionObserver。这是 §2.2.4-bis「迁移表完备 → 无需条件绑定」带来的直接简化。

#### 2.4.3 新增内部函数 `dispatch`

宿主内唯一与纯核交互的收口：

```ts
function dispatch(event: SelectionEvent) {
  const next = reduce(selection, event, pointerMode)   // pointerMode 每次读当前值
  if (next === selection) return          // 依赖 §2.2.3 的恒等性保证，零分配快路径
  selection = next
  onSelectChange(selection.selected)
}
```

`onSelectChange`（`:530–553`）的既有 12 行**全部保留原样**，只在两个分支各追加 1 行 class 切换（改动点 #5）。本次设计改的是「谁来决定 selected」，几乎不改「变化后做什么」——这是把改动面压到最小的关键。

#### 2.4.4 扩展既有 window capture `pointerdown`

```ts
const onPointerDown = (e: PointerEvent) => {
  if (!(window as any).__scene3dActive) return

  // ① 运行时校正指针模式（§2.1.4）——必须最先执行，后续 dispatch 才用得上新值
  const corrected = pointerModeFromEvent(e.pointerType)
  if (corrected) pointerMode = corrected

  // ② 既有：点击画布中心触发浪涌 —— 原样保留
  const r = canvas.getBoundingClientRect()
  const nx = ((e.clientX - r.left) / r.width) * 2 - 1
  const ny = -((e.clientY - r.top) / r.height) * 2 + 1
  if (nx * nx + ny * ny < 0.5 * 0.5) surge()

  // ③ 新增：点在卫星与抽屉之外 → 关闭（hover 模式下被 reduce 吞掉，无害）
  const t = e.target as HTMLElement | null
  if (!t?.closest('.chip') && !t?.closest('#hud')) {
    dispatch({ type: 'tap-outside' })
  }
}
```

**事件顺序的正确性**（已逐条推演）：

| 用户动作 | 事件序列 | 结果 |
| --- | --- | --- |
| 点卫星 | window-capture `pointerdown`（`target` 是 `.chip` → 跳过 ③）→ chip `click` → `tap-chip` | 抽屉打开 ✅ |
| 点抽屉外空白 | window-capture `pointerdown`（③ 命中）→ `tap-outside` | 抽屉关闭 ✅ |
| 点抽屉内 × | window-capture `pointerdown`（`target` 在 `#hud` 内 → 跳过 ③）→ React `onClick` → `CustomEvent` → `close` | 抽屉关闭 ✅ |
| 点另一颗卫星 | 同「点卫星」，`tap-chip` 直接切换 | 无闪烁 ✅ |

capture 阶段先于目标元素触发，因此 ① 的模式校正**必定**早于同一次交互产生的任何 `dispatch`。

**`__scene3dActive` 守卫的影响**（已核实）：该标志只在初始化（`:715`）置 `true`、cleanup（`:751`）置 `false`，**不随滚动或标签页可见性变化**（那两个由 `heroVisible`/`pageVisible` 单独控制、只作用于 rAF 循环）。因此用户滚到页面下方时它仍为 `true`，`tap-outside` 正常工作。

**与页面滚动的关系**：本函数与既有 surge 逻辑均不调用 `preventDefault()`，不阻断触摸滚动（验收 B5 实测确认）。选用 `pointerdown` 而非 `click`：滑动过程中 `click` 可能不触发，`pointerdown` 更跟手。

#### 2.4.5 滚出首屏自动关闭

手机端抽屉是 `position:fixed`（钉在屏底），若不处理，用户点开后往下滑会让它一路盖住下方三屏的底部。桌面端不存在此问题——那边 `#hud` 是 `position:absolute`，随页面自然滚走。

复用**既有**的 IntersectionObserver（`:598–606`，本来就在观察 `.hero-stage` 是否可见）：

```ts
io = new IntersectionObserver((es) => {
  heroVisible = es[0]?.isIntersecting ?? false
  if (!heroVisible) dispatch({ type: 'close' })   // ← 新增这一行
  ...既有逻辑...
}, ...)
```

- **零新增监听器**。
- 语义与桌面端**一致**：都是「离开首屏，卡片就没了」。
- **桌面端不受影响**：迁移表规定 `'hover'` 模式忽略 `close`（返回同引用）→ `dispatch` 走快路径 return，桌面行为逐帧不变。这一行只对 `'touch'` 模式生效，M1 依然成立——**无需额外验证，由 §2.2.4 的完备迁移表直接保证**。

---

### 2.5 `index.tsx` — React 外壳（既有，2 处改动）✏️

#### 2.5.1 职责

渲染静态 DOM 骨架、注入样式表、桥接「关闭按钮 → 宿主」。**不持有任何布局状态**。

#### 2.5.2 改动点

**① 注入移动端样式**

```diff
+import { MOBILE_CSS } from './landing-mobile-css'
-      <style>{LANDING_CSS + SECTIONS_CSS}</style>
+      <style>{LANDING_CSS + SECTIONS_CSS + MOBILE_CSS}</style>
```

**② HUD 关闭按钮**（`<aside id='hud'>` 内，唯一的 DOM 新增，D8 已批准）

```diff
       <aside id='hud'>
+        <button
+          type='button'
+          className='hud-close'
+          aria-label={t('Close')}
+          onClick={() => window.dispatchEvent(new CustomEvent('wd-hud-close'))}
+        >
+          ×
+        </button>
```

#### 2.5.3 为什么用 CustomEvent 而不是 `window.__scene3d.clearSelection()`

| 方案 | 问题 |
| --- | --- |
| `window.__scene3d.clearSelection()` | `__scene3d` 类型是 `any`，React 侧调用需 `(window as any)`，触碰 `no-explicit-any`；且 React 与宿主产生了具名耦合 |
| React state 提升 + props 下传 | 宿主是命令式闭包、不受 React 调度，需要新增 ref 桥 + `useEffect` 同步，复杂度远超收益 |
| **`CustomEvent('wd-hud-close')`** ✅ | 零 `any`、零具名耦合、宿主 cleanup 可干净移除、React 侧是一行无 state 的事件派发 |

#### 2.5.4 无障碍

- `aria-label={t('Close')}` 复用 New API 官方 locale 既有 key，**不新增 i18n 条目**（M2/M4）。
- 按钮为原生 `<button type="button">`，天然可聚焦、可键盘 `Enter`/`Space` 触发。
- 桌面端 `display:none`（写在 `landing-css.ts`），完全不进入无障碍树 → M1。

---

## 3. 端到端关键数据流

### 3.1 触屏点选卫星 → 抽屉升起

```
用户用手指 tap .chip[data-mid="openai"]
  │
  ├─【capture】window pointerdown  ← 既有监听，本次扩展（§2.4.4）
  │     ① pointerModeFromEvent('touch') → 'touch'
  │            pointerMode = 'touch'    ← ★ 运行时校正:触屏笔电在此刻被纠正
  │     ② surge() 判定（既有，命中画布中心才触发）
  │     ③ target.closest('.chip') 命中 → 跳过 tap-outside
  │
  ├─【target】chip.click            ← 无条件绑定（§2.2.4-bis)
  │     e.stopPropagation()
  │     dispatch({type:'tap-chip', key:'openai'})
  │
  ├─ selection-machine.reduce({selected:null}, tap-chip, 'touch')
  │     → {selected:'openai'}       ← 新对象引用 ≠ 旧
  │
  └─ scene3d.ts: next !== selection → onSelectChange('openai')
        ├─ setHud(MODELS.openai)
        │     · 5 个 textContent 填 i18n 文案
        │     · #hud.style.setProperty('--hud-accent', color)   ← 改动点 #4
        │     · .hud-dot.style.background = color
        ├─ #hud.classList.add('show')
        │     → CSS: translateY(100%) → translateY(0)   抽屉从屏底升起
        ├─ varsEl.classList.add('wd-hud-open')          ← 改动点 #5
        │     → CSS: .lp-fab 淡出且不可点              客服球让位
        ├─ .hero-right.classList.add('card-open')
        │     → 手机端 CSS 已中和其位移，桌面端仍横向让位
        ├─ setCoreColor(color)                          three.js 核心光晕换色
        └─ --wd-g1 / --wd-g2 / --wd-brand               标题渐变与页眉 logo 换色
```

**同一时刻，渲染循环仍在每帧派 `hover-tick`**——但 `'touch'` 模式下 `reduce` 返回同引用（不变式 I2）→ `dispatch` 快路径 return，零副作用、零分配。**这就是 R7 被根治的位置**：触屏合成的 `mouseenter` 再也影响不了 `selected`。

**每帧同时发生**：渲染循环仍在 `dispatch({type:'hover-tick', ...})`，但 `reduce` 在 `'touch'` 模式下**返回同引用**（不变式 I2）→ `next === selection` → 快路径直接 return，零副作用、零分配。**这正是 R7 被根治的地方**。

### 3.2 四条关闭路径归一

```
① 点 × 关闭
     .hud-close onClick (React)
       → window.dispatchEvent(new CustomEvent('wd-hud-close'))
       → scene3d.ts 监听 → dispatch({type:'close'})

② 点抽屉外空白
     window capture pointerdown（既有监听，§2.4.4 ③）
       → target 既不在 .chip 内也不在 #hud 内
       → dispatch({type:'tap-outside'})

③ 滚出首屏
     既有 IntersectionObserver（§2.4.5）
       → heroVisible 转 false
       → dispatch({type:'close'})

④ 点另一颗卫星
       → dispatch({type:'tap-chip', key:'gemini'})   ← 直接切换，无需先关闭

  ①②③ ─→ reduce(..., 'touch') → {selected:null}
       └─→ onSelectChange(null)
             ├─ #hud.classList.remove('show')     → translateY(100%) 沉下
             ├─ varsEl.classList.remove('wd-hud-open') → 客服 FAB 淡回
             ├─ .hero-right.classList.remove('card-open')
             ├─ setCoreColor(null)
             └─ 移除 --wd-brand / --wd-g1 / --wd-g2    → 标题渐变复原
```

四条路径**汇聚到同一个 `reduce` + 同一个 `onSelectChange`**——这是「高内聚」的具体含义：关闭这件事只有一处实现，不会出现「× 按钮关了但配色没复原」「滚走了但 FAB 还藏着」这类分叉 bug。

新增路径 ②③ **都复用既有监听器**，全流程只为路径 ① 新增了一个 `wd-hud-close` 监听。

### 3.3 桌面悬停（等价性证明）

```
改造前（scene3d.ts:700–704，每帧）：
    const prevSel = selected
    selected = nextSelection(selected, hoverKey, pointerInCanvas)
    if (selected !== prevSel) onSelectChange(selected)
    speedFactor = approach(speedFactor, selected ? 0 : 1, 3, dt)

改造后（每帧）：
    dispatch({ type:'hover-tick', hoverKey, pointerInside: pointerInCanvas })
    speedFactor = approach(speedFactor, selection.selected ? 0 : 1, 3, dt)

其中 dispatch 内部：
    const next = reduce(selection, event, 'hover')     // 'hover' 模式下 = nextSelection 原逻辑
    if (next === selection) return
    selection = next
    onSelectChange(selection.selected)
```

**逐条等价**：

| 改造前 | 改造后 | 等价 |
| --- | --- | --- |
| `nextSelection(selected, hoverKey, pointerInCanvas)` | `reduce(..., 'hover')` 的 hover-tick 分支（逻辑原样搬迁） | ✅ |
| `if (selected !== prevSel)` 字符串比较 | `if (next !== selection)` 引用比较（恒等性保证：selected 相同则同引用） | ✅ |
| `speedFactor` 每帧更新 | 同，读 `selection.selected` | ✅ |

**新增的无条件绑定为何不污染桌面**：

改动点 #3/#7/#8/#9 让桌面端也会派发 `tap-chip`（点卫星）、`tap-outside`（点空白）、`close`（滚出首屏）三类事件。按 §2.2.4 迁移表，`'hover'` 模式对这三类**一律返回同引用** → `dispatch` 首行 `if (next === selection) return` 命中 → **零副作用、零分配**。

| 桌面动作 | 新派发的事件 | hover 模式下 | 净效果 |
| --- | --- | --- | --- |
| 鼠标点击卫星 | `tap-chip` | 忽略（同引用） | 无（选中仍由 hover 决定） |
| 鼠标点击空白 | `tap-outside` | 忽略 | 无 |
| 滚出首屏 | `close` | 忽略 | 无 |
| 鼠标移动 | `hover-tick` | 生效（原逻辑） | 与改造前一致 |

**`pointerMode` 在桌面永不漂移**：唯一的校正入口是 `pointerModeFromEvent(e.pointerType)`，真实鼠标产生 `'mouse'` → 映射回 `'hover'`（§2.1.4）。纯鼠标设备上该变量恒为 `'hover'`。

> 综上，**桌面路径逐帧行为与改造前完全一致**——这是 M1 在逻辑层的论据；样式层的论据见 §6.1。

---

## 4. 模块独立性与测试矩阵

| 模块 | 依赖 | 可独立单测方式 | 重点用例 |
| --- | --- | --- | --- |
| `viewport-mode` | **无** | **纯函数零 mock** | `resolvePointerMode` 4 条开局规则 + `undefined` 回落到 `'hover'`；`pointerModeFromEvent` 4 条校正规则；`BREAKPOINTS` 值锁定 |
| `selection-machine` | `viewport-mode`（**仅类型**） | **纯函数零 mock** | hover 5 例（迁移）+ touch 4 例（新）+ 跨模式隔离 2 例 + 恒等性 2 例 + 幂等 1 例 |
| `landing-mobile-css` | `viewport-mode`（常量） | **字符串契约断言** | 零裸规则 / 零 `!important` / 断点值单一真源 / 括号平衡 |
| `scene3d-quality` | 无 | 纯函数零 mock（**既有**） | 既有 4 例，不动 |
| `scene3d-config` / `scene3d-assets` | 无 | 纯数据断言（**既有**） | 既有，不动 |
| `scene3d.ts` | 全部 | **不单测** | 命令式宿主，靠验收 B1–B7 与 E2E |
| `index.tsx` | — | **不单测** | 靠验收 A1–A7 截图 |

**结论**：本次新增的**全部逻辑**（设备判定、选中迁移、样式约束）都落在零依赖或仅类型依赖的纯模块里，**无需 DOM、无需 jsdom、无需 mock 即可 100% 覆盖**。命令式宿主 `scene3d.ts` 的新增代码被压缩到只剩「采样、派发、按结果改 DOM」，没有可判定的分支逻辑残留在其中。

### 4.1 `viewport-mode.test.ts` 用例清单

| 组 | 用例 | 断言 |
| --- | --- | --- |
| 开局判定 | 有悬停能力 | `resolvePointerMode({hoverCapable:true})` → `'hover'` |
| 开局判定 | 无悬停能力 | `resolvePointerMode({hoverCapable:false})` → `'touch'` |
| 开局判定 | 特性缺失 + 有触点 | `resolvePointerMode({maxTouchPoints:5})` → `'touch'` |
| 开局判定 | 特性缺失 + 无触点 | `resolvePointerMode({})` → `'hover'`（保守回落 = 保持改造前行为） |
| **运行时校正** ★ | 鼠标 | `pointerModeFromEvent('mouse')` → `'hover'` |
| **运行时校正** ★ | 手指 | `pointerModeFromEvent('touch')` → `'touch'`——**触屏笔电缺陷的回归护栏** |
| 运行时校正 | 触控笔 | `pointerModeFromEvent('pen')` → `'touch'`（保守，见 §2.1.4） |
| 运行时校正 | 未知类型不校正 | `pointerModeFromEvent('')` → `null` |
| 常量 | 主断点锁定 | `BREAKPOINTS.mobile === 768`（与 `hooks/use-mobile.ts` 对齐） |

### 4.2 `selection-machine.test.ts` 用例清单

| 组 | 用例 | 断言 |
| --- | --- | --- |
| hover（迁移） | 命中即锁定 | `hover-tick(hoverKey:'openai', inside:true)` → `'openai'` |
| hover（迁移） | 不同命中即切换 | 从 `'openai'` → `'gemini'` |
| hover（迁移） | 命中空处保持锁定 | `hover-tick(null, true)` → 不变 |
| hover（迁移） | 离开画布即清空 | `hover-tick(null, false)` → `null` |
| hover（迁移） | 离开优先于命中 | `hover-tick('gemini', false)` → `null` |
| touch | tap 卫星即选中 | `tap-chip('openai')` → `'openai'` |
| touch | tap 另一卫星即切换 | → `'gemini'` |
| touch | tap 外部即关闭 | `tap-outside` → `null` |
| touch | close 事件即关闭 | `close` → `null` |
| **跨模式隔离** ★ | **touch 模式忽略 hover-tick** | `reduce(s, hover-tick(...), 'touch') === s`（**同引用**）——R7 回归护栏 |
| 跨模式隔离 | hover 模式忽略 tap 事件 | `reduce(s, tap-chip, 'hover') === s` |
| 恒等性 | 无变化时返回同引用 | `reduce(s, e, m) === s` |
| 恒等性 | 有变化时返回新引用 | `reduce(s, e, m) !== s` |
| 幂等 | 同事件重复施加结果稳定 | `reduce(reduce(s,e,m),e,m) === reduce(s,e,m)` |

> ★ 标记的那条是**本次改造最重要的一条测试**：它把「触屏永久卡死」这个已实测的缺陷钉成了永久回归护栏。

### 4.2 `landing-mobile-css.test.ts` 用例清单

| 用例 | 断言 | 守住什么 |
| --- | --- | --- |
| 零裸规则 | 剥掉所有 `@media(...){...}` 块后，剩余内容仅含空白与注释 | **M1 桌面端零变化** |
| 零 `!important` | `!MOBILE_CSS.includes('!important')` | 覆盖策略靠源码顺序而非权重战争 |
| 断点单一真源 | 所有 `max-width:\s*(\d+)px` 的值 ∈ `{BREAKPOINTS.mobile, BREAKPOINTS.narrow}` | 断点不漂移 |
| 矮屏断点一致 | 所有 `max-height:\s*(\d+)px` === `BREAKPOINTS.shortViewport` | 同上 |
| 与全站断点一致 | `BREAKPOINTS.mobile === 768` | 与 `hooks/use-mobile.ts` 对齐（§7 #3） |
| 括号平衡 | `{` 与 `}` 计数相等 | 防模板字符串拼出坏 CSS |

---

## 5. 目录布局与文件级改动账

```text
web/default/src/features/landing-react/
├─ index.tsx                     ✏️ 2 处（注入 MOBILE_CSS / .hud-close 按钮）
├─ landing-css.ts                ✏️ 2 处（--hud-accent 变量化 / .hud-close 桌面隐藏）
├─ sections-css.ts               ✏️ 1 处（560 断点补 grid-auto-rows:auto）
├─ landing-mobile-css.ts         🆕 移动端样式表          （~215 行）
├─ landing-mobile-css.test.ts    🆕 CSS 契约测试          （~55 行）
├─ viewport-mode.ts              🆕 断点常量 + 指针能力（纯）（~60 行）
├─ viewport-mode.test.ts         🆕                      （~55 行）
├─ selection-machine.ts          🆕 选中状态机（纯）        （~60 行）
├─ selection-machine.test.ts     🆕                      （~70 行）
├─ scene3d.ts                    ✏️ 9 处（见 §2.4.2）
├─ scene3d-interaction.ts        ❌ 删除（逻辑并入 selection-machine）
├─ scene3d-interaction.test.ts   ❌ 删除（5 条用例等价迁移）
├─ scene3d-quality.ts / .test.ts      ✅ 不动
├─ scene3d-config.ts / .test.ts       ✅ 不动
├─ scene3d-assets.ts / .test.ts       ✅ 不动
├─ sections.tsx / kefu-modal.tsx      ✅ 不动
└─ use-reveal.ts                      ✅ 不动

web/default/src/features/home/
└─ index.tsx                     ✏️ 1 处（Footer 包裹层加 pb-20 md:pb-0）
```

**合计 13 个文件**：新增 6 / 修改 5 / 删除 2。
最大新增文件 210 行，远低于 `source-size:check` 的 1500 行预算（M5）。

---

## 6. 横切实现细节

### 6.1 M1「桌面端零变化」的三重保证

| 层 | 手段 | 验证 |
| --- | --- | --- |
| **样式层** | 全部新规则包在媒体查询内 | `landing-mobile-css.test.ts` 的「零裸规则」用例——**CI 可拦截** |
| **逻辑层** | 桌面 `pointerMode === 'hover'`，`reduce` 的 hover 分支逐条等价于 `nextSelection`（§3.3） | `selection-machine.test.ts` 迁移的 5 条 hover 用例 |
| **既有文件改写** | `landing-css.ts` 仅 2 处，且均渲染等价：`#00f0ff` → `var(--hud-accent,#00f0ff)`（回退同色）、新增 `.hud-close{display:none}`（桌面不渲染） | 人工 diff review + 1440×900 逐像素截图比对（验收 C1/C3） |

前两层是**机器守**，第三层是**人工守**——这正是从方案文档的「全靠人工守」升级过来的部分。

### 6.2 CSS 覆盖策略：源码顺序而非 `!important`

`MOBILE_CSS` 拼在 `LANDING_CSS + SECTIONS_CSS` 之后注入同一个 `<style>`。CSS 层叠规则中，**特异性相同时后声明者胜**，因此：

- 移动端规则只需写成与桌面规则**同等或更高**特异性即可覆盖，例如 `.hero-left`（同特异性）、`.wd-landing-root`（同特异性，用于覆盖 `--chip` 变量）。
- 全程**禁用 `!important`**（契约测试强制）。理由：一旦开了口子，后续维护者会级联使用，最终无法推理哪条规则生效。

⚠️ 需注意的少数高特异性既有规则：

| 既有规则 | 特异性 | 移动端覆盖写法 |
| --- | --- | --- |
| `.hero-right.card-open .hero-visual`（`landing-css.ts:289`） | `0,3,0` | 必须写足复合选择器 `.hero-right.card-open .hero-visual{transform:none}`，用 `.hero-visual` 覆盖不了 |
| `.svc-flagship .svc-card-desc`（`sections-css.ts:57`，`max-width:54ch`） | `0,2,0` | 必须同时列出 `.svc-card-desc, .svc-flagship .svc-card-desc{max-width:none}`，只写前者会漏掉旗舰卡 |
| `#sec-services` / `#sec-studio`（区块内边距） | `1,0,0` | 移动端须带 ID 写 `#sec-services{padding:56px 16px}`，用 `.svc-wrap` 之类的类选择器覆盖不了 |
| `#sec-services .svc-card`（`:74`，入场动画的 `opacity`/`transform`） | `0,1,1` | **无需覆盖**——移动端只改 `padding`，与该规则设置的属性不相交 |

> 前三条是实现时的实际陷阱（已比对既有源码行号确认）；第四条列出是为了说明「看起来高特异性、实则不冲突」的情况，避免实现时过度防御。

### 6.3 事件监听生命周期

`scene3d.ts` 的 `initScene3d` 返回一个 cleanup 闭包，由 `index.tsx` 的模块级单例（引用计数 + 延迟卸载，用于化解 StrictMode 双调用）在真正卸载时调用。本次新增的 2 个监听必须进同一个 cleanup：

| 监听 | 注册目标 | cleanup |
| --- | --- | --- |
| `pointerdown`（tap-outside） | `document` | `document.removeEventListener('pointerdown', onDocPointerDown)` |
| `wd-hud-close` | `window` | `window.removeEventListener('wd-hud-close', onHudClose)` |

⚠️ 芯片上的 `click` 监听绑在 `addedChips` 里的元素上，这些元素在 cleanup 时被整体 `remove()`（既有逻辑），随元素一起回收，无需单独解绑。

### 6.4 安全区与视口单位

- 所有贴边元素（顶栏上边、FAB 下边、抽屉下边）叠加 `env(safe-area-inset-*, 0px)`，兼容刘海屏与 Home Indicator。
- 高度一律 `100svh` / `42svh`，并在其前置一行 `100vh` / `42vh` 作为老浏览器回落声明（不支持 `svh` 的浏览器忽略后一行）。
- 落地页未设置 `viewport-fit=cover`，`env()` 在未 cover 时返回 `0px`，回落值兜住 → 无副作用。**若后续要贴到刘海区，需另行确认**（§7 #4）。

### 6.5 与项目硬约束的关系

本设计**不触及** `CLAUDE.md` 的 C1–C8 任何一条（无密钥、无路由装配、无站点配置字段、无风控键、无订单类型、无部署制品、无 `internal/**` 包、无行级锁）——纯前端样式与交互。

需遵守的工作纪律：**W4**（Mac 只调试，构建/部署在服务器）、**W5**（界面文字中文）、**W6**（浏览器进程用完即关）。

---

## 7. 待确认项

| # | 项 | 结论 | 状态 |
| --- | --- | --- | --- |
| 1 | 文件数从方案的 6 个涨到 13 个 | 采纳「统一状态机 + 纯函数化」的直接后果（§1.4）。若要回到 6 文件版，把两个纯模块内联进 `scene3d.ts` 即可，代价是新逻辑不可单测 | ✅ 已确认 13 文件 |
| 2 | 物理删除 `scene3d-interaction.ts` | **删除**，逻辑搬入 `selection-machine` 的 hover 分支，5 条用例等价迁移（§2.2.5）。只留一个选中逻辑入口 | ✅ 已确认 |
| 3 | `BREAKPOINTS.mobile` 与 `hooks/use-mobile.ts` 的 `MOBILE_BREAKPOINT` | 两处各持 `768` 字面量，由注释 + 单测断言钉住。**不互相 import**——全站 hook 不应反向依赖单个 feature，反向依赖是架构错误 | ✅ 已确认 |
| 4 | `viewport-fit=cover` | **不设置**。`env()` 返回 `0px`，回落值兜住。改 viewport meta 属全站改动，会牵连后台/控制台所有页面的安全区布局 | ✅ 已确认 |
| 5 | 混合设备（触屏笔电） | **按实际指针类型运行时校正**（§2.1.4）。用鼠标即 hover 体验、用手指即 tap 体验，可来回切换。这修掉了 R7 的第二个入口 | ✅ 已确认 |
| 6 | 抽屉滚出首屏 | **自动关闭**，复用既有 IntersectionObserver（§2.4.5），语义与桌面端一致 | ✅ 已确认 |
| 7 | 抽屉与客服 FAB 在屏底碰撞 | **抽屉开时 FAB 淡出且不可点**，靠 `.wd-landing-root.wd-hud-open` class 钩子（§2.3.4 ①） | ✅ 已确认 |
| 8 | 可选的滚动流畅度优化（`#grid{display:none}` / `#aurora` 降模糊） | **写入 B1 块**。手机上 4 层 fixed 装饰层持续重绘，降一层对顺滑度有实质改善 | ✅ 已确认 |
| 9 | `hooks/` 下 `use-mobile.ts` 与 `use-mobile.tsx` 内容完全重复 | 本次**不处理**（超出范围）。已登记：`@/hooks/use-mobile` 的解析优先级需单独核实，属既有技术债 | 📝 记录，不动 |

**全部设计决策已确认，可进入实施。** 建议顺序（TDD 起步）：

1. `viewport-mode.ts` + 测试 → 独立可绿
2. `selection-machine.ts` + 测试（含从 `scene3d-interaction.test.ts` 迁移的 5 例）→ 独立可绿，删除旧文件
3. `landing-mobile-css.ts` + 契约测试 → 独立可绿
4. 接线：`scene3d.ts`（9 处）+ `index.tsx`（2 处）+ `landing-css.ts`（2 处）+ `sections-css.ts`（1 处）+ `features/home/index.tsx`（1 处）
5. 门禁：`bun run lint && bun run typecheck && bun run test && bun run source-size:check`
6. 真机验收：方案文档 §11 的 A1–A7 / B1–B7 / C1–C3

> 步骤 1–3 全部是零依赖纯模块，**可并行开发、互不阻塞**，且在接线前就能全绿——这正是模块划分的收益兑现处。

---

## 变更记录

| 版本 | 日期 | 说明 |
| --- | --- | --- |
| v1.0 | 2026-08-03 | 首版。基于 `mobile-landing-proposal.md` v1.0 + 4 项设计决策（独立文档 / 统一状态机 / CSS 契约测试 / 接口契约级详略）成文。待用户审阅。 |
