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
import { execFileSync, spawnSync } from 'node:child_process'
import fs from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'

import { afterEach, expect, test } from 'vitest'

const temporaryRoots = []
const locales = ['en', 'zh', 'fr', 'ja', 'ru', 'vi']

async function createI18nFixture(source, dynamicAllowlist = []) {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'newapi-i18n-sync-'))
  temporaryRoots.push(root)
  const localesRoot = path.join(root, 'src', 'i18n', 'locales')
  await fs.mkdir(localesRoot, { recursive: true })
  await fs.mkdir(path.join(root, 'scripts'), { recursive: true })
  await fs.writeFile(
    path.join(root, 'src', 'i18n', 'static-keys.ts'),
    'export const STATIC_I18N_KEYS = [] as const\n'
  )
  await fs.writeFile(
    path.join(root, 'scripts', 'i18n-dynamic-call-allowlist.json'),
    `${JSON.stringify(dynamicAllowlist, null, 2)}\n`
  )
  await fs.writeFile(
    path.join(root, 'scripts', 'i18n-unchanged-value-allowlist.json'),
    `${JSON.stringify(Object.fromEntries(locales.slice(1).map((locale) => [locale, []])), null, 2)}\n`
  )
  await fs.writeFile(path.join(root, 'src', 'screen.tsx'), source)
  for (const locale of locales) {
    await fs.writeFile(
      path.join(localesRoot, `${locale}.json`),
      `${JSON.stringify({ translation: { Existing: `${locale} existing` } }, null, 2)}\n`
    )
  }
  return { root, localesRoot }
}

afterEach(async () => {
  await Promise.all(
    temporaryRoots
      .splice(0)
      .map((root) => fs.rm(root, { recursive: true, force: true }))
  )
})

test('sync fails when a literal source key is absent from every locale', async () => {
  const { root, localesRoot } = await createI18nFixture(
    "export const Screen = ({ t }) => t('New source key')\n"
  )

  const script = path.resolve('scripts', 'sync-i18n.mjs')
  const failed = spawnSync(process.execPath, [script], {
    cwd: root,
    encoding: 'utf8',
  })
  expect(failed.status).not.toBe(0)
  const failedReport = JSON.parse(
    await fs.readFile(
      path.join(localesRoot, '_reports', '_sync-report.json'),
      'utf8'
    )
  )
  for (const locale of locales) {
    expect(failedReport.locales[locale].missingSourceKeys).toEqual([
      'New source key',
    ])
  }

  for (const locale of locales) {
    const localePath = path.join(localesRoot, `${locale}.json`)
    const catalog = JSON.parse(await fs.readFile(localePath, 'utf8'))
    catalog.translation['New source key'] = `${locale} translated source key`
    await fs.writeFile(localePath, `${JSON.stringify(catalog, null, 2)}\n`)
  }
  expect(() =>
    execFileSync(process.execPath, [script], { cwd: root, stdio: 'pipe' })
  ).not.toThrow()
})

test('sync rejects every dynamic call site until it is explicitly allowlisted', async () => {
  const { root, localesRoot } = await createI18nFixture(
    'export const Screen = ({ t, key, foo, suffixA }) => [t(key), t(`preset.${key}`), t(getKey(foo)\n + suffixA)]\n'
  )
  const script = path.resolve('scripts', 'sync-i18n.mjs')
  const dynamicCalls = [
    'src/screen.tsx::`preset.${key}`',
    'src/screen.tsx::getKey(foo) + suffixA',
    'src/screen.tsx::key',
  ]

  const failed = spawnSync(process.execPath, [script], {
    cwd: root,
    encoding: 'utf8',
  })
  expect(failed.status).not.toBe(0)
  const failedReport = JSON.parse(
    await fs.readFile(
      path.join(localesRoot, '_reports', '_sync-report.json'),
      'utf8'
    )
  )
  expect(failedReport.source.unexpectedDynamicCalls).toEqual(dynamicCalls)

  await fs.writeFile(
    path.join(root, 'scripts', 'i18n-dynamic-call-allowlist.json'),
    `${JSON.stringify(dynamicCalls, null, 2)}\n`
  )
  expect(() =>
    execFileSync(process.execPath, [script], { cwd: root, stdio: 'pipe' })
  ).not.toThrow()

  await fs.writeFile(
    path.join(root, 'src', 'screen.tsx'),
    'export const Screen = ({ t, key, foo, suffixB }) => [t(key), t(`preset.${key}`), t(getKey(foo)\n + suffixB)]\n'
  )
  const changed = spawnSync(process.execPath, [script], {
    cwd: root,
    encoding: 'utf8',
  })
  expect(changed.status).not.toBe(0)
  const changedReport = JSON.parse(
    await fs.readFile(
      path.join(localesRoot, '_reports', '_sync-report.json'),
      'utf8'
    )
  )
  expect(changedReport.source.unexpectedDynamicCalls).toEqual([
    'src/screen.tsx::getKey(foo) + suffixB',
  ])
  expect(changedReport.source.staleDynamicAllowlist).toEqual([
    'src/screen.tsx::getKey(foo) + suffixA',
  ])
})

test('sync rejects stale dynamic-call allowlist entries', async () => {
  const staleEntry = 'src/screen.tsx::key'
  const { root, localesRoot } = await createI18nFixture(
    "export const Screen = ({ t }) => t('Existing')\n",
    [staleEntry]
  )
  const script = path.resolve('scripts', 'sync-i18n.mjs')

  const failed = spawnSync(process.execPath, [script], {
    cwd: root,
    encoding: 'utf8',
  })
  expect(failed.status).not.toBe(0)
  const failedReport = JSON.parse(
    await fs.readFile(
      path.join(localesRoot, '_reports', '_sync-report.json'),
      'utf8'
    )
  )
  expect(failedReport.source.staleDynamicAllowlist).toEqual([staleEntry])

  await fs.writeFile(
    path.join(root, 'scripts', 'i18n-dynamic-call-allowlist.json'),
    '[]\n'
  )
  expect(() =>
    execFileSync(process.execPath, [script], { cwd: root, stdio: 'pipe' })
  ).not.toThrow()
})

test('sync rejects untranslated values and stale unchanged-value approvals', async () => {
  const { root, localesRoot } = await createI18nFixture(
    "export const Screen = ({ t }) => t('Existing')\n"
  )
  const script = path.resolve('scripts', 'sync-i18n.mjs')
  const viPath = path.join(localesRoot, 'vi.json')
  const viCatalog = JSON.parse(await fs.readFile(viPath, 'utf8'))
  viCatalog.translation.Existing = 'en existing'
  await fs.writeFile(viPath, `${JSON.stringify(viCatalog, null, 2)}\n`)

  const untranslated = spawnSync(process.execPath, [script], {
    cwd: root,
    encoding: 'utf8',
  })
  expect(untranslated.status).not.toBe(0)
  let report = JSON.parse(
    await fs.readFile(
      path.join(localesRoot, '_reports', '_sync-report.json'),
      'utf8'
    )
  )
  expect(report.locales.en.untranslatedKeys).toEqual([])
  expect(report.locales.en.staleUnchangedValueAllowlist).toEqual([])
  expect(report.locales.vi.untranslatedKeys).toEqual(['Existing'])

  const unchangedAllowlistPath = path.join(
    root,
    'scripts',
    'i18n-unchanged-value-allowlist.json'
  )
  const approvals = Object.fromEntries(
    locales.slice(1).map((locale) => [locale, []])
  )
  approvals.vi = ['Existing']
  await fs.writeFile(
    unchangedAllowlistPath,
    `${JSON.stringify(approvals, null, 2)}\n`
  )
  expect(() =>
    execFileSync(process.execPath, [script], { cwd: root, stdio: 'pipe' })
  ).not.toThrow()

  viCatalog.translation.Existing = 'Đã dịch'
  await fs.writeFile(viPath, `${JSON.stringify(viCatalog, null, 2)}\n`)
  const stale = spawnSync(process.execPath, [script], {
    cwd: root,
    encoding: 'utf8',
  })
  expect(stale.status).not.toBe(0)
  report = JSON.parse(
    await fs.readFile(
      path.join(localesRoot, '_reports', '_sync-report.json'),
      'utf8'
    )
  )
  expect(report.locales.vi.staleUnchangedValueAllowlist).toEqual(['Existing'])

  approvals.vi = []
  await fs.writeFile(
    unchangedAllowlistPath,
    `${JSON.stringify(approvals, null, 2)}\n`
  )
  expect(() =>
    execFileSync(process.execPath, [script], { cwd: root, stdio: 'pipe' })
  ).not.toThrow()
})

test('sync does not treat e.g. placeholders as language-neutral literals', async () => {
  const { root, localesRoot } = await createI18nFixture(
    "export const Screen = ({ t }) => t('e.g., 100')\n"
  )
  for (const locale of locales) {
    const localePath = path.join(localesRoot, `${locale}.json`)
    const catalog = JSON.parse(await fs.readFile(localePath, 'utf8'))
    catalog.translation['e.g., 100'] =
      locale === 'en' || locale === 'vi' ? 'e.g., 100' : `${locale}: 100`
    await fs.writeFile(localePath, `${JSON.stringify(catalog, null, 2)}\n`)
  }

  const script = path.resolve('scripts', 'sync-i18n.mjs')
  const failed = spawnSync(process.execPath, [script], {
    cwd: root,
    encoding: 'utf8',
  })
  expect(failed.status).not.toBe(0)
  const report = JSON.parse(
    await fs.readFile(
      path.join(localesRoot, '_reports', '_sync-report.json'),
      'utf8'
    )
  )
  expect(report.locales.vi.untranslatedKeys).toContain('e.g., 100')
})
