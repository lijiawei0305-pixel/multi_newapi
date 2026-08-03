# T03 — `landing-mobile-css` 移动端样式表 + CSS 契约测试

> **Wave 2** ｜ 依赖：T01（`BREAKPOINTS` 常量） ｜ 被依赖：T06
> 设计契约：[`detailed-design.md`](../mobile-landing-detailed-design.md) §2.3、§4.3 ｜ CSS 正文：[`mobile-landing-proposal.md`](../mobile-landing-proposal.md) §9

---

## 目标

产出移动端样式表，并用**契约测试把硬约束 M1（桌面端零变化）变成 CI 可拦截的门禁**。

## 交付物

| 文件 | 说明 |
| --- | --- |
| `web/default/src/features/landing-react/landing-mobile-css.ts` | 🆕 约 215 行 |
| `web/default/src/features/landing-react/landing-mobile-css.test.ts` | 🆕 约 55 行 |

> ⚠️ 本任务只产出文件，**不接线**（`index.tsx` 的注入在 T06）。此阶段 `MOBILE_CSS` 是未被引用的导出，四道门不会报错（`knip` 不在门内）。

## 最小可执行任务（MET）

- [ ] **M1** 建 `landing-mobile-css.ts`，导出 `MOBILE_CSS: string`，文件头注释对齐 `landing-css.ts` 风格（中文说明 + 注入顺序 + 回滚方式，**不带 AGPL 版权头**，与同目录兄弟文件一致）
- [ ] **M2** 从 `./viewport-mode` import `BREAKPOINTS`，主断点与窄屏断点用**模板插值**写入媒体查询，杜绝字面量漂移
- [ ] **M3** 逐字落地 `mobile-landing-proposal.md` §9 的四个媒体查询块：

  | 块 | 条件 | 覆盖 |
  | --- | --- | --- |
  | B1 主块 | `max-width: ${BREAKPOINTS.mobile}px` | 顶栏 / Hero 竖排 / 文字 / 卡片 / CTA / HUD 抽屉 / FAB / 三屏收敛 / 滚动流畅度 |
  | B2 窄屏 | `max-width: ${BREAKPOINTS.narrow}px` | 字号再收一档，保 360px 单行 |
  | B3 矮屏 | `max-width: mobile` **and** `max-height: shortViewport` | 降低 3D 区高度占比 |
  | B4 触屏 | `hover: none` | 中和粘滞 hover 态 |

- [ ] **M4** 在 B1 块内**补入设计阶段新增的 2 条**（方案 §9 草稿里没有，见 detailed-design §2.3.4）：

```css
/* ① 抽屉升起时隐藏客服 FAB —— 两者都在屏底会重叠，且 FAB 紧邻 × 按钮易误点。
      .lp-fab 是 .hero-stage 的「前序兄弟」，纯 CSS 选不到 #hud.show，
      故由 onSelectChange 在 .wd-landing-root 上切 wd-hud-open 类（T05）。 */
.wd-landing-root.wd-hud-open .lp-fab {
  opacity: 0; pointer-events: none; transform: translateY(8px);
}
/* ② 抽屉本体需可交互（桌面端 #hud 是 pointer-events:none 的纯展示层） */
#hud { pointer-events: auto; }
```

- [ ] **M5** 落实**特异性陷阱**（detailed-design §6.2，实现时最容易踩）：

  | 既有规则 | 特异性 | 必须这样写 |
  | --- | --- | --- |
  | `.hero-right.card-open .hero-visual`（`landing-css.ts:289`） | `0,3,0` | 写足复合选择器，`.hero-visual` 覆盖不了 |
  | `.svc-flagship .svc-card-desc`（`sections-css.ts:57`） | `0,2,0` | 须同时列 `.svc-card-desc, .svc-flagship .svc-card-desc` |
  | `#sec-services` / `#sec-studio` | `1,0,0` | 须带 ID 写，类选择器覆盖不了 |

- [ ] **M6** 视口单位一律 `svh`，并在其前置一行 `vh` 作老浏览器回落（`min-height:100vh; min-height:100svh;`）
- [ ] **M7** 贴边元素叠加 `env(safe-area-inset-*, 0px)`
- [ ] **M8** 写 `landing-mobile-css.test.ts`，覆盖 ✅ 全部 6 条

## ✅ 验收标准

| # | 用例 | 断言 | 守住 |
| --- | --- | --- | --- |
| 1 ★ | **零裸规则** | 用正则剥掉所有 `@media(...){...}` 块后，剩余内容仅含空白与注释 | **M1 桌面端零变化** |
| 2 | 零 `!important` | `MOBILE_CSS.includes('!important') === false` | 覆盖靠源码顺序而非权重战争 |
| 3 | 断点单一真源 | 所有 `max-width:\s*(\d+)px` 的值 ∈ `{BREAKPOINTS.mobile, BREAKPOINTS.narrow}` | 断点不漂移 |
| 4 | 矮屏断点一致 | 所有 `max-height:\s*(\d+)px` === `BREAKPOINTS.shortViewport` | 同上 |
| 5 | 与全站断点一致 | `BREAKPOINTS.mobile === 768` | 与 `hooks/use-mobile.ts` 对齐 |
| 6 | 括号平衡 | `{` 与 `}` 计数相等 | 防模板字符串拼出坏 CSS |

四道门全绿。

## 🚫 禁止

- 任何规则写在媒体查询**之外**（用例 1 会直接拦下）
- 使用 `!important`
- 断点写字面量而不走 `BREAKPOINTS`
- 改动 `landing-css.ts` / `sections-css.ts`（那是 T04 的范围）
