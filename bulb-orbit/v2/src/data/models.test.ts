import { describe, it, expect } from 'vitest'
import { MODELS, DEFAULT_MODEL, getModel, orbitModels } from './models'

describe('models registry', () => {
  it('has exactly 24 models', () => { expect(MODELS).toHaveLength(24) })
  it('splits 12 intl / 12 domestic', () => {
    expect(orbitModels('intl')).toHaveLength(12)
    expect(orbitModels('domestic')).toHaveLength(12)
  })
  it('has unique ids', () => {
    expect(new Set(MODELS.map(m => m.id)).size).toBe(24)
  })
  it('every brandColor is a 6-digit hex', () => {
    for (const m of [...MODELS, DEFAULT_MODEL]) expect(m.brandColor).toMatch(/^#[0-9a-fA-F]{6}$/)
  })
  it('every field is non-empty and desc/scene are Chinese-bearing', () => {
    for (const m of MODELS) {
      expect(m.name && m.provider && m.desc && m.scene && m.telemetry && m.logo).toBeTruthy()
      expect(/[一-鿿]/.test(m.desc + m.scene)).toBe(true)
    }
  })
  it('getModel resolves and misses correctly', () => {
    expect(getModel('openai')?.provider).toBe('OPENAI')
    expect(getModel('nope')).toBeUndefined()
  })
  it('default model is the WeDream core', () => {
    expect(DEFAULT_MODEL.brandColor).toBe('#00f0ff')
  })
})
