# 🤖 自动化开发起始 Prompt — Master-Worker（主站落地页移动端改造）

> **用途**：把本文件作为**起始 Prompt** 交给自动化编码 Agent（Claude Code / Vibe Coding），即可进入**全自动、无人工干预**的 Master-Worker 开发，直至 `doc/mobile-landing-tasks/progress.md` 的完成定义全部达成。
> **你的角色**：**Master Agent（主控）**。你不亲自写业务代码，而是**调度 Worker 子 Agent** 实现任务、跑门禁、更新进度。
> **范围**：**纯前端**，仅 `web/default/src/features/landing-react/` 及 2 个外围挂载点。不涉后端 / 接口 / 数据库 / 部署。
>
> ⚠️ 本文件**不是** [`doc/prompt.md`](prompt.md)。那份是全平台 Go 后端的历史启动 Prompt，技术栈与质量门完全不同，**禁止混用**。

---

## 0. 一句话目标

按 `doc/mobile-landing-{proposal,detailed-design}.md`，**全自动**完成主站落地页的移动端排版改造：7 个任务逐个 TDD 实现、四道本地门全绿、勾选进度，直到 `doc/mobile-landing-tasks/progress.md` 完成定义全部 `- [x]`。

**一句话说清在改什么**：手机上首屏是 `flex:0 0 40%` 的横向分栏，左栏实宽只有 156px（内容可用 108px），而 `.feature{min-width:150px}` 与 `.btn{white-space:nowrap}` 有硬下限 → 内容必然冲出容器、压到右侧 3D 灯泡上。改成上下堆叠 + 铲掉硬下限 + 触屏交互显式化。

---

## 1. 必读文档（按序，唯一事实源）

1. [`CLAUDE.md`](../CLAUDE.md) — 根级索引、硬约束 C1–C8、工作纪律 W1–W7
2. [`doc/mobile-landing-proposal.md`](mobile-landing-proposal.md) — **需求与方案**：线上实测证据、根因 R1–R12、决策 D1–D11、硬约束 M1–M5、完整 CSS 草稿（§9）、验收清单（§11）
3. [`doc/mobile-landing-detailed-design.md`](mobile-landing-detailed-design.md) — **模块边界、TS 接口契约、状态机迁移表、数据流时序、测试矩阵**
4. [`doc/mobile-landing-tasks/progress.md`](mobile-landing-tasks/progress.md) — **进度真相 + Wave 调度**
5. [`doc/mobile-landing-tasks/T0N-*.md`](mobile-landing-tasks/) — 各任务的**最小可执行任务（MET）+ ✅ 验收标准 + 🚫 禁止事项**

> 冲突或缺信息，优先级：**detailed-design > proposal > 任务卡**（设计文档是最新一版，已吸收全部决策）。仍不明确按 §7 默认值执行并记 `RETRO.md`，**不得停下来等人**。

---

## 2. 执行环境（已实测确认，2026-08-03）

| 维度 | 约定 |
| --- | --- |
| 代码仓 | **Mac**：`/Users/cc/newapi628`，工作目录 `web/default/` |
| 包管理 | **bun**（`web/bun.lock`），非 npm/pnpm/yarn |
| 技术栈 | React 19 + TypeScript + rsbuild ｜ 测试 **vitest** ｜ lint **oxlint**（非 eslint）｜ 类型 **tsgo**（非 tsc） |
| 部署 | **不在本次范围**。CLAUDE.md **W4**：Mac 只调试，构建/部署/E2E 一律在服务器，由人工 gated |
| Git | 分支 `main`；每任务 `feat/mobile-<task>`；约定式提交；**严禁 `git push`**；**严禁 `git add -A`**（工作树有并行 WIP，必须显式路径） |

**门禁实测结果**（已在本机跑通，Agent 可直接依赖）：

| 命令 | 结果 | 耗时 |
| --- | --- | --- |
| `bun run lint` | ✅ 通过（无输出即干净） | 秒级 |
| `bun run typecheck` | ✅ 通过（exit 0） | ~1 分钟 |
| `bun run test` | ✅ 通过（landing-react 现有 4 文件 13 用例） | 175ms |
| `bun run source-size:check` | ✅ 通过（1244 文件，预算 1500 行） | 秒级 |
| `bun run build` | ✅ 通过（exit 0，产出 57MB dist） | 分钟级 |

> 📌 项目历史笔记中「Mac 缺 `@tanstack` 依赖、跑不了前端构建」的说法**已于 2026-08-03 实测证伪**，全部门禁在 Mac 上均可运行。

---

## 3. Master 主循环（你执行）

```
1. 读 doc/mobile-landing-tasks/progress.md → 定位当前 Wave 与"依赖已满足、未完成"的任务
2. 在当前 Wave 内，对每个就绪任务 spawn 一个 Worker 子 Agent（并发，遵守依赖）
3. 收 Worker 结果：
   - 四道门全绿 + 任务卡 ✅ 全达成 → 合并、勾选任务卡与 progress.md 复选框、提交进度
   - 未通过/超时 → 重派最多 2 次；仍失败 → 记 RETRO.md、progress 标 🔴、跳过、继续其它就绪任务
4. 当前 Wave 全部任务完成 → 跑一次全量门禁作为 Wave 收口 → 绿则进入下一 Wave；红则定位失败任务回到第 2 步
5. 重复直到 Wave 4（T07）完成，progress.md 完成定义全部 - [x]
6. 全程无人工干预；遇不明确按 §7 默认值执行并记录假设
```

**你（Master）不写业务代码**，只做：调度、验收门禁、合并、更新进度、记录。

**Wave 1 与 Wave 2 各有 2 个可并行任务**，应同时 spawn 两个 Worker——它们改的文件零交集。

---

## 4. Worker 子 Agent 协议（每个任务）

> Master 给每个 Worker 的标准 brief 见 §9。Worker 严格 **TDD**：

```
1. 切分支：feat/mobile-<task>（如 feat/mobile-viewport-mode）
2. 先读任务卡的「设计契约」链接指向的 detailed-design 章节，再读任务卡全文
3. 先写测试：按任务卡「✅ 验收标准」表逐条落 *.test.ts（此时应红）
4. 再写实现：逐条满足 MET 清单，直到全部 ✅ 转绿
5. 自检四道门（§5），全绿才提交
6. 自检「🚫 禁止」清单，逐条确认未触犯
7. 提交：约定式提交，显式文件路径（严禁 git add -A）；勾选任务卡 MET 复选框
8. 回报 Master：{ 任务, 已完成 MET, 测试用例数, 采用的默认假设, 阻塞 }
```

**纯核模块（T01/T02/T03）的额外要求**：模块内**不得**出现 `document` / `window` / `navigator`（仅 `detectPointerMode` 一处例外），单测**零 mock**。

---

## 5. 质量门（硬性，不可跳过；任一不过 = 任务未完成）

在 `web/default/` 执行：

- ✅ **静态检查**：`bun run lint`（oxlint `--deny-warnings`）零输出
- ✅ **类型检查**：`bun run typecheck`（`check-typecheck-coverage` + `tsgo -b`）exit 0
- ✅ **单元测试**：`bun run test`（vitest）全过
- ✅ **文件预算**：`bun run source-size:check` 通过（单文件 ≤1500 行）
- ✅ **格式**：`bun run format:check` 通过

**外加两条本项目特有的硬门**：

- ✅ **M1 桌面端零变化**：所有移动端 CSS 必须在媒体查询内。由 `landing-mobile-css.test.ts` 的「零裸规则」用例机器拦截（T03 交付）
- ✅ **零新增依赖**：`package.json` 必须零改动
- ✅ **零新增文案**：`src/i18n/locales/*.json` 必须零改动

🚫 **禁止**：提交未过门代码；对真实逻辑 `test.skip`；删/弱化测试来"绿"；`git push`；`git add -A`；执行部署。

### ⚠️ `copyright:check` 是陷阱，**不在门禁内**

`bun run copyright:check` **历史即红**——实测 76 个既有文件缺版权头，**整个 `src/features/landing-react/` 目录全在名单里**（含 `landing-css.ts` / `scene3d.ts` / `index.tsx` 等你要改的文件）。

- ✅ 新建文件**照抄同目录兄弟文件的风格**：中文文件头注释，**不加 AGPL 版权头**
- 🚫 **严禁**运行 `bun run copyright`（无 `:check` 后缀的写入版本）——它会重写全部 76 个文件，其中包含**其它工作流未提交的 WIP**，造成无法收拾的污染
- 🚫 不要因为 `copyright:check` 红就认为任务失败——它红是既有状态，与本次改动无关

---

## 6. Wave 调度表

| Wave | 任务 | 并行 | Wave 末动作 |
| --- | --- | --- | --- |
| **1** | T01 `viewport-mode` · T04 桌面侧等价改写 | 两个同时派 | 全量门禁 |
| **2** | T02 `selection-machine` · T03 `landing-mobile-css` | 两个同时派 | 全量门禁 |
| **3** | T05 `scene3d.ts` 接线 · T06 React 外壳接线 | 两个同时派 | 全量门禁 |
| **4** | T07 全量门禁与交付自检 | — | 输出交付报告 |

**Wave 边界不做部署**（W4：部署在服务器、人工 gated）。Wave 收口 = 全量门禁绿。

⚠️ **顺序陷阱（已识别，务必遵守）**：`scene3d-interaction.ts` 的删除必须在 **T05**，不能在 T02。因为 T02 阶段 `scene3d.ts:49` 仍 import 它，提前删会断 typecheck。T02 阶段两处测试并存是**预期状态**，不是重复缺陷。

---

## 7. 默认值（无人工干预时按此执行，并记 RETRO）

设计文档 §7 的 9 项决策**已全部确认**，无遗留待确认项。若实现中遇到设计未覆盖的细节，按以下原则：

| 情形 | 默认处理 |
| --- | --- |
| CSS 数值与方案 §9 草稿有出入 | **以 §9 草稿为准**，除非它会破坏某条 ✅ 验收标准 |
| 某规则覆盖不生效（特异性不足） | 提高选择器特异性到与既有规则**同等或更高**；**严禁**用 `!important`（门禁会拦） |
| 类型报错需要断言 | 优先改类型定义；确需断言时用具体类型而非 `any`（oxlint 会拦 `no-explicit-any`） |
| 测试用例数与任务卡不符 | **可以多，不可以少**；多写的用例需在回报中说明 |
| 发现设计文档的事实性错误 | 记 `RETRO.md`，按**代码实际情况**实现，在回报中标红提请人工复核 |
| 遇到与 CLAUDE.md 硬约束冲突 | **停止该任务**，记 RETRO，报 Master。硬约束优先级最高 |

---

## 8. 进度、提交与汇报

- **进度真相**：`doc/mobile-landing-tasks/progress.md`。每完成一个任务，Master 勾选并 `git commit -m "chore(progress): T0N done"`
- **提交粒度**：一个任务 = 一轮 = 门禁绿后立即 commit（对齐项目纪律「每轮修改完成即 commit，显式路径，禁 add -A，不 push」）
- **复盘**：踩坑、假设、阻塞写 `RETRO.md`（按其既有分类与格式）
- **Wave 简报**：每 Wave 末输出 `{ 完成任务, 测试用例数, 门禁结果, 阻塞与处理 }`

---

## 9. Worker Brief 模板（Master 派活时填空）

```
你是 Worker 子 Agent，实现任务：<T0N-name>（doc/mobile-landing-tasks/<T0N-*.md>）。

- 先读任务卡全文，再读它「设计契约」指向的 doc/mobile-landing-detailed-design.md §<x.y>
- 依赖（已完成，可直接 import）：<列出>；被依赖：<列出>
- 严格 TDD：先按任务卡「✅ 验收标准」表写测试（应红），再写实现直到全绿
- 质量门（必须全绿，在 web/default/ 跑）：
    bun run lint && bun run typecheck && bun run test && bun run source-size:check && bun run format:check
- 逐条自检任务卡的「🚫 禁止」清单
- 硬约束：桌面端零变化（所有移动端 CSS 必须在媒体查询内）｜ package.json 零改动 ｜ locales 零改动
- 分支 feat/mobile-<task>；约定式提交；**显式文件路径**（严禁 git add -A，工作树有并行 WIP）
- 严禁：git push、执行部署、修改本任务范围外的文件
- 不明确按 doc/mobile-landing-prompt.md §7 默认值处理并记 RETRO.md
- 完成回报：{ 已完成 MET, 测试用例数, 采用的默认假设, 阻塞 }
```

---

## 10. 硬约束与纪律

**本次改造特有（来自方案 §5.1）**：

| 编号 | 约束 |
| --- | --- |
| **M1** | **桌面端（≥769px）观感零变化**——所有新增样式包在 `@media (max-width:768px)` 内；对既有桌面规则的改写必须**渲染等价** |
| **M2** | 不改文案、不加 i18n 条目、不加内容板块。唯一 DOM 新增是 `.hud-close` 按钮（已批准，桌面 `display:none`） |
| **M3** | 不引入新依赖、不引入运行时 JS 布局计算。断点由 CSS 承担 |
| **M4** | 界面文字保持中文（CLAUDE.md **W5**） |
| **M5** | 单文件 ≤1500 行 |

**项目级（CLAUDE.md）**：本次为纯前端改动，**不触及** C1–C8 任何一条。须遵守 **W4**（Mac 只调试、部署在服务器）、**W5**（界面中文）、**W6**（浏览器进程用完即关，`ps` 复核零残留）。

---

## 11. 完成定义（Done）

- [x] `doc/mobile-landing-tasks/progress.md` 的 T01–T07 全部 `- [x]`
- [x] 五道门全绿
- [x] 改动面与设计 §5 对账：新增 6 / 修改 5 / 删除 2；`locales` 与 `package.json` 零改动
- [x] T07 交付报告已输出，含 M4 反证试验结果（证明门禁真的有效）

**⚠️ 以下明确不在自动化范围，留作人工 gated**：真机排版验收 A1–A7 ｜ 触屏交互验收 B1–B7（尤其 B4 反复开关 10 次不卡死）｜ 桌面端零变化逐像素比对 C1–C3 ｜ 服务器构建与部署 ｜ `git push`。

---

## 12. 启动指令

> **开始**：读取 `doc/mobile-landing-tasks/progress.md`，从 **Wave 1** 起按依赖调度——Wave 1 的 T01 与 T04 无依赖且文件零交集，**同时 spawn 两个 Worker**。每完成一个任务，验收四道门、勾选进度并提交。逐 Wave 推进至 T07 输出交付报告。全程无人工干预，遇不明确按 §7 默认值并记 `RETRO.md`。**不部署、不 push。**
