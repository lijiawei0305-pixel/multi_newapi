# 落地页 Hero 三维轨道 & 全息模型改造 — 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把主站 React 落地页 Hero 的轨道与卫星模型换成 yun 式统一 three.js 3D 场景(真三维轨道 + 彗尾光迹 + 全息卫星),并加上指定的运动/悬停缓停/字标染色/黑底衔接/两栏重配比。

**Architecture:** 用一个 `scene3d.ts` 统一场景(灯泡+两条 3D 管环轨道+卫星),挂进 `.hero-right` 盒子;替代现有 `orbit.ts`(SVG)并吸收 `bulb3d.ts`。billboard 纹理与光晕点云运行时从现有 logo 生成,零新资源。HUD/字标/黑底由 `index.tsx` + `landing-css.ts` 承载。

**Tech Stack:** React 18 + Vite + `bun`;`three`(npm,已用)+ `three/examples/jsm`(EffectComposer/UnrealBloomPass/OutputPass);i18next。测试 `bun test`;视觉验证 dev server + playwright-cli 截图。

**权威文档:** 规格 `docs/superpowers/specs/2026-07-15-landing-3d-orbit-redesign.md`;移植底本 `bulb-orbit/yun-reference/main.js`(读过,勿入 web 构建)。

## Global Constraints

- **W5 前端文字一律中文:** HUD/字标等面向用户文案走现有 i18n(key=英文原句,`i18n.t()` 取译文);不引入 yun 的英文硬编码。
- **本任务流程(用户明确,覆盖 W4):** 先在 **Mac** `bun run dev` + playwright 截图迭代到用户满意,**再**上服务器部署;资源在浏览器运行时生成、不依赖服务器。
- **W6 进程用完即关:** 每次验证后关 playwright 浏览器、`kill` dev server,`ps` 复核零残留。
- **相机固定**,不启用 OrbitControls 拖拽。
- **名单(精确,勿改):** 内圈青(美 4)= `openai, anthropic, gemini, xai`;外圈蓝(中 6)= `deepseek, qwen, minimax, doubao, kimi, glm_chatglm`。
- **品牌色(verbatim,来自现 `MODELS`):** `openai #10d075` · `anthropic #d97757` · `gemini #9020f0` · `xai #00a0ff` · `deepseek #4fa3ff` · `qwen #6b6dff` · `minimax #b987ff` · `doubao #39c5ff` · `kimi #6c7cff` · `glm_chatglm #6f7bff`。
- **方向:** 内圈前方(近相机)左→右、外圈前方右→左;彗尾光流跟随图标。
- **进动摆:** 内 ±4°、外反相 ±6°、周期 ≈18s(用户区间内:内 3–5°/外 4–7°/16–24s)。
- **CSS 变量作用域坑(见 `orbit.ts:20-23`):** 动态 `--*` 必须 set 到**声明了兜底值的同一元素**(`.wd-landing-root`),写更外层会被遮蔽。
- **StrictMode 双挂载:** 沿用 `index.tsx` 现有"模块级单例 + 引用计数 + 延迟卸载"。

---

## 文件结构

| 文件 | 职责 | 动作 |
| --- | --- | --- |
| `web/default/src/features/landing-react/scene3d-config.ts` | `MODELS`(含品牌色)、`LOGOS`(内联 SVG/PNG 引用)、`ORBITS`(两环配置) | 新增(从 `orbit.ts` 迁移 MODELS/LOGOS) |
| `.../scene3d-assets.ts` | 纯/准纯:`sampleAlphaToPoints`、`svgToTexture`、`loadImageTexture`、`makeGlowTexture`、`approach` | 新增 |
| `.../scene3d.ts` | `initScene3d(canvas)`:建灯泡+轨道+卫星、animate、raycast/悬停/缓停/锁定、驱动 `#hud`/`--wd-brand`/`.card-open`、cleanup | 新增(替代 `orbit.ts`,吸收 `bulb3d.ts`) |
| `.../index.tsx` | 挂单一 `<canvas id="scene3d">`;去掉 `#orbits`/`.chip`/旧 `#bulb3d`;`.hl2` 白;字标绑 `--wd-brand` | 修改 |
| `.../landing-css.ts` | 35/65、左栏字号、A1 让位、`--wd-brand`、黑底竖直渐变、辉光收束、Hero 星尘/极光调淡 | 修改 |
| `.../sections-css.ts` | 三屏底色对齐新渐变(若需) | 修改 |
| `.../orbit.ts`、`.../bulb3d.ts` | 逻辑迁入 `scene3d.ts` | 删除(Task 13) |

**`scene3d.ts` 对外接口:** `export async function initScene3d(canvas: HTMLCanvasElement): Promise<() => void>`(返回 cleanup;内部自持 `#hud` 驱动,与现 `initBulb3d`/`initOrbit` 合并等价)。

---

## 验证配方 V(视觉任务复用)

> 每个"playwright 截图"步骤按此执行,并遵守 W6。

```bash
# 1. 起 dev server(后台)
cd /Users/cc/newapi628/web/default
VITE_REACT_APP_SERVER_URL=http://127.0.0.1:59999 bun run dev   # run_in_background
# 2. 等 http://localhost:3000/landing-react 就绪(轮询 curl -sI 直到 200)
# 3. playwright-cli 打开该 URL,等 2.5s(场景/贴图加载),截图存 scratchpad;需要交互时用 hover 指定坐标再截
# 4. 查看截图,对照该 Task 的"Expected 视觉";必要时贴给用户
# 5. W6:playwright-cli close / kill dev server;ps 复核零残留
```

截图存 `…/scratchpad/shots/<taskN>-<desc>.png`。灯泡/粒子截图**用 playwright,不用 virtual-time-budget**(见记忆 bulb-orbit)。

---

## Task 1: 资源纯函数 + 单测(`scene3d-assets.ts`)

**Files:**
- Create: `web/default/src/features/landing-react/scene3d-assets.ts`
- Test: `web/default/src/features/landing-react/scene3d-assets.test.ts`

**Interfaces:**
- Produces:
  - `sampleAlphaToPoints(img: {data: Uint8ClampedArray; width: number; height: number}, count: number, zJitter: number, rng?: () => number): Float32Array` — 从 alpha>阈值 的像素采 `count` 个点,归一化到 `[-0.5,0.5]` 的 x/y(y 上正),z=`(rng()*2-1)*zJitter`;返回 `Float32Array(count*3)`。纯函数,DOM 无关。
  - `approach(current: number, target: number, ease: number, dt: number): number` → `current + (target-current)*(1-Math.exp(-ease*dt))`。纯函数。
  - `svgToTexture(svg: string, size: number): Promise<import('three').CanvasTexture>`(浏览器态)。
  - `loadImageTexture(url: string, size: number): Promise<import('three').CanvasTexture>`(浏览器态)。
  - `makeGlowTexture(colorHex: string): import('three').CanvasTexture`(浏览器态,径向渐变 sprite,移植 `yun-reference/main.js:288-304`)。

- [ ] **Step 1: 写失败测试**

```ts
// scene3d-assets.test.ts
import { test, expect } from 'bun:test'
import { sampleAlphaToPoints, approach } from './scene3d-assets'

function solidImg(w: number, h: number) {
  const data = new Uint8ClampedArray(w * h * 4)
  for (let i = 0; i < w * h; i++) data[i * 4 + 3] = 255 // 全不透明
  return { data, width: w, height: h }
}

test('sampleAlphaToPoints: 数量与范围', () => {
  const pts = sampleAlphaToPoints(solidImg(16, 16), 500, 0.05, () => 0.5)
  expect(pts.length).toBe(500 * 3)
  for (let i = 0; i < pts.length; i += 3) {
    expect(pts[i]).toBeGreaterThanOrEqual(-0.5); expect(pts[i]).toBeLessThanOrEqual(0.5)
    expect(pts[i + 1]).toBeGreaterThanOrEqual(-0.5); expect(pts[i + 1]).toBeLessThanOrEqual(0.5)
    expect(pts[i + 2]).toBeCloseTo(0, 5) // rng()=0.5 → z=0
  }
})

test('sampleAlphaToPoints: 透明图不产生点(返回空)', () => {
  const empty = { data: new Uint8ClampedArray(16 * 16 * 4), width: 16, height: 16 }
  expect(sampleAlphaToPoints(empty, 500, 0.05).length).toBe(0)
})

test('approach: 朝目标单调逼近', () => {
  const a = approach(0, 1, 3, 0.016)
  expect(a).toBeGreaterThan(0); expect(a).toBeLessThan(1)
  expect(approach(1, 1, 3, 0.016)).toBeCloseTo(1, 6)
})
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd web/default && bun test src/features/landing-react/scene3d-assets.test.ts`
Expected: FAIL(模块/导出不存在)。

- [ ] **Step 3: 写实现**

```ts
// scene3d-assets.ts
import { CanvasTexture, SRGBColorSpace } from 'three'

export function approach(current: number, target: number, ease: number, dt: number): number {
  return current + (target - current) * (1 - Math.exp(-ease * dt))
}

/** 从 alpha 通道采样归一化点云。透明像素跳过;不足则按可用不透明像素循环取。 */
export function sampleAlphaToPoints(
  img: { data: Uint8ClampedArray; width: number; height: number },
  count: number, zJitter: number, rng: () => number = Math.random,
): Float32Array {
  const { data, width: w, height: h } = img
  const opaque: number[] = []
  for (let i = 0; i < w * h; i++) if (data[i * 4 + 3] > 40) opaque.push(i)
  if (opaque.length === 0) return new Float32Array(0)
  const out = new Float32Array(count * 3)
  for (let i = 0; i < count; i++) {
    const px = opaque[(i * 9973) % opaque.length] // 质数步长打散,避免逐行聚集
    const cx = px % w, cy = Math.floor(px / w)
    out[i * 3] = cx / w - 0.5
    out[i * 3 + 1] = 0.5 - cy / h      // 图像 y 向下 → 世界 y 向上
    out[i * 3 + 2] = (rng() * 2 - 1) * zJitter
  }
  return out
}

/** 内联 SVG 字符串 → 256² CanvasTexture(billboard 用) */
export function svgToTexture(svg: string, size: number): Promise<CanvasTexture> {
  return new Promise((resolve, reject) => {
    const blob = new Blob([svg], { type: 'image/svg+xml' })
    const url = URL.createObjectURL(blob)
    const image = new Image()
    image.onload = () => {
      const c = document.createElement('canvas'); c.width = c.height = size
      const ctx = c.getContext('2d')!; ctx.drawImage(image, 0, 0, size, size)
      URL.revokeObjectURL(url)
      const tex = new CanvasTexture(c); tex.colorSpace = SRGBColorSpace; resolve(tex)
    }
    image.onerror = (e) => { URL.revokeObjectURL(url); reject(e) }
    image.src = url
  })
}

/** PNG url → 256² CanvasTexture(与 SVG 走同一 canvas 通道,便于统一采 alpha) */
export function loadImageTexture(url: string, size: number): Promise<CanvasTexture> {
  return new Promise((resolve, reject) => {
    const image = new Image(); image.crossOrigin = 'anonymous'
    image.onload = () => {
      const c = document.createElement('canvas'); c.width = c.height = size
      const ctx = c.getContext('2d')!; ctx.drawImage(image, 0, 0, size, size)
      const tex = new CanvasTexture(c); tex.colorSpace = SRGBColorSpace; resolve(tex)
    }
    image.onerror = reject; image.src = url
  })
}

/** 径向渐变辉光 sprite 贴图(移植 yun-reference/main.js:288-304) */
export function makeGlowTexture(colorHex: string): CanvasTexture {
  const c = document.createElement('canvas'); c.width = c.height = 64
  const ctx = c.getContext('2d')!
  const g = ctx.createRadialGradient(32, 32, 0, 32, 32, 32)
  g.addColorStop(0, colorHex); g.addColorStop(0.2, colorHex); g.addColorStop(1, 'rgba(0,0,0,0)')
  ctx.fillStyle = g; ctx.fillRect(0, 0, 64, 64)
  return new CanvasTexture(c)
}
```

> 注:`svgToTexture`/`loadImageTexture`/`makeGlowTexture` 依赖 DOM/three,不在 `bun test` 覆盖内,靠后续 playwright 视觉验证。纯函数 `sampleAlphaToPoints`/`approach` 已被测。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd web/default && bun test src/features/landing-react/scene3d-assets.test.ts`
Expected: PASS(3 个用例)。

- [ ] **Step 5: 提交**

```bash
git add web/default/src/features/landing-react/scene3d-assets.ts web/default/src/features/landing-react/scene3d-assets.test.ts
git commit -m "feat(landing): scene3d 资源纯函数(alpha 采样/easing/纹理)+ 单测"
```

---

## Task 2: 场景配置(`scene3d-config.ts`)

**Files:**
- Create: `web/default/src/features/landing-react/scene3d-config.ts`
- Test: `web/default/src/features/landing-react/scene3d-config.test.ts`

**Interfaces:**
- Consumes: 无。
- Produces:
  - `MODELS: Record<string, {name;provider;desc;scene;telemetry;color:string}>` — 从 `orbit.ts:44-55` **原样迁移**(值不变)。
  - `LOGOS: Record<string, string>` — 从 `orbit.ts:28-39` **原样迁移**(5 内联 SVG + 5 `<img>` PNG 引用)。
  - `type OrbitCfg = { ring:'inner'|'outer'; color:string; radius:number; tilt:[number,number,number]; wobbleDeg:number; wobblePeriod:number; wobblePhase:number; speed:number; wakeStrength:number; wakeFalloff:number; keys:string[] }`
  - `ORBITS: OrbitCfg[]`(2 条)。

- [ ] **Step 1: 写失败测试**

```ts
// scene3d-config.test.ts
import { test, expect } from 'bun:test'
import { MODELS, LOGOS, ORBITS } from './scene3d-config'

test('两环名单精确 & 计数 4/6', () => {
  const inner = ORBITS.find(o => o.ring === 'inner')!, outer = ORBITS.find(o => o.ring === 'outer')!
  expect(inner.keys).toEqual(['openai', 'anthropic', 'gemini', 'xai'])
  expect(outer.keys).toEqual(['deepseek', 'qwen', 'minimax', 'doubao', 'kimi', 'glm_chatglm'])
})

test('每个 key 都有 MODELS(含品牌色)与 LOGOS', () => {
  for (const o of ORBITS) for (const k of o.keys) {
    expect(MODELS[k]?.color).toMatch(/^#[0-9a-f]{6}$/i)
    expect(LOGOS[k]).toBeTruthy()
  }
})

test('外圈反相 & 半径更大 & 方向相反', () => {
  const inner = ORBITS.find(o => o.ring === 'inner')!, outer = ORBITS.find(o => o.ring === 'outer')!
  expect(outer.radius).toBeGreaterThan(inner.radius)
  expect(Math.sign(outer.speed)).toBe(-Math.sign(inner.speed))
})
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd web/default && bun test src/features/landing-react/scene3d-config.test.ts`
Expected: FAIL。

- [ ] **Step 3: 写实现**

把 `orbit.ts:28-39`(LOGOS)与 `:44-55`(MODELS)整段复制进来(**值一字不改**),再加:

```ts
// scene3d-config.ts —— MODELS、LOGOS 从 orbit.ts 原样迁移(略,复制粘贴)
export type OrbitCfg = {
  ring: 'inner' | 'outer'; color: string; radius: number; tilt: [number, number, number]
  wobbleDeg: number; wobblePeriod: number; wobblePhase: number; speed: number
  wakeStrength: number; wakeFalloff: number; keys: string[]
}

// speed 的正负号先按下面填,Task 6 对着截图校正到"内前方左→右 / 外前方右→左"。
// radius/tilt 为初值,Task 3/5 对着截图微调使其落在 65% 盒内、6 图标不挤。
export const ORBITS: OrbitCfg[] = [
  {
    ring: 'inner', color: '#8fd8ff', radius: 1.0, tilt: [0.5, 0, 0.12],
    wobbleDeg: 4, wobblePeriod: 18, wobblePhase: 0, speed: 0.10,
    wakeStrength: 0.9, wakeFalloff: 5.0, keys: ['openai', 'anthropic', 'gemini', 'xai'],
  },
  {
    ring: 'outer', color: '#5f7cff', radius: 1.32, tilt: [0.72, 0, -0.38],
    wobbleDeg: 6, wobblePeriod: 18, wobblePhase: Math.PI, speed: -0.075,
    wakeStrength: 0.6, wakeFalloff: 6.0, keys: ['deepseek', 'qwen', 'minimax', 'doubao', 'kimi', 'glm_chatglm'],
  },
]
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd web/default && bun test src/features/landing-react/scene3d-config.test.ts`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add web/default/src/features/landing-react/scene3d-config.ts web/default/src/features/landing-react/scene3d-config.test.ts
git commit -m "feat(landing): scene3d 配置(MODELS/LOGOS 迁移 + 两环 ORBITS)"
```

---

## Task 3: 统一场景骨架 + 灯泡(首个可见里程碑)

**Files:**
- Create: `.../scene3d.ts`(本任务只到"灯泡渲染在盒子里")
- Modify: `.../index.tsx`(用 `initScene3d` 挂单一 canvas,替换 `initBulb3d`+`initOrbit`)

**Interfaces:**
- Consumes: Task 1 的 `makeGlowTexture`;`/lp-assets/dengpao_points.bin`(已有)。
- Produces: `initScene3d(canvas): Promise<()=>void>`;`window.__scene3d = { setCoreColor(hex|null) }`(供后续任务/调试;先占位)。

- [ ] **Step 1:** 写 `scene3d.ts` 骨架。移植 `yun-reference/main.js`:
  - 灯泡粒子着色器 `vertexShader`/`fragmentShader`(:13-92)整段;`particleMaterial`(:372-387);点云读取(:344-390)但 URL 改 `/lp-assets/dengpao_points.bin`,且**对齐现 `bulb3d.ts` 的数据布局**(现版每点 6 float=pos+normal;若现版无 normal 则相应调整——以 `bulb3d.ts:107-173` 现有解码为准)。
  - `setupGlassCore`(:428-492)、`setupPostProcessing`(:408-425,bloom 参数 `0.6/0.45/0.85` 同现版)。
  - **改为盒子尺寸**:`renderer/composer/camera` 用传入 `canvas` 的 `clientWidth/clientHeight`(不是 `window.inner*`);加 `ResizeObserver(canvas.parentElement)` 重设 size/aspect。
  - **不建 OrbitControls**。相机 `position.set(0,0,4.6)`、`fov 45`(Task 5/13 按盒子微调)。
  - `animate()`:先只推进灯泡 `uTime=elapsedTime`、`particleSystem.rotation.y=elapsedTime*0.02`、`bulbGroup.rotation.y=...`、`composer.render()`。保留鼠标斥力 uniform 更新(:971-987)。
  - `initScene3d` 返回 cleanup:`cancelAnimationFrame` + `renderer.dispose()` + 断开 observer/监听 + 移除动态节点。
  - `window.__scene3d = { setCoreColor: () => {} }`(占位)。

- [ ] **Step 2:** 改 `index.tsx`:
  - `boot()` 里把 `initOrbit()` + `initBulb3d(canvas)` 两段替换为 `initScene3d(canvas)`(单 cleanup)。保留单例/refcount/延迟 unboot。
  - Hero JSX:`.hero-visual` 内保留 `<canvas id='scene3d' />`;暂时保留 `#hud`(后续任务用);删除 `<svg id='orbits'>`、`#halo`、`<img id='bulb'>`(PNG 兜底 Task 13 再定)、旧 `#bulb3d`。
  - `import` 换成 `initScene3d`,移除 `initBulb3d`/`initOrbit` 引用(此时 `orbit.ts`/`bulb3d.ts` 仍在,不删,Task 13 删)。

- [ ] **Step 3: 视觉验证(配方 V)**
  - Expected 视觉:`/landing-react` 右侧盒子里出现**发光的灯泡粒子云**(与改造前灯泡观感一致),无报错;控制台无 "bulb3d 回退 PNG"。
  - 截图存 `shots/3-bulb.png`。给用户看一眼(首个里程碑)。W6 收尾。

- [ ] **Step 4: 提交**

```bash
git add web/default/src/features/landing-react/scene3d.ts web/default/src/features/landing-react/index.tsx
git commit -m "feat(landing): 统一 3D 场景骨架 + 灯泡并入(盒子尺寸/固定相机)"
```

---

## Task 4: 两条 3D 管环轨道(暂无卫星)

**Files:** Modify `.../scene3d.ts`

**Interfaces:**
- Consumes: Task 2 `ORBITS`;Task 3 的 `scene`/`camera`。
- Produces: `orbitMaterials: {material, cfg}[]`(供 animate 喂 uniform);场景内两条倾斜管环。

- [ ] **Step 1:** 移植轨道着色器 `orbitVertexShader`/`orbitFragmentShader`(`yun-reference/main.js:97-156`)整段(含深度淡化 + 灯泡剪影遮罩 + 彗尾 + 能量流)。
- [ ] **Step 2:** 移植 `OrbitCurve`(:528-537)、`createOrbit`(:543-582,两层 core+halo)。改为**读 `ORBITS`**:`group.rotation.set(...cfg.tilt)`;`uColor=cfg.color`、`uDir=Math.sign(cfg.speed)`、`uSatCount=cfg.keys.length`、`uWakeStrength/uWakeFalloff` 取 cfg;`uMaskRadius` 沿用 `(0.48,0.7)`。把每条 group 存入 `orbitGroups[]`(Task 7 摆动用)。
- [ ] **Step 3:** animate 里加轨道 uniform 更新(移植 :960-969):`uTime`、`uBulbView`(bulb 视空间中心)。`uSatAngles` 先填 0(卫星 Task 5 加)。
- [ ] **Step 4: 视觉验证(配方 V)** — Expected:灯泡外围出现**两条倾斜发光细环**(一青一蓝、粗细/亮度不同),环绕到灯泡背面处被灯泡剪影**淡出遮住**(非硬线穿过灯泡)。截图 `shots/4-orbits.png`。W6。
- [ ] **Step 5: 提交** `git commit -m "feat(landing): 两条 3D 管环轨道 + 剪影遮罩/彗尾着色器"`

---

## Task 5: 卫星三层全息 + 我方名单(运行时纹理/光晕)

**Files:** Modify `.../scene3d.ts`

**Interfaces:**
- Consumes: Task 1(`sampleAlphaToPoints`/`svgToTexture`/`loadImageTexture`/`makeGlowTexture`)、Task 2(`MODELS`/`LOGOS`/`ORBITS`)。
- Produces: `satellites: Group[]`(每个 `userData={ key, radius, baseAngle, speed, hoverScale:1, pointsMaterial, glowMaterial, rimMaterial, logoMaterial }`)。

- [ ] **Step 1: 纹理+光晕装配(替代 yun 的 loadLogoPoints/loadLogoTexture :584-641)。** 对每个 key:
  - billboard 纹理:`LOGOS[key]` 是 `<svg…>` → `svgToTexture(svg,256)`;是 `<img src="/lp-assets/logos/X.png">` → 取出 src → `loadImageTexture(src,256)`。(`LOGOS` 里 SVG 含 `__id__` 占位,`replaceAll('__id__','u'+key)` 后再用。)
  - 光晕点云:把该纹理的 canvas `getImageData(0,0,256,256)` → **降采样到 128²** 或直接 `sampleAlphaToPoints(imageData, 1600, 0.06)`。
  - 并行 `Promise.all` 预载全部(移植 :630-641 的批量结构)。
- [ ] **Step 2: `createSatellite3D`(移植 :666-766)**,三层:①光晕 `Points`(`PointsMaterial` 品牌色 `MODELS[key].color`,additive,`raycast=()=>{}`)②glow sprite(`makeGlowTexture(色)`)+ 细 `TorusGeometry` 环 + 淡玻璃盘 ③billboard `PlaneGeometry(0.22)` + `MeshBasicMaterial{map, toneMapped:false, transparent, depthWrite:false}`。`userData` 按上面的 Produces 存。
- [ ] **Step 3: 布点**(移植 :644-659):遍历 `ORBITS`,`createOrbit` 已在 Task 4;对每条 `cfg.keys` 等分 `baseAngle = i*2π/keys.length`,`speed=cfg.speed`,加入对应 group。
- [ ] **Step 4: animate 卫星更新**(移植 :999-1049 的位置/billboard/distFactor/透明度/缩放),但**角度公式改用 `baseAngle + speed*orbitPhase`**(`orbitPhase` Task 8 引入;本任务先用 `orbitPhase=elapsedTime` 顶替,Task 8 换成累积时钟)。billboard 抵消父倾斜 `local=parentQuat⁻¹·cameraQuat`(:1010-1011)。同时把 `uSatAngles` 用同一角度公式喂给轨道着色器(:965-968),使彗尾对齐图标。
- [ ] **Step 5: 视觉验证(配方 V)** — Expected:两环上出现**会公转的模型 logo**(内 4 外 6),logo 背后有品牌色粒子光晕;转到灯泡**前面压住灯泡、后面被灯泡挡且变暗变小**。逐一核对 10 个 logo 是你项目的图。截图 `shots/5-satellites.png`。W6。
- [ ] **Step 6: 提交** `git commit -m "feat(landing): 卫星三层全息(运行时纹理/光晕)+ 内美4/外中6 布点"`

---

## Task 6: 轨道方向 + 彗尾跟随(对截图定号)

**Files:** Modify `.../scene3d-config.ts`(仅 `speed` 符号)

- [ ] **Step 1:** dev + playwright **连拍两帧**(间隔 ~1.2s,`shots/6-a.png`/`6-b.png`),看内/外环前方(近相机、压住灯泡那半段)图标位移方向。
- [ ] **Step 2:** 判定并改 `ORBITS[*].speed` 符号,使:**内环前方 左→右**、**外环前方 右→左**。(彗尾 `uDir=sign(speed)`,自动跟随,无需另改。)
- [ ] **Step 3:** 复拍确认方向正确;确认外环仍与内环反向。截图 `shots/6-final.png`。W6。
- [ ] **Step 4: 提交** `git commit -m "fix(landing): 校正两环公转方向(内前左→右/外前右→左)"`

---

## Task 7: 进动摆(小幅反相振荡)

**Files:** Modify `.../scene3d.ts`(animate 中给 `orbitGroups` 施加摆动)

- [ ] **Step 1:** animate 里,对每条轨道 group:`group.rotation.set(tiltX + wob, tiltY, tiltZ)`,其中 `wob = (cfg.wobbleDeg*π/180) * Math.sin(2π*orbitPhase/cfg.wobblePeriod + cfg.wobblePhase)`。内 `wobbleDeg=4`、外 `=6`、`wobblePhase` 差 `π`(反相),周期 18s。(用 `orbitPhase` 使停轨时摆动也停;Task 8 前先用 elapsedTime 顶替。)
- [ ] **Step 2: 视觉验证** — 连拍 3 帧(间隔 ~4s),确认两环有**轻微反相左右摆**、幅度小、不翻转。截图 `shots/7-wobble-*.png`。W6。
- [ ] **Step 3: 提交** `git commit -m "feat(landing): 两环反相小幅进动摆(±4°/±6°/18s)"`

---

## Task 8: 轨道相位时钟 + 悬停缓停/恢复 + 吸附锁定

**Files:**
- Modify: `.../scene3d.ts`
- Create: `.../scene3d-interaction.test.ts`(纯 reducer 单测)

**Interfaces:**
- Consumes: Task 1 `approach`;Task 5 `satellites`。
- Produces: 纯函数 `nextSelection(current: string|null, hitKey: string|null, pointerInside: boolean): string|null`(锁定/切换/离场规则)。animate 用 `orbitPhase`/`speedFactor`。

- [ ] **Step 1: 写失败测试(锁定 reducer)**

```ts
// scene3d-interaction.test.ts
import { test, expect } from 'bun:test'
import { nextSelection } from './scene3d-interaction'

test('命中即锁定;不同命中即切换', () => {
  expect(nextSelection(null, 'openai', true)).toBe('openai')
  expect(nextSelection('openai', 'gemini', true)).toBe('gemini')
})
test('命中空处(仍在画布内)保持锁定,不松手', () => {
  expect(nextSelection('openai', null, true)).toBe('openai')
})
test('离开画布即清空', () => {
  expect(nextSelection('openai', null, false)).toBe(null)
  expect(nextSelection('openai', 'gemini', false)).toBe(null) // 已离场,忽略命中
})
```

- [ ] **Step 2: 跑测试确认失败** — `cd web/default && bun test src/features/landing-react/scene3d-interaction.test.ts` → FAIL。

- [ ] **Step 3: 写实现**。新建 `scene3d-interaction.ts`:

```ts
export function nextSelection(current: string | null, hitKey: string | null, pointerInside: boolean): string | null {
  if (!pointerInside) return null           // 明显离开视觉区 → 关闭
  if (hitKey) return hitKey                 // 命中(含切换到另一图标)
  return current                            // 命中空处 → 吸附,不松手
}
```

  在 `scene3d.ts`:
  - 状态:`let orbitPhase=0, speedFactor=1, selected: string|null=null, pointerInside=false`;`raycaster.params.Points.threshold=0.02`(:775)。
  - 事件:`mousemove` 更新 `mouse`(NDC 用 **`canvas.getBoundingClientRect()`**,兼容 A1 transform)+ `pointerInside=true`;`mouseleave` → `pointerInside=false`。
  - animate 每帧:`dt=clamp(elapsed-last,0.001,0.05)`;`orbitPhase += dt*speedFactor`;raycast 得 `hitKey`;`selected=nextSelection(selected,hitKey,pointerInside)`;`speedFactor=approach(speedFactor, selected?0:1, 3, dt)`。**卫星角度/`uSatAngles`/能量流/进动摆全部改用 `orbitPhase`**(替换 Task 5/7 里临时的 elapsedTime)。灯泡 `uTime` 仍用 elapsedTime(呼吸不停)。
  - `selected` 变化时调 `onSelectChange(selected)`(Task 9 填内容;本任务先只做 stop/resume + 被选图标 `hoverScale` gsap/lerp 到 1.2、其余回 1)。

- [ ] **Step 4: 跑测试确认通过** — 同 Step 2 命令 → PASS(4 用例)。

- [ ] **Step 5: 视觉验证(配方 V,含交互)** — playwright hover 到某 logo 坐标:Expected **轨道平滑减速到停**(非瞬停)、移开后**平滑加速恢复**;hover 期间在画布内挪到空处不恢复(锁定);移出画布才恢复。连拍验证缓停曲线。截图 `shots/8-hover-stop-*.png`。W6。
- [ ] **Step 6: 提交** `git add … && git commit -m "feat(landing): 轨道相位时钟 + 悬停缓停/恢复 + 吸附锁定"`

---

## Task 9: 选中副作用(高亮 + 灯泡染色 + HUD + A1 让位 + 字标)

**Files:** Modify `.../scene3d.ts`、`.../index.tsx`(确保 `#hud` DOM 结构在)、`.../landing-css.ts`(`.card-open` 已有,确认触发)

**Interfaces:**
- Consumes: Task 8 `onSelectChange`;`MODELS`;`#hud` DOM;`.hero-visual`/`.hero-right`。
- Produces: `setCoreColor(hex|null)` 真身(灯泡染色);`renderHud(key|null)`。

- [ ] **Step 1: HUD 驱动**(移植 `orbit.ts:56-72` 的 `hudEls`/`renderCard`,用 `i18n.t()` 取译文,W5)。`onSelectChange(key)`:key 有值 → `renderCard(MODELS[key])` + `#hud` 加 `show` + `heroRight.classList.add('card-open')`;否则移除。
- [ ] **Step 2: 灯泡染色 `setCoreColor(hex|null)`** —— 把灯泡 `pointLight.color`/`glowSprite`/核心 lerp 到品牌色(移植 `triggerCoreSurge` 的色彩部分 :859-881,但做成**持续染色**而非脉冲:selected 时 → 品牌色,清空 → 回默认 `#00f0ff`)。`onSelectChange` 里调用。挂 `window.__scene3d.setCoreColor`。
- [ ] **Step 3: 字标变量** —— `onSelectChange` 里 `varsEl.style.setProperty('--wd-brand', key?MODELS[key].color:'')`(`varsEl=.wd-landing-root`;空串回落 CSS 默认,Task 10 声明默认)。
- [ ] **Step 4: 被选图标视觉** —— 已在 Task 8 放大;此处加"变亮"(logo/glow opacity 提升,移植 hoverBoost :1026-1041)。
- [ ] **Step 5: 视觉验证** — hover 某 logo:Expected 该图标放大变亮 + **灯泡核心染成该品牌色** + 右侧 **HUD 中文卡淡入** + 画面按 **A1 向左缩让位**;移开复位、灯泡回青。截图 `shots/9-select-*.png`。W6。
- [ ] **Step 6: 提交** `git commit -m "feat(landing): 选中副作用(高亮/灯泡染色/中文HUD/A1让位/字标变量)"`

---

## Task 10: 字标染色(hl1/页眉)+ hl2 纯白

**Files:** Modify `.../landing-css.ts`、`.../index.tsx`

- [ ] **Step 1:** `landing-css.ts` 的 `.wd-landing-root` 声明默认 `--wd-brand:<hl1 现默认色/或白>`;`.hl1` 与 `.lp-logo span` 改 `color:var(--wd-brand); transition:color .4s ease;`(若原为渐变文字,改为纯色 fill 以便变量生效,或用 `background/-webkit-text-fill` 方案二选一,实现时定)。
- [ ] **Step 2:** `.hl2` 固定 `color:#fff`(移除原渐变/彩色)。
- [ ] **Step 3: 视觉验证** — 默认态两行观感 OK;hover 某模型 → **"WeDream AI"(标题 + 左上页眉)平滑染品牌色**、**"让灵感不再受限"恒白**;移开退回。截图 `shots/10-wordmark-*.png`。W6。
- [ ] **Step 4: 提交** `git commit -m "feat(landing): 选中时 WeDream AI 字标染品牌色 + 副标题纯白"`

---

## Task 11: 黑色 Hero + 向下无缝衔接

**Files:** Modify `.../landing-css.ts`(必要时 `.../sections-css.ts`)

- [ ] **Step 1:** `.wd-landing-root` 底色改**竖直渐变**:顶部近纯黑 `#01020a` → 下行渐融到 `#050b1a`/`#081226` 一带,渐变跨过 Hero 与第一屏交界(用足够高的 `linear-gradient` + `background-attachment` 视滚动定;若 `#bg` 为 `fixed` 则改为随内容滚动的底层或分区底)。**无硬线**。
- [ ] **Step 2:** 蓝紫中心辉光(`#bg` 的 center radial)**收进灯泡背后**(缩小范围/仅 Hero 段),不铺满全页。
- [ ] **Step 3:** Hero 段把 `#aurora`/`#stardust*` 透明度调淡(保证读作黑);下方三屏维持现蓝调。
- [ ] **Step 4: 视觉验证** — 全页滚动截图:Hero **纯黑**、发光灯泡/轨道更炸;往下**平滑过渡**到藏青、**无硬边**;三屏卡片配色不变。截图 `shots/11-black-hero-*.png`(顶/交界/三屏)。W6。
- [ ] **Step 5: 提交** `git commit -m "feat(landing): 黑色 Hero + 向下渐融藏青、辉光收束灯泡后"`

---

## Task 12: 两栏重配比(左 35% / 右 65% + 收字)

**Files:** Modify `.../landing-css.ts`

- [ ] **Step 1:** `.hero-stage`/`.hero-left`/`.hero-right` 配比调到 **左 ≈35% / 右 ≈65%**(现约五五)。
- [ ] **Step 2:** 左栏 `h1`/`#sub`/`.feature*`/badge/CTA 字号与间距按比例**收小**,右视觉区盒子随之变大;`scene3d` 的 ResizeObserver 应自动适配(核对相机/环仍居中不裁切,必要时回 Task 5 调 `radius`/相机 z)。
- [ ] **Step 3: 视觉验证** — Expected:左字明显收小、灯泡+轨道更突出、整体不失衡;A1 让位仍正常。截图 `shots/12-layout.png`。W6。
- [ ] **Step 4: 提交** `git commit -m "feat(landing): Hero 两栏改 35/65、左栏收字、视觉放大"`

---

## Task 13: 兜底与清理

**Files:** Modify `.../scene3d.ts`、`.../index.tsx`;Delete `.../orbit.ts`、`.../bulb3d.ts`

- [ ] **Step 1: `prefers-reduced-motion`** — 命中则:轨道 `speedFactor` 固定 0(静止)、灯泡降/停自转、进动摆关;仍可 hover 选中出 HUD。
- [ ] **Step 2: WebGL 失败兜底** — `initScene3d` try/catch:失败则 `document.body.classList.remove('webgl3d')` 并显示静态 `#bulb` PNG(保留该 `<img>` 作兜底,默认隐藏,失败时显示),`console.warn`。
- [ ] **Step 3: 触屏兜底** — 无 hover:轻触 canvas 命中卫星即选中(raycast on `click`/`touchstart`);无命中不锁。
- [ ] **Step 4: 删除** `orbit.ts`、`bulb3d.ts`;全仓 `grep` 确认无残留 import(`rg "bulb3d|initOrbit|from './orbit'" web/default/src`)。
- [ ] **Step 5: 验证** — 桌面正常;`prefers-reduced-motion` 模拟下静止且可选中;强制 WebGL 失败(临时抛错)时回退 PNG。`bun test` 全绿。`rg` 无残留。截图 `shots/13-*.png`。W6。
- [ ] **Step 6: 提交** `git commit -m "chore(landing): reduced-motion/WebGL/触屏兜底 + 删除旧 orbit.ts/bulb3d.ts"`

---

## Task 14: Mac 验收 → 用户签字 → 服务器部署

> **本任务是用户要求的"先 Mac 满意、再部署"闸门。未获用户明确 OK,不部署。**

- [ ] **Step 1: 逐条 DoD 截图(规格 §10)** — dev + playwright 把这些各截一张贴给用户:灯泡前后遮挡 / 内前左→右·外前右→左 / 进动摆 / 悬停缓停·恢复·不抽搐 / 中文 HUD / 字标染色·hl2 白 / 黑底衔接 / 35-65。存 `shots/14-dod-*.png`。W6。
- [ ] **Step 2: 用户签字闸门** — 把整组截图交用户;**等用户明确"满意/可部署"**。有不满 → 回对应 Task 调,重截。
- [ ] **Step 3: 提交 spec/plan + 代码收尾**(若尚未):`git add docs/superpowers/… bulb-orbit/yun-reference && git commit`(**提交前先 `git status` 看有无并行 WIP,隔离本次 scope**,见记忆 deploy-scope)。
- [ ] **Step 4: 服务器部署** — 按 `deploy/ops/deploy.sh` 流程 rsync 到服务器构建(前端 build+embed 在服务器)、重启 `newapi_test`;`curl -sI` 健康 200;线上 `https://www.wedreamhub.com/`(或对应域名)复看 Hero 效果与线上一致(代理站问题以线上为准)。
- [ ] **Step 5: 收尾** — `RETRO.md` 记录本次(若有坑);更新 `doc/tasks/STATUS.md` 落地页条目。提交。

---

## Self-Review(计划 vs 规格)

- **Spec 覆盖:** §4 布局→T12;§5.1 灯泡→T3;§5.2 轨道(环/方向/彗尾/进动/遮罩)→T4/T6/T7;§5.3 卫星→T5;§5.4 相机/悬停/缓停/锁定→T3(固定相机)/T8/T9;§6 HUD→T9;§7 资源(运行时纹理/光晕)→T1/T5;§7.1 字标+黑底→T9(变量)/T10/T11;§8 文件→贯穿;§9 取舍→T5(z 抖动)/T13(兜底);§10 DoD→T14;§11 待定→T6/T12(调参)/T13(点击)。**无遗漏。**
- **占位扫描:** 无 TBD/TODO;调参项(radius/相机/号)均绑定"对截图定"的具体步骤,非占位。
- **类型一致:** `initScene3d`、`ORBITS/OrbitCfg`、`MODELS/LOGOS`、`sampleAlphaToPoints`、`approach`、`nextSelection`、`onSelectChange`、`setCoreColor` 全程同名同签名。`orbitPhase`/`speedFactor`/`selected`/`pointerInside` 状态名贯穿 T5/T7/T8/T9 一致(T5/T7 临时用 elapsedTime,T8 显式替换为 orbitPhase)。
