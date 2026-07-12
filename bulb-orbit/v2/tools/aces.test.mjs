import { describe, it, expect } from 'vitest'
import { forwardAcesFilmic, inverseAcesFilmic } from './aces.cjs'

describe('inverse ACES filmic', () => {
  it('round-trips ACES(invACES(c)) ≈ c for in-gamut linear colors', () => {
    const samples = [[0.1,0.2,0.3],[0.4,0.05,0.5],[0.2,0.6,0.1],[0.05,0.05,0.05]]
    for (const c of samples) {
      const back = forwardAcesFilmic(inverseAcesFilmic(c))
      for (let k=0;k<3;k++) expect(back[k]).toBeCloseTo(c[k], 3)
    }
  })
  it('clamps output into [0,1]', () => {
    for (const v of inverseAcesFilmic([0.9,0.9,0.9])) { expect(v).toBeGreaterThanOrEqual(0); expect(v).toBeLessThanOrEqual(1) }
  })
})
