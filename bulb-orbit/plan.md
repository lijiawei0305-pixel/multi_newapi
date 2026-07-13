# WeDream AI 首页改版 · 实施计划（Master-Worker 自动化）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
> **依据文档：** [`proposal.md`](proposal.md)（v1.0，已确认）。本计划把该需求逐模块拆成**最小可执行任务（MET）**，供主-子 Agent 全自动执行。

**Goal（一句话）：** 在 `bulb-orbit/v2/` 用 Vite + TypeScript + three.js 复刻 yun 粒子灯泡首页，叠加 2:2:1 三栏布局、放大圆润灯泡、24 家双轨卫星、轨道进动、点击变色与中文 HUD，产出可经平台 iframe 接入的静态站点。

**Architecture（3 句话）：** 把可测逻辑（24 模型注册表、轨道/进动数学、HUD 状态、响应式、素材管线）抽成**纯 TypeScript 模块并写 Vitest 单测**；three.js 渲染装配层（灯泡/轨道/卫星/场景/交互）保持薄胶水，仅做**类型检查 + lint + Playwright 视觉门**验收（着色器/粒子渲染无法有意义地单测，强测即测试剧场）。素材（平滑灯泡点云、24 logo）由 Node 工具脚本一次性生成并入库。

**Tech Stack：** Vite 5 ｜ TypeScript 5（strict, `--noEmit`）｜ three.js 0.160 ｜ GSAP 3 ｜ Vitest 2（happy-dom 环境）｜ ESLint 9 + typescript-eslint ｜ Playwright（视觉门 + E2E）。

---

## Global Constraints（每个任务隐含遵守 · 逐条来自 proposal 与 CLAUDE.md）

- **语言：** 面向用户的界面文字**一律中文**（W5）；仅品牌名 `WeDream AI` 与供应商英文名（OpenAI/Anthropic…）保留英文。
- **品牌写法：** 恒为 `WeDream AI`（W、D 大写，中间**一个空格**）。旧版 “Wedream AI” 一律不用。
- **类型门：** 每个任务收尾前 `npm run typecheck`（= `tsc --noEmit`，`strict:true`）必须**零错误**。
- **Lint 门：** `npm run lint`（ESLint + typescript-eslint）必须**零错误**（warning 允许但需在任务说明中列出）。
- **测试门：** 纯逻辑模块**必须 TDD**（先写失败测试）；`npm run test`（Vitest）全绿。渲染胶水层无单测，走**视觉门**（见下）。
- **视觉门（渲染任务专用）：** 用 **Playwright**（**禁用** headless CLI 的 `--virtual-time-budget`——本页 WebGL rAF 常驻，该参数恒 ~1s 成像不准）；视口 `1920×1080`，导航后**真实等待 ≥3s** 墙钟再截图，逐条核对「预期观察」。
- **进程纪律（W6）：** Playwright / 无头 Chrome / 本地 `vite preview` 等验证一结束**立即关闭**，并 `ps aux | grep -iE 'playwright|headless|vite' | grep -v grep` 复核零残留。
- **提交纪律：** 每个任务末尾 commit；提交信息用 `feat/test/chore/fix(landing): …`。**不 push、不部署**——最终构建/部署在服务器（W4），由用户另行指示。
- **无外链：** 去除 Google Fonts 等外链，中文用系统字体栈（国内可用）。
- **大文件不入库：** `dengpao.glb`（83MB）加入 `.gitignore`，只入库重采样产物 bin。
- **依赖边界：** `v2/` 是独立小工程，**不碰**平台 `web/` 的依赖（避开 @tanstack 构建坑）。

---

## 总体进度看板（Master 维护）

> Master 每完成一个任务，把 `⬜` 改 `✅`（进行中 `🔄`、阻塞 `⛔`）。「类型」列决定验收方式：`单测`=Vitest 全绿；`视觉`=Playwright 视觉门 + 类型/lint。

| # | 模块 | 任务 | 类型 | 依赖 | 状态 |
| --- | --- | --- | --- | --- | --- |
| 0 | 脚手架 | Vite+TS+Vitest+ESLint 工程初始化，工具链三门全绿 | 单测 | — | ⬜ |
| 1 | 数据 | `data/models.ts` 24 家模型注册表（类型+查询） | 单测 | 0 | ⬜ |
| 2 | 场景数学 | `scene/config.ts`+`scene/orbit-math.ts` 尺寸/轨道/进动/相机偏移纯函数 | 单测 | 0 | ⬜ |
| 3 | 响应式 | `ui/layout.ts` 断点/粒子数/bloom 开关纯函数 | 单测 | 0 | ⬜ |
| 4 | HUD | `ui/hud.ts` 右栏卡片状态与 DOM 渲染（happy-dom） | 单测 | 1 | ⬜ |
| 5 | 交互逻辑 | `interactions/logic.ts` 悬停解析/核心变色纯函数 | 单测 | 1 | ⬜ |
| 6 | 灯泡素材 | `tools/resample-bulb.mjs` GLB 细分平滑重采样→bin | 单测 | 0 | ⬜ |
| 7 | Logo 素材 | `tools/process-logos.cjs` 逆 ACES 补偿→24 PNG | 单测 | 0 | ⬜ |
| 8 | 布局壳 | `index.html`+`style.css` 2:2:1 三栏+品牌文案+CTA+指标条+HUD 卡 | 视觉 | 3 | ⬜ |
| 9 | 灯泡渲染 | `scene/bulb.ts` 放大圆润粒子灯泡+底座 | 视觉 | 2,6,8 | ⬜ |
| 10 | 轨道卫星 | `scene/orbits.ts`+`scene/satellites.ts` 双轨+进动+24 卫星 | 视觉 | 1,2,7,9 | ⬜ |
| 11 | 场景装配 | `scene/scene.ts`+`main.ts` 相机偏移+动画循环+交互接线 | 视觉 | 4,5,10 | ⬜ |
| 12 | 降级适配 | 移动端竖排 + WebGL 兜底海报 | 视觉 | 3,11 | ⬜ |
| 13 | 集成部署 | `npm run build` + Playwright E2E + nginx/iframe 接入说明 | 视觉 | 12 | ⬜ |

### 并行波次（Master 调度用 · 同波次可并发派发 Worker）

- **Wave A（0 完成后并发）：** 1 ｜ 2 ｜ 3 ｜ 6 ｜ 7 —— 互不依赖的纯逻辑/工具。
- **Wave B：** 4（依赖 1）｜ 5（依赖 1）｜ 8（依赖 3）。
- **Wave C：** 9（依赖 2,6,8）。
- **Wave D：** 10（依赖 1,2,7,9）。
- **Wave E：** 11 → **F：** 12 → **G：** 13（线性收尾）。

> `.gitignore` 由 Task 0 建立；`dengpao.glb` 与 `node_modules/` 忽略。

---

## 执行模型（Master-Worker）

- **Master（主 Agent）：** 按上表依赖调度，同波次并发派发 Worker；每个 Worker 交付后跑该任务验收门（类型/lint/测试 或 视觉门），通过则更新看板 `✅` 并推进下一波；失败则退回同一任务附失败输出。Master 不写业务代码。
- **Worker（子 Agent）：** 领一个任务，**严格按其 TDD 步骤**实现（先失败测试→最小实现→通过→提交），交付独立可测的成果。Worker 只看到自己任务，靠每个任务的 **Interfaces 块**得知邻居的函数名与类型。
- 推荐用 `superpowers:subagent-driven-development`（每任务一个全新子 Agent + 两段式评审）承载该模式。

---

## 文件结构（决策锁定）

```
bulb-orbit/v2/
├── package.json            # three@0.160 gsap ｜ dev: vite vitest typescript eslint typescript-eslint happy-dom @playwright/test
├── tsconfig.json           # strict, moduleResolution "bundler", noEmit
├── vite.config.ts
├── eslint.config.js        # flat config
├── vitest.config.ts        # environment happy-dom
├── .gitignore              # node_modules, *.glb, dist
├── index.html              # 2:2:1 三栏 DOM 壳
├── src/
│   ├── main.ts             # 入口：挂载场景 + 接线交互（Task 11）
│   ├── data/
│   │   └── models.ts       # 24 家注册表（Task 1）★单测
│   ├── scene/
│   │   ├── config.ts       # 尺寸/轨道/进动常量（Task 2）★单测
│   │   ├── orbit-math.ts   # 角度/进动/相机偏移纯函数（Task 2）★单测
│   │   ├── bulb.ts         # 粒子灯泡装配（Task 9）视觉门
│   │   ├── orbits.ts       # 轨道线+进动（Task 10）视觉门
│   │   ├── satellites.ts   # 24 卫星（Task 10）视觉门
│   │   └── scene.ts        # 场景/相机/renderer/composer/animate（Task 11）视觉门
│   ├── ui/
│   │   ├── hud.ts          # 右栏 HUD（Task 4）★单测（happy-dom）
│   │   └── layout.ts       # 响应式纯函数（Task 3）★单测
│   ├── interactions/
│   │   ├── logic.ts        # 悬停/变色纯逻辑（Task 5）★单测
│   │   └── raycast.ts      # raycast 接线（Task 11）视觉门
│   ├── fallback.ts         # webglAvailable() + 兜底海报（Task 12）★单测（guard）
│   └── style.css           # 2:2:1 布局 + 中文字体栈（Task 8）
├── public/
│   ├── models/             # dengpao_points_smooth.bin + 7 家 halo bin
│   ├── logos/              # 24 张处理后 PNG
│   └── poster.png          # WebGL 兜底海报（Task 12 收尾实截）
├── tools/
│   ├── resample-bulb.mjs   # GLB 平滑重采样（Task 6）★单测（纯采样）
│   ├── sampler.mjs         # 被上面 import 的纯采样函数（Task 6）★单测
│   └── process-logos.cjs   # logo 逆 ACES 管线（Task 7）★单测（纯补偿）
└── tests/                  # 或就近 *.test.ts；本计划用就近同名 .test.ts
```

---

## Task 0: 工程脚手架与工具链

**Files:**
- Create: `bulb-orbit/v2/package.json`, `tsconfig.json`, `vite.config.ts`, `vitest.config.ts`, `eslint.config.js`, `.gitignore`, `index.html`（占位）, `src/main.ts`（占位）
- Test: `src/sanity.test.ts`

**Interfaces:**
- Consumes: 无。
- Produces: 工作脚本 `npm run dev|build|typecheck|lint|test`；确立 `strict` TS + flat ESLint + happy-dom Vitest。

- [ ] **Step 1: 写 package.json**

```json
{
  "name": "wedream-landing",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "tsc --noEmit && vite build",
    "preview": "vite preview",
    "typecheck": "tsc --noEmit",
    "lint": "eslint .",
    "test": "vitest run"
  },
  "dependencies": { "three": "0.160.0", "gsap": "^3.12.5" },
  "devDependencies": {
    "typescript": "^5.6.0",
    "vite": "^5.4.0",
    "vitest": "^2.1.0",
    "happy-dom": "^15.0.0",
    "eslint": "^9.10.0",
    "typescript-eslint": "^8.5.0",
    "@types/three": "0.160.0"
  }
}
```

- [ ] **Step 2: 写 tsconfig.json**

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ESNext",
    "moduleResolution": "bundler",
    "strict": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "noImplicitReturns": true,
    "noEmit": true,
    "skipLibCheck": true,
    "types": ["vite/client"]
  },
  "include": ["src", "tests", "tools", "vite.config.ts", "vitest.config.ts"]
}
```

- [ ] **Step 3: 写 vite/vitest/eslint/gitignore 配置**

`vite.config.ts`:
```ts
import { defineConfig } from 'vite'
export default defineConfig({ base: './' })
```
`vitest.config.ts`:
```ts
import { defineConfig } from 'vitest/config'
export default defineConfig({ test: { environment: 'happy-dom', include: ['src/**/*.test.ts', 'tools/**/*.test.mjs'] } })
```
`eslint.config.js`:
```js
import tseslint from 'typescript-eslint'
export default tseslint.config(
  ...tseslint.configs.recommended,
  { ignores: ['dist', 'node_modules', 'public'] },
)
```
`.gitignore`:
```
node_modules
dist
*.glb
```
`index.html`（占位，Task 8 覆盖）:
```html
<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><title>WeDream AI</title></head>
<body><script type="module" src="/src/main.ts"></script></body></html>
```
`src/main.ts`（占位）:
```ts
export {}
```

- [ ] **Step 4: 写通过性测试（证明工具链跑通）**

`src/sanity.test.ts`:
```ts
import { describe, it, expect } from 'vitest'
describe('toolchain', () => {
  it('runs vitest', () => { expect(1 + 1).toBe(2) })
})
```

- [ ] **Step 5: 安装并跑三门**

Run: `cd bulb-orbit/v2 && npm install && npm run typecheck && npm run lint && npm run test`
Expected: 三者均 PASS，无错误。

- [ ] **Step 6: Commit**

```bash
git add bulb-orbit/v2
git commit -m "chore(landing): scaffold v2 vite+ts+vitest+eslint toolchain"
```

---

## Task 1: 24 家模型注册表 `data/models.ts`

**Files:**
- Create: `src/data/models.ts`
- Test: `src/data/models.test.ts`

**Interfaces:**
- Consumes: 无。
- Produces:
  - `type Orbit = 'intl' | 'domestic'`
  - `interface Model { id: string; name: string; provider: string; desc: string; scene: string; telemetry: string; brandColor: string; orbit: Orbit; logo: string }`
  - `const MODELS: readonly Model[]`（24 条）
  - `const DEFAULT_MODEL: Model`（WeDream 核心，brandColor `#00f0ff`）
  - `function getModel(id: string): Model | undefined`
  - `function orbitModels(orbit: Orbit): Model[]`

- [ ] **Step 1: 写失败测试**

`src/data/models.test.ts`:
```ts
import { describe, it, expect } from 'vitest'
import { MODELS, DEFAULT_MODEL, getModel, orbitModels } from './models'

describe('models registry', () => {
  it('has exactly 24 models', () => { expect(MODELS).toHaveLength(24) })
  it('splits 12 intl / 12 domestic', () => {
    expect(orbitModels('intl')).toHaveLength(12)
    expect(orbitModels('domestic')).toHaveLength(12)
  })
  it('has unique ids', () => {
    expect(new Set(MODELS.map(m => m.id)).size).toBe(24)
  })
  it('every brandColor is a 6-digit hex', () => {
    for (const m of [...MODELS, DEFAULT_MODEL]) expect(m.brandColor).toMatch(/^#[0-9a-fA-F]{6}$/)
  })
  it('every field is non-empty and desc/scene are Chinese-bearing', () => {
    for (const m of MODELS) {
      expect(m.name && m.provider && m.desc && m.scene && m.telemetry && m.logo).toBeTruthy()
      expect(/[一-鿿]/.test(m.desc + m.scene)).toBe(true)
    }
  })
  it('getModel resolves and misses correctly', () => {
    expect(getModel('openai')?.provider).toBe('OPENAI')
    expect(getModel('nope')).toBeUndefined()
  })
  it('default model is the WeDream core', () => {
    expect(DEFAULT_MODEL.brandColor).toBe('#00f0ff')
  })
})
```

- [ ] **Step 2: 跑测试确认失败**

Run: `npm run test -- models`
Expected: FAIL（`Cannot find module './models'`）。

- [ ] **Step 3: 写实现（数据取自 proposal 附录 A/B，全 24 条 + 默认）**

`src/data/models.ts`:
```ts
export type Orbit = 'intl' | 'domestic'

export interface Model {
  id: string
  name: string
  provider: string
  desc: string
  scene: string
  telemetry: string
  brandColor: string
  orbit: Orbit
  logo: string
}

export const DEFAULT_MODEL: Model = {
  id: 'core', name: 'WeDream 核心', provider: '系统运行中',
  desc: '中央智能核心，环绕的卫星代表已接入的大模型，实时互联。', scene: '——',
  telemetry: '核心负载 100%', brandColor: '#00f0ff', orbit: 'intl', logo: '',
}

export const MODELS: readonly Model[] = [
  // —— 国际轨（12）——
  { id: 'openai', name: 'GPT 系列', provider: 'OPENAI', desc: '多模态旗舰，通用智能与工具调用能力顶尖，生态最成熟。', scene: '通用对话、智能体、多模态理解与生成。', telemetry: '多模态管线在线', brandColor: '#10d075', orbit: 'intl', logo: 'openai.png' },
  { id: 'anthropic', name: 'Claude 系列', provider: 'ANTHROPIC', desc: '代码与复杂推理标杆，长上下文稳定可靠，安全对齐出色。', scene: '编程智能体、长文档分析、严肃写作。', telemetry: '200K 上下文激活', brandColor: '#d97757', orbit: 'intl', logo: 'anthropic.png' },
  { id: 'gemini', name: 'Gemini 系列', provider: 'GOOGLE DEEPMIND', desc: '原生多模态引擎，百万级 token 上下文，视频音频理解强。', scene: '多模态分析、超长资料库问答。', telemetry: '1M token 管线', brandColor: '#9020f0', orbit: 'intl', logo: 'gemini.png' },
  { id: 'meta', name: 'Llama 系列', provider: 'META AI', desc: '开源权重旗舰，社区生态庞大，可完全私有化部署。', scene: '私有化部署、定制微调、开源研究。', telemetry: '开源权重可用', brandColor: '#0596ff', orbit: 'intl', logo: 'meta.png' },
  { id: 'mistral', name: 'Mistral 系列', provider: 'MISTRAL AI', desc: '欧洲开源新锐，小模型效率极高，MoE 架构先行者。', scene: '低成本推理、边缘部署、多语种应用。', telemetry: 'MoE 引擎在线', brandColor: '#ff7000', orbit: 'intl', logo: 'mistral.png' },
  { id: 'deepseek', name: 'DeepSeek 系列', provider: 'DEEPSEEK', desc: '推理与代码性价比之王，开源开放，数学推理尤强。', scene: '高性价比推理、代码生成、数学解题。', telemetry: '推理链激活', brandColor: '#4fa3ff', orbit: 'intl', logo: 'deepseek.png' },
  { id: 'xai', name: 'Grok 系列', provider: 'XAI', desc: '接入实时资讯流，风格鲜明，推理能力快速迭代。', scene: '实时信息问答、热点分析。', telemetry: '实时检索同步', brandColor: '#00a0ff', orbit: 'intl', logo: 'xai.png' },
  { id: 'cohere', name: 'Command 系列', provider: 'COHERE', desc: '企业级检索与嵌入见长，RAG 工具链完善，多语种企业部署。', scene: '企业知识库、语义搜索、RAG 应用。', telemetry: 'RAG 管线在线', brandColor: '#ff7759', orbit: 'intl', logo: 'cohere.png' },
  { id: 'midjourney', name: 'Midjourney', provider: 'MIDJOURNEY', desc: '顶级艺术风格图像生成，美学表现力公认最强。', scene: '概念设计、海报插画、艺术创作。', telemetry: '渲染农场在线', brandColor: '#9bb5ff', orbit: 'intl', logo: 'midjourney.png' },
  { id: 'stability', name: 'Stable Diffusion 系列', provider: 'STABILITY AI', desc: '开源图像生成标杆，插件生态丰富，可本地部署。', scene: '可控图像生成、二次开发、本地出图。', telemetry: '扩散管线就绪', brandColor: '#b266ff', orbit: 'intl', logo: 'stability.png' },
  { id: 'huggingface', name: '开源模型枢纽', provider: 'HUGGING FACE', desc: '全球最大开源模型社区，数十万模型即取即用。', scene: '开源模型试用、推理 API、数据集。', telemetry: 'Hub 已连接', brandColor: '#ffd21e', orbit: 'intl', logo: 'huggingface.png' },
  { id: 'perplexity', name: 'Sonar 系列', provider: 'PERPLEXITY', desc: 'AI 原生搜索引擎，答案附引用来源，实时联网。', scene: '联网问答、资料调研、事实核查。', telemetry: '联网检索激活', brandColor: '#2bb8ce', orbit: 'intl', logo: 'perplexity.png' },
  // —— 国产轨（12）——
  { id: 'qwen', name: '通义千问系列', provider: '阿里云', desc: '国产开源旗舰，代码与多语种能力强，模型尺寸谱系最全。', scene: '中文对话、代码生成、结构化输出。', telemetry: '全尺寸谱系在线', brandColor: '#6b6dff', orbit: 'domestic', logo: 'qwen.png' },
  { id: 'minimax', name: 'MiniMax 系列', provider: 'MINIMAX', desc: '长上下文与多模态并进，语音合成表现出色。', scene: '长文处理、语音应用、角色对话。', telemetry: '百万级上下文', brandColor: '#b987ff', orbit: 'domestic', logo: 'minimax.png' },
  { id: 'doubao', name: '豆包大模型', provider: '字节跳动', desc: '高并发低成本，中文日常对话体验佳，规模化验证充分。', scene: '大规模 C 端应用、智能客服、翻译。', telemetry: '火山引擎管线', brandColor: '#39c5ff', orbit: 'domestic', logo: 'doubao.png' },
  { id: 'stepfun', name: 'Step 系列', provider: '阶跃星辰', desc: '多模态理解见长，万亿参数 MoE 路线探索者。', scene: '图文理解、多模态创作。', telemetry: '多模态管线在线', brandColor: '#92a2ff', orbit: 'domestic', logo: 'stepfun.png' },
  { id: 'kimi', name: 'Kimi 系列', provider: '月之暗面', desc: '超长上下文先行者，网页与文档整理利器，推理模型开源。', scene: '长文档阅读、资料汇总、深度推理。', telemetry: '超长上下文激活', brandColor: '#6c7cff', orbit: 'domestic', logo: 'kimi.png' },
  { id: 'huawei_pangu', name: '盘古大模型', provider: '华为云', desc: '行业大模型深耕，政企场景与昇腾算力生态深度结合。', scene: '政企行业方案、私有云部署。', telemetry: '昇腾集群在线', brandColor: '#ef3340', orbit: 'domestic', logo: 'huawei_pangu.png' },
  { id: 'baidu_wenxin', name: '文心大模型', provider: '百度', desc: '中文知识增强路线，检索增强与插件生态成熟。', scene: '中文创作、企业应用、搜索增强。', telemetry: '知识增强激活', brandColor: '#2d78ff', orbit: 'domestic', logo: 'baidu_wenxin.png' },
  { id: 'zeroone_ai', name: 'Yi 系列', provider: '零一万物', desc: '中英双语开源佳作，长文本与多模态兼备。', scene: '双语应用、开源定制。', telemetry: '双语管线在线', brandColor: '#61d3ff', orbit: 'domestic', logo: 'zeroone_ai.png' },
  { id: 'tencent_hunyuan', name: '混元大模型', provider: '腾讯', desc: '全链路自研，文生图与视频多模态齐全，微信生态天然接入。', scene: '内容创作、腾讯生态应用。', telemetry: '多模态就绪', brandColor: '#25d6ff', orbit: 'domestic', logo: 'tencent_hunyuan.png' },
  { id: 'baichuan_ai', name: 'Baichuan 系列', provider: '百川智能', desc: '中文开源先锋，医疗等垂直领域持续深化。', scene: '中文垂直领域、开源部署。', telemetry: '垂直增强在线', brandColor: '#2de2a0', orbit: 'domestic', logo: 'baichuan_ai.png' },
  { id: 'glm_chatglm', name: 'GLM 系列', provider: '智谱 AI', desc: '清华系技术底蕴，Agent 与代码能力强，开源开放。', scene: '智能体开发、代码辅助、学术研究。', telemetry: 'Agent 管线激活', brandColor: '#6f7bff', orbit: 'domestic', logo: 'glm_chatglm.png' },
  { id: 'iflytek_spark', name: '星火大模型', provider: '科大讯飞', desc: '语音交互天然优势，教育医疗行业落地深。', scene: '语音助手、教育应用、办公纪要。', telemetry: '语音引擎在线', brandColor: '#ff395d', orbit: 'domestic', logo: 'iflytek_spark.png' },
]

const BY_ID = new Map(MODELS.map(m => [m.id, m]))
export function getModel(id: string): Model | undefined { return BY_ID.get(id) }
export function orbitModels(orbit: Orbit): Model[] { return MODELS.filter(m => m.orbit === orbit) }
```

- [ ] **Step 4: 跑测试确认通过 + 类型 + lint**

Run: `npm run test -- models && npm run typecheck && npm run lint`
Expected: 全 PASS。

- [ ] **Step 5: Commit**

```bash
git add src/data/models.ts src/data/models.test.ts
git commit -m "feat(landing): add 24-model registry with unit tests"
```

---

## Task 2: 场景数学 `scene/config.ts` + `scene/orbit-math.ts`

**Files:**
- Create: `src/scene/config.ts`, `src/scene/orbit-math.ts`
- Test: `src/scene/orbit-math.test.ts`

**Interfaces:**
- Consumes: 无。
- Produces:
  - `config.ts`: `const BULB_SCALE: number`（相对 yun 放大系数，1.6）；`interface OrbitCfg { radius:number; tiltX:number; tiltZ:number; satSpeed:number; precessPeriod:number; precessDir:1|-1 }`；`const ORBITS: { intl: OrbitCfg; domestic: OrbitCfg }`；`const CAMERA_Z:number`；`const VIEW_OFFSET_X_RATIO:number`（0.10，把灯泡推到 x≈60%）。
  - `orbit-math.ts`:
    - `satelliteAngle(baseAngle:number, speed:number, t:number): number`
    - `precessionAngle(dir:1|-1, period:number, t:number): number` → 弧度，`0..2π`
    - `evenAngles(n:number, phase:number): number[]`（n 等分，含 phase 偏移）
    - `viewOffset(width:number, ratio:number): number`（返回 `camera.setViewOffset` 的 offsetX 像素，正值把内容右移使主体偏左…注意号：见测试）

- [ ] **Step 1: 写失败测试**

`src/scene/orbit-math.test.ts`:
```ts
import { describe, it, expect } from 'vitest'
import { satelliteAngle, precessionAngle, evenAngles, viewOffset } from './orbit-math'
import { ORBITS } from './config'

const TAU = Math.PI * 2

describe('orbit-math', () => {
  it('satelliteAngle is deterministic and linear in t', () => {
    expect(satelliteAngle(0, 0.1, 0)).toBe(0)
    expect(satelliteAngle(1, 0.1, 10)).toBeCloseTo(2, 6)
  })
  it('precessionAngle wraps into [0, 2π) and respects direction', () => {
    const a = precessionAngle(1, 40, 10)   // +
    const b = precessionAngle(-1, 40, 10)  // -
    expect(a).toBeGreaterThanOrEqual(0); expect(a).toBeLessThan(TAU)
    expect(b).toBeGreaterThanOrEqual(0); expect(b).toBeLessThan(TAU)
    expect(a).toBeCloseTo(TAU - b, 6)      // opposite directions are mirror-wrapped
  })
  it('evenAngles splits the circle with phase offset', () => {
    const xs = evenAngles(4, 0)
    expect(xs).toHaveLength(4)
    expect(xs[1] - xs[0]).toBeCloseTo(TAU / 4, 6)
    expect(evenAngles(3, 0.5)[0]).toBeCloseTo(0.5, 6)
  })
  it('viewOffset is proportional to width', () => {
    expect(viewOffset(1000, 0.1)).toBeCloseTo(100, 6)
    expect(viewOffset(0, 0.1)).toBe(0)
  })
})

describe('config', () => {
  it('domestic orbit is wider than intl', () => {
    expect(ORBITS.domestic.radius).toBeGreaterThan(ORBITS.intl.radius)
  })
  it('the two orbits precess in opposite directions', () => {
    expect(ORBITS.intl.precessDir).toBe(1)
    expect(ORBITS.domestic.precessDir).toBe(-1)
  })
  it('precession periods are within the spec 45s/70s ballpark', () => {
    expect(ORBITS.intl.precessPeriod).toBeGreaterThanOrEqual(30)
    expect(ORBITS.domestic.precessPeriod).toBeGreaterThan(ORBITS.intl.precessPeriod)
  })
})
```

- [ ] **Step 2: 跑测试确认失败**

Run: `npm run test -- orbit-math`
Expected: FAIL（模块缺失）。

- [ ] **Step 3: 写实现**

`src/scene/config.ts`:
```ts
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
```
`src/scene/orbit-math.ts`:
```ts
const TAU = Math.PI * 2
const wrap = (a: number) => ((a % TAU) + TAU) % TAU

export function satelliteAngle(baseAngle: number, speed: number, t: number): number {
  return baseAngle + speed * t
}

export function precessionAngle(dir: 1 | -1, period: number, t: number): number {
  return wrap(dir * (TAU / period) * t)
}

export function evenAngles(n: number, phase: number): number[] {
  const step = TAU / n
  return Array.from({ length: n }, (_, i) => phase + i * step)
}

export function viewOffset(width: number, ratio: number): number {
  return width * ratio
}
```

- [ ] **Step 4: 跑测试 + 类型 + lint**

Run: `npm run test -- orbit-math && npm run typecheck && npm run lint`
Expected: 全 PASS。

- [ ] **Step 5: Commit**

```bash
git add src/scene/config.ts src/scene/orbit-math.ts src/scene/orbit-math.test.ts
git commit -m "feat(landing): add scene config + orbit/precession math with tests"
```

---

## Task 3: 响应式 `ui/layout.ts`

**Files:**
- Create: `src/ui/layout.ts`
- Test: `src/ui/layout.test.ts`

**Interfaces:**
- Consumes: 无。
- Produces:
  - `type Breakpoint = 'desktop' | 'tablet' | 'mobile'`
  - `function breakpointFor(width: number): Breakpoint`（≥1024 desktop，≥768 tablet，否则 mobile）
  - `function particleCount(bp: Breakpoint): number`（desktop/tablet 40000，mobile 20000）
  - `function bloomEnabled(bp: Breakpoint): boolean`（mobile=false）

- [ ] **Step 1: 写失败测试**

`src/ui/layout.test.ts`:
```ts
import { describe, it, expect } from 'vitest'
import { breakpointFor, particleCount, bloomEnabled } from './layout'

describe('layout', () => {
  it('maps width to breakpoint at 1024/768', () => {
    expect(breakpointFor(1920)).toBe('desktop')
    expect(breakpointFor(1024)).toBe('desktop')
    expect(breakpointFor(1023)).toBe('tablet')
    expect(breakpointFor(768)).toBe('tablet')
    expect(breakpointFor(767)).toBe('mobile')
    expect(breakpointFor(390)).toBe('mobile')
  })
  it('halves particles on mobile', () => {
    expect(particleCount('desktop')).toBe(40000)
    expect(particleCount('mobile')).toBe(20000)
  })
  it('disables bloom on mobile only', () => {
    expect(bloomEnabled('desktop')).toBe(true)
    expect(bloomEnabled('mobile')).toBe(false)
  })
})
```

- [ ] **Step 2: 跑测试确认失败** — Run: `npm run test -- layout` → FAIL。

- [ ] **Step 3: 写实现**

`src/ui/layout.ts`:
```ts
export type Breakpoint = 'desktop' | 'tablet' | 'mobile'

export function breakpointFor(width: number): Breakpoint {
  if (width >= 1024) return 'desktop'
  if (width >= 768) return 'tablet'
  return 'mobile'
}

export function particleCount(bp: Breakpoint): number {
  return bp === 'mobile' ? 20000 : 40000
}

export function bloomEnabled(bp: Breakpoint): boolean {
  return bp !== 'mobile'
}
```

- [ ] **Step 4: 跑测试 + 类型 + lint** — Run: `npm run test -- layout && npm run typecheck && npm run lint` → PASS。

- [ ] **Step 5: Commit**

```bash
git add src/ui/layout.ts src/ui/layout.test.ts
git commit -m "feat(landing): add responsive breakpoint helpers with tests"
```

---

## Task 4: 右栏 HUD `ui/hud.ts`

**Files:**
- Create: `src/ui/hud.ts`
- Test: `src/ui/hud.test.ts`

**Interfaces:**
- Consumes: `Model`, `DEFAULT_MODEL`, `getModel` from `../data/models`.
- Produces:
  - `interface HudEls { name: HTMLElement; provider: HTMLElement; desc: HTMLElement; telemetry: HTMLElement; panel: HTMLElement }`
  - `function renderHud(els: HudEls, model: Model): void`（写 textContent + `panel.style.borderLeftColor = brandColor`）
  - `function hudModelFor(id: string | null): Model`（null 或未命中 → `DEFAULT_MODEL`）

- [ ] **Step 1: 写失败测试（happy-dom）**

`src/ui/hud.test.ts`:
```ts
import { describe, it, expect, beforeEach } from 'vitest'
import { renderHud, hudModelFor, type HudEls } from './hud'
import { getModel, DEFAULT_MODEL } from '../data/models'

function makeEls(): HudEls {
  const mk = () => document.createElement('div')
  return { name: mk(), provider: mk(), desc: mk(), telemetry: mk(), panel: mk() }
}

describe('hud', () => {
  let els: HudEls
  beforeEach(() => { els = makeEls() })

  it('renders a model into the DOM with brand border', () => {
    const m = getModel('anthropic')!
    renderHud(els, m)
    expect(els.name.textContent).toBe('Claude 系列')
    expect(els.provider.textContent).toBe('ANTHROPIC')
    expect(els.desc.textContent).toContain('推理')
    expect(els.telemetry.textContent).toBe('200K 上下文激活')
    // happy-dom normalises hex to rgb(...)
    expect(els.panel.style.borderLeftColor).toBeTruthy()
  })

  it('hudModelFor falls back to the core on null / miss', () => {
    expect(hudModelFor(null)).toBe(DEFAULT_MODEL)
    expect(hudModelFor('nope')).toBe(DEFAULT_MODEL)
    expect(hudModelFor('openai').id).toBe('openai')
  })
})
```

- [ ] **Step 2: 跑测试确认失败** — Run: `npm run test -- hud` → FAIL。

- [ ] **Step 3: 写实现**

`src/ui/hud.ts`:
```ts
import { type Model, DEFAULT_MODEL, getModel } from '../data/models'

export interface HudEls {
  name: HTMLElement
  provider: HTMLElement
  desc: HTMLElement
  telemetry: HTMLElement
  panel: HTMLElement
}

export function renderHud(els: HudEls, model: Model): void {
  els.name.textContent = model.name
  els.provider.textContent = model.provider
  els.desc.textContent = model.desc
  els.telemetry.textContent = model.telemetry
  els.panel.style.borderLeftColor = model.brandColor
}

export function hudModelFor(id: string | null): Model {
  if (!id) return DEFAULT_MODEL
  return getModel(id) ?? DEFAULT_MODEL
}
```

- [ ] **Step 4: 跑测试 + 类型 + lint** — Run: `npm run test -- hud && npm run typecheck && npm run lint` → PASS。

- [ ] **Step 5: Commit**

```bash
git add src/ui/hud.ts src/ui/hud.test.ts
git commit -m "feat(landing): add HUD render + fallback logic with dom tests"
```

---

## Task 5: 交互逻辑 `interactions/logic.ts`

**Files:**
- Create: `src/interactions/logic.ts`
- Test: `src/interactions/logic.test.ts`

**Interfaces:**
- Consumes: `getModel`, `DEFAULT_MODEL` from `../data/models`.
- Produces:
  - `function coreColorFor(id: string | null): string`（命中→brandColor，否则→`#00f0ff`）
  - `interface HoverResult { modelId: string | null; changed: boolean }`
  - `function resolveHover(prevId: string | null, hitId: string | null): HoverResult`（`changed` 仅当 id 变化时 true）

- [ ] **Step 1: 写失败测试**

`src/interactions/logic.test.ts`:
```ts
import { describe, it, expect } from 'vitest'
import { coreColorFor, resolveHover } from './logic'

describe('interaction logic', () => {
  it('coreColorFor returns brand color or the cyan core default', () => {
    expect(coreColorFor('anthropic')).toBe('#d97757')
    expect(coreColorFor(null)).toBe('#00f0ff')
    expect(coreColorFor('nope')).toBe('#00f0ff')
  })
  it('resolveHover flags change only on transition', () => {
    expect(resolveHover(null, 'openai')).toEqual({ modelId: 'openai', changed: true })
    expect(resolveHover('openai', 'openai')).toEqual({ modelId: 'openai', changed: false })
    expect(resolveHover('openai', null)).toEqual({ modelId: null, changed: true })
  })
})
```

- [ ] **Step 2: 跑测试确认失败** — Run: `npm run test -- logic` → FAIL。

- [ ] **Step 3: 写实现**

`src/interactions/logic.ts`:
```ts
import { getModel } from '../data/models'

const CORE_CYAN = '#00f0ff'

export function coreColorFor(id: string | null): string {
  if (!id) return CORE_CYAN
  return getModel(id)?.brandColor ?? CORE_CYAN
}

export interface HoverResult { modelId: string | null; changed: boolean }

export function resolveHover(prevId: string | null, hitId: string | null): HoverResult {
  return { modelId: hitId, changed: prevId !== hitId }
}
```

- [ ] **Step 4: 跑测试 + 类型 + lint** — Run: `npm run test -- logic && npm run typecheck && npm run lint` → PASS。

- [ ] **Step 5: Commit**

```bash
git add src/interactions/logic.ts src/interactions/logic.test.ts
git commit -m "feat(landing): add hover/core-color interaction logic with tests"
```

---

## Task 6: 灯泡平滑重采样 `tools/resample-bulb.mjs` + `tools/sampler.mjs`

> 目标：把 yun 的 `dengpao.glb`（一次性下载，83MB，不入库）网格**细分 + 拉普拉斯平滑**后，均匀重采样 **40000 个点（position + 法线）** 写入 `public/models/dengpao_points_smooth.bin`（`Float32Array`，每点 6 个 float，与 yun 现有 bin 格式一致）。纯采样数学抽到 `sampler.mjs` 单测；GLB 装载/文件 IO 在 `resample-bulb.mjs` 中，不单测。

**Files:**
- Create: `tools/sampler.mjs`, `tools/resample-bulb.mjs`
- Test: `tools/sampler.test.mjs`
- Output（运行后生成，入库）: `public/models/dengpao_points_smooth.bin`

**Interfaces:**
- Consumes: 无（`resample-bulb.mjs` 用 `three` 的 `GLTFLoader` + `BufferGeometryUtils`）。
- Produces（`sampler.mjs`）:
  - `export function areaWeightedSample(positions: Float32Array, indices: Uint32Array, normals: Float32Array, count: number, rand: () => number): Float32Array` —— 按三角面积加权在表面撒 `count` 个点，返回长度 `count*6`（xyz + nxyz，法线单位化）。
  - `export function laplacianSmooth(positions: Float32Array, indices: Uint32Array, iterations: number): Float32Array` —— 邻接平均平滑顶点，返回新 positions。

- [ ] **Step 1: 写失败测试（合成一个单位四面体，验证纯采样性质）**

`tools/sampler.test.mjs`:
```js
import { describe, it, expect } from 'vitest'
import { areaWeightedSample, laplacianSmooth } from './sampler.mjs'

// 单位四面体：4 顶点 4 面
const positions = new Float32Array([0,0,0, 1,0,0, 0,1,0, 0,0,1])
const indices = new Uint32Array([0,1,2, 0,1,3, 0,2,3, 1,2,3])
const normals = new Float32Array([0,0,-1, 0,-1,0, -1,0,0, 0.577,0.577,0.577])

describe('areaWeightedSample', () => {
  it('emits exactly count points × 6 floats', () => {
    let s = 1; const rand = () => (s = (s * 16807) % 2147483647) / 2147483647
    const out = areaWeightedSample(positions, indices, normals, 500, rand)
    expect(out).toBeInstanceOf(Float32Array)
    expect(out.length).toBe(500 * 6)
  })
  it('normals are unit length', () => {
    let s = 7; const rand = () => (s = (s * 16807) % 2147483647) / 2147483647
    const out = areaWeightedSample(positions, indices, normals, 100, rand)
    for (let i = 0; i < 100; i++) {
      const nx = out[i*6+3], ny = out[i*6+4], nz = out[i*6+5]
      expect(Math.hypot(nx, ny, nz)).toBeCloseTo(1, 4)
    }
  })
  it('sampled points stay within the mesh bounding box', () => {
    let s = 3; const rand = () => (s = (s * 16807) % 2147483647) / 2147483647
    const out = areaWeightedSample(positions, indices, normals, 300, rand)
    for (let i = 0; i < 300; i++) {
      for (let k = 0; k < 3; k++) { expect(out[i*6+k]).toBeGreaterThanOrEqual(-1e-6); expect(out[i*6+k]).toBeLessThanOrEqual(1 + 1e-6) }
    }
  })
})

describe('laplacianSmooth', () => {
  it('keeps vertex count and moves interior vertices toward neighbours', () => {
    const out = laplacianSmooth(positions, indices, 1)
    expect(out.length).toBe(positions.length)
    // 平滑后仍在原包围盒内（不发散）
    for (const v of out) { expect(v).toBeGreaterThanOrEqual(-1); expect(v).toBeLessThanOrEqual(1.5) }
  })
})
```

- [ ] **Step 2: 跑测试确认失败** — Run: `npm run test -- sampler` → FAIL。

- [ ] **Step 3: 写 `tools/sampler.mjs`（纯函数）**

```js
// 面积加权表面采样：每个三角按面积占比分配点数，在三角内用重心坐标撒点，法线按顶点法线插值后单位化。
export function areaWeightedSample(positions, indices, normals, count, rand) {
  const triCount = indices.length / 3
  const areas = new Float64Array(triCount)
  let total = 0
  const ax=[0,0,0], bx=[0,0,0], cx=[0,0,0], e1=[0,0,0], e2=[0,0,0], cr=[0,0,0]
  const load = (dst, i) => { dst[0]=positions[i*3]; dst[1]=positions[i*3+1]; dst[2]=positions[i*3+2] }
  for (let t = 0; t < triCount; t++) {
    load(ax, indices[t*3]); load(bx, indices[t*3+1]); load(cx, indices[t*3+2])
    for (let k=0;k<3;k++){ e1[k]=bx[k]-ax[k]; e2[k]=cx[k]-ax[k] }
    cr[0]=e1[1]*e2[2]-e1[2]*e2[1]; cr[1]=e1[2]*e2[0]-e1[0]*e2[2]; cr[2]=e1[0]*e2[1]-e1[1]*e2[0]
    areas[t] = 0.5 * Math.hypot(cr[0], cr[1], cr[2]); total += areas[t]
  }
  // 前缀和用于按面积随机选面
  const cdf = new Float64Array(triCount); let acc = 0
  for (let t=0;t<triCount;t++){ acc += areas[t]/total; cdf[t]=acc }
  const out = new Float32Array(count * 6)
  const pickTri = (r) => { let lo=0, hi=triCount-1; while(lo<hi){ const mid=(lo+hi)>>1; if(cdf[mid]<r) lo=mid+1; else hi=mid } return lo }
  for (let i = 0; i < count; i++) {
    const t = pickTri(rand())
    const ia=indices[t*3], ib=indices[t*3+1], ic=indices[t*3+2]
    let u = rand(), v = rand(); if (u+v>1){ u=1-u; v=1-v } const w = 1-u-v
    for (let k=0;k<3;k++) out[i*6+k] = w*positions[ia*3+k] + u*positions[ib*3+k] + v*positions[ic*3+k]
    let nx = w*normals[ia*3]+u*normals[ib*3]+v*normals[ic*3]
    let ny = w*normals[ia*3+1]+u*normals[ib*3+1]+v*normals[ic*3+1]
    let nz = w*normals[ia*3+2]+u*normals[ib*3+2]+v*normals[ic*3+2]
    const len = Math.hypot(nx,ny,nz) || 1; out[i*6+3]=nx/len; out[i*6+4]=ny/len; out[i*6+5]=nz/len
  }
  return out
}

// 拉普拉斯平滑：每个顶点向其一环邻居的平均位置靠拢（uniform weights），迭代 iterations 次。
export function laplacianSmooth(positions, indices, iterations) {
  const n = positions.length / 3
  const adj = Array.from({ length: n }, () => new Set())
  for (let t = 0; t < indices.length; t += 3) {
    const a=indices[t], b=indices[t+1], c=indices[t+2]
    adj[a].add(b); adj[a].add(c); adj[b].add(a); adj[b].add(c); adj[c].add(a); adj[c].add(b)
  }
  let cur = Float32Array.from(positions)
  for (let it = 0; it < iterations; it++) {
    const next = Float32Array.from(cur)
    for (let v = 0; v < n; v++) {
      if (adj[v].size === 0) continue
      let sx=0, sy=0, sz=0
      for (const w of adj[v]) { sx+=cur[w*3]; sy+=cur[w*3+1]; sz+=cur[w*3+2] }
      const inv = 1 / adj[v].size
      next[v*3] = sx*inv; next[v*3+1] = sy*inv; next[v*3+2] = sz*inv
    }
    cur = next
  }
  return cur
}
```

- [ ] **Step 4: 写 `tools/resample-bulb.mjs`（IO 编排，不单测）**

```js
// 用法：node tools/resample-bulb.mjs <dengpao.glb 路径>
// 依赖 three 的 GLTFLoader + BufferGeometryUtils（node 端用 file:// fetch polyfill 或直接读 buffer）。
import { readFileSync, writeFileSync } from 'node:fs'
import { GLTFLoader } from 'three/examples/jsm/loaders/GLTFLoader.js'
import * as BufferGeometryUtils from 'three/examples/jsm/utils/BufferGeometryUtils.js'
import { LoopSubdivision } from 'three/examples/jsm/... ' // 若无则用 sampler 直接在原网格上撒点，跳过细分
import { areaWeightedSample, laplacianSmooth } from './sampler.mjs'

const glbPath = process.argv[2]
if (!glbPath) { console.error('usage: node tools/resample-bulb.mjs <glb>'); process.exit(1) }

const buf = readFileSync(glbPath)
const loader = new GLTFLoader()
loader.parse(buf.buffer.slice(buf.byteOffset, buf.byteOffset + buf.byteLength), '', (gltf) => {
  const geos = []
  gltf.scene.traverse((o) => { if (o.isMesh) geos.push(o.geometry.toNonIndexed().clone().applyMatrix4(o.matrixWorld)) })
  let geo = BufferGeometryUtils.mergeGeometries(geos)
  geo = BufferGeometryUtils.mergeVertices(geo)   // 建立索引以便平滑/邻接
  geo.computeVertexNormals()
  const positions = geo.attributes.position.array
  const indices = geo.index.array
  const smoothed = laplacianSmooth(positions, indices, 2)   // 2 次平滑消棱角
  geo.attributes.position.array.set(smoothed); geo.computeVertexNormals()
  const normals = geo.attributes.normal.array
  let seed = 12345; const rand = () => (seed = (seed * 1103515245 + 12345) & 0x7fffffff) / 0x7fffffff
  const out = areaWeightedSample(new Float32Array(smoothed), new Uint32Array(indices), new Float32Array(normals), 40000, rand)
  writeFileSync('public/models/dengpao_points_smooth.bin', Buffer.from(out.buffer))
  console.log('wrote public/models/dengpao_points_smooth.bin', out.length / 6, 'points')
}, (e) => { console.error(e); process.exit(1) })
```
> 注：`LoopSubdivision` 若在 three 0.160 addons 不可用，删掉该行，仅靠 `laplacianSmooth(…, 2)` + 面积采样即可达成圆润（细分为可选增强）。Worker 若遇 import 报错，走无细分路径并在提交信息注明。

- [ ] **Step 5: 跑纯函数测试；再一次性生成 bin**

Run: `npm run test -- sampler`
Expected: PASS。
下载 glb 并生成（一次性；glb 不入库）:
```bash
curl -L --max-time 600 https://raw.githubusercontent.com/wei500L/yun/main/dengpao.glb -o /tmp/dengpao.glb
node tools/resample-bulb.mjs /tmp/dengpao.glb
```
Expected: 打印 `wrote … 40000 points`，`public/models/dengpao_points_smooth.bin` 约 960KB。

- [ ] **Step 6: 类型/lint（mjs 纳入 lint）+ Commit**

Run: `npm run lint`
```bash
git add tools/sampler.mjs tools/sampler.test.mjs tools/resample-bulb.mjs public/models/dengpao_points_smooth.bin
git commit -m "feat(landing): add glb smooth-resample tool + smoothed bulb point cloud"
```

---

## Task 7: Logo 素材管线 `tools/process-logos.cjs`

> 移植 yun `scripts/process_logos.cjs`：把 logo 渲染成透明底 512px PNG，并烘焙 **逆 ACES 补偿**（页面经 EffectComposer OutputPass 做 ACES 色调映射，补偿后品牌色在屏上还原）。国产 12 家源图用 `bulb-orbit/logos/*.png`；国际 12 家用 yun 内嵌 SVG 栅格化。纯补偿数学抽出单测（往返 `ACES(invACES(c))≈c`）。

**Files:**
- Create: `tools/process-logos.cjs`, `tools/aces.cjs`（抽出的纯补偿）
- Test: `tools/aces.test.mjs`
- Output（入库）: `public/logos/{24 个 id}.png`

**Interfaces:**
- Produces（`aces.cjs`）:
  - `forwardAcesFilmic(rgb: number[]): number[]`
  - `inverseAcesFilmic(rgb: number[]): number[]`（线性域，往返自洽）

- [ ] **Step 1: 写失败测试（往返性质）**

`tools/aces.test.mjs`:
```js
import { describe, it, expect } from 'vitest'
import { forwardAcesFilmic, inverseAcesFilmic } from './aces.cjs'

describe('inverse ACES filmic', () => {
  it('round-trips ACES(invACES(c)) ≈ c for in-gamut linear colors', () => {
    const samples = [[0.1,0.2,0.3],[0.4,0.05,0.5],[0.2,0.6,0.1],[0.05,0.05,0.05]]
    for (const c of samples) {
      const back = forwardAcesFilmic(inverseAcesFilmic(c))
      for (let k=0;k<3;k++) expect(back[k]).toBeCloseTo(c[k], 3)
    }
  })
  it('clamps output into [0,1]', () => {
    for (const v of inverseAcesFilmic([0.9,0.9,0.9])) { expect(v).toBeGreaterThanOrEqual(0); expect(v).toBeLessThanOrEqual(1) }
  })
})
```

- [ ] **Step 2: 跑测试确认失败** — Run: `npm run test -- aces` → FAIL。

- [ ] **Step 3: 写 `tools/aces.cjs`（移植 yun 的矩阵与拟合逆）**

> 直接采用 yun `scripts/process_logos.cjs` 第 49–109 行的 `ACES_INPUT/ACES_OUTPUT`、`invertMat3`、`fitInv`、`forwardAcesFilmic`、`inverseAcesFilmic` 实现，原样搬到 `aces.cjs` 并 `module.exports = { forwardAcesFilmic, inverseAcesFilmic }`。数学与 three r160 `tonemapping_pars_fragment` 完全一致，本任务只是抽成可测单元。

- [ ] **Step 4: 写 `tools/process-logos.cjs`（IO 编排）**

> 移植 yun 原脚本其余部分（背景洪水填充、裁切归一、512 画布、`inverseAcesFilmic` 补偿写像素、输出 PNG，用 `pngjs`）。改动两点：①源目录支持 `bulb-orbit/logos/`（国产 12）与内嵌 SVG 栅格产物目录（国际 12）；②输出到 `v2/public/logos/<id>.png`，文件名对齐 `models.ts` 的 `logo` 字段。国际 12 家 SVG 来自旧版 `bulb-orbit/index.html` 的 `LOGOS` 内联 svg（openai/anthropic/gemini/meta/mistral/deepseek/xai/cohere/midjourney/stability/huggingface/perplexity），用 `sharp` 或 headless 渲染成 512 PNG 作为源图。

- [ ] **Step 5: 跑纯函数测试 + 生成 24 PNG**

Run: `npm run test -- aces`
Expected: PASS。
```bash
node tools/process-logos.cjs
ls public/logos | wc -l   # 期望 24
```

- [ ] **Step 6: lint + Commit**

```bash
git add tools/aces.cjs tools/aces.test.mjs tools/process-logos.cjs public/logos
git commit -m "feat(landing): add logo pipeline (inverse-ACES) + 24 processed logos"
```

---

## Task 8: 布局壳 `index.html` + `style.css`（视觉门）

**Files:**
- Modify: `index.html`（覆盖 Task 0 占位）
- Create: `src/style.css`
- 引用（consume）: `src/ui/layout.ts`（断点值，仅供 CSS 媒体查询对齐）

**Interfaces:**
- Produces（DOM 契约，后续 Task 4/11 按 id 取元素）:
  - 画布 `#webgl-canvas`
  - 左栏：`.hero-left`，含 `#brand-title`(WeDream AI)、`#brand-slogan`(让灵感不再受限)、`#brand-sub`(自由穿梭于顶尖大模型之间)、`#cta-primary`(立即开始)、`#cta-docs`(查看文档)、`.metrics`(三格)
  - 右栏：`#hud-panel` + 子元素 `#hud-name` `#hud-provider` `#hud-desc` `#hud-telemetry`

- [ ] **Step 1: 写 `index.html`（2:2:1 三栏 + 中文文案）**

```html
<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>WeDream AI · 让灵感不再受限</title>
  <link rel="stylesheet" href="/src/style.css" />
</head>
<body>
  <canvas id="webgl-canvas"></canvas>
  <div class="glow-bg glow-bg-1"></div><div class="glow-bg glow-bg-2"></div><div class="vignette"></div>
  <main class="hero">
    <section class="hero-left">
      <div class="badge"><span class="badge-dot"></span>新一代 AI 聚合平台</div>
      <h1 id="brand-title">WeDream AI</h1>
      <p id="brand-slogan" class="slogan">让灵感不再受限</p>
      <p id="brand-sub" class="sub">自由穿梭于顶尖大模型之间</p>
      <div class="actions">
        <a id="cta-primary" class="btn-primary" href="/console">立即开始</a>
        <a id="cta-docs" class="btn-secondary" href="/docs">查看文档</a>
      </div>
      <div class="metrics">
        <div><b>24+</b><span>顶尖模型</span></div><i></i>
        <div><b>1</b><span>统一 API</span></div><i></i>
        <div><b>99.9%</b><span>服务可用</span></div>
      </div>
    </section>
    <section class="hero-stage"><!-- 画布铺底，此栏留白对齐灯泡 --></section>
    <aside class="hero-right">
      <div id="hud-panel" class="hud">
        <div class="hud-head"><span class="hud-dot"></span><span id="hud-name">WeDream 核心</span><span id="hud-provider">系统运行中</span></div>
        <p id="hud-desc" class="hud-desc">中央智能核心，环绕的卫星代表已接入的大模型，实时互联。</p>
        <div class="hud-foot"><span>遥测</span><span id="hud-telemetry">核心负载 100%</span></div>
      </div>
    </aside>
  </main>
  <script type="module" src="/src/main.ts"></script>
</body>
</html>
```

- [ ] **Step 2: 写 `src/style.css`（2:2:1 grid + 中文字体栈 + 深空底 + 媒体查询占位）**

```css
:root { --bg:#01040a; --cyan:#00f0ff; }
* { box-sizing: border-box; }
html,body { margin:0; height:100%; background:var(--bg); color:#eaf2ff;
  font-family: "PingFang SC","Microsoft YaHei","Noto Sans SC",system-ui,-apple-system,sans-serif; overflow:hidden; }
#webgl-canvas { position:fixed; inset:0; width:100vw; height:100vh; display:block; }
.vignette { position:fixed; inset:0; pointer-events:none; box-shadow: inset 0 0 40vmax rgba(0,0,0,.7); }
.hero { position:relative; z-index:2; height:100vh; display:grid; grid-template-columns:2fr 2fr 1fr; align-items:center; padding:0 clamp(24px,4vw,72px); pointer-events:none; }
.hero-left, .hero-right { pointer-events:auto; }
.badge { display:inline-flex; gap:8px; align-items:center; padding:6px 14px; border:1px solid rgba(0,240,255,.35); border-radius:999px; font-size:13px; color:#8fe8ff; }
.badge-dot { width:6px;height:6px;border-radius:50%;background:var(--cyan); box-shadow:0 0 8px var(--cyan); }
#brand-title { font-size:clamp(40px,5vw,72px); font-weight:800; margin:.4em 0 .1em; letter-spacing:.02em; }
.slogan { font-size:clamp(28px,3.4vw,48px); font-weight:700; margin:0;
  background:linear-gradient(90deg,#00f0ff,#7000ff); -webkit-background-clip:text; background-clip:text; color:transparent; }
.sub { color:#9fb2cc; font-size:clamp(14px,1.3vw,18px); margin:.6em 0 1.4em; }
.actions { display:flex; gap:14px; }
.btn-primary,.btn-secondary { padding:12px 22px; border-radius:999px; text-decoration:none; font-weight:600; font-size:15px; }
.btn-primary { background:linear-gradient(90deg,#0af,#06f); color:#fff; }
.btn-secondary { border:1px solid rgba(255,255,255,.2); color:#dbe6ff; }
.metrics { display:flex; align-items:center; gap:18px; margin-top:28px; }
.metrics b { font-size:24px; font-weight:800; } .metrics span{ display:block; color:#7d90ab; font-size:12px; }
.metrics i { width:1px; height:28px; background:rgba(255,255,255,.15); }
.hero-right { display:flex; justify-content:flex-end; }
.hud { width:280px; padding:20px; border-radius:16px; border-left:3px solid var(--cyan);
  background:rgba(10,20,35,.55); backdrop-filter:blur(12px); }
.hud-head { display:flex; align-items:center; gap:8px; flex-wrap:wrap; }
#hud-name { font-weight:700; } #hud-provider { color:#6fd0ff; font-size:12px; letter-spacing:.08em; }
.hud-desc { color:#b9c8dd; font-size:13px; line-height:1.6; }
.hud-foot { display:flex; justify-content:space-between; color:#7d90ab; font-size:12px; border-top:1px solid rgba(255,255,255,.08); padding-top:10px; }
/* 响应式在 Task 12 补全 */
```

- [ ] **Step 3: 视觉门 — 桌面布局截图核对**

Run:
```bash
npm run dev &   # 记住 PID
# Playwright：视口 1920×1080，导航 http://localhost:5173，等 3s，截图 /tmp/landing-task8.png
# 关闭 dev server 与 playwright；ps 复核零残留（W6）
```
预期观察（核对截图）：① 左栏「WeDream AI / 让灵感不再受限 / 自由穿梭…」渐变标语正确；② 三格指标条「24+ / 1 / 99.9%」中文；③ 右栏毛玻璃 HUD 卡默认「WeDream 核心」；④ 整体 2:2:1 分栏，无横向滚动；⑤ 文案**全中文**（品牌/供应商英文名除外）。

- [ ] **Step 4: 类型 + lint** — Run: `npm run typecheck && npm run lint` → PASS。

- [ ] **Step 5: Commit**

```bash
git add index.html src/style.css
git commit -m "feat(landing): add 2:2:1 hero shell + brand copy + hud card (cn)"
```

---

## Task 9: 粒子灯泡 `scene/bulb.ts`（视觉门）

**Files:**
- Create: `src/scene/bulb.ts`
- Consume: `BULB_SCALE` from `./config`；平滑 bin `/models/dengpao_points_smooth.bin`（Task 6）。

**Interfaces:**
- Produces:
  - `interface Bulb { group: THREE.Group; setCoreColor(hex: string): void; surge(hex: string): void; update(t: number, mouse: {x:number;y:number}, hasPointer: boolean, camera: THREE.Camera): void }`
  - `async function createBulb(): Promise<Bulb>`（内部：加载 40000 点 ShaderMaterial 粒子 + 玻璃罩 + 底座 + 辉光 Sprite；整组 `group.scale.setScalar(BULB_SCALE)`）。着色器、玻璃/底座材质、鼠标流体排斥 uniform 全部移植自 yun `src/main.js`（vertex/fragment shader、`setupGlassCore`、animate 中的排斥场逻辑），改用平滑 bin、放大 `BULB_SCALE`。

- [ ] **Step 1: 端口实现（移植 yun 灯泡，换平滑 bin + 放大 + 变色接口）** — 见 Interfaces；`setCoreColor` 改 `pointLight`/辉光 Sprite/灯丝色，`surge` 复刻 yun `triggerCoreSurge` 的迸发。

- [ ] **Step 2: 类型 + lint** — Run: `npm run typecheck && npm run lint` → PASS（three 类型齐全）。

- [ ] **Step 3: 视觉门 — 灯泡截图核对**

临时在 `main.ts` 挂 `createBulb()` 到最小场景，Playwright 截图 `/tmp/landing-task9.png`（视口 1920×1080，等 3s），关闭并 ps 复核（W6）。
预期观察：① 灯泡+底座**明显放大**（占屏高 ≥55%）；② 剪影**圆润无面片棱角**（对比旧 `阶段2` 截图）；③ 青色生物发光核心 + 发光底座；④ 鼠标移入灯泡区粒子被推开成环、移出回流。

- [ ] **Step 4: Commit**

```bash
git add src/scene/bulb.ts src/main.ts
git commit -m "feat(landing): enlarged smoothed particle bulb with core-color api"
```

---

## Task 10: 轨道与卫星 `scene/orbits.ts` + `scene/satellites.ts`（视觉门）

**Files:**
- Create: `src/scene/orbits.ts`, `src/scene/satellites.ts`
- Consume: `ORBITS` from `./config`；`precessionAngle/satelliteAngle/evenAngles` from `./orbit-math`；`MODELS/orbitModels` from `../data/models`；logo PNG `/logos/<id>.png`（Task 7）。

**Interfaces:**
- Produces（`orbits.ts`）:
  - `interface OrbitRig { groups: THREE.Group[]; update(t: number, camera: THREE.Camera): void }` —— 内部持有两条轨道 group；`update` 中对每条 group 施加 `group.rotation.y = precessionAngle(cfg.precessDir, cfg.precessPeriod, t)`（**轨道整体进动**），并喂 yun 轨道着色器的 `uTime/uBulbView/uSatAngles`。
  - `function createOrbits(): OrbitRig`（移植 yun `setupOrbits` 的能量线双层 + 着色器；半径用 `ORBITS.*.radius`）。
- Produces（`satellites.ts`）:
  - `interface Satellite { group: THREE.Group; modelId: string }`
  - `async function createSatellites(rig: OrbitRig): Promise<Satellite[]>` —— 对 `orbitModels('intl')`/`orbitModels('domestic')` 各 12 家，用 `evenAngles(12, phase)` 均布，挂到对应轨道 group；卫星三层（品牌色光晕 + 玻璃环盘 + logo billboard）移植 yun `createSatellite3D`，`userData.modelId = m.id`，`glow/border` 用 `m.brandColor`。

- [ ] **Step 1: 端口实现** — 见 Interfaces。卫星 billboard、近大远小、彗尾 uSatAngles 与 `satelliteAngle` 公式对齐（保证尾迹跟随）。

- [ ] **Step 2: 类型 + lint** — Run: `npm run typecheck && npm run lint` → PASS。

- [ ] **Step 3: 视觉门 — 轨道/卫星截图 + 进动核对**

Playwright：截 t≈3s 与 t≈15s 两帧（`/tmp/landing-task10-a.png`、`-b.png`），关闭并 ps 复核（W6）。
预期观察：① **24 个 logo** 全部出现（国际 12 + 国产 12），logo 可辨；② 两条轨道倾角不同、转向相反；③ 对比两帧：**轨道椭圆朝向发生变化**（进动可见，非仅卫星位移）；④ 彗尾拖光跟随卫星；⑤ 远侧弧线不横穿灯泡（剪影遮罩生效）。

- [ ] **Step 4: Commit**

```bash
git add src/scene/orbits.ts src/scene/satellites.ts
git commit -m "feat(landing): dual orbits with precession + 24 satellites"
```

---

## Task 11: 场景装配与交互接线 `scene/scene.ts` + `interactions/raycast.ts` + `main.ts`（视觉门）

**Files:**
- Create: `src/scene/scene.ts`, `src/interactions/raycast.ts`
- Modify: `src/main.ts`
- Consume: `createBulb`(T9), `createOrbits/createSatellites`(T10), `renderHud/hudModelFor`(T4), `resolveHover/coreColorFor`(T5), `viewOffset/VIEW_OFFSET_X_RATIO/CAMERA_Z`(T2)。

**Interfaces:**
- Produces（`scene.ts`）: `async function mountScene(canvas: HTMLCanvasElement): Promise<{ onResize(): void }>` —— 建 scene/camera/renderer/composer(UnrealBloom+OutputPass，移植 yun `setupPostProcessing`)/controls；`camera.setViewOffset` 用 `viewOffset(innerWidth, VIEW_OFFSET_X_RATIO)` 把灯泡推到 x≈60%；animate 循环里依次 `bulb.update / orbitRig.update / satellites 位置` 后 `composer.render()`，再跑 hover 检测。
- Produces（`raycast.ts`）: `function makeRaycaster(camera, satellites): { hover(mouse): string|null; click(mouse): string|null }` —— 返回命中的 `modelId`。
- 接线：hover→`resolveHover`→变则 `renderHud(els, hudModelFor(id))`；click→`bulb.surge(coreColorFor(id))` + `bulb.setCoreColor(...)` + CSS 变量。

- [ ] **Step 1: 实现 `scene.ts`（装配 + animate + 相机偏移）** — 移植 yun `init/animate`，用上述模块替换内联逻辑。

- [ ] **Step 2: 实现 `raycast.ts` + `main.ts` 接线** — 取 HUD DOM 元素组 `HudEls`，绑定 mousemove/click，接 hover/变色。

- [ ] **Step 3: 类型 + lint** — Run: `npm run typecheck && npm run lint` → PASS。

- [ ] **Step 4: 视觉门 — 全页 + 交互核对**

Playwright（视口 1920×1080）：① 加载等 3s 截 `/tmp/landing-task11-idle.png`；② 用 `page.mouse.move` 悬停到某卫星屏幕坐标，截 `-hover.png`；③ `page.mouse.click` 该卫星，截 `-click.png`；关闭并 ps 复核（W6）。
预期观察：① 灯泡视觉中心在 **x≈60%**（落在中栏），左栏文案不被画布压盖；② 悬停卫星→右栏 HUD 切换为该供应商中文卡片、左边框变品牌色；③ 点击卫星→灯泡核心**渐变为品牌色**；④ 拖拽可环视；⑤ 控制台零报错。

- [ ] **Step 5: Commit**

```bash
git add src/scene/scene.ts src/interactions/raycast.ts src/main.ts
git commit -m "feat(landing): assemble scene, camera offset, hover/click wiring"
```

---

## Task 12: 移动端竖排 + WebGL 兜底 `fallback.ts` + 响应式 CSS（视觉门 + 单测）

**Files:**
- Create: `src/fallback.ts`
- Test: `src/fallback.test.ts`
- Modify: `src/style.css`（媒体查询）, `src/main.ts`（挂降级/兜底）, `index.html`（兜底海报节点）
- Consume: `breakpointFor/particleCount/bloomEnabled`(T3)。

**Interfaces:**
- Produces: `function webglAvailable(win?: { WebGLRenderingContext?: unknown }, doc?: Document): boolean`（可注入以便测试）；`function showPoster(root: HTMLElement, src: string): void`（隐藏画布、显示 `<img>` 海报 + 保留左栏 DOM）。

- [ ] **Step 1: 写失败测试（注入桩验证 guard 与海报）**

`src/fallback.test.ts`:
```ts
import { describe, it, expect } from 'vitest'
import { webglAvailable, showPoster } from './fallback'

describe('fallback', () => {
  it('reports false when WebGL is absent', () => {
    expect(webglAvailable({}, document)).toBe(false)
  })
  it('reports true when a context can be created (stubbed)', () => {
    const doc = { createElement: () => ({ getContext: () => ({}) }) } as unknown as Document
    expect(webglAvailable({ WebGLRenderingContext: function(){} }, doc)).toBe(true)
  })
  it('showPoster injects an img with the given src', () => {
    const root = document.createElement('div')
    showPoster(root, '/poster.png')
    const img = root.querySelector('img')
    expect(img?.getAttribute('src')).toBe('/poster.png')
  })
})
```

- [ ] **Step 2: 跑测试确认失败** — Run: `npm run test -- fallback` → FAIL。

- [ ] **Step 3: 写实现 `src/fallback.ts`**

```ts
export function webglAvailable(
  win: { WebGLRenderingContext?: unknown } = window,
  doc: Document = document,
): boolean {
  try {
    if (!win.WebGLRenderingContext) return false
    const c = doc.createElement('canvas')
    return !!(c.getContext('webgl') || c.getContext('experimental-webgl'))
  } catch { return false }
}

export function showPoster(root: HTMLElement, src: string): void {
  const canvas = root.querySelector('#webgl-canvas')
  if (canvas instanceof HTMLElement) canvas.style.display = 'none'
  const img = document.createElement('img')
  img.src = src
  img.alt = 'WeDream AI'
  img.className = 'poster'
  root.appendChild(img)
}
```

- [ ] **Step 4: 补响应式 CSS（竖排）+ main.ts 接线**

`src/style.css` 追加：
```css
.poster { position:fixed; inset:0; width:100%; height:100%; object-fit:cover; z-index:0; }
@media (max-width:1023px) {
  .hero { grid-template-columns:1fr; grid-auto-rows:auto; align-content:start; padding-top:12vh; gap:4vh; }
  .hero-stage { height:60vh; }
  .hero-right { justify-content:center; }
  .hud { width:min(92vw,360px); }
}
@media (max-width:767px) {
  #brand-title { font-size:34px; } .slogan { font-size:26px; }
  .metrics { flex-wrap:wrap; gap:10px 18px; }
}
```
`main.ts`：入口先 `const bp = breakpointFor(innerWidth)`；`if (!webglAvailable()) showPoster(document.body, '/poster.png')` 否则 `mountScene(canvas)`，粒子数/bloom 传 `particleCount(bp)/bloomEnabled(bp)`（`createBulb`/`setupPostProcessing` 接受参数——若 Task 9/11 未参数化，本任务补一个可选入参，默认桌面值，保持向后兼容）。

- [ ] **Step 5: 跑测试 + 视觉门（移动端 + 兜底）**

Run: `npm run test -- fallback && npm run typecheck && npm run lint` → PASS。
Playwright：① 视口 390×844 截 `/tmp/landing-task12-mobile.png`；② `http://localhost:5173/?nowebgl`（或桩掉 WebGL）截 `-poster.png`；关闭并 ps 复核（W6）。
预期观察：① 390 宽下三区竖排、**无横向滚动**、文案可读、点卫星可切 HUD；② 兜底态显示海报 + 左栏文案（无黑屏）。

> 海报 `public/poster.png`：在 Task 11 桌面全景态 Playwright 实截一张 1920×1080 存入（本步顺带完成并入库）。

- [ ] **Step 6: Commit**

```bash
git add src/fallback.ts src/fallback.test.ts src/style.css src/main.ts index.html public/poster.png
git commit -m "feat(landing): mobile vertical layout + webgl fallback poster with tests"
```

---

## Task 13: 集成、E2E 与部署接入（视觉门）

**Files:**
- Create: `tests/e2e.spec.ts`（Playwright test）, `playwright.config.ts`, `DEPLOY.md`
- Modify: `package.json`（加 `"e2e": "playwright test"`）

**Interfaces:**
- Consumes: 全部前序任务。
- Produces: 通过的生产构建 + E2E 冒烟 + 部署说明。

- [ ] **Step 1: 写 `playwright.config.ts` + E2E 冒烟**

`tests/e2e.spec.ts`:
```ts
import { test, expect } from '@playwright/test'

test('landing renders brand + 24 satellites + hud interaction', async ({ page }) => {
  const errors: string[] = []
  page.on('console', m => { if (m.type() === 'error') errors.push(m.text()) })
  await page.goto('/')
  await expect(page.locator('#brand-title')).toHaveText('WeDream AI')
  await expect(page.locator('#brand-slogan')).toHaveText('让灵感不再受限')
  await page.waitForTimeout(3000)              // 真实等待入场 + rAF（禁 virtual-time）
  await expect(page.locator('#hud-name')).toHaveText('WeDream 核心')
  await page.screenshot({ path: 'test-results/landing-full.png' })
  expect(errors, errors.join('\n')).toEqual([])
})
```
`playwright.config.ts`:
```ts
import { defineConfig } from '@playwright/test'
export default defineConfig({
  webServer: { command: 'npm run build && npm run preview', url: 'http://localhost:4173', reuseExistingServer: false },
  use: { baseURL: 'http://localhost:4173', viewport: { width: 1920, height: 1080 } },
})
```

- [ ] **Step 2: 生产构建（含类型门）**

Run: `npm run build`
Expected: `tsc --noEmit` 零错误 + `vite build` 成功，产出 `dist/`。

- [ ] **Step 3: 跑 E2E（用完即关，W6）**

Run: `npm run e2e`
Expected: PASS，`test-results/landing-full.png` 生成。
收尾：`pkill -f playwright; pkill -f 'vite preview'`；`ps aux | grep -iE 'playwright|vite|headless' | grep -v grep` 复核零残留。

- [ ] **Step 4: 全套门总跑**

Run: `npm run typecheck && npm run lint && npm run test && npm run build`
Expected: 四门全绿。

- [ ] **Step 5: 写 `DEPLOY.md`（服务器接入，W4——不在 Mac 执行）**

内容：① `npm run build` 产物 `dist/` rsync 到服务器 `/root/newapi-test/landing/`；② 宝塔 nginx 在 `*.wedreamhub.com` server 块加 `location /landing/ { alias /root/newapi-test/landing/; try_files $uri $uri/ /landing/index.html; }`（置于反代之前）；③ 后台「系统设置 → 首页内容」填 `https://www.wedreamhub.com/landing/` → 平台首页 iframe 嵌入，**顶部导航保持 newapi 原生**；④（可选，随平台构建）`web/default/src/features/home/index.tsx` iframe `sandbox` 追加 `allow-top-navigation-by-user-activation`，令「立即开始」同页跳转（不改则 CTA 用 `target="_blank"`）。

- [ ] **Step 6: Commit**

```bash
git add tests/e2e.spec.ts playwright.config.ts package.json DEPLOY.md
git commit -m "feat(landing): production build + e2e smoke + deploy notes"
```

---

## 自审（Self-Review）

**1. Spec 覆盖：** proposal 各节 → 任务映射（无缺口）
- §三布局 2:2:1 → T8 ｜ §四灯泡放大/圆润 → T6+T9 ｜ §五轨道进动+24 卫星 → T2+T10 ｜ §六交互联动（hover/点击变色） → T4+T5+T11 ｜ §七中文文案 → T1(HUD 文案)+T8(界面) ｜ §八移动端 → T3+T12 ｜ §九 Vite 工程/部署 → T0+T13 ｜ §十性能兜底 → T3+T12 ｜ §十一 DoD → T13 E2E + 各视觉门 ｜ 附录 A/B → T1 数据。全部有归属。
- 需求「右栏 yun 原样单卡 HUD」→ T4+T8+T11；「iframe 原生导航」→ T13 DEPLOY.md（本页不含导航）；「直接呈现（无点击点亮门控）」→ 计划全程未引入门控，一致。

**2. 占位符扫描：** 无 “TBD/TODO/后续补充/类似 Task N”。唯一「移植自 yun」处（T7 aces、T9 着色器、T10/T11 端口）均指向**具体已存在的源文件行号/函数名**并给出可测的纯函数边界与视觉门判据，非占位。T6 的 `LoopSubdivision` 明确标注为可选、给出无细分回退路径。

**3. 类型一致性：** 跨任务签名核对——`Model`/`Orbit`(T1) 被 T4/T5/T10 一致引用；`OrbitCfg.precessDir:1|-1`(T2) 与 `precessionAngle(dir:1|-1,…)`(T2)、T10 `group.rotation.y=precessionAngle(cfg.precessDir,…)` 一致；`HudEls`(T4) 被 T11 接线复用；`coreColorFor/resolveHover`(T5) 被 T11 调用名一致；`viewOffset/VIEW_OFFSET_X_RATIO`(T2) 被 T11 一致；`particleCount/bloomEnabled`(T3) 被 T12 一致；bin 文件名 `dengpao_points_smooth.bin`(T6) 与 T9 加载路径一致；logo 文件名 `<id>.png`(T7) 与 `models.ts.logo`(T1) 及 T10 加载一致。无漂移。

> 说明（诚实边界）：T9/T10/T11 为 three.js 渲染装配，**无单元测试**，以「类型门 + lint 门 + Playwright 视觉门」验收——着色器/粒子渲染的正确性只能视觉验证，强加单测为测试剧场；可单测的判定逻辑已前置抽到 T1–T5 纯模块并 100% 覆盖。此为本计划对「完整单元测试」在 WebGL 场景下的落地解释。
