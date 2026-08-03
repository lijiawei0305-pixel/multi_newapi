import { expect, test } from 'vitest'

import {
  BREAKPOINTS,
  pointerModeFromEvent,
  resolvePointerMode,
} from './viewport-mode'

// ── 开局判定 resolvePointerMode ─────────────────────────────────

test('有悬停能力 → hover', () => {
  expect(resolvePointerMode({ hoverCapable: true })).toBe('hover')
  // maxTouchPoints 任意值不影响
  expect(resolvePointerMode({ hoverCapable: true, maxTouchPoints: 5 })).toBe(
    'hover'
  )
})

test('无悬停能力 → touch', () => {
  expect(resolvePointerMode({ hoverCapable: false })).toBe('touch')
  expect(resolvePointerMode({ hoverCapable: false, maxTouchPoints: 0 })).toBe(
    'touch'
  )
})

test('特性缺失 + 有触点 → touch', () => {
  expect(resolvePointerMode({ maxTouchPoints: 5 })).toBe('touch')
})

test('特性缺失 + 无触点 → hover（保守回落）', () => {
  expect(resolvePointerMode({})).toBe('hover')
  expect(resolvePointerMode({ maxTouchPoints: 0 })).toBe('hover')
  expect(resolvePointerMode()).toBe('hover')
})

// ── 运行时校正 pointerModeFromEvent ─────────────────────────────

test('鼠标校正 → hover', () => {
  expect(pointerModeFromEvent('mouse')).toBe('hover')
})

// ★ 触屏笔电缺陷回归护栏：手指交互必须校正为 touch
test('手指校正 → touch', () => {
  expect(pointerModeFromEvent('touch')).toBe('touch')
})

test('触控笔校正 → touch（保守）', () => {
  expect(pointerModeFromEvent('pen')).toBe('touch')
})

test('未知类型不校正 → null', () => {
  expect(pointerModeFromEvent('')).toBe(null)
  expect(pointerModeFromEvent('unknown')).toBe(null)
})

// ── 断点常量锁定 ────────────────────────────────────────────────

test('主断点锁定 mobile === 768', () => {
  expect(BREAKPOINTS.mobile).toBe(768)
  expect(BREAKPOINTS.narrow).toBe(400)
  expect(BREAKPOINTS.shortViewport).toBe(700)
})
