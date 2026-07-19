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
// @vitest-environment jsdom

import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'

import { Markdown } from '@/components/ui/markdown'

import { sanitizeUntrustedRichHtml } from './safe-html'

describe('rich HTML sanitization', () => {
  it('removes user-controlled layout styles and unsafe navigation', () => {
    const html = sanitizeUntrustedRichHtml(`
      <a
        href="javascript:globalThis.pwned=true"
        style="position:fixed;inset:0;z-index:999999;background:white"
      >
        Re-authenticate
      </a>
    `)

    expect(html).toContain('Re-authenticate')
    expect(html).not.toContain('style=')
    expect(html).not.toContain('position:fixed')
    expect(html).not.toContain('javascript:')
    expect(html).not.toContain('href=')
  })

  it('strips raw Markdown HTML styles without breaking trusted math or diagrams', () => {
    const html = renderToStaticMarkup(
      <Markdown>{`
<a href="https://example.com" style="position:fixed;inset:0">Overlay</a>

$$
\\frac{a}{b} + x^2
$$

\`\`\`flow
start=>start: Start
finish=>end: Finish
start->finish
\`\`\`
`}</Markdown>
    )

    expect(html).toContain('Overlay')
    expect(html).not.toContain('position:fixed')
    expect(html).toContain('class="katex"')
    expect(html).toMatch(/style="[^"]*(?:height|top|margin-right):/)
    expect(html).toContain('data-diagram="flow"')
    expect(html).toContain('markdown-diagram-node')
    expect(html).not.toContain('data-new-api-safe-math')
  })
})
