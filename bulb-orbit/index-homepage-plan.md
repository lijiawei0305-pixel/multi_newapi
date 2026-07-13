# WeDream AI 滚动首页（index.html）实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
> **依据 spec：** [`index-homepage-design.md`](index-homepage-design.md)（v2.0，已定稿，20 项决策）。

**Goal:** 把单文件 `bulb-orbit/index.html` 从"居中单屏 + 点击门控"改造成"2:2:1 灯泡 Hero + 向下滚动 3 板块（创作搭档/优势/套餐）"的平台滚动首页。

**Architecture:** 只改 index.html 的**手写区**（HTML/CSS/IIFE 场景脚本 1–491 行 + 3D 装配 2121–2394 行），两大块 base64（vendor 492–1794、data 1795–2120）**全程不碰**。所有改动用 `Edit` 精确替换，**绝不整读** 2MB 文件。灯泡是 three.js 粒子云 + DOM/SVG 轨道卫星的混合；轨道进动把 2D 假投影升级为伪3D 环投影。

**Tech Stack:** 原生 HTML/CSS/JS（无框架、无构建）；内联 three r160（vendor 块，已在文件里）；验证用 playwright-cli 视觉门（**无** Vitest/单测工具）。

## Global Constraints（每个任务隐含遵守）

- **禁改区：** base64 巨块 **492–2120 行**（vendor + data）一字不动；只改 1–491 与 2121–2394。**绝不整读** index.html（498 行单行 661KB）——用 `Edit` 精确 old/new，或按行号 `Read offset/limit` 只读手写区。
- **品牌：** 恒 `WeDream AI`（W、D 大写，中间一个空格）；旧 `Wedream AI` 全部订正。
- **中文（W5）：** 所有界面文字中文；仅品牌名与供应商英文名（OPENAI/ANTHROPIC…）保留英文。
- **无外链：** 不引 Google Fonts / 外部图片 / CDN；插画用内联 SVG + CSS；中文用系统字体栈（文件已有）。
- **验证门（每个任务收尾）：** 见下「验证配方」；截图逐条核对「预期观察」+ 控制台零 error + **关浏览器并 `ps` 复核零残留（W6）**。
- **无单测：** index.html 无 Vitest/测试运行器 → 验证一律走视觉门 + 控制台，**不写单测**（写了也是测试剧场）。
- **提交：** 每任务末尾可本地 `git commit`（`feat/fix/chore(landing): …`）；**不 push、不部署**——除非用户另行指示（W4）。
- **Mac 只调试：** 本页是静态文件，直接用 playwright 打开本地文件验证即可，无需构建/服务器。

## 验证配方（每个任务复用，记为 VERIFY）

```
1. playwright-cli 打开 file:///Users/cc/newapi628/bulb-orbit/index.html，视口 1920×1080
   （禁用 --virtual-time-budget：本页 WebGL rAF 常驻，该参数恒 ~1s 成像不准）
2. 真实等待 ≥3s 墙钟，再截图
3. 逐条核对该任务「预期观察」
4. 读 console → 必须零 error（warning 记录即可）
5. 关闭浏览器（playwright-cli close / kill-all）+ ps aux|grep -iE 'playwright|headless|chrome'|grep -v grep 复核零残留（W6）
```

> 滚动类任务额外：滚到各板块位置再截图；进动类任务：间隔 ~2s 连截 2–3 张对比"椭圆宽窄/朝向变化"。

---

## 文件结构（本计划只动一个文件）

```
bulb-orbit/index.html
├── 1–491    手写区①：<head> + <style> + <body> DOM + IIFE 场景脚本（DOM/SVG 轨道卫星、门控、layout/frame）
├── 492–1794 vendor base64（three + 后期）——禁改
├── 1795–2120 data base64（点云）——禁改
└── 2121–2394 手写区②：3D 灯泡装配（three.js 粒子云/玻璃罩/辉光/交互）
```

任务按"每步后页面仍可渲染"排序，逐步演进。

---

## Task 1: 去两段式门控 + 品牌订正

**Files:** Modify `bulb-orbit/index.html`（行 7、189–198、228–230、237、252、260–270）

**目标：** 页面加载即完整呈现（删「点击点亮」），品牌显示 `WeDream AI`。

- [ ] **Step 1: 订正 `<title>`（行 7）**

Edit：
old:
```html
<title>Wedream AI · 让灵感不再受限</title>
```
new:
```html
<title>WeDream AI · 让灵感不再受限</title>
```

- [ ] **Step 2: 订正主标题品牌词（行 237）**

Edit：
old:
```html
      <span class="hl1 rise" style="--rd:.3s">Wedream AI</span>
```
new:
```html
      <span class="hl1 rise" style="--rd:.3s">WeDream AI</span>
```

- [ ] **Step 3: body 默认 `lit`，删门控 hash 脚本（行 228–230）**

Edit：
old:
```html
<body class="pre">
  <script>/* 直达终态入口（截图/演示用）：index.html#final */
    if (/final|lit/i.test(location.hash + location.search)) document.body.className = 'lit';</script>
```
new:
```html
<body class="lit">
```

- [ ] **Step 4: 删「点击点亮」提示（行 252）**

Edit：
old:
```html
  <div id="hint" aria-hidden="true">点 击 点 亮</div>
```
new:
```html
```
（整行删除，替换为空。）

- [ ] **Step 5: IIFE 里恒 lit、拆除 ignite 门控（行 260–270）**

Edit：
old:
```js
  /* ---------------- 两段式交互：pre(只亮灯泡) --点击/回车/空格--> lit(全景点亮) ---------------- */
  const body = document.body;
  let revealT0 = body.classList.contains('lit') ? -1e9 : Infinity;   /* 秒；-1e9 = 直达终态 */
  function ignite() {
    if (!body.classList.contains('pre')) return;
    body.classList.remove('pre');
    body.classList.add('lit');
    revealT0 = performance.now() / 1000;
  }
  addEventListener('pointerdown', ignite);
  addEventListener('keydown', e => { if (e.key === 'Enter' || e.key === ' ') ignite(); });
```
new:
```js
  /* ---------------- 直接呈现：加载即完整 2:2:1（入场动画播一次）---------------- */
  const body = document.body;
  let revealT0 = performance.now() / 1000;   /* 秒；入场基准 = 加载时刻，chip 逐个浮现播一次 */
```

- [ ] **Step 6: 删 `body.pre` 隐藏样式（行 189–198）**

Edit：
old:
```css
  /* ---------- 两段式交互：pre = 只亮灯泡蓄势；lit = 点击后全景点亮 ---------- */
  body.pre { cursor:pointer; }
  body.pre .chip { pointer-events:none; }
  #bg, #grid { transition:opacity 1.6s ease .1s; }
  #orbits    { transition:opacity 1.5s ease .12s; }
  .ring      { transition:opacity 1.4s ease .55s; }
  body.pre :is(#bg, #grid, #orbits, .ring) { opacity:0; }
  .p   { transition:visibility 0s linear .95s; }   /* lit 后稍晚并入背景氛围 */
  .ray { transition:visibility 0s linear .7s; }
  body.pre :is(.p, .ray) { visibility:hidden; }
```
new:
```css
  /* 直接呈现：无 pre 隐藏；入场靠 body.lit .rise 与 chip 逐个浮现 */
```

> 注：`#bulb3d` 脚本里（2365–2376 行）的 `pointerdown` surge 逻辑仍引用 `body.classList.contains('pre')`——`pre` 类已不存在，该分支恒 false、自然退化为"点灯泡区域 surge"，本任务不改；Task 5 再接管点击语义。`#hint` 的 CSS（209–219 行）成孤儿无害，Task 6 清 CSS 时一并删。

- [ ] **Step 7: VERIFY**

预期观察：① 打开即完整呈现（**无需点击**），灯泡亮、24 卫星浮现、轨道能量线在；② 顶部中央标题为 **`WeDream AI`**（大写 D、有空格）+ `让灵感不再受限` + `自由穿梭于顶尖大模型之间`；③ 无「点 击 点 亮」字样；④ 控制台零 error。

- [ ] **Step 8: Commit**

```bash
git add bulb-orbit/index.html
git commit -m "feat(landing): direct reveal (drop click-gate) + brand WeDream AI"
```

---

## Task 2: 2:2:1 Hero 布局（左栏左对齐 + 视觉中心移到 x≈60%）

**Files:** Modify `bulb-orbit/index.html`（行 102、152–157、167、152–185 的 `#hero`；IIFE `layout()` 行 422）

**目标：** 三栏 2:2:1；品牌落左栏左对齐垂直居中；灯泡/轨道/卫星视觉中心移到中栏 x≈60%。

- [ ] **Step 1: 加 `--cx` 变量并把居中锚点改为 60%（行 102）**

Edit：
old:
```css
  :root { --chip:56px; --cy:57%; --b3d:min(73.1vh, 98.6vw); }   /* --cy must match CY in the JS layout; --b3d = 1.7×灯泡高 */
```
new:
```css
  :root { --chip:56px; --cy:57%; --cx:60%; --b3d:min(64vh, 66vw); }   /* --cx=中栏中心；--cy match JS layout；--b3d 略收使灯泡落中栏 */
```

- [ ] **Step 2: 把 `left:50%` 锚点批量改成 `var(--cx)`**

对以下选择器的 `left:50%;` → `left:var(--cx);`（逐个 Edit，保留各自其余属性）：
`.ring`(31)、`.fp`(57)、`.ray`(65)、`#halo`(76)、`#bulb`(86)、`#bulb3d`(93)、`.chip`(104)。

示例（`#bulb3d` 行 92–93）：
old:
```css
  #bulb3d {   /* 3D 粒子灯泡画布：占原 #halo/#bulb 层位，方形、居中于 --cy，事件全穿透 */
    position:absolute; left:50%; top:var(--cy); z-index:6; pointer-events:none;
```
new:
```css
  #bulb3d {   /* 3D 粒子灯泡画布：方形，居中于 (--cx,--cy)，事件全穿透 */
    position:absolute; left:var(--cx); top:var(--cy); z-index:6; pointer-events:none;
```
其余 6 个同理（只改 `left:50%`→`left:var(--cx)`）。

- [ ] **Step 3: `#hero` 改左栏左对齐垂直居中（行 152–157）**

Edit：
old:
```css
  #hero {
    position:fixed; left:0; right:0; top:5vh; z-index:13;
    display:flex; flex-direction:column; align-items:center; gap:12px;
    text-align:center; pointer-events:none;
    font-family:"PingFang SC","HarmonyOS Sans SC","MiSans","Microsoft YaHei",system-ui,sans-serif;
  }
```
new:
```css
  #hero {
    position:fixed; left:0; top:0; width:40%; height:100%; z-index:13;
    display:flex; flex-direction:column; align-items:flex-start; justify-content:center; gap:14px;
    padding-left:clamp(32px, 5vw, 96px); padding-right:24px;
    text-align:left; pointer-events:none;
    font-family:"PingFang SC","HarmonyOS Sans SC","MiSans","Microsoft YaHei",system-ui,sans-serif;
  }
```

- [ ] **Step 4: 副标 `#sub` 字距左对齐微调（行 181–185，可选保留）**

Edit：
old:
```css
  #sub {
    margin-top:2px; color:rgba(160,184,216,.88);
    font-size:clamp(13px, 1.5vw, 16px);
    letter-spacing:.34em; padding-left:.34em;
  }
```
new:
```css
  #sub {
    margin-top:2px; color:rgba(160,184,216,.88);
    font-size:clamp(13px, 1.5vw, 16px);
    letter-spacing:.18em;
  }
```

- [ ] **Step 5: `layout()` 的 cx 移到 60%（行 422）**

Edit：
old:
```js
      const cx = w / 2, cy = h * 0.57;   /* 0.57 must match --cy in CSS */
```
new:
```js
      const cx = w * 0.60, cy = h * 0.57;   /* 0.60 must match --cx；0.57 match --cy */
```

- [ ] **Step 6: VERIFY**

预期观察：① 左 40% 是品牌区（徽章/`WeDream AI`/两行标语）左对齐、垂直居中；② 灯泡 + 双轨 + 卫星整体视觉中心落在中栏（x≈60%），不再屏幕正中；③ 左栏文字不被灯泡直接压住（青色辉光溢到左侧属正常/预期）；④ 右侧 ~20% 目前留空（Task 4 放 HUD 卡）；⑤ 控制台零 error。若轨道左翼过多压左栏文字，微调 `layout()` 里 `a = Math.min(w*0.42, h*0.52)` 的 0.42→0.36。

- [ ] **Step 7: Commit**

```bash
git add bulb-orbit/index.html
git commit -m "feat(landing): 2:2:1 hero — left brand column + bulb centered at x≈60%"
```

---

## Task 3: 轨道伪3D 进动（绕竖轴翻转、宽窄呼吸）

**Files:** Modify `bulb-orbit/index.html`（IIFE：bands 定义 311–314、`layout()` 段构建 417–444、`frame()` 456–488）

**Interfaces:**
- Produces：`projectRing(band, phi, t) → {x,y,z}`（单位坐标）；`frame()` 每帧用它同时定位卫星与重建能量线。

**目标：** 每条轨道当作"绕竖(Y)轴进动的立体环"重新投影：卫星沿轨道跑（保留），整条环随 ψ 翻转、椭圆宽窄呼吸。A 顺 45s / B 逆 70s。

- [ ] **Step 1: 扩展 bands 配置（行 311–314）**

Edit：
old:
```js
  const bands = [
    { tilt: 20 * DEG,  period: 40, dir: +1, order: ORDER_A, phase0: 0,        g: document.getElementById('bandA') },
    { tilt: -20 * DEG, period: 52, dir: -1, order: ORDER_B, phase0: TAU / 24, g: document.getElementById('bandB') }
  ];
```
new:
```js
  /* 伪3D 环：incl=环相对水平面倾角(定椭圆开合)，period=卫星沿轨周期，
     precessPeriod/precessDir=整条环绕竖轴进动(与 period 错开，防合拍)。以下常量在视觉门里微调。 */
  const bands = [
    { incl: 0.62, period: 40, dir: +1, precessPeriod: 45, precessDir: +1, order: ORDER_A, phase0: 0,        g: document.getElementById('bandA') },
    { incl: 0.92, period: 52, dir: -1, precessPeriod: 70, precessDir: -1, order: ORDER_B, phase0: TAU / 24, g: document.getElementById('bandB') }
  ];
```

- [ ] **Step 2: 加投影纯函数（紧接 bands 定义之后插入）**

在 `const SVG_NS = ...`（行 319）之前插入：
```js
  /* 伪3D 投影：水平单位环 → RotX(incl) 抬起 → RotY(ψ) 进动 → 单位坐标(×S 落屏)。
     ψ 随时间转 → 环朝向变、投影椭圆宽窄呼吸；z 作近远深度。 */
  function ringPsi(band, t) { return band.precessDir * TAU * (t / band.precessPeriod); }
  function projectRing(band, phi, psi) {
    const cf = Math.cos(phi), sf = Math.sin(phi);
    const ci = Math.cos(band.incl), si = Math.sin(band.incl);
    const cp = Math.cos(psi), sp = Math.sin(psi);
    return {
      x: cf * cp + sf * ci * sp,
      y: -sf * si,
      z: -cf * sp + sf * ci * cp
    };
  }
```

- [ ] **Step 3: `layout()` 只保留尺寸/环基准，移除一次性段构建（行 412–451）**

把 `layout()` 内**从 `for (const band of bands) {` 到该循环结束**（约 417–444）的整块段端点计算删除，改为只缓存每条环的像素半径 `S` 与屏心 `cx/cy`。Edit：
old（行 413–451 的函数体，节选起止）:
```js
  function layout() {
    const w = innerWidth, h = innerHeight, m = Math.min(w, h);
    const a = Math.min(w * 0.42, h * 0.52);   /* semi-major (leaves the top clear for the hero copy) */
    const bb = a * 0.30;                      /* semi-minor */
    for (const band of bands) {
      band.a = a; band.b = bb;
      band.cos = Math.cos(band.tilt); band.sin = Math.sin(band.tilt);
      band.ymax = Math.hypot(a * band.sin, bb * band.cos);
      /* rebuild band segments: brightness/width peak near the bulb, fade at the wings */
      const cx = w * 0.60, cy = h * 0.57;   /* 0.60 must match --cx；0.57 match --cy */
      const pt = th => {
        const ex = a * Math.cos(th), ey = bb * Math.sin(th);
        return [cx + ex * band.cos - ey * band.sin, cy + ex * band.sin + ey * band.cos];
      };
      for (let i = 0; i < SEG; i++) {
        const t1 = i / SEG * TAU, t2 = (i + 1) / SEG * TAU, tm = (t1 + t2) / 2;
        const [x1, y1] = pt(t1), [x2, y2] = pt(t2);
        const r = Math.hypot(a * Math.cos(tm), bb * Math.sin(tm));   /* distance to center */
        const tl = (r - bb) / (a - bb);                              /* 0 near bulb, 1 at wings */
        const pe = Math.pow(1 - tl, 1.35);                           /* 1 near bulb, 0 at wings */
        const put = (seg, wdt, stroke) => {
          seg.setAttribute('x1', x1.toFixed(1)); seg.setAttribute('y1', y1.toFixed(1));
          seg.setAttribute('x2', x2.toFixed(1)); seg.setAttribute('y2', y2.toFixed(1));
          seg.setAttribute('stroke-width', wdt.toFixed(2)); seg.setAttribute('stroke', stroke);
        };
        put(band.layers.halo[i], (7 + 5 * pe) * 2.6, `rgba(80,160,255,${((0.05 + 0.13 * pe) * 0.34).toFixed(3)})`);
        put(band.layers.mid[i], 1.8 + 1.2 * pe,
            `rgba(${Math.round(77 + 48 * pe)},${Math.round(166 + 18 * pe)},255,${(0.18 + 0.72 * pe).toFixed(3)})`);
        put(band.layers.cglow[i], 3.4, `rgba(150,215,255,${(0.04 + 0.24 * pe).toFixed(3)})`);
        put(band.layers.core[i], 1, `rgba(207,232,255,${(0.10 + 0.8 * pe).toFixed(3)})`);
      }
    }
    document.documentElement.style.setProperty(
      '--chip', Math.round(Math.min(60, Math.max(38, m * 0.058))) + 'px');
    for (const r of rings) {
      const d = Math.round(m * r.k);
      r.el.style.width = d + 'px'; r.el.style.height = d + 'px';
    }
  }
```
new:
```js
  let CX = 0, CY = 0, RS = 0;   /* 屏心与环像素半径，layout() 更新，frame() 用 */
  function layout() {
    const w = innerWidth, h = innerHeight, m = Math.min(w, h);
    CX = w * 0.60; CY = h * 0.57;              /* 须与 --cx / --cy 一致 */
    RS = Math.min(w * 0.34, h * 0.46);         /* 环像素半径（视觉门微调 0.34/0.46） */
    document.documentElement.style.setProperty(
      '--chip', Math.round(Math.min(60, Math.max(38, m * 0.058))) + 'px');
    for (const r of rings) {
      const d = Math.round(m * r.k);
      r.el.style.width = d + 'px'; r.el.style.height = d + 'px';
    }
  }
```

- [ ] **Step 4: `frame()` 改伪3D 进动（卫星 + 能量线每帧重投影，行 456–488）**

Edit（整块替换 `frame` 函数体的 chips/flows 两段与线重建）:
old（456–488，节选起止）:
```js
  function frame(now) {
    const t = now / 1000;
    const tr = t - revealT0;               /* 点亮后经过的秒数（点亮前为负） */
    for (const c of chips) {
      const B = c.band;
      const th = c.phase + B.dir * TAU * (t / B.period);
      const ex = B.a * Math.cos(th), ey = B.b * Math.sin(th);
      const X = ex * B.cos - ey * B.sin;
      const Y = ex * B.sin + ey * B.cos;
      const d = (Y / B.ymax + 1) / 2;              /* 0 = far (top), 1 = near (bottom) */
      const cr = easeOut3(clamp01((tr - c.idx * 0.055) / 0.75));   /* 点亮后沿轨道逐个浮现 */
      c.el.style.transform =
        `translate3d(${X.toFixed(2)}px,${Y.toFixed(2)}px,0) scale(${((0.7 + d * 0.4) * (0.55 + 0.45 * cr)).toFixed(3)})`;
      c.el.style.opacity = ((0.6 + d * 0.4) * cr).toFixed(3);
      const z = 2 + Math.round(d * 8);             /* 2..10 around bulb at z:6 */
      if (z !== c.z) { c.z = z; c.el.style.zIndex = z; }
    }
    for (const f of flows) {
      const B = f.band;
      const th = f.phase + B.dir * TAU * (t / f.period);
      const ex = B.a * Math.cos(th), ey = B.b * Math.sin(th);
      const X = ex * B.cos - ey * B.sin;
      const Y = ex * B.sin + ey * B.cos;
      const d = (Y / B.ymax + 1) / 2;
      const pe = Math.pow(1 - (Math.hypot(ex, ey) - B.b) / (B.a - B.b), 1.35);
      f.el.style.transform =
        `translate3d(${X.toFixed(2)}px,${Y.toFixed(2)}px,0) scale(${(0.7 + d * 0.5).toFixed(3)})`;
      const fr = easeOut3(clamp01((tr - 0.35) / 1.0));             /* 流光稍晚汇入 */
      f.el.style.opacity = ((0.2 + 0.8 * pe) * (0.5 + 0.5 * d) * fr).toFixed(3);
    }
    requestAnimationFrame(frame);
  }
  frame(performance.now());   /* position chips before first paint; frame() self-schedules the loop */
```
new:
```js
  const REDUCED = matchMedia('(prefers-reduced-motion: reduce)').matches;
  function rebuildBand(B, psi, t) {   /* 每帧按当前 ψ 重投影 64 段能量线，近亮远暗 */
    for (let i = 0; i < SEG; i++) {
      const a1 = i / SEG * TAU, a2 = (i + 1) / SEG * TAU, am = (a1 + a2) / 2;
      const p1 = projectRing(B, a1, psi), p2 = projectRing(B, a2, psi), pm = projectRing(B, am, psi);
      const x1 = CX + p1.x * RS, y1 = CY + p1.y * RS;
      const x2 = CX + p2.x * RS, y2 = CY + p2.y * RS;
      const d = (pm.z + 1) / 2;               /* 0 远 .. 1 近 */
      const pe = 0.25 + 0.75 * d;             /* 近端亮、远端淡（深度剪影） */
      const put = (seg, wdt, stroke) => {
        seg.setAttribute('x1', x1.toFixed(1)); seg.setAttribute('y1', y1.toFixed(1));
        seg.setAttribute('x2', x2.toFixed(1)); seg.setAttribute('y2', y2.toFixed(1));
        seg.setAttribute('stroke-width', wdt.toFixed(2)); seg.setAttribute('stroke', stroke);
      };
      put(B.layers.halo[i], (7 + 5 * pe) * 2.2, `rgba(80,160,255,${((0.05 + 0.13 * pe) * 0.34).toFixed(3)})`);
      put(B.layers.mid[i], 1.6 + 1.2 * pe,
          `rgba(${Math.round(77 + 48 * pe)},${Math.round(166 + 18 * pe)},255,${(0.14 + 0.72 * pe).toFixed(3)})`);
      put(B.layers.cglow[i], 3.2, `rgba(150,215,255,${(0.04 + 0.24 * pe).toFixed(3)})`);
      put(B.layers.core[i], 1, `rgba(207,232,255,${(0.10 + 0.8 * pe).toFixed(3)})`);
    }
  }
  let lineTick = 0;
  function frame(now) {
    const t = now / 1000;
    const tr = t - revealT0;
    for (const B of bands) B.psi = ringPsi(B, REDUCED ? 6.0 : t);   /* reduced-motion 冻结进动相位 */
    /* 能量线每帧重建（掉帧兜底：改成 if (++lineTick % 2 === 0) 每 2 帧一次） */
    ++lineTick;
    for (const B of bands) rebuildBand(B, B.psi, t);
    for (const c of chips) {
      const B = c.band;
      const phi = c.phase + B.dir * TAU * (t / B.period);
      const p = projectRing(B, phi, B.psi);
      const X = p.x * RS, Y = p.y * RS;
      const d = (p.z + 1) / 2;
      const cr = easeOut3(clamp01((tr - c.idx * 0.055) / 0.75));
      c.el.style.transform =
        `translate3d(${(CX + X - innerWidth * 0.60).toFixed(2)}px,${(CY + Y - innerHeight * 0.57).toFixed(2)}px,0) scale(${((0.62 + d * 0.5) * (0.55 + 0.45 * cr)).toFixed(3)})`;
      c.el.style.opacity = ((0.5 + 0.5 * d) * cr).toFixed(3);
      const z = 2 + Math.round(d * 8);
      if (z !== c.z) { c.z = z; c.el.style.zIndex = z; }
    }
    for (const f of flows) {
      const B = f.band;
      const phi = f.phase + B.dir * TAU * (t / f.period);
      const p = projectRing(B, phi, B.psi);
      const d = (p.z + 1) / 2;
      const fr = easeOut3(clamp01((tr - 0.35) / 1.0));
      f.el.style.transform =
        `translate3d(${(CX + p.x * RS - innerWidth * 0.60).toFixed(2)}px,${(CY + p.y * RS - innerHeight * 0.57).toFixed(2)}px,0) scale(${(0.7 + d * 0.5).toFixed(3)})`;
      f.el.style.opacity = ((0.25 + 0.75 * d) * fr).toFixed(3);
    }
    requestAnimationFrame(frame);
  }
  frame(performance.now());
```
> 说明：`.chip`/`.fp` 的 CSS 锚点已是 `left:var(--cx); top:var(--cy)`（60%/57%），故 transform 里减去 `innerWidth*0.60 / innerHeight*0.57` 把"绝对屏坐标"换算成"相对锚点的位移"。`REDUCED` 时 ψ 用固定 6.0s 相位（进动停），卫星仍缓行。

- [ ] **Step 5: VERIFY**

预期观察（间隔 ~2s 连截 2–3 张对比）：① 两条轨道**整体在缓慢旋转**——椭圆的宽窄/朝向随时间变化（伪3D 翻转呼吸），A、B 方向相反；② 卫星仍沿各自轨道运行，与进动叠加、不合拍；③ 近端卫星大而亮、远端小而淡（深度正确）；④ 能量线随环重建、不撕裂；⑤ 1080p 主观 ≥50fps 无明显卡顿（掉帧则按 Step 4 注释启用"每 2 帧重建线" + 调小 SEG）；⑥ 控制台零 error。

- [ ] **Step 6: Commit**

```bash
git add bulb-orbit/index.html
git commit -m "feat(landing): pseudo-3D orbit precession (rings rotate around bulb)"
```

---

## Task 4: 右栏动态 HUD 卡 + 24 家 MODELS + 悬停切卡

**Files:** Modify `bulb-orbit/index.html`（`<style>` 末尾加卡样式；`#hero` 后加卡 DOM；IIFE 加 `MODELS`/`renderCard`/chip 事件）

**Interfaces:**
- Produces：`MODELS`（id→{name,provider,desc,scene,telemetry,color}）、`DEFAULT_MODEL`、`renderCard(model)`、chip 上 `data-mid` 属性。Task 5 复用 `MODELS[id].color`。

- [ ] **Step 1: 右栏卡 CSS（在 `</style>` 前、行 225 附近插入）**

```css
  /* ---------- 右栏动态 HUD 卡 ---------- */
  #hud {
    position:fixed; right:clamp(20px,2.4vw,48px); top:50%; transform:translateY(-50%);
    width:min(20vw,300px); min-width:230px; z-index:13; pointer-events:none;
    padding:20px 20px 18px; border-radius:16px;
    background:rgba(10,20,42,.55); backdrop-filter:blur(14px); -webkit-backdrop-filter:blur(14px);
    border:1px solid rgba(120,180,255,.22); border-left:3px solid #00f0ff;
    box-shadow:0 8px 40px rgba(0,10,40,.45), inset 0 0 20px rgba(80,170,255,.06);
    font-family:"PingFang SC","HarmonyOS Sans SC","MiSans","Microsoft YaHei",system-ui,sans-serif;
    transition:border-left-color .35s ease, opacity .15s ease;
  }
  #hud.swapping { opacity:0; }
  #hud .hud-prov { display:flex; align-items:center; gap:8px; color:#9fd8ff; font-size:12.5px; letter-spacing:.12em; }
  #hud .hud-dot { width:8px; height:8px; border-radius:50%; background:#00f0ff; box-shadow:0 0 8px currentColor; }
  #hud .hud-name { margin-top:8px; color:#f2f7ff; font-size:19px; font-weight:700; }
  #hud .hud-desc { margin-top:10px; color:rgba(198,214,238,.9); font-size:13px; line-height:1.6; }
  #hud .hud-scene { margin-top:8px; color:rgba(150,175,210,.8); font-size:12px; line-height:1.5; }
  #hud .hud-tele { margin-top:12px; padding-top:10px; border-top:1px solid rgba(120,180,255,.14);
    color:#8fd0ff; font-size:11.5px; letter-spacing:.06em; }
```

- [ ] **Step 2: 右栏卡 DOM（在 `</header>` 行 241 之后插入）**

```html
  <aside id="hud">
    <div class="hud-prov"><span class="hud-dot"></span><span id="hud-prov">系统运行中</span></div>
    <div class="hud-name" id="hud-name">WeDream 核心</div>
    <div class="hud-desc" id="hud-desc">中央智能核心，环绕的卫星代表已接入的大模型，实时互联。</div>
    <div class="hud-scene" id="hud-scene"></div>
    <div class="hud-tele" id="hud-tele">核心负载 100%</div>
  </aside>
```

- [ ] **Step 3: 加 `MODELS` 数据 + 默认（IIFE 内 `LOGOS` 定义之后、行 301 之后插入）**

```js
  /* 24 家 HUD 文案（proposal 附录A）+ 品牌色（附录B）；id 对齐 LOGOS/ORDER_A/ORDER_B */
  const DEFAULT_MODEL = { name:'WeDream 核心', provider:'系统运行中', desc:'中央智能核心，环绕的卫星代表已接入的大模型，实时互联。', scene:'', telemetry:'核心负载 100%', color:'#00f0ff' };
  const MODELS = {
    openai:{name:'GPT 系列',provider:'OPENAI',desc:'多模态旗舰，通用智能与工具调用能力顶尖，生态最成熟。',scene:'通用对话、智能体、多模态理解与生成。',telemetry:'多模态管线在线',color:'#10d075'},
    anthropic:{name:'Claude 系列',provider:'ANTHROPIC',desc:'代码与复杂推理标杆，长上下文稳定可靠，安全对齐出色。',scene:'编程智能体、长文档分析、严肃写作。',telemetry:'200K 上下文激活',color:'#d97757'},
    gemini:{name:'Gemini 系列',provider:'GOOGLE DEEPMIND',desc:'原生多模态引擎，百万级 token 上下文，视频音频理解强。',scene:'多模态分析、超长资料库问答。',telemetry:'1M token 管线',color:'#9020f0'},
    meta:{name:'Llama 系列',provider:'META AI',desc:'开源权重旗舰，社区生态庞大，可完全私有化部署。',scene:'私有化部署、定制微调、开源研究。',telemetry:'开源权重可用',color:'#0596ff'},
    mistral:{name:'Mistral 系列',provider:'MISTRAL AI',desc:'欧洲开源新锐，小模型效率极高，MoE 架构先行者。',scene:'低成本推理、边缘部署、多语种应用。',telemetry:'MoE 引擎在线',color:'#ff7000'},
    deepseek:{name:'DeepSeek 系列',provider:'DEEPSEEK',desc:'推理与代码性价比之王，开源开放，数学推理尤强。',scene:'高性价比推理、代码生成、数学解题。',telemetry:'推理链激活',color:'#4fa3ff'},
    xai:{name:'Grok 系列',provider:'XAI',desc:'接入实时资讯流，风格鲜明，推理能力快速迭代。',scene:'实时信息问答、热点分析。',telemetry:'实时检索同步',color:'#00a0ff'},
    cohere:{name:'Command 系列',provider:'COHERE',desc:'企业级检索与嵌入见长，RAG 工具链完善，多语种企业部署。',scene:'企业知识库、语义搜索、RAG 应用。',telemetry:'RAG 管线在线',color:'#ff7759'},
    midjourney:{name:'Midjourney',provider:'MIDJOURNEY',desc:'顶级艺术风格图像生成，美学表现力公认最强。',scene:'概念设计、海报插画、艺术创作。',telemetry:'渲染农场在线',color:'#9bb5ff'},
    stability:{name:'Stable Diffusion 系列',provider:'STABILITY AI',desc:'开源图像生成标杆，插件生态丰富，可本地部署。',scene:'可控图像生成、二次开发、本地出图。',telemetry:'扩散管线就绪',color:'#b266ff'},
    huggingface:{name:'开源模型枢纽',provider:'HUGGING FACE',desc:'全球最大开源模型社区，数十万模型即取即用。',scene:'开源模型试用、推理 API、数据集。',telemetry:'Hub 已连接',color:'#ffd21e'},
    perplexity:{name:'Sonar 系列',provider:'PERPLEXITY',desc:'AI 原生搜索引擎，答案附引用来源，实时联网。',scene:'联网问答、资料调研、事实核查。',telemetry:'联网检索激活',color:'#2bb8ce'},
    qwen:{name:'通义千问系列',provider:'阿里云',desc:'国产开源旗舰，代码与多语种能力强，模型尺寸谱系最全。',scene:'中文对话、代码生成、结构化输出。',telemetry:'全尺寸谱系在线',color:'#6b6dff'},
    minimax:{name:'MiniMax 系列',provider:'MINIMAX',desc:'长上下文与多模态并进，语音合成表现出色。',scene:'长文处理、语音应用、角色对话。',telemetry:'百万级上下文',color:'#b987ff'},
    doubao:{name:'豆包大模型',provider:'字节跳动',desc:'高并发低成本，中文日常对话体验佳，规模化验证充分。',scene:'大规模 C 端应用、智能客服、翻译。',telemetry:'火山引擎管线',color:'#39c5ff'},
    stepfun:{name:'Step 系列',provider:'阶跃星辰',desc:'多模态理解见长，万亿参数 MoE 路线探索者。',scene:'图文理解、多模态创作。',telemetry:'多模态管线在线',color:'#92a2ff'},
    kimi:{name:'Kimi 系列',provider:'月之暗面',desc:'超长上下文先行者，网页与文档整理利器，推理模型开源。',scene:'长文档阅读、资料汇总、深度推理。',telemetry:'超长上下文激活',color:'#6c7cff'},
    huawei_pangu:{name:'盘古大模型',provider:'华为云',desc:'行业大模型深耕，政企场景与昇腾算力生态深度结合。',scene:'政企行业方案、私有云部署。',telemetry:'昇腾集群在线',color:'#ef3340'},
    baidu_wenxin:{name:'文心大模型',provider:'百度',desc:'中文知识增强路线，检索增强与插件生态成熟。',scene:'中文创作、企业应用、搜索增强。',telemetry:'知识增强激活',color:'#2d78ff'},
    zeroone_ai:{name:'Yi 系列',provider:'零一万物',desc:'中英双语开源佳作，长文本与多模态兼备。',scene:'双语应用、开源定制。',telemetry:'双语管线在线',color:'#61d3ff'},
    tencent_hunyuan:{name:'混元大模型',provider:'腾讯',desc:'全链路自研，文生图与视频多模态齐全，微信生态天然接入。',scene:'内容创作、腾讯生态应用。',telemetry:'多模态就绪',color:'#25d6ff'},
    baichuan_ai:{name:'Baichuan 系列',provider:'百川智能',desc:'中文开源先锋，医疗等垂直领域持续深化。',scene:'中文垂直领域、开源部署。',telemetry:'垂直增强在线',color:'#2de2a0'},
    glm_chatglm:{name:'GLM 系列',provider:'智谱 AI',desc:'清华系技术底蕴，Agent 与代码能力强，开源开放。',scene:'智能体开发、代码辅助、学术研究。',telemetry:'Agent 管线激活',color:'#6f7bff'},
    iflytek_spark:{name:'星火大模型',provider:'科大讯飞',desc:'语音交互天然优势，教育医疗行业落地深。',scene:'语音助手、教育应用、办公纪要。',telemetry:'语音引擎在线',color:'#ff395d'}
  };
  const hudEls = {
    box: document.getElementById('hud'),
    prov: document.getElementById('hud-prov'), name: document.getElementById('hud-name'),
    desc: document.getElementById('hud-desc'), scene: document.getElementById('hud-scene'),
    tele: document.getElementById('hud-tele'), dot: document.querySelector('#hud .hud-dot')
  };
  function renderCard(m) {
    hudEls.prov.textContent = m.provider;
    hudEls.name.textContent = m.name;
    hudEls.desc.textContent = m.desc;
    hudEls.scene.textContent = m.scene || '';
    hudEls.tele.textContent = m.telemetry;
    hudEls.box.style.borderLeftColor = m.color;
    hudEls.dot.style.background = m.color;
  }
```

- [ ] **Step 4: chip 打 `data-mid` + 悬停切卡（改 chip 构建 336–349，加事件）**

在 chip 构建循环里给元素加 `data-mid`。Edit：
old:
```js
      const el = document.createElement('div');
      el.className = 'chip ' + (b === 0 ? 'main' : 'alt');  /* main orbit = larger focal nodes */
```
new:
```js
      const el = document.createElement('div');
      el.className = 'chip ' + (b === 0 ? 'main' : 'alt');  /* main orbit = larger focal nodes */
      el.dataset.mid = name;
```
然后在 `chips.push(...)` 循环结束后（行 349 之后）插入事件绑定：
```js
  /* 悬停切卡：淡出→换→淡入；移开 1s 回默认 */
  let hudTimer = 0;
  function swapCard(m) {
    hudEls.box.classList.add('swapping');
    setTimeout(() => { renderCard(m); hudEls.box.classList.remove('swapping'); }, 150);
  }
  for (const c of chips) {
    c.el.style.pointerEvents = 'auto';   /* chip 可交互（#hero/#hud 均 pointer-events:none 不挡） */
    c.el.style.cursor = 'pointer';
    c.el.addEventListener('mouseenter', () => { clearTimeout(hudTimer); swapCard(MODELS[c.el.dataset.mid]); });
    c.el.addEventListener('mouseleave', () => { hudTimer = setTimeout(() => swapCard(DEFAULT_MODEL), 1000); });
  }
```

- [ ] **Step 5: VERIFY**

预期观察：① 默认右栏卡 = 「系统运行中 / WeDream 核心 / 中央智能核心… / 核心负载 100%」，左边框青色；② 用 playwright 把指针移到某卫星（如 Anthropic）→ 卡 ~0.15s 切成「ANTHROPIC / Claude 系列 / 代码与复杂推理… / 200K 上下文激活」，左边框变品牌橙 `#d97757`；③ 移开 1s 后回默认卡、边框回青；④ 卫星仍可悬停放大（原 CSS `:hover`）；⑤ 控制台零 error。

- [ ] **Step 6: Commit**

```bash
git add bulb-orbit/index.html
git commit -m "feat(landing): dynamic HUD card + 24-model data + hover-to-switch"
```

---

## Task 5: 点击卫星 → 灯泡核心染品牌色

**Files:** Modify `bulb-orbit/index.html`（3D 装配区 2126–2176 着色器加 uniform；2257–2272 material；2333–2350 renderTick；2378–2383 `__bulb3d` 暴露 `setCoreColor`；IIFE Task 4 事件加 click）

**Interfaces:**
- Consumes：`MODELS[id].color`（Task 4）。
- Produces：`window.__bulb3d.setCoreColor(hexOrNull)`（null=回青）。

- [ ] **Step 1: 着色器 edgeColor 改用 uniform（行 2156–2158）**

Edit：
old:
```glsl
    float radialDist = length(position.xz);
    vec3 coreColor = vec3(0.85, 0.94, 1.0);
    vec3 edgeColor = vec3(0.0, 0.65, 1.0);
    vColor = mix(coreColor, edgeColor, smoothstep(0.06, 0.35, radialDist));
```
new:
```glsl
    float radialDist = length(position.xz);
    vec3 coreColor = vec3(0.85, 0.94, 1.0);
    vColor = mix(coreColor, uEdgeColor, smoothstep(0.06, 0.35, radialDist));
```
并在顶部 uniform 声明区（行 2128–2133 之间）加：
old:
```glsl
  uniform float uSize;
```
new:
```glsl
  uniform float uSize;
  uniform vec3 uEdgeColor;
```

- [ ] **Step 2: material uniforms 加 `uEdgeColor`（行 2260–2268）**

Edit：
old:
```js
      uTime: { value: 0.0 },
      uSize: { value: 0.055 },   /* yun 原值 0.045；2026-07-07 调大补饱满（删灯丝后中心发光量下降，用户定夺） */
```
new:
```js
      uTime: { value: 0.0 },
      uSize: { value: 0.055 },
      uEdgeColor: { value: new Vector3(0.0, 0.65, 1.0) },   /* 默认青；setCoreColor 改写 */
```

- [ ] **Step 3: 目标色 lerp + setCoreColor（renderTick 行 2333–2350 内加过渡；文件顶部状态区加目标色）**

在 `let surgeT0 = -1e9;`（行 2214）附近加：
```js
const coreCur = { r: 0.0, g: 0.65, b: 1.0 };     /* 当前 edgeColor（渐变用） */
const coreTarget = { r: 0.0, g: 0.65, b: 1.0 };  /* 目标 edgeColor */
```
在 `renderTick` 里 `root.rotation.y = t * 0.02;`（行 2344）之前插入：
```js
  {   /* edgeColor 平滑趋近目标 + 点光源/辉光联动 */
    const k = 1 - Math.exp(-6 * dt);
    coreCur.r += (coreTarget.r - coreCur.r) * k;
    coreCur.g += (coreTarget.g - coreCur.g) * k;
    coreCur.b += (coreTarget.b - coreCur.b) * k;
    u.uEdgeColor.value.set(coreCur.r, coreCur.g, coreCur.b);
    pointLight.color.setRGB(0.0 + coreCur.r, 0.94 * (0.3 + 0.7 * coreCur.g), coreCur.b);
    glowSprite.material.color.setRGB(coreCur.r, 0.5 + 0.5 * coreCur.g, coreCur.b);
  }
```

- [ ] **Step 4: 暴露 `setCoreColor`（行 2378–2383 的 `window.__bulb3d`）**

Edit：
old:
```js
window.__bulb3d = {
  surge,
  get surgeT0() { return surgeT0; },
  get repel() { return particleMaterial ? particleMaterial.uniforms.uRepelStrength.value : -1; },
  get points() { return bulbPoints ? bulbPoints.geometry.attributes.position.count : 0; }
};
```
new:
```js
function hexToRgb(hex) {
  const n = parseInt(hex.slice(1), 16);
  return { r: ((n >> 16) & 255) / 255, g: ((n >> 8) & 255) / 255, b: (n & 255) / 255 };
}
function setCoreColor(hex) {   /* hex=品牌色 → 染核心；null/无 → 回青 */
  const c = hex ? hexToRgb(hex) : { r: 0.0, g: 0.65, b: 1.0 };
  coreTarget.r = c.r; coreTarget.g = c.g; coreTarget.b = c.b;
  surge();   /* 顺带迸发一次 */
}
window.__bulb3d = {
  surge, setCoreColor,
  get surgeT0() { return surgeT0; },
  get repel() { return particleMaterial ? particleMaterial.uniforms.uRepelStrength.value : -1; },
  get points() { return bulbPoints ? bulbPoints.geometry.attributes.position.count : 0; }
};
```

- [ ] **Step 5: chip click → 染色；移开回青（Task 4 的事件块里补 click，并在 mouseleave 回青）**

把 Task 4 Step 4 插入的事件块改为：
old:
```js
    c.el.addEventListener('mouseenter', () => { clearTimeout(hudTimer); swapCard(MODELS[c.el.dataset.mid]); });
    c.el.addEventListener('mouseleave', () => { hudTimer = setTimeout(() => swapCard(DEFAULT_MODEL), 1000); });
```
new:
```js
    c.el.addEventListener('mouseenter', () => { clearTimeout(hudTimer); swapCard(MODELS[c.el.dataset.mid]); });
    c.el.addEventListener('mouseleave', () => {
      hudTimer = setTimeout(() => { swapCard(DEFAULT_MODEL); window.__bulb3d?.setCoreColor(null); }, 1000);
    });
    c.el.addEventListener('click', () => { window.__bulb3d?.setCoreColor(MODELS[c.el.dataset.mid].color); });
```

- [ ] **Step 6: VERIFY**

预期观察：① 点击某卫星（如 DeepSeek）→ 灯泡核心 ~0.5s 渐变为该品牌色（`#4fa3ff` 蓝）、辉光/点光源联动、伴一次迸发；② 移开该卫星 1s 后灯泡渐回青 `#00f0ff`、右卡回默认；③ WebGL 回退（`?nowebgl`）时点击不报错（`__bulb3d` 未定义、`?.` 兜底）；④ 控制台零 error。

- [ ] **Step 7: Commit**

```bash
git add bulb-orbit/index.html
git commit -m "feat(landing): click satellite tints bulb core to brand color"
```

---

## Task 6: 解锁滚动 + Hero 包裹 + 离屏暂停 + 板块① 创作搭档

**Files:** Modify `bulb-orbit/index.html`（全局 CSS 行 10；包裹 Hero；加板块①；3D 离屏暂停）

**目标：** 页面可向下滚动；Hero=100vh 滚走、深空背景延续；滚出视口时 3D 暂停；下方出现板块①。

- [ ] **Step 1: 解锁滚动（行 10）**

Edit：
old:
```css
  html, body { width:100%; height:100%; overflow:hidden; background:#020610; }
```
new:
```css
  html, body { width:100%; background:#020610; }
  body { overflow-x:hidden; }
  #hero, #hud, #bg, #grid, #vignette { }   /* fixed 元素仍相对视口；Hero 内容在 .hero-stage 里滚走见下 */
```

- [ ] **Step 2: Hero 舞台包裹 + 板块样式（`<style>` 末尾插入）**

```css
  /* Hero 舞台：占满首屏，随滚动上移；灯泡/轨道/卫星在此层，滚出即离场 */
  .hero-stage { position:relative; height:100vh; }
  /* 让原本相对视口的画布/轨道/卫星/HUD 改为相对首屏舞台，随之滚走 */
  .hero-stage #bulb3d, .hero-stage #halo, .hero-stage #bulb,
  .hero-stage #orbits, .hero-stage .chip, .hero-stage .fp, .hero-stage .ray,
  .hero-stage .ring, .hero-stage #hero, .hero-stage #hud { position:absolute; }
  #hud { top:50%; }   /* 舞台内垂直居中 */
  /* 深空背景 #bg/#grid 仍 position:fixed（全页延续，不随滚动） */

  /* ---------- 下方内容板块通用 ---------- */
  .section { position:relative; z-index:14; padding:96px clamp(32px,7vw,140px); }
  .section-head { text-align:center; max-width:920px; margin:0 auto 56px; }
  .section-title { color:#f2f7ff; font-size:clamp(26px,3.4vw,40px); font-weight:800; letter-spacing:.02em; }
  .section-title .grad { background:linear-gradient(94deg,#3ecfff,#4d9bff 55%,#8f7bff);
    -webkit-background-clip:text; background-clip:text; color:transparent; -webkit-text-fill-color:transparent; }
  .section-sub { margin-top:14px; color:rgba(160,184,216,.85); font-size:clamp(14px,1.5vw,17px); line-height:1.6; }
  .reveal { opacity:0; transform:translateY(28px); transition:opacity .7s ease, transform .7s cubic-bezier(.22,.7,.25,1); }
  .reveal.in { opacity:1; transform:none; }
  @media (prefers-reduced-motion: reduce) { .reveal { opacity:1; transform:none; } }

  /* ① 创作搭档：4 个一键生成大卡 */
  .agent-grid { display:grid; grid-template-columns:repeat(4,1fr); gap:22px; max-width:1280px; margin:0 auto; }
  .agent-card { position:relative; border-radius:18px; overflow:hidden; min-height:300px;
    background:rgba(12,22,44,.5); border:1px solid rgba(120,180,255,.16);
    box-shadow:0 10px 40px rgba(0,10,40,.35); transition:transform .3s ease, box-shadow .3s ease; }
  .agent-card:hover { transform:translateY(-6px); box-shadow:0 18px 50px rgba(40,120,255,.28); }
  .agent-illus { height:170px; display:grid; place-items:center;   /* ← 首期内联插画；用户日后给图替换此块 */
    background:radial-gradient(circle at 50% 40%, rgba(60,150,255,.22), transparent 70%); }
  .agent-illus svg { width:88px; height:88px; }
  .agent-body { padding:18px 20px 22px; }
  .agent-name { color:#eaf2ff; font-size:18px; font-weight:700; }
  .agent-desc { margin-top:8px; color:rgba(170,190,220,.82); font-size:13px; line-height:1.6; }
  @media (max-width:1180px){ .agent-grid{ grid-template-columns:repeat(2,1fr);} }
```

- [ ] **Step 3: 用 `.hero-stage` 包裹 Hero DOM**

在 `#bg`/`#grid` 之后、`#vignette` 之前的 Hero 相关节点（`#hero`、`#hud`、`#orbits`、`#halo`、`#bulb`、`#bulb3d`）用一个 `.hero-stage` 包住。Edit：
在 `<header id="hero">` 之前插入 `<div class="hero-stage">`；在 `<canvas id="bulb3d"></canvas>`（行 250）之后插入 `</div>`（`#vignette` 留在外面作全页压角）。
具体：
old:
```html
  <header id="hero">
```
new:
```html
  <div class="hero-stage">
  <header id="hero">
```
old:
```html
  <canvas id="bulb3d"></canvas>
  <div id="vignette"></div>
```
new:
```html
  <canvas id="bulb3d"></canvas>
  </div><!-- /.hero-stage -->
  <div id="vignette"></div>
```
> 注：`.chip`/`.fp`/`.ray`/`.ring` 是 JS `document.body.appendChild` 动态加的，仍挂在 body 上、`position:absolute` 相对最近定位祖先。为让它们随舞台滚走，Step 4 把它们 append 到 `.hero-stage` 而非 body。

- [ ] **Step 4: 动态元素挂到 `.hero-stage`（IIFE 内）**

在 IIFE 顶部（`const body = document.body;` 之后）加：
```js
  const stage = document.querySelector('.hero-stage');
```
把 chip/flow/ring/particle/ray 五处 `document.body.appendChild(x)` 改为 `stage.appendChild(x)`（行 346、357、367、383、397、409）。`.p`（星尘）保留在 body（全页氛围）——即行 383、397 的星尘/motes**留 body**，仅 chip(346)/flow(357)/ring(367)/ray(409) 改 `stage`。

- [ ] **Step 5: 3D 离屏暂停（loop3d 行 2326–2331）**

Edit：
old:
```js
let last3d = 0;
function loop3d(nowMs) {
  requestAnimationFrame(loop3d);
  const t = nowMs / 1000;
  renderTick(t, Math.min(Math.max(t - last3d, 0.001), 0.05));
  last3d = t;
}
```
new:
```js
let last3d = 0, heroVisible = true;
{ const st = document.querySelector('.hero-stage');
  if (st && 'IntersectionObserver' in window)
    new IntersectionObserver(es => { heroVisible = es[0].isIntersecting; }, { threshold: 0 }).observe(st); }
function loop3d(nowMs) {
  requestAnimationFrame(loop3d);
  if (!heroVisible) return;   /* Hero 滚出视口 → 3D 停渲染，省电防空转 */
  const t = nowMs / 1000;
  renderTick(t, Math.min(Math.max(t - last3d, 0.001), 0.05));
  last3d = t;
}
```

- [ ] **Step 6: 板块① DOM（在 `</div><!-- /.hero-stage -->` 与 `#vignette` 之后、`<script>` 之前插入）**

> 每个 `.agent-illus` 内是**首期内联占位插画**（简单 SVG）；用户日后给图时，直接把 `<svg>` 换成 `<img src="…">` 或内嵌 base64 即可。

```html
  <section class="section" id="sec-agent">
    <div class="section-head reveal">
      <div class="section-title">AI Agent，不只是工具，<span class="grad">更是你的灵感创作搭档</span></div>
      <div class="section-sub">一个入口，把灵感变成对话、图片、视频与代码。</div>
    </div>
    <div class="agent-grid">
      <div class="agent-card reveal">
        <div class="agent-illus"><svg viewBox="0 0 24 24" fill="none" stroke="#7cc4ff" stroke-width="1.5"><path d="M4 5h16v10H8l-4 4z"/></svg></div>
        <div class="agent-body"><div class="agent-name">一键智能对话</div><div class="agent-desc">与 24+ 顶尖模型自由对话，一个入口任意切换。</div></div>
      </div>
      <div class="agent-card reveal">
        <div class="agent-illus"><svg viewBox="0 0 24 24" fill="none" stroke="#8f7bff" stroke-width="1.5"><rect x="3" y="4" width="18" height="16" rx="2"/><circle cx="9" cy="10" r="2"/><path d="M4 18l5-4 4 3 3-2 4 3"/></svg></div>
        <div class="agent-body"><div class="agent-name">一键生成图片</div><div class="agent-desc">描述一句，出图即刻，聚合主流绘图模型。</div></div>
      </div>
      <div class="agent-card reveal">
        <div class="agent-illus"><svg viewBox="0 0 24 24" fill="none" stroke="#3ecfff" stroke-width="1.5"><rect x="3" y="5" width="18" height="14" rx="2"/><path d="M10 9l5 3-5 3z" fill="#3ecfff"/></svg></div>
        <div class="agent-body"><div class="agent-name">一键生成视频</div><div class="agent-desc">文字 / 图片一键成片，创意快速落地。</div></div>
      </div>
      <div class="agent-card reveal">
        <div class="agent-illus"><svg viewBox="0 0 24 24" fill="none" stroke="#2de2a0" stroke-width="1.5"><path d="M8 9l-3 3 3 3"/><path d="M16 9l3 3-3 3"/><path d="M13 6l-2 12"/></svg></div>
        <div class="agent-body"><div class="agent-name">一键 Vibe Coding</div><div class="agent-desc">说人话就能写代码、造应用，自然语言编程。</div></div>
      </div>
    </div>
  </section>
```

- [ ] **Step 7: reveal 入场观察（IIFE 末尾、`})();` 之前插入）**

```js
  if ('IntersectionObserver' in window) {
    const io = new IntersectionObserver(es => es.forEach(e => { if (e.isIntersecting) { e.target.classList.add('in'); io.unobserve(e.target); } }), { threshold: 0.15 });
    document.querySelectorAll('.reveal').forEach(el => io.observe(el));
  } else document.querySelectorAll('.reveal').forEach(el => el.classList.add('in'));
```

- [ ] **Step 8: VERIFY**

预期观察：① 首屏与之前一致（灯泡/轨道/卡）；② **可向下滚动**：Hero 整屏上移滚出，露出板块①「AI Agent…创作搭档」标题 + 4 张大卡（一键对话/图片/视频/Vibe Coding，各带占位插画），滚入时淡入上浮；③ 深空背景 `#bg` 全程延续、无割裂；④ Hero 滚出后再滚回，灯泡动画照常（离屏暂停恢复）；⑤ 无横向滚动条；⑥ 控制台零 error。

- [ ] **Step 9: Commit**

```bash
git add bulb-orbit/index.html
git commit -m "feat(landing): unlock scroll + hero stage + offscreen pause + section① creation partner"
```

---

## Task 7: 板块② 我们的优势（4 张卖点卡）

**Files:** Modify `bulb-orbit/index.html`（`<style>` 加优势卡样式；板块① 后加板块② DOM）

- [ ] **Step 1: 优势卡 CSS（`<style>` 末尾插入）**

```css
  /* ② 我们的优势 */
  .adv-grid { display:grid; grid-template-columns:repeat(4,1fr); gap:20px; max-width:1200px; margin:0 auto; }
  .adv-card { padding:30px 24px; border-radius:16px; text-align:left;
    background:rgba(12,22,44,.5); border:1px solid rgba(120,180,255,.16);
    transition:transform .3s ease, border-color .3s ease; }
  .adv-card:hover { transform:translateY(-5px); border-color:rgba(120,180,255,.4); }
  .adv-ico { width:46px; height:46px; display:grid; place-items:center; border-radius:12px;
    background:rgba(60,150,255,.14); margin-bottom:16px; }
  .adv-ico svg { width:26px; height:26px; }
  .adv-name { color:#eaf2ff; font-size:17px; font-weight:700; }
  .adv-desc { margin-top:8px; color:rgba(170,190,220,.82); font-size:13px; line-height:1.6; }
  @media (max-width:1180px){ .adv-grid{ grid-template-columns:repeat(2,1fr);} }
```

- [ ] **Step 2: 板块② DOM（`</section>`(板块①) 之后插入）**

```html
  <section class="section" id="sec-adv">
    <div class="section-head reveal">
      <div class="section-title">我们的<span class="grad">优势</span></div>
      <div class="section-sub">聚合顶尖大模型，把创作的每一步都交给最合适的 AI。</div>
    </div>
    <div class="adv-grid">
      <div class="adv-card reveal"><div class="adv-ico"><svg viewBox="0 0 24 24" fill="none" stroke="#3ecfff" stroke-width="1.6"><path d="M21 12a8 8 0 1 1-3-6.2L21 5"/></svg></div><div class="adv-name">智能对话</div><div class="adv-desc">顶尖大模型任你切换，多轮上下文稳定可靠。</div></div>
      <div class="adv-card reveal"><div class="adv-ico"><svg viewBox="0 0 24 24" fill="none" stroke="#8f7bff" stroke-width="1.6"><rect x="3" y="4" width="18" height="16" rx="2"/><circle cx="9" cy="10" r="2"/><path d="M4 18l5-4 4 3 3-2 4 3"/></svg></div><div class="adv-name">图像创作</div><div class="adv-desc">聚合主流绘图模型，风格随心、出图高清。</div></div>
      <div class="adv-card reveal"><div class="adv-ico"><svg viewBox="0 0 24 24" fill="none" stroke="#39c5ff" stroke-width="1.6"><rect x="3" y="5" width="18" height="14" rx="2"/><path d="M10 9l5 3-5 3z" fill="#39c5ff"/></svg></div><div class="adv-name">视频生成</div><div class="adv-desc">文生 / 图生视频，创意快速成片。</div></div>
      <div class="adv-card reveal"><div class="adv-ico"><svg viewBox="0 0 24 24" fill="none" stroke="#2de2a0" stroke-width="1.6"><path d="M8 9l-3 3 3 3"/><path d="M16 9l3 3-3 3"/></svg></div><div class="adv-name">智能编程</div><div class="adv-desc">代码生成 / 补全 / 重构，开发全程提速。</div></div>
    </div>
  </section>
```

- [ ] **Step 3: VERIFY**

预期观察：① 滚过创作搭档后见「我们的优势」标题 + 4 张卡（智能对话/图像创作/视频生成/智能编程），图标+标题+卖点；② 卡片 hover 上浮描边变亮；③ 滚入淡入；④ 全中文；⑤ 控制台零 error。

- [ ] **Step 4: Commit**

```bash
git add bulb-orbit/index.html
git commit -m "feat(landing): section② advantages (4 selling cards)"
```

---

## Task 8: 板块③ 套餐价格（6 档 tokenplan）

**Files:** Modify `bulb-orbit/index.html`（`<style>` 加套餐样式；板块② 后加板块③ DOM）

- [ ] **Step 1: 套餐卡 CSS（`<style>` 末尾插入）**

```css
  /* ③ 套餐价格 */
  .plan-grid { display:grid; grid-template-columns:repeat(6,1fr); gap:16px; max-width:1320px; margin:0 auto; }
  .plan-card { padding:26px 18px; border-radius:16px; text-align:center;
    background:rgba(12,22,44,.5); border:1px solid rgba(120,180,255,.16); transition:transform .3s ease, border-color .3s ease; }
  .plan-card:hover { transform:translateY(-6px); border-color:rgba(120,180,255,.45); }
  .plan-card.hot { border-color:rgba(143,123,255,.7); box-shadow:0 12px 44px rgba(120,80,255,.28); }
  .plan-name { color:#9fd8ff; font-size:14px; letter-spacing:.14em; }
  .plan-price { margin-top:12px; color:#f2f7ff; font-size:30px; font-weight:800; }
  .plan-price small { font-size:15px; font-weight:600; color:rgba(200,215,240,.7); }
  .plan-quota { margin-top:10px; color:rgba(170,190,220,.82); font-size:13px; }
  .plan-tag { margin-top:12px; color:rgba(150,175,210,.72); font-size:12px; }
  .plan-note { max-width:920px; margin:34px auto 0; text-align:center; color:rgba(150,175,210,.7); font-size:12.5px; line-height:1.6; }
  @media (max-width:1180px){ .plan-grid{ grid-template-columns:repeat(3,1fr);} }
  @media (max-width:640px){ .plan-grid{ grid-template-columns:repeat(2,1fr);} }
```

- [ ] **Step 2: 板块③ DOM（`</section>`(板块②) 之后插入）**

> 真实价（proposal §2.4）；额度写「$X 等值」（决策18）；**无购买按钮**（决策19）。

```html
  <section class="section" id="sec-plan">
    <div class="section-head reveal">
      <div class="section-title">套餐<span class="grad">价格</span></div>
      <div class="section-sub">固定价买 30 天额度包，独立于钱包，用满 / 到期即停，随时重购。</div>
    </div>
    <div class="plan-grid">
      <div class="plan-card reveal"><div class="plan-name">TRIAL</div><div class="plan-price"><small>¥</small>6.9</div><div class="plan-quota">月度额度 $80 等值</div><div class="plan-tag">尝鲜体验</div></div>
      <div class="plan-card reveal"><div class="plan-name">MINI</div><div class="plan-price"><small>¥</small>119</div><div class="plan-quota">月度额度 $220 等值</div><div class="plan-tag">轻度个人</div></div>
      <div class="plan-card reveal"><div class="plan-name">SOLO</div><div class="plan-price"><small>¥</small>279</div><div class="plan-quota">月度额度 $560 等值</div><div class="plan-tag">独立开发者</div></div>
      <div class="plan-card reveal hot"><div class="plan-name">LITE</div><div class="plan-price"><small>¥</small>899</div><div class="plan-quota">月度额度 $2,200 等值</div><div class="plan-tag">小团队 · 推荐</div></div>
      <div class="plan-card reveal"><div class="plan-name">PRO</div><div class="plan-price"><small>¥</small>2,699</div><div class="plan-quota">月度额度 $7,200 等值</div><div class="plan-tag">专业重度</div></div>
      <div class="plan-card reveal"><div class="plan-name">MAX</div><div class="plan-price"><small>¥</small>8,999</div><div class="plan-quota">月度额度 $25,000 等值</div><div class="plan-tag">企业级</div></div>
    </div>
    <div class="plan-note reveal">额度独立计量，不占用钱包余额；套餐与按量充值并行互不干扰。</div>
  </section>
```

- [ ] **Step 3: VERIFY**

预期观察：① 页面最下方「套餐价格」板块，6 张卡横排（Trial¥6.9 … Max¥8999），各显「月度额度 $X 等值」+ 定位；LITE 高亮"推荐"；② **无购买按钮**；③ 机制说明一句在标题下、注脚一句在下方；④ 全中文（除套餐英文名/$）；⑤ 控制台零 error。

- [ ] **Step 4: Commit**

```bash
git add bulb-orbit/index.html
git commit -m "feat(landing): section③ tokenplan pricing (6 tiers, display-only)"
```

---

## Task 9: 集成收尾（全页 E2E 视觉门 + 回退 + reduced-motion + 清理 CSS）

**Files:** Modify `bulb-orbit/index.html`（清 `#hint` 孤儿 CSS 行 209–219；`prefers-reduced-motion` 补充）

- [ ] **Step 1: 删 `#hint` 孤儿 CSS（行 209–219）**

Edit：删除 `#hint {…}`、`@keyframes hintPulse`、`body.lit #hint {…}` 三块（`#hint` DOM 已在 Task 1 删）。

- [ ] **Step 2: reduced-motion 补充（`@media (prefers-reduced-motion: reduce)` 块 221–225 内加）**

Edit：
old:
```css
  @media (prefers-reduced-motion: reduce) {
    body.lit .rise { animation:none; opacity:1; transform:none; }
    .p, .ray, #halo, .disc, #hint, body.lit #halo { animation:none; }
    #halo { opacity:.85; }
  }
```
new:
```css
  @media (prefers-reduced-motion: reduce) {
    body.lit .rise { animation:none; opacity:1; transform:none; }
    .p, .ray, #halo, .disc, body.lit #halo { animation:none; }
    #halo { opacity:.85; }
    .reveal { opacity:1; transform:none; }
  }
```

- [ ] **Step 3: 全页 E2E 视觉门（VERIFY 加强版，逐条核对 spec §10 DoD 1–9）**

用 playwright-cli：
1. 1920×1080 打开，等 ≥3s，**首屏截图** → 核对 DoD 1/2/4（2:2:1、`WeDream AI`、即时呈现、悬停切卡）。
2. 间隔 2s 连截 2 张 → 核对 DoD 3（轨道进动可见）。
3. 悬停 + 点击一颗卫星截图 → 核对 DoD 4（切卡 + 灯泡变色）。
4. **滚到底**逐屏截图 → 核对 DoD 5（3 板块顺序、深空延续、淡入）。
5. `?nowebgl` 打开截图 → 核对 DoD 8（PNG 灯泡回退 + 卡片/板块照常 + 无报错）。
6. 读 console 全程零 error（DoD 9）。
7. **关浏览器 + ps 复核零残留（W6）**。

- [ ] **Step 4: Commit**

```bash
git add bulb-orbit/index.html
git commit -m "chore(landing): cleanup orphan hint CSS + reduced-motion + full e2e visual gate"
```

---

## Self-Review 覆盖对照（spec §2 决策 → 任务）

| 决策 | 任务 | | 决策 | 任务 |
| --- | --- | --- | --- | --- |
| 1 目标 index.html | 全部 | | 11 点击变色 | T5 |
| 2 滚动首页 | T6 | | 12 轨道进动 | T3 |
| 3 板块顺序 | T6/7/8 | | 13 品牌订正 | T1 |
| 4 不做模型矩阵 | —(不做) | | 14 四能力 | T6/7 |
| 5 灯泡滚走 | T6 | | 15 视频不标注 | T6/7 |
| 6 iframe 无外壳 | —(不加) | | 16 移动端不做 | —(范围外) |
| 7 动态 HUD 卡 | T4 | | 17 全中文 | 全部 |
| 8 直接呈现 | T1 | | 18 额度 $X 等值 | T8 |
| 9 左栏极简 | T2 | | 19 无购买按钮 | T8 |
| 10 x≈60% | T2 | | 20 内联插画可替换 | T6 |

> **视频真实性**：DoD 未含"视频生成必须可用"——用户决策保留不标注，宣传口径归用户。
> **移动端**：`<1024px` 三栏会挤压，属已确认范围外（spec §11）；各板块已给 `@media` 折列基础降级，但首屏 2:2:1 不保证窄屏——不作为验收项。
