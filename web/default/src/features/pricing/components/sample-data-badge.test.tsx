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
import { readFileSync } from 'node:fs'

import { expect, test } from 'vitest'

test('SampleDataBadge composes a keyboard-focusable Base UI trigger', () => {
  const source = readFileSync(
    new URL('./sample-data-badge.tsx', import.meta.url),
    'utf8'
  )

  expect(source).toContain('<TooltipTrigger')
  expect(source).toMatch(/<TooltipTrigger\s+render=/)
  expect(source).toMatch(/<Badge\s+render=\{<button type='button' \/>\}/)
  expect(source).not.toContain('asChild')
})
