import { describe, it, expect } from 'vitest'
import { webglAvailable, showPoster } from './fallback'

describe('fallback', () => {
  it('reports false when WebGL is absent', () => {
    expect(webglAvailable({}, document)).toBe(false)
  })
  it('reports true when a context can be created (stubbed)', () => {
    const doc = { createElement: () => ({ getContext: () => ({}) }) } as unknown as Document
    expect(webglAvailable({ WebGLRenderingContext: function(){} }, doc)).toBe(true)
  })
  it('showPoster injects an img with the given src', () => {
    const root = document.createElement('div')
    showPoster(root, '/poster.png')
    const img = root.querySelector('img')
    expect(img?.getAttribute('src')).toBe('/poster.png')
  })
})
