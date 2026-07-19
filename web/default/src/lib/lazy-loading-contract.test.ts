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

import { describe, expect, it } from 'vitest'

function readSource(relativePath: string) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8')
}

describe('initial loading boundaries', () => {
  it('keeps the branded landing experience behind the home interaction boundary', () => {
    const source = readSource('../features/home/index.tsx')

    expect(source).toContain('const LandingReact = lazy(')
    expect(source).toContain("import('@/features/landing-react')")
    expect(source).not.toMatch(
      /import\s+\{\s*LandingReact\s*\}\s+from\s+['"]@\/features\/landing-react['"]/
    )
  })

  it('loads the administrator-selected Lobe icon namespace on demand', () => {
    const source = readSource('./lobe-icon-components.tsx')

    expect(source).toContain("await import('@lobehub/icons')")
    expect(source).not.toMatch(
      /import\s+[^('"].*from\s+['"]@lobehub\/icons['"]/
    )
  })

  it('loads react-icons one pack at a time instead of importing its root namespace', () => {
    const source = readSource('../components/react-icon-by-name.tsx')
    const packImports =
      source.match(/import\('react-icons\/[a-z0-9]+'\)/g) ?? []

    expect(packImports.length).toBeGreaterThan(20)
    expect(source).not.toMatch(
      /^import\s+(?!type\b).*from\s+['"]react-icons['"]\s*$/m
    )
  })

  it('keeps all six locale catalogs behind asynchronous loaders', () => {
    const source = readSource('../i18n/config.ts')

    for (const locale of ['en', 'zh', 'fr', 'ja', 'ru', 'vi']) {
      expect(source).toContain(`import('./locales/${locale}.json')`)
    }
    expect(source).not.toMatch(
      /import\s+[^('"].*from\s+['"].*\/locales\/(?:en|zh|fr|ja|ru|vi)\.json['"]/
    )
  })
})
