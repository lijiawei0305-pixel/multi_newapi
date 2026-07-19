/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  normalizeBase64VideoDataUrl,
  normalizeHttpNavigationUrl,
  normalizeHttpResourceUrl,
  normalizeImageResourceUrl,
  normalizePaymentQrNavigationUrl,
  normalizeRasterImageDataUrl,
  normalizeSameOriginNavigationPath,
  openHttpResourceUrlInNewTab,
  openHttpUrlInNewTab,
} from './safe-navigation'

describe('safe navigation', () => {
  const originalWindow = globalThis.window

  afterEach(() => {
    if (originalWindow === undefined) {
      Reflect.deleteProperty(globalThis, 'window')
    } else {
      Object.defineProperty(globalThis, 'window', {
        configurable: true,
        value: originalWindow,
      })
    }
  })

  it('accepts only absolute HTTP(S) URLs', () => {
    expect(normalizeHttpNavigationUrl(' https://pay.example/a ')).toBe(
      'https://pay.example/a'
    )
    expect(normalizeHttpNavigationUrl('http://pay.example')).toBe(
      'http://pay.example/'
    )
    for (const value of [
      'javascript:globalThis.pwned=true',
      'data:text/html,<script>alert(1)</script>',
      'blob:https://example.com/id',
      'file:///tmp/report',
      'https://user:password@pay.example/path',
      '//evil.example/path',
      '/relative',
      '',
      null,
    ]) {
      expect(normalizeHttpNavigationUrl(value)).toBeNull()
    }
  })

  it('opens without an opener and never opens rejected schemes', () => {
    const opened = { opener: {} }
    const open = vi.fn(() => opened)
    Object.defineProperty(globalThis, 'window', {
      configurable: true,
      value: { open },
    })

    expect(openHttpUrlInNewTab('https://pay.example/checkout')).toBe(true)
    expect(open).toHaveBeenCalledWith(
      'https://pay.example/checkout',
      '_blank',
      'noopener,noreferrer'
    )
    expect(opened.opener).toBeNull()

    expect(openHttpUrlInNewTab('javascript:alert(1)')).toBe(false)
    expect(open).toHaveBeenCalledTimes(1)
  })

  it('resolves same-origin resources without allowing executable schemes', () => {
    const opened = { opener: {} }
    const open = vi.fn(() => opened)
    Object.defineProperty(globalThis, 'window', {
      configurable: true,
      value: {
        location: { origin: 'https://console.example' },
        open,
      },
    })

    expect(normalizeHttpResourceUrl('/audio.mp3')).toBe(
      'https://console.example/audio.mp3'
    )
    expect(openHttpResourceUrlInNewTab('/audio.mp3')).toBe(true)
    expect(open).toHaveBeenCalledWith(
      'https://console.example/audio.mp3',
      '_blank',
      'noopener,noreferrer'
    )
    expect(opened.opener).toBeNull()
    expect(normalizeHttpResourceUrl('javascript:alert(1)')).toBeNull()
    expect(normalizeHttpResourceUrl('data:text/html,unsafe')).toBeNull()
    expect(normalizeHttpResourceUrl('file:///tmp/report')).toBeNull()
    expect(
      normalizeHttpResourceUrl('https://user@cdn.example/image')
    ).toBeNull()
    expect(openHttpResourceUrlInNewTab('javascript:alert(1)')).toBe(false)
  })

  it('keeps redirects on the current origin', () => {
    Object.defineProperty(globalThis, 'window', {
      configurable: true,
      value: { location: { origin: 'https://console.example' } },
    })

    expect(normalizeSameOriginNavigationPath('/keys?tab=active#first')).toBe(
      '/keys?tab=active#first'
    )
    expect(
      normalizeSameOriginNavigationPath(
        'https://console.example/pricing?model=gpt'
      )
    ).toBe('/pricing?model=gpt')
    for (const value of [
      'https://evil.example/phish',
      '//evil.example/phish',
      'javascript:alert(1)',
      'data:text/html,unsafe',
      'blob:https://console.example/id',
      'file:///tmp/report',
      '/\\evil.example',
    ]) {
      expect(normalizeSameOriginNavigationPath(value)).toBeNull()
    }
  })

  it('allows only the expected payment QR schemes', () => {
    expect(
      normalizePaymentQrNavigationUrl('weixin://wxpay/bizpayurl?pr=test')
    ).toBe('weixin://wxpay/bizpayurl?pr=test')
    expect(normalizePaymentQrNavigationUrl('https://pay.example/qr')).toBe(
      'https://pay.example/qr'
    )
    expect(
      normalizePaymentQrNavigationUrl('javascript:globalThis.pwned=true')
    ).toBeNull()
  })

  it('allows raster image data but rejects active SVG or HTML data', () => {
    expect(normalizeRasterImageDataUrl('data:image/png;base64,aGVsbG8=')).toBe(
      'data:image/png;base64,aGVsbG8='
    )
    expect(
      normalizeRasterImageDataUrl(
        'data:image/svg+xml,<svg onload="alert(1)"></svg>'
      )
    ).toBeNull()
    expect(normalizeRasterImageDataUrl('data:text/html,unsafe')).toBeNull()
    expect(normalizeImageResourceUrl('data:text/html,unsafe')).toBeNull()
    expect(normalizeImageResourceUrl('https://cdn.example/image.webp')).toBe(
      'https://cdn.example/image.webp'
    )
  })

  it('accepts only constrained base64 video data URLs', () => {
    expect(normalizeBase64VideoDataUrl('data:video/mp4;base64,aGVsbG8=')).toBe(
      'data:video/mp4;base64,aGVsbG8='
    )
    expect(
      normalizeBase64VideoDataUrl(
        'data:image/svg+xml;base64,PHN2ZyBvbmxvYWQ9ImFsZXJ0KDEpIi8+'
      )
    ).toBeNull()
    expect(
      normalizeBase64VideoDataUrl('data:text/html;base64,PHNjcmlwdD4=')
    ).toBeNull()
  })
})
