/* 卫星选中状态机：桌面悬停 + 触屏点击分流。
   覆盖 hover 迁移 5 + touch 4 + 跨模式隔离 2 + 恒等 2 + 幂等 1 = 14 条。 */
import { expect, test } from 'vitest'

import {
  INITIAL_SELECTION,
  reduce,
  type SelectionState,
} from './selection-machine'

// ── hover 迁移（改造前 hover 迟滞语义等价搬迁）────────────────────

test('hover：命中即锁定', () => {
  const next = reduce(
    INITIAL_SELECTION,
    { type: 'hover-tick', hoverKey: 'openai', pointerInside: true },
    'hover'
  )
  expect(next.selected).toBe('openai')
})

test('hover：不同命中即切换', () => {
  const s: SelectionState = { selected: 'openai' }
  const next = reduce(
    s,
    { type: 'hover-tick', hoverKey: 'gemini', pointerInside: true },
    'hover'
  )
  expect(next.selected).toBe('gemini')
})

test('hover：命中空处保持锁定（迟滞不松手）', () => {
  const s: SelectionState = { selected: 'openai' }
  const next = reduce(
    s,
    { type: 'hover-tick', hoverKey: null, pointerInside: true },
    'hover'
  )
  expect(next.selected).toBe('openai')
  // 无变化 → 同引用
  expect(next).toBe(s)
})

test('hover：离开画布即清空', () => {
  const s: SelectionState = { selected: 'openai' }
  const next = reduce(
    s,
    { type: 'hover-tick', hoverKey: null, pointerInside: false },
    'hover'
  )
  expect(next.selected).toBe(null)
})

test('hover：离开优先于命中', () => {
  const s: SelectionState = { selected: 'openai' }
  const next = reduce(
    s,
    { type: 'hover-tick', hoverKey: 'gemini', pointerInside: false },
    'hover'
  )
  expect(next.selected).toBe(null)
})

// ── touch 路径 ──────────────────────────────────────────────────

test('touch：tap 卫星即选中', () => {
  const next = reduce(
    INITIAL_SELECTION,
    { type: 'tap-chip', key: 'openai' },
    'touch'
  )
  expect(next.selected).toBe('openai')
})

test('touch：tap 另一卫星即切换', () => {
  const s: SelectionState = { selected: 'openai' }
  const next = reduce(s, { type: 'tap-chip', key: 'gemini' }, 'touch')
  expect(next.selected).toBe('gemini')
})

test('touch：tap 外部即关闭', () => {
  const s: SelectionState = { selected: 'openai' }
  const next = reduce(s, { type: 'tap-outside' }, 'touch')
  expect(next.selected).toBe(null)
})

test('touch：close 即关闭', () => {
  const s: SelectionState = { selected: 'openai' }
  const next = reduce(s, { type: 'close' }, 'touch')
  expect(next.selected).toBe(null)
})

// ── 跨模式隔离 ──────────────────────────────────────────────────

// ★ R7 回归护栏：触屏合成 mousemove/mouseenter 不得改 selected
test('隔离：touch 忽略 hover-tick（同引用）', () => {
  const s: SelectionState = { selected: 'openai' }
  const next = reduce(
    s,
    { type: 'hover-tick', hoverKey: 'gemini', pointerInside: true },
    'touch'
  )
  expect(next).toBe(s)
  expect(next.selected).toBe('openai')
})

test('隔离：hover 忽略 tap 事件（同引用）', () => {
  const s: SelectionState = { selected: 'openai' }
  expect(reduce(s, { type: 'tap-chip', key: 'gemini' }, 'hover')).toBe(s)
  expect(reduce(s, { type: 'tap-outside' }, 'hover')).toBe(s)
  expect(reduce(s, { type: 'close' }, 'hover')).toBe(s)
})

// ── 恒等性 ──────────────────────────────────────────────────────

test('恒等：无变化返回同引用', () => {
  const s: SelectionState = { selected: 'openai' }
  // hover 迟滞：命中空处不变
  expect(
    reduce(
      s,
      { type: 'hover-tick', hoverKey: null, pointerInside: true },
      'hover'
    )
  ).toBe(s)
  // hover 已选同一 key 再 tick
  expect(
    reduce(
      s,
      { type: 'hover-tick', hoverKey: 'openai', pointerInside: true },
      'hover'
    )
  ).toBe(s)
  // touch 再 tap 同一 key
  expect(reduce(s, { type: 'tap-chip', key: 'openai' }, 'touch')).toBe(s)
  // 已 null 再 close / tap-outside
  expect(reduce(INITIAL_SELECTION, { type: 'close' }, 'touch')).toBe(
    INITIAL_SELECTION
  )
  expect(reduce(INITIAL_SELECTION, { type: 'tap-outside' }, 'touch')).toBe(
    INITIAL_SELECTION
  )
})

test('恒等：有变化返回新引用', () => {
  const s: SelectionState = { selected: 'openai' }
  const toGemini = reduce(
    s,
    { type: 'hover-tick', hoverKey: 'gemini', pointerInside: true },
    'hover'
  )
  expect(toGemini).not.toBe(s)
  expect(toGemini.selected).toBe('gemini')

  const cleared = reduce(
    s,
    { type: 'hover-tick', hoverKey: null, pointerInside: false },
    'hover'
  )
  expect(cleared).not.toBe(s)
  expect(cleared.selected).toBe(null)

  const tapped = reduce(
    INITIAL_SELECTION,
    { type: 'tap-chip', key: 'openai' },
    'touch'
  )
  expect(tapped).not.toBe(INITIAL_SELECTION)
  expect(tapped.selected).toBe('openai')
})

// ── 幂等 ────────────────────────────────────────────────────────

test('幂等：同事件重复施加结果稳定', () => {
  const s: SelectionState = { selected: null }
  const e = {
    type: 'hover-tick' as const,
    hoverKey: 'openai',
    pointerInside: true,
  }
  const once = reduce(s, e, 'hover')
  const twice = reduce(once, e, 'hover')
  expect(twice).toBe(once)
  expect(twice.selected).toBe('openai')

  const t1 = reduce(
    INITIAL_SELECTION,
    { type: 'tap-chip', key: 'gemini' },
    'touch'
  )
  const t2 = reduce(t1, { type: 'tap-chip', key: 'gemini' }, 'touch')
  expect(t2).toBe(t1)

  const open: SelectionState = { selected: 'openai' }
  const c1 = reduce(open, { type: 'close' }, 'touch')
  const c2 = reduce(c1, { type: 'close' }, 'touch')
  expect(c2).toBe(c1)
  expect(c2.selected).toBe(null)
})
