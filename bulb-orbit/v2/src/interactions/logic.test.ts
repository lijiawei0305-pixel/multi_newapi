import { describe, it, expect } from 'vitest'
import { coreColorFor, resolveHover } from './logic'

describe('interaction logic', () => {
  it('coreColorFor returns brand color or the cyan core default', () => {
    expect(coreColorFor('anthropic')).toBe('#d97757')
    expect(coreColorFor(null)).toBe('#00f0ff')
    expect(coreColorFor('nope')).toBe('#00f0ff')
  })
  it('resolveHover flags change only on transition', () => {
    expect(resolveHover(null, 'openai')).toEqual({ modelId: 'openai', changed: true })
    expect(resolveHover('openai', 'openai')).toEqual({ modelId: 'openai', changed: false })
    expect(resolveHover('openai', null)).toEqual({ modelId: null, changed: true })
  })
})
