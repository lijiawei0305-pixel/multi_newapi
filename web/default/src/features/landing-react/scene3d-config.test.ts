import { expect, test } from 'vitest'

import { LOGOS, MODELS, ORBITS } from './scene3d-config'

test('两环名单精确 & 计数 4/6', () => {
  const inner = ORBITS.find((o) => o.ring === 'inner')
  const outer = ORBITS.find((o) => o.ring === 'outer')
  if (!inner || !outer) throw new Error('内外轨道配置不完整')
  expect(inner.keys).toEqual(['openai', 'anthropic', 'gemini', 'xai'])
  expect(outer.keys).toEqual([
    'deepseek',
    'qwen',
    'minimax',
    'doubao',
    'kimi',
    'glm_chatglm',
  ])
})

test('每个 key 都有 MODELS(含品牌色)与 LOGOS', () => {
  for (const o of ORBITS) {
    for (const k of o.keys) {
      expect(MODELS[k]?.color).toMatch(/^#[0-9a-f]{6}$/i)
      expect(LOGOS[k]).toBeTruthy()
    }
  }
})

test('外圈反相 & 半径更大 & 方向相反', () => {
  const inner = ORBITS.find((o) => o.ring === 'inner')
  const outer = ORBITS.find((o) => o.ring === 'outer')
  if (!inner || !outer) throw new Error('内外轨道配置不完整')
  expect(outer.radius).toBeGreaterThan(inner.radius)
  expect(Math.sign(outer.speed)).toBe(-Math.sign(inner.speed))
})
