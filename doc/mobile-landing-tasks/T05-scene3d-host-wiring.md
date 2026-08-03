# T05 — `scene3d.ts` 宿主接线（9 处）

> **Wave 3** ｜ 依赖：T01、T02 ｜ 被依赖：T07
> 设计契约：[`detailed-design.md`](../mobile-landing-detailed-design.md) §2.4、§3.1–3.3

---

## 目标

把 `scene3d.ts` 从「自己判断选中」收窄为「采样 → 交给纯核 → 按结果改 DOM」。**新增监听器仅 1 个**，其余全部复用既有监听。

## 交付物

| 文件 | 改动 |
| --- | --- |
| `web/default/src/features/landing-react/scene3d.ts` | ✏️ 9 处 |
| `web/default/src/features/landing-react/scene3d-interaction.ts` | ❌ 删除 |
| `web/default/src/features/landing-react/scene3d-interaction.test.ts` | ❌ 删除（用例已在 T02 迁移） |

## 最小可执行任务（MET）

- [ ] **M1** import 段（`:49`）：`nextSelection` → `reduce, INITIAL_SELECTION, type SelectionEvent`；新增 `detectPointerMode, pointerModeFromEvent`
- [ ] **M2** 初始化段（`:512`）：
  - `let pointerMode = detectPointerMode()` —— **必须是 `let`**，运行时会被校正
  - `let selection = INITIAL_SELECTION` 取代 `let selected: string | null = null`
- [ ] **M3** 新增内部收口函数：

```ts
function dispatch(event: SelectionEvent) {
  const next = reduce(selection, event, pointerMode)   // 每次读 pointerMode 当前值
  if (next === selection) return                        // 恒等性快路径，零分配
  selection = next
  onSelectChange(selection.selected)
}
```

- [ ] **M4** 芯片事件绑定（`:381–386`）：**无条件双绑**——保留原 `mouseenter`/`mouseleave`，**并增绑** `click`：

```ts
el.addEventListener('click', (e) => {
  e.stopPropagation()
  dispatch({ type: 'tap-chip', key })
})
```

  **不要写 `if (pointerMode === ...)` 分支**。迁移表完备 → 不该生效的事件会被 `reduce` 吞掉且零副作用（T02 已保证）。这也是运行时切换模式无需重绑的前提。

- [ ] **M5** `setHud`（`:526`）：`box.style.borderLeftColor = m.color` → `box.style.setProperty('--hud-accent', m.color)`（配合 T04 的 M1）
- [ ] **M6** `onSelectChange`（`:530–553`）：既有 12 行**全部原样保留**，只在两个分支各追加 1 行：
  - `key` 分支：`varsEl.classList.add('wd-hud-open')`
  - `else` 分支：`varsEl.classList.remove('wd-hud-open')`
  - （`varsEl` 是 `:505` 已有的 `.wd-landing-root` 引用，直接复用，**不要新建查询**）
- [ ] **M7** 渲染循环（`:700–704`）：改为

```ts
dispatch({ type: 'hover-tick', hoverKey, pointerInside: pointerInCanvas })
speedFactor = approach(speedFactor, selection.selected ? 0 : 1, 3, dt)
```

- [ ] **M8** **扩展**既有 window capture `pointerdown`（`:464–478`，**不是新增监听**）：

```ts
const onPointerDown = (e: PointerEvent) => {
  if (!(window as any).__scene3dActive) return

  // ① 运行时校正指针模式 —— 必须最先执行，后续 dispatch 才用得上新值
  const corrected = pointerModeFromEvent(e.pointerType)
  if (corrected) pointerMode = corrected

  // ② 既有 surge 逻辑 —— 原样保留，一行不改
  ...

  // ③ 点在卫星与抽屉之外 → 关闭（hover 模式下被 reduce 吞掉，无害）
  const t = e.target as HTMLElement | null
  if (!t?.closest('.chip') && !t?.closest('#hud')) {
    dispatch({ type: 'tap-outside' })
  }
}
```

- [ ] **M9** 既有 IntersectionObserver 回调（`:598–606`）补一行，治「抽屉滚出首屏仍钉在屏底」：

```ts
heroVisible = es[0]?.isIntersecting ?? false
if (!heroVisible) dispatch({ type: 'close' })   // ← 新增
```

- [ ] **M10** 新增 `window` 级 `wd-hud-close` 监听（**本任务唯一新增的监听器**）→ `dispatch({ type: 'close' })`；并在 cleanup 段（`:737` 附近）成对 `removeEventListener`
- [ ] **M11** 删除 `scene3d-interaction.ts` 与 `scene3d-interaction.test.ts`（此时已无引用）

## ✅ 验收标准

- [ ] 四道门全绿
- [ ] `git grep nextSelection` 在 `src/` 下**零命中**
- [ ] `git grep "scene3d-interaction"` 在 `src/` 下**零命中**
- [ ] 新增监听器**只有** `wd-hud-close` 一个，且在 cleanup 中成对移除
- [ ] 芯片绑定处**没有** `if (pointerMode` 之类的条件分支
- [ ] `onSelectChange` 的既有 12 行未被改写（只追加了 2 行 class 切换）

**事件顺序自检**（逐条推演，写进回报）：

| 用户动作 | 事件序列 | 期望 |
| --- | --- | --- |
| 点卫星 | capture `pointerdown`（`target` 在 `.chip` 内 → 跳过 ③）→ chip `click` → `tap-chip` | 抽屉打开 |
| 点抽屉外空白 | capture `pointerdown`（③ 命中）→ `tap-outside` | 抽屉关闭 |
| 点抽屉内 × | capture `pointerdown`（`target` 在 `#hud` 内 → 跳过 ③）→ React `onClick` → `CustomEvent` | 抽屉关闭 |
| 点另一颗卫星 | 同「点卫星」 | 直接切换，无闪烁 |

## 🚫 禁止

- 改动 `onSelectChange` 既有的 12 行逻辑（只许追加 class 切换）
- 新增第 2 个监听器（`tap-outside` 必须复用既有 capture `pointerdown`，自动关闭必须复用既有 IntersectionObserver）
- 在 `dispatch` / `reduce` 调用点加任何 `pointerMode` 条件判断
- 给新增的 `pointerdown` 逻辑加 `preventDefault()`（会阻断触摸滚动）
- 把 `pointerMode` 写成 `const`
