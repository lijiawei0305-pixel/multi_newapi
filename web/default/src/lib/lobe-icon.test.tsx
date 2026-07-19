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
import { renderToString } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'

import { getLobeIcon } from './lobe-icon'

vi.mock('@lobehub/icons', () => {
  const Mono = (props: Record<string, unknown>) => (
    <svg data-testid='mono-icon' data-size={String(props.size)} />
  )
  const Color = (props: Record<string, unknown>) => (
    <svg data-testid='color-icon' data-size={String(props.size)} />
  )
  Object.assign(Mono, { Color })
  return { TestProvider: Mono }
})

describe('getLobeIcon', () => {
  it('keeps an inline fallback while a configured provider icon loads', () => {
    const html = renderToString(getLobeIcon('TestProvider.Color', 24))

    expect(html).toContain('T')
    expect(html).toContain('width:24px')
  })

  it('uses a deterministic fallback for an unknown configured icon', () => {
    const html = renderToString(getLobeIcon('UnknownProvider', 18))

    expect(html).toContain('U')
    expect(html).toContain('width:18px')
  })
})
