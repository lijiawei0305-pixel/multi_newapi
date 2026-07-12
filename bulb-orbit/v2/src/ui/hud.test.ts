import { describe, it, expect, beforeEach } from 'vitest'
import { renderHud, hudModelFor, type HudEls } from './hud'
import { getModel, DEFAULT_MODEL } from '../data/models'

function makeEls(): HudEls {
  const mk = () => document.createElement('div')
  return { name: mk(), provider: mk(), desc: mk(), telemetry: mk(), panel: mk() }
}

describe('hud', () => {
  let els: HudEls
  beforeEach(() => { els = makeEls() })

  it('renders a model into the DOM with brand border', () => {
    const m = getModel('anthropic')!
    renderHud(els, m)
    expect(els.name.textContent).toBe('Claude 系列')
    expect(els.provider.textContent).toBe('ANTHROPIC')
    expect(els.desc.textContent).toContain('推理')
    expect(els.telemetry.textContent).toBe('200K 上下文激活')
    // happy-dom normalises hex to rgb(...)
    expect(els.panel.style.borderLeftColor).toBeTruthy()
  })

  it('hudModelFor falls back to the core on null / miss', () => {
    expect(hudModelFor(null)).toBe(DEFAULT_MODEL)
    expect(hudModelFor('nope')).toBe(DEFAULT_MODEL)
    expect(hudModelFor('openai').id).toBe('openai')
  })
})
