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
import { readFile, readdir } from 'node:fs/promises'
import path from 'node:path'

const SOURCE_ROOT = path.resolve('src')
const DEFAULT_MAX_LINES = 1500
const SOURCE_EXTENSIONS = new Set(['.js', '.jsx', '.ts', '.tsx'])

// These files predate the size gate. Their exact budgets prevent further
// growth while keeping the allowlist small and making future removal obvious.
const LEGACY_LINE_BUDGETS = new Map([
  [
    'src/features/system-settings/integrations/payment-settings-section.tsx',
    2248,
  ],
  ['src/features/system-settings/models/tiered-pricing-editor.tsx', 1904],
])

function isGeneratedSource(fileName) {
  return (
    fileName.endsWith('.d.ts') ||
    fileName.includes('.gen.') ||
    fileName.includes('.generated.')
  )
}

async function collectSourceFiles(directory) {
  const files = []
  const entries = await readdir(directory, { withFileTypes: true })

  for (const entry of entries) {
    const absolutePath = path.join(directory, entry.name)
    if (entry.isDirectory()) {
      files.push(...(await collectSourceFiles(absolutePath)))
      continue
    }

    if (
      entry.isFile() &&
      SOURCE_EXTENSIONS.has(path.extname(entry.name)) &&
      !isGeneratedSource(entry.name)
    ) {
      files.push(absolutePath)
    }
  }

  return files
}

function countLines(source) {
  if (source.length === 0) return 0
  const lineCount = source.split(/\r?\n/).length
  return source.endsWith('\n') ? lineCount - 1 : lineCount
}

const sourceFiles = await collectSourceFiles(SOURCE_ROOT)
const failures = []
const seenLegacyFiles = new Set()

for (const absolutePath of sourceFiles) {
  const relativePath = path
    .relative(process.cwd(), absolutePath)
    .split(path.sep)
    .join('/')
  const source = await readFile(absolutePath, 'utf8')
  const lineCount = countLines(source)
  const legacyBudget = LEGACY_LINE_BUDGETS.get(relativePath)
  const budget = legacyBudget ?? DEFAULT_MAX_LINES

  if (legacyBudget !== undefined) {
    seenLegacyFiles.add(relativePath)
    if (lineCount <= DEFAULT_MAX_LINES) {
      failures.push(
        `${relativePath}: ${lineCount} lines; remove its stale legacy allowance`
      )
      continue
    }
  }

  if (lineCount > budget) {
    failures.push(`${relativePath}: ${lineCount} lines (budget ${budget})`)
  }
}

for (const relativePath of LEGACY_LINE_BUDGETS.keys()) {
  if (!seenLegacyFiles.has(relativePath)) {
    failures.push(`${relativePath}: legacy allowance points to a missing file`)
  }
}

if (failures.length > 0) {
  console.error('Source file size check failed:')
  for (const failure of failures) console.error(`  - ${failure}`)
  console.error(
    `Split oversized files by stable domain responsibility; the default budget is ${DEFAULT_MAX_LINES} lines.`
  )
  process.exitCode = 1
} else {
  console.log(
    `Source file size check passed for ${sourceFiles.length} files (default budget: ${DEFAULT_MAX_LINES} lines; legacy allowances: ${LEGACY_LINE_BUDGETS.size}).`
  )
}
