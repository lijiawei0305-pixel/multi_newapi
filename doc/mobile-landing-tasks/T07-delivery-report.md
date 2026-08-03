## 移动端落地页改造 — 交付报告

> 任务 T07 · 日期 2026-08-03 · Master Agent 收口

### 门禁

| 门 | 结果 | 说明 |
| --- | --- | --- |
| lint | ✅ | oxlint `--deny-warnings` 0 warnings / 0 errors |
| typecheck | ✅ | check-typecheck-coverage + tsgo -b |
| test | ✅ | 31 files / **196** tests（viewport-mode 9 + selection-machine 14 + landing-mobile-css 6；已删 scene3d-interaction 3） |
| source-size | ✅ | 1248 files，预算 1500 行 |
| format:check | ✅ | oxfmt check 通过 |

### 改动面（对账设计 §5）

| 类别 | 期望 | 实际 |
| --- | --- | --- |
| 新增文件 | 6 | 6：`viewport-mode.ts/.test.ts`、`selection-machine.ts/.test.ts`、`landing-mobile-css.ts/.test.ts` |
| 修改文件 | 5 | 5：`landing-css.ts`、`sections-css.ts`、`scene3d.ts`、`landing-react/index.tsx`、`features/home/index.tsx` |
| 删除文件 | 2 | 2：`scene3d-interaction.ts`、`scene3d-interaction.test.ts` |
| `locales/*.json` 改动 | 0 | 0 |
| 新增运行时依赖 | 0 | 0（`package.json` 零改动） |

### 死引用自检（M3）

| 检索 | 结果 |
| --- | --- |
| `nextSelection` in `src/` | 零命中 |
| `scene3d-interaction` in `src/` | 零命中 |
| `borderLeftColor` in `landing-react/` | 零命中 |

### M1（桌面端零变化）自检

- 零裸规则用例：✅ 绿（`landing-mobile-css.test.ts`）
- **反证试验（M4）**：在 `MOBILE_CSS` 媒体查询外插入 `.foo{color:red}` → 零裸规则用例 ❌ 变红（`expected '.foo{color:red}' to be ''`）→ 已撤销 → 恢复 ✅ 绿
- 门禁结论：**机器门真的有效**，不是「代码碰巧对」
- `landing-css.ts` 改动：2 处，均渲染等价（`--hud-accent` 回退 `#00f0ff` / `.hud-close` 桌面 `display:none`）
- `sections-css.ts`：仅在既有 `max-width:560px` 块内补 `grid-auto-rows:auto`

### 采用的默认假设

1. T03 相对 proposal §9：为部分仅写 `svh` 的属性补前置 `vh` 回落（对齐任务卡 M6）
2. T02 hover 迟滞 helper 命名为 `hoverSelect`（避免 `git grep nextSelection` 假阳性，语义与改造前一致）
3. M2 关闭按钮用既有 i18n key `t('Close')`，不新增 locale
4. 并行 Worker 共享工作树、不切分支；由 Master 门禁验收后显式路径 commit

### 阻塞与遗留

无。

### ⚠️ 仍需人工验收（不在自动化范围）

- 真机排版 A1–A7（390×844 / 360×640 / 320×568）
- 触屏交互 B1–B7（尤其 B4：反复开关 10 次不卡死）
- 桌面端零变化 C1–C3（1440×900 逐像素比对）
- 服务器构建与部署（W4：Mac 只调试，部署在服务器）
- `git push`（需人工明示）

### Wave 简报汇总

| Wave | 任务 | 测试增量 | 门禁 |
| --- | --- | --- | --- |
| 1 | T01 + T04 | +9（viewport） | ✅ |
| 2 | T02 + T03 | +14 +6 | ✅ |
| 3 | T05 + T06 | −3（删旧交互测）→ 196 | ✅ |
| 4 | T07 | 全量复验 + M4 反证 | ✅ |
