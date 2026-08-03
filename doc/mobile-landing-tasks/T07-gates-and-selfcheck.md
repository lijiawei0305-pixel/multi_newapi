# T07 — 全量门禁与交付自检

> **Wave 4** ｜ 依赖：T01–T06 全部完成 ｜ 被依赖：无（终点）
> 设计契约：[`detailed-design.md`](../mobile-landing-detailed-design.md) §6.1 ｜ 验收清单：[`mobile-landing-proposal.md`](../mobile-landing-proposal.md) §11

---

## 目标

在全部代码落地后跑一次完整门禁，并做**机器可判定**的交付自检。真机截图验收（A1–A7 / B1–B7 / C1–C3）**留作人工 gated 步骤**，不在本任务内。

## 最小可执行任务（MET）

- [ ] **M1** 在 `web/default/` 跑全量门禁，逐条记录输出：

```bash
bun run lint              # oxlint --deny-warnings
bun run typecheck         # check-typecheck-coverage + tsgo -b
bun run test              # vitest
bun run source-size:check # 1500 行预算
bun run format:check
```

- [ ] **M2** 新增测试全部被执行：`bun run test` 输出中应含
  `viewport-mode.test.ts` / `selection-machine.test.ts` / `landing-mobile-css.test.ts` 三个新文件，
  且**不含** `scene3d-interaction.test.ts`（已在 T05 删除）
- [ ] **M3** 死引用自检：

```bash
git grep -n "nextSelection"          src/   # 期望零命中
git grep -n "scene3d-interaction"    src/   # 期望零命中
git grep -n "borderLeftColor"        src/features/landing-react/   # 期望零命中
```

- [ ] **M4** **M1 硬约束机器自检**（本任务最重要的一条）：确认 `landing-mobile-css.test.ts` 的「零裸规则」用例确实在跑且为绿。
  手工反证一次：临时在 `MOBILE_CSS` 的媒体查询**之外**插一条 `.foo{color:red}` → `bun run test` 应**变红**；确认后撤销。
  **这一步验证的是「门禁真的有效」，而不是「代码碰巧对」。**
- [ ] **M5** 改动面盘点，与设计 §5 对账：

  | 类别 | 期望 | 实际 |
  | --- | --- | --- |
  | 新增文件 | 6 | |
  | 修改文件 | 5 | |
  | 删除文件 | 2 | |
  | `locales/*.json` 改动 | 0 | |
  | 新增运行时依赖 | 0（`package.json` 零改动） | |

- [ ] **M6** 输出交付报告（见下方模板）

## ✅ 验收标准

- [ ] 五条命令全部退出码 0
- [ ] M3 的三条 `git grep` 均零命中
- [ ] M4 的反证试验确实让测试变红（证明门禁有效），且已撤销
- [ ] M5 表格与设计 §5 完全对账一致
- [ ] `git status` 中无意外文件（尤其 `dist/`、`node_modules/`、日志）

## 交付报告模板

```
## 移动端落地页改造 — 交付报告

### 门禁
| 门 | 结果 | 说明 |
| lint | ✅/❌ | |
| typecheck | ✅/❌ | |
| test | ✅/❌ | N 个文件 M 个用例 |
| source-size | ✅/❌ | |
| format:check | ✅/❌ | |

### 改动面
新增 N / 修改 N / 删除 N；locales 改动 0；package.json 改动 0

### M1（桌面端零变化）自检
- 零裸规则用例：✅ 绿
- 反证试验：插入裸规则后测试 ❌ 变红 → 已撤销 → 恢复 ✅ 绿
- landing-css.ts 改动：2 处，均渲染等价（--hud-accent 回退同色 / .hud-close 桌面 display:none）

### 采用的默认假设
（列出 prompt §7 中实际用到的默认值）

### 阻塞与遗留
（无则写"无"）

### ⚠️ 仍需人工验收（不在自动化范围）
- 真机排版 A1–A7（390×844 / 360×640 / 320×568）
- 触屏交互 B1–B7（尤其 B4：反复开关 10 次不卡死）
- 桌面端零变化 C1–C3（1440×900 逐像素比对）
- 服务器构建与部署（W4：Mac 只调试，部署在服务器）
```

## 🚫 禁止

- 为了让门变绿而删除/弱化任何测试
- 对真实逻辑写 `it.skip` / `test.skip`
- 提交 `dist/` 或构建产物
- 自行执行部署或 `git push`（W4 + 项目纪律：部署与推送均需人工确认）
