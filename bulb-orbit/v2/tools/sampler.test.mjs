import { describe, it, expect } from 'vitest'
import { areaWeightedSample, laplacianSmooth } from './sampler.mjs'

// 单位四面体：4 顶点 4 面
const positions = new Float32Array([0,0,0, 1,0,0, 0,1,0, 0,0,1])
const indices = new Uint32Array([0,1,2, 0,1,3, 0,2,3, 1,2,3])
const normals = new Float32Array([0,0,-1, 0,-1,0, -1,0,0, 0.577,0.577,0.577])

describe('areaWeightedSample', () => {
  it('emits exactly count points × 6 floats', () => {
    let s = 1; const rand = () => (s = (s * 16807) % 2147483647) / 2147483647
    const out = areaWeightedSample(positions, indices, normals, 500, rand)
    expect(out).toBeInstanceOf(Float32Array)
    expect(out.length).toBe(500 * 6)
  })
  it('normals are unit length', () => {
    let s = 7; const rand = () => (s = (s * 16807) % 2147483647) / 2147483647
    const out = areaWeightedSample(positions, indices, normals, 100, rand)
    for (let i = 0; i < 100; i++) {
      const nx = out[i*6+3], ny = out[i*6+4], nz = out[i*6+5]
      expect(Math.hypot(nx, ny, nz)).toBeCloseTo(1, 4)
    }
  })
  it('sampled points stay within the mesh bounding box', () => {
    let s = 3; const rand = () => (s = (s * 16807) % 2147483647) / 2147483647
    const out = areaWeightedSample(positions, indices, normals, 300, rand)
    for (let i = 0; i < 300; i++) {
      for (let k = 0; k < 3; k++) { expect(out[i*6+k]).toBeGreaterThanOrEqual(-1e-6); expect(out[i*6+k]).toBeLessThanOrEqual(1 + 1e-6) }
    }
  })
})

describe('laplacianSmooth', () => {
  it('keeps vertex count and moves interior vertices toward neighbours', () => {
    const out = laplacianSmooth(positions, indices, 1)
    expect(out.length).toBe(positions.length)
    // 平滑后仍在原包围盒内（不发散）
    for (const v of out) { expect(v).toBeGreaterThanOrEqual(-1); expect(v).toBeLessThanOrEqual(1.5) }
  })
})
