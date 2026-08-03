# T06 — React 外壳接线（`index.tsx` + `features/home/index.tsx`）

> **Wave 3** ｜ 依赖：T03（`MOBILE_CSS`）、T04（`.hud-close{display:none}`） ｜ 被依赖：T07
> 设计契约：[`detailed-design.md`](../mobile-landing-detailed-design.md) §2.5、§8.3

---

## 目标

注入移动端样式表，并加上本次改造**唯一的 DOM 新增**——抽屉关闭按钮。

## 交付物

| 文件 | 改动 |
| --- | --- |
| `web/default/src/features/landing-react/index.tsx` | ✏️ 2 处 |
| `web/default/src/features/home/index.tsx` | ✏️ 1 处 |

## 最小可执行任务（MET）

- [ ] **M1** `index.tsx` 注入移动端样式（**必须拼在最后**，靠源码顺序覆盖）：

```diff
+import { MOBILE_CSS } from './landing-mobile-css'
-      <style>{LANDING_CSS + SECTIONS_CSS}</style>
+      <style>{LANDING_CSS + SECTIONS_CSS + MOBILE_CSS}</style>
```

- [ ] **M2** `index.tsx` 的 `<aside id='hud'>` 内（`:279` 附近）加关闭按钮，**放在第一个子元素位置**：

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

  - `t('Close')` 是 New API 官方 locale **既有 key**，**不得新增 i18n 条目**（约束 M2/M4）
  - 用原生 `<button type="button">`：天然可聚焦、可键盘触发
  - `×` 用字符而非 SVG，与同文件既有的 `.lp-kefu-close` 写法一致
  - **不要**引入 state / ref / 新 hook —— 这是一行无状态的事件派发

- [ ] **M3** `features/home/index.tsx`（`:54`）给落地页专属的 Footer 包裹层加下内边距，让页脚内容滚得过客服 FAB：

```diff
-      <div className='dark relative z-20 bg-[#061127]'>
+      <div className='dark relative z-20 bg-[#061127] pb-20 md:pb-0'>
         <Footer className='border-transparent' />
       </div>
```

  - `md:pb-0`（≥768px）归零 → 桌面零变化
  - 只作用于 `MainSiteLanding`，**不改 `Footer` 组件本身**（多页共用）

## ✅ 验收标准

- [ ] 四道门全绿
- [ ] **契约测试不回归**：`src/lib/lazy-loading-contract.test.ts` 仍绿——它断言 `features/home/index.tsx` 里 `const LandingReact = lazy(` 与 import 语句的形态，M3 不得触碰这两处
- [ ] `git diff` 自检：`locales/*.json` **零改动**
- [ ] 关闭按钮在桌面视口下不可见（T04 的 `.hud-close{display:none}` 生效）
- [ ] `index.tsx` 内无新增 `useState` / `useRef` / `(window as any)`

## 🚫 禁止

- 新增任何 i18n 条目或界面文案
- 用 `window.__scene3d.clearSelection()` 之类的全局对象调用（会引入 `any`，且与宿主产生具名耦合）——必须走 `CustomEvent`
- 改 `Footer` 组件本身
- 在 `<style>` 里把 `MOBILE_CSS` 拼在 `LANDING_CSS` 之前（覆盖会失效）
