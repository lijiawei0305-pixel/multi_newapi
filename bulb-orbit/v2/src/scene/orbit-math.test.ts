import { describe, it, expect } from 'vitest'
import { satelliteAngle, precessionAngle, evenAngles, viewOffset } from './orbit-math'
import { ORBITS } from './config'

const TAU = Math.PI * 2

describe('orbit-math', () => {
  it('satelliteAngle is deterministic and linear in t', () => {
    expect(satelliteAngle(0, 0.1, 0)).toBe(0)
    expect(satelliteAngle(1, 0.1, 10)).toBeCloseTo(2, 6)
  })
  it('precessionAngle wraps into [0, 2π) and respects direction', () => {
    const a = precessionAngle(1, 40, 10)   // +
    const b = precessionAngle(-1, 40, 10)  // -
    expect(a).toBeGreaterThanOrEqual(0); expect(a).toBeLessThan(TAU)
    expect(b).toBeGreaterThanOrEqual(0); expect(b).toBeLessThan(TAU)
    expect(a).toBeCloseTo(TAU - b, 6)      // opposite directions are mirror-wrapped
  })
  it('evenAngles splits the circle with phase offset', () => {
    const xs = evenAngles(4, 0)
    expect(xs).toHaveLength(4)
    expect(xs[1] - xs[0]).toBeCloseTo(TAU / 4, 6)
    expect(evenAngles(3, 0.5)[0]).toBeCloseTo(0.5, 6)
  })
  it('viewOffset is proportional to width', () => {
    expect(viewOffset(1000, 0.1)).toBeCloseTo(100, 6)
    expect(viewOffset(0, 0.1)).toBe(0)
  })
})

describe('config', () => {
  it('domestic orbit is wider than intl', () => {
    expect(ORBITS.domestic.radius).toBeGreaterThan(ORBITS.intl.radius)
  })
  it('the two orbits precess in opposite directions', () => {
    expect(ORBITS.intl.precessDir).toBe(1)
    expect(ORBITS.domestic.precessDir).toBe(-1)
  })
  it('precession periods are within the spec 45s/70s ballpark', () => {
    expect(ORBITS.intl.precessPeriod).toBeGreaterThanOrEqual(30)
    expect(ORBITS.domestic.precessPeriod).toBeGreaterThan(ORBITS.intl.precessPeriod)
  })
})
