import { expect, test } from 'vitest'

import { approach, sampleAlphaToPoints } from './scene3d-assets'

function solidImg(w: number, h: number) {
  const data = new Uint8ClampedArray(w * h * 4)
  for (let i = 0; i < w * h; i++) data[i * 4 + 3] = 255 // 全不透明
  return { data, width: w, height: h }
}

test('sampleAlphaToPoints: 数量与范围', () => {
  const pts = sampleAlphaToPoints(solidImg(16, 16), 500, 0.05, () => 0.5)
  expect(pts.length).toBe(500 * 3)
  for (let i = 0; i < pts.length; i += 3) {
    expect(pts[i]).toBeGreaterThanOrEqual(-0.5)
    expect(pts[i]).toBeLessThanOrEqual(0.5)
    expect(pts[i + 1]).toBeGreaterThanOrEqual(-0.5)
    expect(pts[i + 1]).toBeLessThanOrEqual(0.5)
    expect(pts[i + 2]).toBeCloseTo(0, 5) // rng()=0.5 → z=0
  }
})

test('sampleAlphaToPoints: 透明图不产生点(返回空)', () => {
  const empty = {
    data: new Uint8ClampedArray(16 * 16 * 4),
    width: 16,
    height: 16,
  }
  expect(sampleAlphaToPoints(empty, 500, 0.05).length).toBe(0)
})

test('approach: 朝目标单调逼近', () => {
  const a = approach(0, 1, 3, 0.016)
  expect(a).toBeGreaterThan(0)
  expect(a).toBeLessThan(1)
  expect(approach(1, 1, 3, 0.016)).toBeCloseTo(1, 6)
})
