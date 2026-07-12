import { describe, it, expect } from 'vitest'
import { breakpointFor, particleCount, bloomEnabled } from './layout'

describe('layout', () => {
  it('maps width to breakpoint at 1024/768', () => {
    expect(breakpointFor(1920)).toBe('desktop')
    expect(breakpointFor(1024)).toBe('desktop')
    expect(breakpointFor(1023)).toBe('tablet')
    expect(breakpointFor(768)).toBe('tablet')
    expect(breakpointFor(767)).toBe('mobile')
    expect(breakpointFor(390)).toBe('mobile')
  })
  it('halves particles on mobile', () => {
    expect(particleCount('desktop')).toBe(40000)
    expect(particleCount('mobile')).toBe(20000)
  })
  it('disables bloom on mobile only', () => {
    expect(bloomEnabled('desktop')).toBe(true)
    expect(bloomEnabled('mobile')).toBe(false)
  })
})
