import { expect, test } from 'vitest'

import { MOBILE_CSS } from './landing-mobile-css'
import { BREAKPOINTS } from './viewport-mode'

/** 剥掉全部 @media(...){...} 块（支持嵌套花括号），用于「零裸规则」门禁。 */
function stripMediaBlocks(css: string): string {
  let result = ''
  let i = 0
  while (i < css.length) {
    const mediaIdx = css.indexOf('@media', i)
    if (mediaIdx === -1) {
      result += css.slice(i)
      break
    }
    result += css.slice(i, mediaIdx)
    const openIdx = css.indexOf('{', mediaIdx)
    if (openIdx === -1) {
      result += css.slice(mediaIdx)
      break
    }
    let depth = 1
    let j = openIdx + 1
    while (j < css.length && depth > 0) {
      const ch = css[j]
      if (ch === '{') depth++
      else if (ch === '}') depth--
      j++
    }
    i = j
  }
  return result
}

// ★ M1 桌面端零变化：媒体查询外不得有任何 CSS 规则
test('零裸规则：剥掉 @media 块后仅剩空白与注释', () => {
  const remainder = stripMediaBlocks(MOBILE_CSS)
  const withoutComments = remainder.replaceAll(/\/\*[\s\S]*?\*\//g, '')
  expect(withoutComments.trim()).toBe('')
})

test('零 !important：覆盖靠源码顺序而非权重战争', () => {
  expect(MOBILE_CSS.includes('!important')).toBe(false)
})

test('断点单一真源：max-width 值 ∈ {mobile, narrow}', () => {
  const allowed = new Set<number>([BREAKPOINTS.mobile, BREAKPOINTS.narrow])
  const widths = [...MOBILE_CSS.matchAll(/max-width:\s*(\d+)px/g)].map((m) =>
    Number(m[1])
  )
  expect(widths.length).toBeGreaterThan(0)
  for (const w of widths) {
    expect(allowed.has(w)).toBe(true)
  }
})

test('矮屏断点一致：max-height 值 === shortViewport', () => {
  const heights = [...MOBILE_CSS.matchAll(/max-height:\s*(\d+)px/g)].map((m) =>
    Number(m[1])
  )
  expect(heights.length).toBeGreaterThan(0)
  for (const h of heights) {
    expect(h).toBe(BREAKPOINTS.shortViewport)
  }
})

test('与全站断点一致：mobile === 768', () => {
  expect(BREAKPOINTS.mobile).toBe(768)
})

test('括号平衡：{ 与 } 计数相等', () => {
  const open = (MOBILE_CSS.match(/\{/g) ?? []).length
  const close = (MOBILE_CSS.match(/\}/g) ?? []).length
  expect(open).toBe(close)
})
