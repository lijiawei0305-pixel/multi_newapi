# 主站落地页移动端排版改造方案（mobile-landing-proposal v1.0）

> **文档定位**：主站落地页 `LandingReact` 的**手机端排版改造设计方案**。只讲「手机上怎么摆、React/CSS 怎么改」，不涉及业务需求变更。
> **与 `doc/proposal.md` 的关系**：`proposal.md` 是项目权威**需求**文档（v2.0），本文件是它 §9（前端/UIUX）范围下的一份**实现级设计方案**，不替代、不修改它。
> **状态**：待用户确认 → 确认后才写实施计划、才动代码。本轮**不改任何代码**。
> **日期**：2026-08-03

---

## 目录

1. [范围与目标](#1-范围与目标)
2. [已确认的决策（本次对话锁定）](#2-已确认的决策本次对话锁定)
3. [现状实测：证据与量测数据](#3-现状实测证据与量测数据)
4. [根因分析：逐条定位到代码](#4-根因分析逐条定位到代码)
5. [设计原则与硬约束](#5-设计原则与硬约束)
6. [断点体系](#6-断点体系)
7. [分区改造设计](#7-分区改造设计)
8. [React / TypeScript 改动清单（逐文件逐处）](#8-react--typescript-改动清单逐文件逐处)
9. [完整 MOBILE_CSS 草稿](#9-完整-mobile_css-草稿)
10. [明确不做的事（YAGNI）](#10-明确不做的事yagni)
11. [验收清单](#11-验收清单)
12. [风险、门禁与回滚](#12-风险门禁与回滚)
13. [附录 A：本次取证方法（可复现）](#附录-a本次取证方法可复现)

---

## 1. 范围与目标

### 1.1 目标

让 `https://www.wedreamhub.com/`（主站首页）在手机上**排版清爽、元素互不重叠、滚动顺畅**。

### 1.2 范围内

| 项 | 路径 |
| --- | --- |
| 落地页外壳（顶栏 / Hero / 客服 FAB / HUD） | `web/default/src/features/landing-react/index.tsx` |
| 落地页 Hero 与全局样式 | `web/default/src/features/landing-react/landing-css.ts` |
| 三屏样式（AI 工坊 / 核心能力 / 页尾 CTA） | `web/default/src/features/landing-react/sections-css.ts` |
| 3D 场景与卫星交互 | `web/default/src/features/landing-react/scene3d.ts` |
| 落地页页脚包裹层 | `web/default/src/features/home/index.tsx` |
| **新增**：移动端样式表 | `web/default/src/features/landing-react/landing-mobile-css.ts` |

### 1.3 范围外（本轮不动）

- **代理站首页**（`features/home/components/` 下的 `Hero`/`Stats`/`Features`/`HowItWorks`/`CTA`，Tailwind 编写，与落地页完全独立）——已确认另开一轮。
- 后台 / 控制台 / 播放场（playground）等任何非首页页面。
- 任何文案、i18n 条目、业务逻辑、接口。
- **桌面端（≥769px）观感**——见 §5.1 硬约束。

---

## 2. 已确认的决策（本次对话锁定）

| # | 决策点 | 结论 |
| --- | --- | --- |
| D1 | 方案文档落点 | 新建本文件；`doc/proposal.md` 原封不动 |
| D2 | 改造范围 | 仅主站落地页 `LandingReact` |
| D3 | 首屏 3D 灯泡布局 | **上下堆叠**：文案在上、3D 在下，各自独占整行宽度 |
| D4 | 首屏高度策略 | **不锁死**，`min-height:100svh` + 内容自然撑高，允许略微溢出 |
| D5 | 桌面端 | **观感零变化**——所有改动包在 `@media (max-width:768px)` 内 |
| D6 | 卫星信息卡（`#hud`） | 改为**底部抽屉** |
| D7 | 客服入口 | 手机端**只留右下 FAB**，隐藏顶栏耳机按钮 |
| D8 | 抽屉关闭方式 | **新增 1 个 × 关闭按钮**（仅手机可见）——用户已批准这唯一的 DOM 新增 |
| D9 | 最小兼容宽度 | **360px** 完整调优；320–359px 保证不破版（不溢出、不重叠），不逐像素调 |
| D10 | 首屏两张小卡片 | **横向两列并排** |
| D11 | 本轮交付边界 | **只出本方案文档**，不动代码 |

> D8 说明：你的原始要求是「不需要增加元素」。这里新增的是 1 个交互必需的关闭按钮（不是新内容板块），且**桌面端 `display:none`**。原因见 §4 R7——现有关闭逻辑依赖 `mouseleave`，触屏永不触发，抽屉弹出后会永久卡死。

---

## 3. 现状实测：证据与量测数据

**取证环境**：Playwright + Chromium，iPhone 视口 `390×844`、DPR 2、`is_mobile=true`、`has_touch=true`，中英文各跑一遍，目标为**线上生产站** `https://www.wedreamhub.com/`。方法见[附录 A](#附录-a本次取证方法可复现)。

### 3.1 首屏元素实测盒模型（zh-CN，390×844）

| 元素 | left | top | width | height | 判定 |
| --- | ---: | ---: | ---: | ---: | --- |
| `.hero-stage` | 0 | 0 | 390 | 844 | — |
| `.hero-left` | 0 | 0 | **156** | 844 | ⚠️ 40% 分栏，可用内容宽仅 **108px** |
| `.hero-right` | 156 | 0 | 234 | 844 | — |
| `.lp-topbar` | 0 | 0 | 390 | 72 | ⚠️ `fixed` 且**无背景** |
| `#badge` | 24 | 125 | 108 | **54** | ⚠️ 本应单行，实际断成 2 行 |
| `#hero h1` | 24 | 197 | **152** | 166 | ⚠️ 宽度已越过 `.hero-left` 的 padding 盒 |
| `.hl2`（主标语） | 24 | 289 | 152 | 74 | ⚠️「让灵感不再受限」断成「让灵感不 / 再受限」 |
| `.features` | 24 | 442 | **108** | 142 | ⚠️ 子项 `min-width:150px` > 容器 108px |
| `.cta` | 24 | 611 | **108** | 108 | ⚠️ 按钮 `nowrap`，必然溢出；双按钮被迫竖排 |

### 3.2 横向溢出扫描（en-US，390×844）

页面本身无横向滚动条（`scrollWidth == clientWidth == 390`，靠 `.wd-landing-root{overflow-x:hidden}` 兜住），但**元素级溢出确实存在**：

```
{'t': 'ASIDE', 'id': 'hud',        'l': 201, 'r': 398, 'w': 197}   ← 右边缘出屏 8px
{'t': 'DIV',   'c': 'chip alt',    'l': 351, 'r': 396, 'w': 45}    ← 卫星芯片出屏
{'t': 'SPAN',  'c': 'disc',        'l': 349, 'r': 398, 'w': 49}
```

即：溢出被 `overflow-x:hidden` **藏起来了，但没有被解决**——内容仍然叠在一起。

### 3.3 下方三屏实测

| 现象 | 量测 |
| --- | --- |
| `.svc-card` 全部被拉成等高 | 5 张卡片高度**全部 = 362px**，最矮的实际内容仅约 110px → 每张卡中间空出 **≈250px** |
| `.svc-cap` | 3 张各 200px（`sec-studio` 已有 820px 断点改单列，此处正常） |
| 顶栏遮挡 | 滚动到 `sec-services` 时，`CORE CAPABILITIES` eyebrow 只露出尾部 `LS`；卡片图标被顶栏压住 |
| FAB 遮挡页脚 | 右下 56px 悬浮球压在页脚「隐私政策」链接一侧 |

### 3.4 首屏视觉问题（截图确认，zh-CN）

1. 「自由穿梭于顶尖大模型之间」**正压在两颗卫星芯片上**（文字与图标互相穿透）。
2. 「开始体验」按钮**正压在一颗紫色卫星芯片上**。
3. 「多模型接入 / 安全稳定」两张卡片**溢出到 3D 灯泡区域**。
4. 「WeDream AI」断成「WeDream / AI」两行。
5. 顶栏「开始使用」按钮宽度占屏幕近一半，与 logo、语言切换器挤在一起。

---

## 4. 根因分析：逐条定位到代码

| # | 现象 | 根因 | 代码位置 |
| --- | --- | --- | --- |
| **R1** | 文案压在 3D 灯泡上 | `.hero-stage` 是 `flex`（横向），`.hero-left` 写死 `flex:0 0 40%` — 手机上仍然分栏，左栏只剩 156px | `landing-css.ts:281–285` |
| **R2** | 两张小卡片溢出 | `.feature{width:calc(50% - 14px); min-width:150px}` — `min-width` 硬下限 150px > 容器可用宽 108px，**必然溢出 42px** | `landing-css.ts:292` |
| **R3** | CTA 按钮溢出并竖排 | `.btn{padding:12px 28px; white-space:nowrap}` + `.cta{flex-wrap:wrap}` — 单按钮最小宽 > 108px | `landing-css.ts:298–301` |
| **R4** | 标题/副标题异常断行 | `.hl1{letter-spacing:.1em}`、`.hl2{letter-spacing:.05em}`、`#sub{letter-spacing:.18em}`、`#badge{letter-spacing:.18em}` — 为宽屏设计的字距，在窄容器里放大了断行概率 | `landing-css.ts:198,209,219,227` |
| **R5** | 顶栏遮挡下方内容 | `.lp-topbar{position:fixed}` 但**无 `background`**，内容直接从它身下穿过 | `landing-css.ts:308` |
| **R6** | bento 卡片大片空白 | `.svc-bento{grid-auto-rows:1fr}` — 单列下所有行等高，被最高的旗舰卡撑到 362px | `sections-css.ts:37` |
| **R7** | **HUD 抽屉在触屏上会永久卡死** | 选中靠 `mouseenter`/`mouseleave`（`scene3d.ts:381–386`）+ 每帧 `nextSelection(selected, hoverKey, pointerInCanvas)`；触屏 tap 只合成一次 `mousemove`（置 `pointerInCanvas=true`）、**永不触发 `mouseleave`**，于是 `nextSelection(current, null, true)` 恒返回 `current` → 选中永不释放 | `scene3d.ts:381–386, 455–463, 700–704` + `scene3d-interaction.ts` |
| **R8** | HUD 盖住半个屏幕 | `#hud{position:absolute; right:…; top:50%; width:min(84%,320px)}` — 为「右侧栏」设计 | `landing-css.ts:249–260` |
| **R9** | 客服入口重复 + 遮挡页脚 | 顶栏 `.lp-kefu-btn` 与右下 `.lp-fab` 同时存在；FAB `56px` @ `bottom:26px;right:26px` 压住页脚链接 | `landing-css.ts:314, 328` |
| **R10** | iOS 地址栏收缩导致跳动/裁切 | `.hero-stage{height:100vh}`、`#bg{height:100vh}` 用 `vh`（在 iOS Safari 上是「地址栏展开时」的高度） | `landing-css.ts:281, 14` |
| **R11** | 触屏 hover 粘滞 | `.chip:hover .shell{transform:scale(1.1)}` 等 hover 态在触屏上 tap 后不释放 | `landing-css.ts:180–186` |
| **R12** | 卫星芯片在窄屏挤成一团 | `--chip:52px` 为宽屏设计；窄屏轨道投影半径变小，芯片间距不足 | `landing-css.ts:139` |

> **R1 / R2 / R3 是「元素混在一起」的三大直接根因，且互相叠加**：R1 把容器压到 108px，R2/R3 的硬下限让内容必然冲出容器，冲出的方向恰好是 3D 场景所在的右侧。

---

## 5. 设计原则与硬约束

### 5.1 硬约束（违反即返工）

| 编号 | 约束 | 验证方式 |
| --- | --- | --- |
| **M1** | **桌面端（≥769px）零变化**。所有新增样式必须包在 `@media (max-width:768px)` 内；对既有桌面规则的改写**必须渲染等价**（见 §8.2 的 `--hud-accent` 说明） | 改动前后 1440×900 桌面截图逐像素对比 |
| **M2** | **不改文案、不加 i18n 条目、不加内容板块**。唯一 DOM 新增是 `.hud-close` 按钮（D8 已批准，桌面 `display:none`） | `git diff` 中 `locales/*.json` 零改动 |
| **M3** | **不引入新依赖、不引入新的运行时 JS 布局计算**。断点由 CSS 承担；JS 只改触屏交互语义 | `package.json` 零改动 |
| **M4** | **W5：所有界面文字保持中文**。本次不新增可见文案；`.hud-close` 用 `aria-label` + `×` 字符，`aria-label` 走既有 i18n key | `git grep` 新增字符串 |
| **M5** | 单文件不超过 1500 行（`source-size:check` 门禁） | `bun run source-size:check` |

### 5.2 设计原则

1. **先隔离，再美化**。手机端每个元素独占自己的行/列，物理上不可能重叠；先把 R1–R3 的溢出根因铲掉，再谈字号间距。
2. **改 CSS 优先，改 JSX 最后**。能用媒体查询解决的绝不动组件树——这样桌面端零变化是**可证明的**（媒体查询外的规则一行没动）。
3. **新增独立样式文件**。移动端规则集中在 `landing-mobile-css.ts`，注入顺序排在最后，靠**源码顺序**取胜，全程不用 `!important`。这样「哪些是移动端改动」一目了然，回滚只需删一个 import。
4. **触屏交互显式化**。用 `matchMedia('(hover: hover)')` 分流：有 hover 能力走既有悬停逻辑（桌面完全不变），无 hover 能力走 tap 显式选中/关闭。
5. **视口单位换 `svh`**。用 `100svh`（small viewport height）替代 `100vh`，消除 iOS 地址栏收缩导致的高度跳变。

---

## 6. 断点体系

| 断点 | 用途 | 状态 |
| --- | --- | --- |
| `(max-width: 768px)` | **本次主断点**——手机端全部规则 | 🆕 新增。数值对齐已有 `hooks/use-mobile.ts` 的 `MOBILE_BREAKPOINT = 768`，保持全站一致 |
| `(max-width: 400px)` | 窄屏微调（字号/间距再收一档），覆盖 360–400px | 🆕 新增 |
| `(max-height: 700px)` | 矮屏（如 360×640）降低 3D 区高度占比 | 🆕 新增 |
| `(hover: none)` | 触屏 hover 态中和 | 🆕 新增 |
| `(max-width: 900px)` / `(max-width: 820px)` | 既有：`svc-head`/`svc-bento`/`svc-caps` 降列 | ✅ 不动 |
| `(max-width: 640px)` | 既有：隐藏 logo 文字、收窄客服按钮 | ✅ 不动 |
| `(max-width: 560px)` / `(max-width: 520px)` | 既有：bento 单列、join 单列按钮 | ⚠️ 仅 `560` 内**补一条** `grid-auto-rows:auto`（见 §7.7） |
| `(prefers-reduced-motion: reduce)` | 既有：关闭动画 | ✅ 不动，新增规则需与其兼容 |

> **为什么不动既有的 900/820/640/560/520**：用户确认的是「桌面端零变化」，未要求改平板。这些断点在 561–900px 区间的既有表现是可接受的，动它们只会扩大回归面。

---

## 7. 分区改造设计

### 7.1 首屏总体：横向分栏 → 竖向堆叠

**目标形态**

```
┌───────────────────────┐
│ [logo]      [开始使用] [文A] │ ← 顶栏 fixed + 毛玻璃（新）
├───────────────────────┤
│  ✦ 新一代 AI 平台        │ ← badge 单行
│                       │
│  WeDream AI            │ ← 单行
│  让灵感不再受限          │ ← 单行，不断词
│  自由穿梭于顶尖大模型之间   │ ← 单行
│                       │
│  ⬡ 多模型接入   ⬡ 安全稳定 │ ← 两列并排（D10）
│    接入全球顶尖…  企业级…   │
│                       │
│  [ 开始体验 ] [ 了解更多 ] │ ← 并排等宽双按钮
├───────────────────────┤
│                       │
│        ╭───────╮       │
│       (  3D 灯泡  )      │ ← 独占整行，≥42svh
│        ╰───────╯       │
│                       │  ← 卫星在此区域内公转，永不越界
└───────────────────────┘
                    ( 客服 ) ← FAB 48px（缩小）
```

**关键改动**

| 选择器 | 改法 | 对应根因 |
| --- | --- | --- |
| `.hero-stage` | `height:100vh` → `height:auto; min-height:100svh`；`flex-direction:column`；`overflow:hidden` 保留 | R1 / R10 |
| `.hero-left` | `flex:0 0 40%` → `flex:0 0 auto`；`padding` 改为 `calc(顶栏高 + 安全区)` / 20px / 8px；`justify-content:flex-start` | R1 |
| `.hero-right` | `flex:1 1 0` → `flex:1 1 auto; min-height:42svh` | R1 |

`.hero-right` 用 `flex:1 1 auto` 而非固定高度：文案区自然高度约 395px（390×844 实测推算），剩余约 449px（≈53svh）全部给 3D，灯泡比 42svh 下限更舒展；矮屏时退回 `min-height` 保底。

**为什么 `overflow:hidden` 保留**：`.hero-right` 是卫星芯片的定位容器，`hidden` 保证芯片投影到边缘时被裁而不是撑出屏幕。改成 `visible` 反而会引入新的横向溢出。

### 7.2 首屏文字：字距与字号

| 选择器 | 现状 | 手机端 | 依据 |
| --- | --- | --- | --- |
| `#badge` | `12.5px` / `ls .18em` / `padding 7px 16px` | `12px` / `ls .08em` / `6px 13px` | 「新一代 AI 平台」7 字符，收字距后单行必定放得下 |
| `.hl1` | `clamp(28,4.4vw,52)` / `ls .1em` | `26px` / `ls .04em` / `padding-left:0` | `WeDream AI` ≈145px < 350px 可用宽 → 单行 |
| `.hl2` | `clamp(32,4vw,56)` / `ls .05em` | `34px` / `ls .01em` / `line-height 1.2` | 「让灵感不再受限」7 字 × 34px ≈ 238px < 350px → 单行。360px 屏可用宽 320px，仍单行 |
| `#sub` | `clamp(13,1.5vw,16)` / `ls .18em` | `14px` / `ls .06em` / `lh 1.7` | 「自由穿梭于顶尖大模型之间」12 字 × 14px ≈ 168px → 单行 |
| `#hero` | `gap:14px` | `gap:10px`，`h1` 内 `gap:4px` | 压缩首屏垂直占用 |

`@media (max-width:400px)` 再收一档：`.hl2 → 30px`，`.hl1 → 24px`，保证 360px 屏仍单行。

### 7.3 首屏两张小卡片（R2 · D10）

```
.features { max-width:none; gap:14px 12px; }
.feature  { width:calc(50% - 6px); min-width:0; gap:9px; }   ← min-width:0 是关键
```

`min-width:150px` → `min-width:0` 是**直接铲除 R2 根因**的那一行。
390px 屏每列 `(350-12)/2 = 169px`；360px 屏每列 `154px`——图标 30px + 文字区 ≈115px，标题「多模型接入」4 字 ×13.5px = 54px，描述折 2 行。可读。

图标与字号同步收：`.feature-ico` 34→30px、`.feature-name` 14.5→13.5px、`.feature-desc` 12→11.5px。

### 7.4 CTA 双按钮（R3）

```
.cta { gap:10px; margin-top:6px; width:100%; }
.btn { flex:1 1 0; min-width:0; padding:12px 10px; font-size:14px; }
```

两个按钮各占一半、等宽并排。`white-space:nowrap` 保留（4 字 ×14px = 56px + padding 20px = 76px，远小于 165px 的一半宽），不会溢出。

### 7.5 顶栏（R5 · D7）

| 项 | 改法 |
| --- | --- |
| 背景 | **常驻**半透明毛玻璃：`background:rgba(3,7,18,.72)` + `backdrop-filter:blur(14px)` + 底部 1px 微弱分隔线 |
| 内边距 | `16px clamp(20px,4vw,52px)` → `12px 16px`，顶部叠加 `env(safe-area-inset-top)`（刘海屏） |
| 客服按钮 | `.lp-kefu-btn{display:none}`（D7：手机只留 FAB） |
| logo | 图标 40→34px，字号 19→16px；文字显隐沿用既有 640px 规则，不动 |
| 「开始使用」 | 高度 40→36px，`padding 0 20px`→`0 14px`，字号 14.5→13.5px |

**为什么常驻毛玻璃而不是「滚动后才出现」**：后者需要 `scroll` 监听 + React state + 每帧 class 切换，属于 §5.1-M3 排除的运行时布局计算。首屏背景本就是近纯黑，常驻半透明深色条在视觉上几乎不可察，但一进入下方三屏就立刻解决了 R5 的穿透遮挡。

### 7.6 3D 场景与卫星交互（R7 · R11 · R12 · D6）

#### 7.6.1 样式侧

```
.wd-landing-root { --chip:40px; }            /* 52 → 40，窄屏芯片不挤 */
.hero-right.card-open .hero-visual { transform:none; }  /* 抽屉在底部，无需横向让位 */
#bulb { height:min(38svh,70vw); }            /* 非-WebGL 兜底图，按 svh */
#bg   { height:100svh; }                     /* R10 */

@media (hover:none){                         /* R11：中和触屏粘滞 hover */
  .chip:hover .shell { transform:none; }
  .chip:hover .shell::before { border-color:rgba(120,180,255,.6); box-shadow:/* 复原初始值 */; }
}
```

#### 7.6.2 交互侧（`scene3d.ts`，本方案唯一的逻辑改动）

新增一个能力探测常量，**桌面路径一行不变**：

```ts
const HOVER_CAPABLE = matchMedia('(hover: hover)').matches
```

| 位置 | 现状 | 改法 |
| --- | --- | --- |
| 芯片事件（`:381–386`） | 无条件绑 `mouseenter`/`mouseleave` | `HOVER_CAPABLE` 时保持原样；否则改绑 `click` → `setSelection(key)`（并 `stopPropagation`） |
| 渲染循环（`:700–704`） | 每帧 `selected = nextSelection(selected, hoverKey, pointerInCanvas)` | 用 `if (HOVER_CAPABLE) { …原逻辑… }` 包住；触屏下 `selected` 只由显式 `setSelection` 改写 |
| 新增 | — | `function setSelection(key: string \| null)`：`key === selected` 则早返回，否则赋值 + `onSelectChange(selected)` |
| 新增 | — | 文档级 `pointerdown` 监听：触屏下，若 `e.target.closest('.chip')` 与 `.closest('#hud')` 皆为空 → `setSelection(null)`（点抽屉外关闭） |
| 新增 | — | `window` 上监听自定义事件 `wd-hud-close` → `setSelection(null)`（供 React 关闭按钮调用，见 §8.1） |
| 清理（`:737`） | 已有 `removeEventListener` 段 | 同步移除上面两个新监听 |

**为什么用 `hover` 媒体特性而不是宽度**：这是「有没有真实指针悬停能力」的语义判断，比宽度更准确——桌面窄窗口仍是 hover 设备，应保持悬停体验；平板/手机无论宽窄都该走 tap 路径。

**为什么用 CustomEvent 而不是 `window.__scene3d`**：`__scene3d` 全局对象类型是 `any`，React 侧调用需要 `(window as any)`，会触碰 `no-explicit-any` 规则。`window.dispatchEvent(new CustomEvent('wd-hud-close'))` 零 `any`、零耦合、可被 `scene3d` 的 cleanup 干净移除。

### 7.7 HUD → 底部抽屉（R8 · D6 · D8）

```
#hud {
  position:fixed; z-index:70; pointer-events:auto;
  left:0; right:0; bottom:0; top:auto;
  width:auto; max-width:none;
  border:1px solid rgba(120,180,255,.22);
  border-top:3px solid var(--hud-accent,#00f0ff);
  border-radius:18px 18px 0 0;
  padding:16px 18px calc(18px + env(safe-area-inset-bottom,0px));
  transform:translateY(100%);                       /* 收起：沉到屏下 */
}
#hud.show { transform:translateY(0); }              /* 展开：升起 */
```

- **`pointer-events` 必须从 `none` 翻成 `auto`**（桌面端 `#hud` 是 `pointer-events:none` 的纯展示层），否则关闭按钮点不动。
- **强调色改用 CSS 变量**：桌面端 `border-left:3px solid #00f0ff` 由 JS 用 `style.borderLeftColor = m.color` 着色（`scene3d.ts:526`）。抽屉的强调边在**顶部**，若 JS 直接改 `borderTopColor`，桌面端顶边框也会被染色 → 违反 M1。
  改法：桌面 CSS 改成 `border-left:3px solid var(--hud-accent,#00f0ff)`，JS 改成 `setProperty('--hud-accent', m.color)`。**渲染结果与现在逐像素相同**（同一个颜色、同一个位置），但同一把变量可以同时喂给手机端的 `border-top`。
- 关闭按钮 `.hud-close`：`display:none` 写在桌面样式里，仅在手机断点显为 32px 圆形按钮（右上角）。

### 7.8 客服 FAB 与页脚（R9 · D7）

```
.lp-fab { width:48px; height:48px; right:16px;
          bottom:calc(18px + env(safe-area-inset-bottom,0px)); }
.lp-fab svg { width:22px; height:22px; }
```

页脚遮挡则在 React 侧解决：给落地页专属的 Footer 包裹层加下内边距（见 §8.3），让页脚内容永远滚得过 FAB。**不改 `Footer` 组件本身**——它被多个页面共用。

### 7.9 「核心能力」bento（R6）

**一行修掉大片空白**：

```
@media (max-width:560px){
  .svc-bento { grid-auto-rows:auto; }
}
```

`grid-auto-rows:1fr` 在多列时用于对齐卡片高度（合理），但单列下它把每一行都拉成「最高行」的高度 → 5 张卡片全部 362px。单列时卡片本就上下排列，等高毫无意义。

断点用 **560**（对齐既有的 bento 单列断点）而非 768：561–768px 仍是两列，那里保留等高是对的。

配套的手机端收敛（`max-width:768px`）：

| 选择器 | 改法 |
| --- | --- |
| `.svc-card` | `padding:22px` → `18px` |
| `.svc-flagship` | `padding:30px 32px` → `22px 18px` |
| `.svc-card-desc` / `.svc-flagship .svc-card-desc` | `max-width:44ch/54ch` → `none`（窄屏下 ch 限制只会制造右侧空白） |
| `#sec-services` | `padding:clamp(64,8vw,110) clamp(20,5vw,44)` → `56px 16px` |

### 7.10 「AI 工坊」sec-studio

已有 820px 断点改单列，基本可用。手机端仅做收敛：

| 选择器 | 改法 |
| --- | --- |
| `.svc-stu-title` | `clamp(30,4.5vw,50)` → `27px` |
| `.svc-stu-sub` | `15.5px` → `14px`，`max-width:52ch` → `none` |
| `.svc-cap` | `padding:26px` → `20px` |
| `.svc-cap-ic` | `46px` → `40px`，`margin-bottom:18px` → `14px` |
| `#sec-studio` | 左右 padding → `16px` |

### 7.11 页尾 sec-join

已有 520px 断点（单列全宽按钮）表现尚可。手机端仅补一条：

```
.join-constellation { opacity:.4; }   /* 现 .78 —— 星座连线在窄屏会穿过标题文字 */
```

### 7.12 可选 · 滚动流畅度

手机端有 4 个 `position:fixed` 的全屏装饰层（`#aurora` 带 `blur(40px)`、`#grid`、`#stardust`、`#stardust2`），滚动时会持续触发合成层重绘。建议（**观感近乎无变化，滚动明显更顺**）：

```
@media (max-width:768px){
  #grid   { display:none; }              /* 56px 网格线在手机上几乎不可见 */
  #aurora { filter:blur(28px); opacity:.22; }
}
```

标为**可选**：若你希望完全保留装饰层，删掉这一段即可，其余方案不受影响。

---

## 8. React / TypeScript 改动清单（逐文件逐处）

> 共 **6 个文件**：新增 1、改动 5。JSX 结构改动只有 1 处（关闭按钮）。

### 8.0 新增：`landing-mobile-css.ts`

```ts
/* WeDream 落地页移动端样式 —— 全部规则包在 @media (max-width:768px) 等媒体查询内，
   宽屏(≥769px)一行不生效，桌面端观感零变化。
   注入顺序排在 LANDING_CSS + SECTIONS_CSS 之后（见 index.tsx），靠源码顺序覆盖同权重规则，
   全文件不使用 !important。回滚只需从 index.tsx 的 <style> 里去掉本常量。 */
export const MOBILE_CSS = `…（完整内容见 §9）…`
```

- 位置：`web/default/src/features/landing-react/landing-mobile-css.ts`
- 风格与 `landing-css.ts` / `sections-css.ts` **完全对齐**（同为导出模板字符串常量、同样的中文文件头注释、同样不带 AGPL 版权头）。
- 预估约 200 行，远低于 1500 行门禁。

### 8.1 `index.tsx`（2 处）

**① 注入移动端样式**（第 118 行附近）

```diff
+import { MOBILE_CSS } from './landing-mobile-css'
 ...
-      <style>{LANDING_CSS + SECTIONS_CSS}</style>
+      <style>{LANDING_CSS + SECTIONS_CSS + MOBILE_CSS}</style>
```

**② HUD 关闭按钮**（`<aside id='hud'>` 内，第 279–296 行）

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
         <div className='hud-prov'>
```

- `t('Close')` 是既有 i18n key（New API 官方 locale 已有），**不新增条目**，符合 M2/M4。
- 无 state、无 ref、无 `any`、无新 hook——一个纯 DOM 事件派发。
- 桌面端该按钮 `display:none`（写在 `landing-css.ts`，见 §8.2）。

### 8.2 `landing-css.ts`（2 处 · 均为渲染等价改写）

**① `#hud` 强调色改用变量**（第 254 行）

```diff
-    border:1px solid rgba(120,180,255,.22); border-left:3px solid #00f0ff;
+    border:1px solid rgba(120,180,255,.22); border-left:3px solid var(--hud-accent,#00f0ff);
```

回退值 `#00f0ff` 与原字面量相同 → 未选中时渲染完全一致；选中时由 JS 写入 `--hud-accent`，与原来写 `borderLeftColor` 效果一致。**桌面端逐像素不变。**

**② 关闭按钮桌面端隐藏**（`#hud` 规则块之后新增一行）

```diff
+  .hud-close { display:none; }
```

桌面端该按钮完全不渲染 → 桌面端零变化。

### 8.3 `features/home/index.tsx`（1 处）

```diff
-      <div className='dark relative z-20 bg-[#061127]'>
+      <div className='dark relative z-20 bg-[#061127] pb-20 md:pb-0'>
         <Footer className='border-transparent' />
       </div>
```

- 手机端给页脚兜 80px 下内边距，让内容能滚过 48px 的 FAB；`md:` 起（≥768px）归零 → 桌面零变化。
- 只作用于 `MainSiteLanding`（主站落地页），**不影响** `Footer` 组件本身与其他使用它的页面。
- ⚠️ 该文件有契约测试 `lib/lazy-loading-contract.test.ts`，断言 `const LandingReact = lazy(` 与 import 语句形态——本改动不触及这两处，测试不受影响。

### 8.4 `scene3d.ts`（7 处）

| # | 位置 | 改动 |
| --- | --- | --- |
| 1 | 模块内初始化处 | 新增 `const HOVER_CAPABLE = matchMedia('(hover: hover)').matches` |
| 2 | `:381–386` 芯片事件绑定 | `if (HOVER_CAPABLE)` 走原 `mouseenter`/`mouseleave`；`else` 绑 `click` → `e.stopPropagation(); setSelection(key)` |
| 3 | `:512` `selected` 声明之后 | 新增 `setSelection(key)` 函数（幂等：同值早返回，异值赋值并调 `onSelectChange`） |
| 4 | `:526–528` `setHud` 内 | `box.style.borderLeftColor = m.color` → `box.style.setProperty('--hud-accent', m.color)`（配合 §8.2①） |
| 5 | `:700–704` 渲染循环 | 用 `if (HOVER_CAPABLE) { … }` 包住 `nextSelection` 那三行；`speedFactor` 一行留在外面（触屏选中同样应该缓停轨道） |
| 6 | 事件注册段（`:478` 附近） | 新增 `document` 级 `pointerdown`（触屏点抽屉外关闭）与 `window` 级 `wd-hud-close` 监听 |
| 7 | cleanup 段（`:737` 附近） | 同步 `removeEventListener` 上面两个新监听 |

> `scene3d-interaction.ts` 的 `nextSelection` 纯函数**不改**——它的单测（`scene3d-interaction.test.ts`）继续覆盖桌面悬停语义。触屏路径是绕过它、而不是改它。

### 8.5 `sections-css.ts`（1 处）

在既有 `@media (max-width:560px)` 块内补一行：

```diff
   @media (max-width:560px){
     .svc-bento { grid-template-columns:1fr; }
+    .svc-bento { grid-auto-rows:auto; }
     .svc-card,.svc-wide,.svc-flagship { grid-column:span 1; }
```

（也可直接并入上一条选择器，效果相同；分开写便于 review 时看清这是本次新增。）

---

## 9. 完整 MOBILE_CSS 草稿

> 这是 `landing-mobile-css.ts` 的内容草案。**确认方案后才落盘**；实施时以此为基线微调。

```css
/* ============ 手机端（≤768px）============ */
@media (max-width: 768px) {

  /* --- 全局 --- */
  .wd-landing-root { -webkit-text-size-adjust:100%; --chip:40px; }
  #bg { height:100svh; }

  /* --- 顶栏 --- */
  .lp-topbar {
    padding:12px 16px;
    padding-top:calc(12px + env(safe-area-inset-top, 0px));
    background:rgba(3,7,18,.72);
    backdrop-filter:blur(14px); -webkit-backdrop-filter:blur(14px);
    border-bottom:1px solid rgba(120,180,255,.10);
  }
  .lp-kefu-btn { display:none; }                       /* D7：手机只留 FAB */
  .lp-logo { font-size:16px; gap:8px; }
  .lp-logo .lp-mark-img { width:34px; height:34px; }
  .lp-actions { gap:8px; }
  .lp-start { height:36px; padding:0 14px; font-size:13.5px; }
  .lp-actions [aria-haspopup="menu"] { height:36px; width:36px; }

  /* --- 首屏容器：横向分栏 → 竖向堆叠（R1/R10）--- */
  .hero-stage {
    height:auto;
    min-height:100vh;        /* 老浏览器回落 */
    min-height:100svh;       /* 支持则覆盖，消除 iOS 地址栏跳变 */
    flex-direction:column; align-items:stretch;
  }
  /* 顶栏改造后实际高 ≈60px（12px×2 内边距 + 36px 按钮），故留 64px 余量 + 16px 呼吸 */
  .hero-left {
    flex:0 0 auto;
    padding:calc(64px + env(safe-area-inset-top, 0px) + 16px) 20px 8px;
    gap:10px; justify-content:flex-start;
  }
  .hero-right { flex:1 1 auto; min-height:42vh; min-height:42svh; }
  .hero-right.card-open .hero-visual { transform:none; }
  #bulb { height:min(38svh, 70vw); }

  /* --- 首屏文字（R4）--- */
  #hero { gap:10px; }
  #hero h1 { gap:4px; margin-top:2px; }
  #badge { font-size:12px; letter-spacing:.08em; padding:6px 13px; }
  .hl1 { font-size:26px; letter-spacing:.04em; padding-left:0; }
  .hl2 { font-size:34px; letter-spacing:.01em; padding-left:0; line-height:1.2; }
  #sub { font-size:14px; letter-spacing:.06em; line-height:1.7; margin-top:0; }

  /* --- 首屏两张卡片：两列并排（R2/D10）--- */
  .features { max-width:none; gap:14px 12px; margin-top:2px; }
  .feature { width:calc(50% - 6px); min-width:0; gap:9px; }   /* min-width:0 = R2 根因 */
  .feature-ico { width:30px; height:30px; border-radius:8px; }
  .feature-ico svg { width:17px; height:17px; }
  .feature-name { font-size:13.5px; }
  .feature-desc { font-size:11.5px; margin-top:2px; line-height:1.45; }

  /* --- CTA：等宽并排（R3）--- */
  .cta { gap:10px; margin-top:6px; width:100%; }
  .btn { flex:1 1 0; min-width:0; padding:12px 10px; font-size:14px; }

  /* --- HUD → 底部抽屉（R8/D6）--- */
  #hud {
    position:fixed; z-index:70; pointer-events:auto;
    left:0; right:0; bottom:0; top:auto;
    width:auto; max-width:none;
    border:1px solid rgba(120,180,255,.22);
    border-top:3px solid var(--hud-accent, #00f0ff);
    border-radius:18px 18px 0 0;
    padding:16px 18px calc(18px + env(safe-area-inset-bottom, 0px));
    transform:translateY(100%);
    transition:opacity .3s ease,
               transform .38s cubic-bezier(.22,.7,.25,1),
               visibility 0s linear .38s;
  }
  #hud.show {
    transform:translateY(0);
    transition:opacity .3s ease, transform .38s cubic-bezier(.22,.7,.25,1);
  }
  #hud .hud-desc { font-size:13px; }
  .hud-close {
    display:grid; place-items:center;
    position:absolute; top:10px; right:12px;
    width:32px; height:32px; border-radius:50%;
    background:rgba(255,255,255,.06);
    border:1px solid rgba(120,180,255,.18);
    color:#cfe0f5; font-size:20px; line-height:1;
    font-family:inherit; cursor:pointer; padding:0;
  }

  /* --- 客服 FAB（R9/D7）--- */
  .lp-fab {
    width:48px; height:48px; right:16px;
    bottom:calc(18px + env(safe-area-inset-bottom, 0px));
  }
  .lp-fab svg { width:22px; height:22px; }

  /* --- AI 工坊 --- */
  #sec-studio { padding:56px 16px 28px; }
  .svc-stu-title { font-size:27px; }
  .svc-stu-sub { font-size:14px; max-width:none; }
  .svc-cap { padding:20px; }
  .svc-cap-ic { width:40px; height:40px; margin-bottom:14px; }
  .svc-cap h3 { font-size:16.5px; }
  .svc-cap p { font-size:13px; }

  /* --- 核心能力（R6 的配套收敛；等高修复在 sections-css 的 560 断点）--- */
  #sec-services { padding:56px 16px; }
  .svc-card { padding:18px; }
  .svc-flagship { padding:22px 18px; }
  .svc-card-desc,
  .svc-flagship .svc-card-desc { max-width:none; }
  .svc-lede { font-size:14px; }

  /* --- 页尾 CTA --- */
  .join-constellation { opacity:.4; }

  /* --- 可选 · 滚动流畅度（见 §7.12，可整段删除）--- */
  #grid { display:none; }
  #aurora { filter:blur(28px); opacity:.22; }
}

/* ============ 窄屏微调（≤400px，覆盖 360–400）============ */
@media (max-width: 400px) {
  .hl1 { font-size:24px; }
  .hl2 { font-size:30px; }
  #sub { font-size:13px; letter-spacing:.04em; }
  .hero-left { padding-left:16px; padding-right:16px; }
  .feature-desc { font-size:11px; }
  .btn { font-size:13.5px; padding:11px 8px; }
}

/* ============ 矮屏（如 360×640）============ */
@media (max-width: 768px) and (max-height: 700px) {
  .hero-right { min-height:34svh; }
  .hero-left { padding-top:calc(56px + env(safe-area-inset-top, 0px) + 12px); }
  #hero { gap:8px; }
}

/* ============ 触屏：中和粘滞 hover（R11）============ */
@media (hover: none) {
  .chip:hover .shell { transform:none; }
  .chip:hover .shell::before {
    border-color:rgba(120,180,255,.6);
    box-shadow:0 0 12px rgba(80,160,255,.42), 0 0 30px rgba(50,120,255,.18),
               inset 0 0 14px rgba(90,170,255,.16), inset 0 -5px 12px rgba(0,0,0,.5);
  }
}
```

---

## 10. 明确不做的事（YAGNI）

| 不做 | 理由 |
| --- | --- |
| 不加汉堡菜单 / 抽屉导航 | 顶栏只有 3 个元素，收窄后完全放得下；加菜单 = 增加元素（违反 M2） |
| 不加「回到顶部」按钮 | 同上，且已有 FAB 占据右下角 |
| 不做手机专属的独立组件树（`{isMobile ? <A/> : <B/>}`） | 会引入首屏 JS 布局判断与闪烁（hydration 前 `isMobile` 为 `undefined`），且让「桌面端零变化」无法用 CSS diff 证明 |
| 不改 3D 场景的粒子数/相机/轨道参数 | 现有 `scene3d-quality.ts` 已按 DPR/核数/内存自动降档（手机走 `mid`/`low`）；再动会牵连桌面观感 |
| 不改任何文案与 i18n | M2 |
| 不改 `Footer` 组件本身 | 多页共用，改它等于改全站 |
| 不动既有 900/820/640/560/520 断点的现有规则 | 扩大回归面，且不在本次目标内（用户确认只要「桌面端零变化」，未要求改平板） |
| 不改代理站首页 | D2 已确认另开一轮 |

---

## 11. 验收清单

### 11.1 排版（每项需真机或 DevTools 设备模拟截图存证）

| # | 检查项 | 判定标准 |
| --- | --- | --- |
| A1 | 390×844 首屏 | 文案区与 3D 区**完全不重叠**；badge/主标题/副标题各自单行；两张小卡片并排一行；CTA 双按钮并排且完整可见 |
| A2 | 360×640 首屏 | 同 A1；允许 CTA 需轻微下滑可见（D4） |
| A3 | 320×568 | **不破版**：无横向滚动、无元素重叠、无文字截断 |
| A4 | 元素级横向溢出扫描 | 附录 A 的扫描脚本输出中，除 `#aurora`（设计上就是 `inset:-25%`）与装饰性 SVG 外，**无业务元素越界** |
| A5 | 滚动全程 | 顶栏毛玻璃生效，下方任何内容都不被顶栏「穿透遮挡」 |
| A6 | 核心能力屏 | 每张 bento 卡片高度贴合内容，**无 200px 以上的空白** |
| A7 | 页脚 | FAB 不遮挡「隐私政策 / 用户协议」链接，可正常点击 |

### 11.2 交互

| # | 检查项 | 判定标准 |
| --- | --- | --- |
| B1 | 触屏点卫星 | 底部抽屉从屏下升起，内容正确，顶部强调边为该模型色 |
| B2 | 点 × 关闭 | 抽屉沉下，标题渐变色复原 |
| B3 | 点抽屉外空白 | 抽屉沉下 |
| B4 | **反复 10 次 B1→B2→B1→B3** | 每次都能正常开关，**绝不卡死**（这是 R7 的回归验证） |
| B5 | 触屏滑动页面 | 手指划过 3D 区可正常滚动页面，不被拦截 |
| B6 | 桌面悬停 | 鼠标悬停卫星 → 右侧卡片弹出（**与改动前完全一致**）；移开 → 收起 |
| B7 | `prefers-reduced-motion` | 开启后无异常动画、抽屉仍可开关 |

### 11.3 桌面端零变化（M1 硬门）

| # | 检查项 | 判定标准 |
| --- | --- | --- |
| C1 | 1440×900 全页截图，改动前 vs 改动后 | **逐像素一致**（允许 3D 场景因动画相位产生的差异——比对时对 canvas 区域做遮罩，或用 `prefers-reduced-motion` 冻结画面后比对） |
| C2 | 1024×768 | 同上（属既有 900/820 断点管辖区，本次未动） |
| C3 | `git diff` 审查 | `landing-css.ts` 的改动只有 2 处且均为渲染等价（§8.2）；其余 CSS 改动全在媒体查询内 |

### 11.4 门禁

```bash
cd web/default
bun run lint              # oxlint --deny-warnings
bun run typecheck         # check-typecheck-coverage + tsgo -b
bun run source-size:check # 1500 行预算
bun run test              # vitest（含 scene3d-*.test.ts、lazy-loading-contract.test.ts）
bun run format:check
bun run copyright:check   # 新文件若被要求版权头则补，勿全局修（历史已红 46 个）
```

⚠️ **`bun run build` 在 Mac 上跑不通**（本机 `@tanstack` 相关依赖缺失，见记忆「前端构建环境坑」）——完整构建与产物验证在**服务器 Docker 内**进行（W4）。

---

## 12. 风险、门禁与回滚

### 12.1 风险登记

| 风险 | 等级 | 缓解 |
| --- | --- | --- |
| `svh` 兼容性 | 低 | iOS 15.4+ / Chrome 108+ 支持；更旧浏览器回落到 `height:auto` + 内容自然高（`min-height` 失效但不破版）。可加 `min-height:100vh` 作为前置回落声明 |
| `backdrop-filter` 在低端安卓不支持 | 低 | 已同时设 `background:rgba(3,7,18,.72)` 实色兜底，无模糊也不透明穿透 |
| `matchMedia('(hover: hover)')` 在混合设备（触屏笔电）判为 `true` | 低 | 判为 `true` = 走桌面悬停路径 = 与现状一致，不引入新问题 |
| 触屏 `click` 与既有 window 级 `pointerdown`（surge 浪涌，`scene3d.ts:464`）冲突 | 中 | surge 只在点击画布中心 0.5 半径内触发且不 `preventDefault`；新增的文档级 `pointerdown` 只做 `setSelection(null)`，两者互不阻断。**B5 需实测确认滚动不受影响** |
| `.hero-stage` 去掉固定 `height` 后，`ResizeObserver` 触发 `layoutSize()` 频率上升 | 低 | `scene3d.ts:441–446` 已用 `ResizeObserver` 观察 `.hero-visual` 父容器，其高度在手机端稳定（`flex:1 1 auto` 只在旋屏时变） |
| 桌面端「零变化」被 `--hud-accent` 改写破坏 | 中 | C1/C3 双重验收；回退值与原字面量相同 |

### 12.2 回滚

三层，从轻到重：

1. **只回滚移动端样式**：从 `index.tsx` 的 `<style>` 里去掉 `+ MOBILE_CSS` 一个 token。
2. **回滚交互改动**：`scene3d.ts` 把 `HOVER_CAPABLE` 常量硬编码为 `true`，触屏路径完全休眠。
3. **整体回滚**：`git revert` 本次提交（预计 1 个提交，涉及 6 个文件）。

### 12.3 与 CLAUDE.md 硬约束的关系

本方案**不触及** C1–C8 任何一条：无密钥、无路由装配、无站点配置字段、无风控键、无订单类型、无部署制品、无 `internal/**` 包、无行级锁。纯前端样式与交互。

**需遵守的工作纪律**：W4（Mac 只调试、构建部署在服务器）、W5（界面文字中文）、W6（浏览器进程用完即关）、以及记忆中的「每轮修改完成即 commit（显式路径，禁 `add -A`，不 push）」。

---

## 附录 A：本次取证方法（可复现）

```python
from playwright.sync_api import sync_playwright

with sync_playwright() as p:
    b = p.chromium.launch()
    ctx = b.new_context(
        viewport={"width": 390, "height": 844}, device_scale_factor=2,
        is_mobile=True, has_touch=True, locale="zh-CN",
        user_agent="Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) "
                   "AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 "
                   "Mobile/15E148 Safari/604.1")
    pg = ctx.new_page()
    pg.goto("https://www.wedreamhub.com/", wait_until="networkidle", timeout=60000)
    pg.wait_for_timeout(4500)
    pg.screenshot(path="zh-hero.png")

    # 元素级横向溢出扫描
    print(pg.evaluate("""() => {
      const w = document.documentElement.clientWidth, out = [];
      document.querySelectorAll('body *').forEach(el => {
        const r = el.getBoundingClientRect();
        if (r.width > 0 && (r.right > w + 1 || r.left < -1))
          out.push({ t: el.tagName, c: (el.className || '').toString().slice(0, 60),
                     id: el.id, l: Math.round(r.left), r: Math.round(r.right) });
      });
      return { clientW: w, scrollW: document.documentElement.scrollWidth,
               over: out.slice(0, 40) };
    }"""))
    ctx.close(); b.close()
```

> **W6 提醒**：跑完必须 `pkill` 残留 chromium 并用 `ps` 复核零残留。

---

## 变更记录

| 版本 | 日期 | 说明 |
| --- | --- | --- |
| v1.0 | 2026-08-03 | 首版。基于线上生产站真机视口实测取证 + 11 项用户确认决策成文。待用户审阅。 |
