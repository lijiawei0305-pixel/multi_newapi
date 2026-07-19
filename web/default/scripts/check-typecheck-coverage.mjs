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
const GENERATED_NOCHECK_FILES = new Set(['src/routeTree.gen.ts'])

async function collectTypeScriptFiles(directory) {
  const files = []
  const entries = await readdir(directory, { withFileTypes: true })

  for (const entry of entries) {
    const absolutePath = path.join(directory, entry.name)
    if (entry.isDirectory()) {
      files.push(...(await collectTypeScriptFiles(absolutePath)))
      continue
    }
    if (entry.isFile() && /\.(?:ts|tsx)$/.test(entry.name)) {
      files.push(absolutePath)
    }
  }

  return files
}

const violations = []
const seenGeneratedFiles = new Set()

for (const absolutePath of await collectTypeScriptFiles(SOURCE_ROOT)) {
  const relativePath = path
    .relative(process.cwd(), absolutePath)
    .split(path.sep)
    .join('/')
  const source = await readFile(absolutePath, 'utf8')
  if (!source.includes('@ts-nocheck')) continue

  if (GENERATED_NOCHECK_FILES.has(relativePath)) {
    seenGeneratedFiles.add(relativePath)
    continue
  }
  violations.push(`${relativePath}: @ts-nocheck hides the file from tsgo`)
}

for (const relativePath of GENERATED_NOCHECK_FILES) {
  if (!seenGeneratedFiles.has(relativePath)) {
    violations.push(
      `${relativePath}: generated @ts-nocheck allowance is missing or stale`
    )
  }
}

if (violations.length > 0) {
  console.error('Typecheck coverage check failed:')
  for (const violation of violations) console.error(`  - ${violation}`)
  process.exitCode = 1
} else {
  console.log(
    `Typecheck coverage passed; only ${[...GENERATED_NOCHECK_FILES].join(', ')} is exempt.`
  )
}
