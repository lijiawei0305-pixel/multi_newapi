import { expect, test } from 'vitest'

import { pickRenderQuality } from './scene3d-quality'

test('high: desktop multi-core moderate dpr', () => {
  const q = pickRenderQuality({ dpr: 2, cores: 8, saveData: false, deviceMemory: 8 })
  expect(q.tier).toBe('high')
  expect(q.pixelRatio).toBe(2)
  expect(q.antialias).toBe(true)
  expect(q.bloomScale).toBe(0.5)
})

test('mid: phone-like high dpr or few cores', () => {
  const byDpr = pickRenderQuality({ dpr: 3, cores: 6, saveData: false, deviceMemory: 4 })
  expect(byDpr.tier).toBe('mid')
  expect(byDpr.pixelRatio).toBe(1.5)
  expect(byDpr.antialias).toBe(false)
  expect(byDpr.bloomScale).toBe(0.5)

  const byCores = pickRenderQuality({ dpr: 2, cores: 4, saveData: false, deviceMemory: 4 })
  expect(byCores.tier).toBe('mid')
  expect(byCores.pixelRatio).toBe(1.5)
})

test('low: saveData / low memory / dual core', () => {
  expect(pickRenderQuality({ dpr: 3, cores: 8, saveData: true }).tier).toBe('low')
  expect(pickRenderQuality({ dpr: 2, cores: 8, deviceMemory: 2 }).tier).toBe('low')
  const q = pickRenderQuality({ dpr: 2, cores: 2, deviceMemory: 4 })
  expect(q.tier).toBe('low')
  expect(q.pixelRatio).toBe(1.25)
  expect(q.antialias).toBe(false)
  expect(q.bloomScale).toBe(0.5)
})

test('pixelRatio never exceeds device dpr', () => {
  expect(pickRenderQuality({ dpr: 1, cores: 8 }).pixelRatio).toBe(1)
  expect(pickRenderQuality({ dpr: 1.1, cores: 2 }).pixelRatio).toBe(1.1)
})
