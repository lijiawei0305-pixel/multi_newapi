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
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import {
  buildGitHubOAuthUrl,
  buildLinuxDOOAuthUrl,
  buildOIDCOAuthUrl,
} from './oauth'

describe('OAuth URL builders', () => {
  const originalWindow = globalThis.window

  beforeEach(() => {
    Object.defineProperty(globalThis, 'window', {
      configurable: true,
      value: { location: { origin: 'https://console.example' } },
    })
  })

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

  it('encodes provider-controlled query values', () => {
    const github = new URL(buildGitHubOAuthUrl('client&scope=repo', 'a&b=c'))
    expect(github.origin).toBe('https://github.com')
    expect(github.searchParams.get('client_id')).toBe('client&scope=repo')
    expect(github.searchParams.get('state')).toBe('a&b=c')
    expect(github.searchParams.get('scope')).toBe('user:email')

    const linuxDo = new URL(buildLinuxDOOAuthUrl('client&x=1', 'state&x=2'))
    expect(linuxDo.searchParams.get('client_id')).toBe('client&x=1')
    expect(linuxDo.searchParams.get('state')).toBe('state&x=2')
  })

  it('accepts only HTTP(S) OIDC authorization endpoints', () => {
    const oidc = new URL(
      buildOIDCOAuthUrl('https://identity.example/authorize', 'client', 'state')
    )
    expect(oidc.origin).toBe('https://identity.example')
    expect(oidc.searchParams.get('redirect_uri')).toBe(
      'https://console.example/oauth/oidc'
    )
    expect(() =>
      buildOIDCOAuthUrl('javascript:alert(1)', 'client', 'state')
    ).toThrow(/HTTP or HTTPS/)
    expect(() => buildOIDCOAuthUrl('/relative', 'client', 'state')).toThrow(
      /HTTP or HTTPS/
    )
  })
})
