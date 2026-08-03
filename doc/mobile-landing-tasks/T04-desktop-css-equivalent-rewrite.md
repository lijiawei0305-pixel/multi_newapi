# T04 — 桌面侧等价改写（`landing-css.ts` + `sections-css.ts`）

> **Wave 1** ｜ 依赖：**无** ｜ 被依赖：T06
> 设计契约：[`detailed-design.md`](../mobile-landing-detailed-design.md) §8.2、§8.5、§7.9

---

## 目标

对两个既有样式文件做**共 3 处**改动。前 2 处是**渲染等价改写**（桌面端逐像素不变），第 3 处只在 ≤560px 生效。

> 本任务**可与 T01 并行**，且**单独落地是安全的**：`--hud-accent` 未定义时回退值 `#00f0ff` 与原字面量同色；此时 `scene3d.ts` 仍在写 inline `borderLeftColor`，inline 样式优先级更高 → 选中态配色照常工作。T05 再把 JS 切到 `setProperty`。

## 交付物

| 文件 | 改动 |
| --- | --- |
| `web/default/src/features/landing-react/landing-css.ts` | ✏️ 2 处 |
| `web/default/src/features/landing-react/sections-css.ts` | ✏️ 1 处 |

## 最小可执行任务（MET）

- [x] **M1** `landing-css.ts` 第 254 行附近，`#hud` 规则块内，强调色改用 CSS 变量：

```diff
-    border:1px solid rgba(120,180,255,.22); border-left:3px solid #00f0ff;
+    border:1px solid rgba(120,180,255,.22); border-left:3px solid var(--hud-accent,#00f0ff);
```

  **为什么必须这样改**：抽屉的强调边在**顶部**。若 T05 让 JS 直接写 `borderTopColor`，桌面端的顶边框（`border:1px solid rgba(...)` 简写的一部分）也会被染色 → 违反 M1。改用变量后，同一把变量可分别喂给桌面的 `border-left` 与手机的 `border-top`。回退值与原字面量同色 → **桌面渲染逐像素不变**。

- [x] **M2** `landing-css.ts` 的 `#hud` 规则块之后，新增一行，让 T06 要加的关闭按钮在桌面端完全不渲染：

```diff
+  .hud-close { display:none; }
```

- [x] **M3** `sections-css.ts` 既有的 `@media (max-width:560px)` 块内补一行，修掉 bento 卡片大片空白：

```diff
   @media (max-width:560px){
     .svc-bento { grid-template-columns:1fr; }
+    .svc-bento { grid-auto-rows:auto; }
     .svc-card,.svc-wide,.svc-flagship { grid-column:span 1; }
```

  **根因（R6）**：`.svc-bento{grid-auto-rows:1fr}`（`sections-css.ts:37`）在多列时用于对齐卡片高度（合理），但**单列**下它把每一行都拉成「最高行」的高度——线上实测 5 张卡片**全部 362px**，最矮的内容仅约 110px，每张卡中间空出约 250px。
  断点用 **560**（对齐既有 bento 单列断点）而非 768：561–768px 仍是两列，那里保留等高是对的。

## ✅ 验收标准

- [x] 四道门全绿
- [x] `git diff` 自检：本任务对 `landing-css.ts` 的改动**只有** M1、M2 两处；对 `sections-css.ts` **只有** M3 一处
- [x] M1 改动后，`#00f0ff` 仍是 `var()` 的回退值（**不得**改成别的颜色或删掉回退）
- [x] M3 的新规则确实落在 `max-width:560px` 块内（**不是** 768）

## 🚫 禁止

- 在这两个文件里新增任何**移动端**规则——移动端规则一律进 `landing-mobile-css.ts`（T03）
- 改动 `#hud` 的其余任何属性（位置、尺寸、`pointer-events` 等都归 T03 的移动端块）
- 顺手"优化"这两个文件里的其它样式
