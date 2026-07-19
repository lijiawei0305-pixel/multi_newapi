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
import { describe, expect, it } from 'vitest'

import { resolveChatUrl } from './chat-links'

const resolve = (template: string) =>
  resolveChatUrl({
    template,
    apiKey: 'test-key',
    serverAddress: 'https://gateway.example.com',
  })

describe('resolveChatUrl', () => {
  it('allows absolute web URLs and application protocols', () => {
    expect(resolve('https://chat.example.com/?key={key}')).toBe(
      'https://chat.example.com/?key=sk-test-key'
    )
    expect(resolve('ama://set-api-key?key={key}')).toBe(
      'ama://set-api-key?key=sk-test-key'
    )
    expect(
      resolve('cherrystudio://providers/api-keys?data={cherryConfig}')
    ).toMatch(/^cherrystudio:\/\/providers\/api-keys\?data=/)
  })

  it.each([
    'javascript:alert(1)',
    'data:text/html,<script>alert(1)</script>',
    'blob:https://example.com/id',
    'file:///etc/passwd',
    'vbscript:msgbox(1)',
    'about:blank',
    'chrome-extension://example/page.html',
    '/relative/path',
    'ccswitch',
  ])('rejects unsafe or non-navigable template %s', (template) => {
    expect(resolve(template)).toBe('')
  })
})
