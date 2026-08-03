# 📋 进度看板 — 主站落地页移动端改造

> **依据**：[`mobile-landing-proposal.md`](../mobile-landing-proposal.md)（需求 v1.0） ｜ [`mobile-landing-detailed-design.md`](../mobile-landing-detailed-design.md)（设计 v1.0）
> **起始 Prompt**：[`mobile-landing-prompt.md`](../mobile-landing-prompt.md)
> **本文件是进度唯一真相**。Master Agent 每完成一个任务即勾选并提交。
> **范围**：仅 `web/default/src/features/landing-react/` 及 2 个外围挂载点。纯前端，不涉后端/接口/数据库。

---

## Wave 调度表

| Wave | 任务 | 依赖 | 可并行 | 状态 |
| --- | --- | --- | --- | --- |
| **1** | T01 `viewport-mode` | 无 | ✅ 与 T04 | ✅ |
| **1** | T04 桌面侧等价改写 | 无 | ✅ 与 T01 | ✅ |
| **2** | T02 `selection-machine` | T01（仅类型） | ✅ 与 T03 | ✅ |
| **2** | T03 `landing-mobile-css` | T01（常量） | ✅ 与 T02 | ✅ |
| **3** | T05 `scene3d.ts` 宿主接线 | T01, T02 | ✅ 与 T06 | ✅ |
| **3** | T06 React 外壳接线 | T03, T04 | ✅ 与 T05 | ✅ |
| **4** | T07 全量门禁与自检 | T01–T06 | — | ✅ |

> Wave 1、2 的四个任务**全部是零依赖或仅取常量的纯模块**，落地后即可独立全绿——这是模块划分的收益兑现处。

---

## 任务清单

### Wave 1 — 独立基座（可并行）

- [x] **T01** [`viewport-mode` 视口断点与指针能力](T01-viewport-mode.md)
  - 交付：`viewport-mode.ts` + `viewport-mode.test.ts`（9 条用例）
  - 关键：`BREAKPOINTS` 单一真源；`pointerModeFromEvent` 是触屏笔电缺陷的回归护栏
- [x] **T04** [桌面侧等价改写](T04-desktop-css-equivalent-rewrite.md)
  - 交付：`landing-css.ts`（2 处）+ `sections-css.ts`（1 处）
  - 关键：两处必须**渲染等价**；bento 等高修复用 560 断点而非 768

### Wave 2 — 纯核与样式（可并行）

- [x] **T02** [`selection-machine` 选中状态机](T02-selection-machine.md)
  - 交付：`selection-machine.ts` + `selection-machine.test.ts`（14 条用例）
  - 关键：**根治 R7**；迁移表必须完备；**本任务不删旧文件**（会断 typecheck）
- [x] **T03** [`landing-mobile-css` 移动端样式表](T03-landing-mobile-css.md)
  - 交付：`landing-mobile-css.ts` + `landing-mobile-css.test.ts`（6 条用例）
  - 关键：「零裸规则」用例把硬约束 M1 变成 CI 门禁

### Wave 3 — 接线（可并行）

- [x] **T05** [`scene3d.ts` 宿主接线](T05-scene3d-host-wiring.md)
  - 交付：`scene3d.ts` 9 处；删除 `scene3d-interaction.ts` 及其测试
  - 关键：**新增监听器只许 1 个**；芯片无条件双绑、不写条件分支
- [x] **T06** [React 外壳接线](T06-react-shell-wiring.md)
  - 交付：`index.tsx` 2 处 + `features/home/index.tsx` 1 处
  - 关键：唯一 DOM 新增（关闭按钮）；`MOBILE_CSS` 必须拼在最后

### Wave 4 — 收口

- [x] **T07** [全量门禁与交付自检](T07-gates-and-selfcheck.md)
  - 关键：M4 反证试验——证明「门禁真的有效」而非「代码碰巧对」
  - 交付报告：[`T07-delivery-report.md`](T07-delivery-report.md)

---

## 完成定义（本阶段 Done）

- [x] T01–T07 全部 `- [x]`
- [x] 五道门全绿：`lint` / `typecheck` / `test` / `source-size:check` / `format:check`
- [x] 改动面与设计 §5 对账一致：新增 6 / 修改 5 / 删除 2；`locales` 与 `package.json` 零改动
- [x] 交付报告已输出（T07 模板）

## ⚠️ 明确不在自动化范围（人工 gated）

| 项 | 原因 |
| --- | --- |
| 真机排版验收 A1–A7 | 需真实设备/DevTools 目视判定 |
| 触屏交互验收 B1–B7 | 尤其 B4「反复开关 10 次不卡死」需真机触摸 |
| 桌面端零变化 C1–C3 | 1440×900 逐像素比对需人工确认基线 |
| 服务器构建与部署 | CLAUDE.md **W4**：Mac 只调试，构建/部署/E2E 一律在服务器 |
| `git push` | 项目纪律：推送需人工明示 |

---

## 变更记录

| 日期 | 说明 |
| --- | --- |
| 2026-08-03 | 初始化。按 detailed-design §7 的实施顺序切分为 7 个任务、4 个 Wave。 |
| 2026-08-03 | Wave 1 完成：T01 + T04。五道门全绿（lint/typecheck/test 179/source-size/format）。 |
| 2026-08-03 | Wave 2 完成：T02 + T03。五道门全绿（test 199，含 selection 14 + mobile-css 6）。 |
| 2026-08-03 | Wave 3 完成：T05 + T06。删除 scene3d-interaction；test 196（-3 旧交互测）；死引用 grep 清。 |
| 2026-08-03 | Wave 4 / T07 完成。五道门绿；M4 反证（裸规则→红→撤销→绿）；交付报告已输出。**阶段 Done。** |
