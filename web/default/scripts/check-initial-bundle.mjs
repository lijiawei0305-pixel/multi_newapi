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
import { readFile } from 'node:fs/promises'
import path from 'node:path'
import { gzipSync } from 'node:zlib'

const distDir = path.resolve('dist')
const htmlPath = path.join(distDir, 'index.html')
const maxRawBytes = Number(process.env.MAX_INITIAL_JS_BYTES ?? 2_500_000)
const maxGzipBytes = Number(process.env.MAX_INITIAL_JS_GZIP_BYTES ?? 700_000)

if (!Number.isFinite(maxRawBytes) || !Number.isFinite(maxGzipBytes)) {
  throw new Error('Initial bundle budgets must be finite byte counts.')
}

const html = await readFile(htmlPath, 'utf8')
const scriptSources = [
  ...html.matchAll(
    /<script\b[^>]*\bsrc=["']([^"']+\.js(?:[?#][^"']*)?)["'][^>]*>/gi
  ),
].map((match) => match[1])

if (scriptSources.length === 0) {
  throw new Error(`No initial JavaScript files found in ${htmlPath}`)
}

const assets = []
for (const source of new Set(scriptSources)) {
  if (/^https?:\/\//i.test(source)) {
    throw new Error(`Remote initial script is not budgeted: ${source}`)
  }
  const relativePath = source.split(/[?#]/, 1)[0].replace(/^\/+/, '')
  const filePath = path.resolve(distDir, relativePath)
  if (!filePath.startsWith(`${distDir}${path.sep}`)) {
    throw new Error(`Initial script escapes dist/: ${source}`)
  }
  const content = await readFile(filePath)
  assets.push({
    source,
    rawBytes: content.byteLength,
    gzipBytes: gzipSync(content, { level: 9 }).byteLength,
  })
}

const rawBytes = assets.reduce((total, asset) => total + asset.rawBytes, 0)
const gzipBytes = assets.reduce((total, asset) => total + asset.gzipBytes, 0)

for (const asset of assets.sort((a, b) => b.rawBytes - a.rawBytes)) {
  console.log(
    `${asset.source}: ${asset.rawBytes} bytes (${asset.gzipBytes} gzip)`
  )
}
console.log(`Initial JavaScript: ${rawBytes} bytes (${gzipBytes} gzip)`)

if (rawBytes > maxRawBytes || gzipBytes > maxGzipBytes) {
  console.error(
    `Initial JavaScript exceeds budget: raw ${rawBytes}/${maxRawBytes}, gzip ${gzipBytes}/${maxGzipBytes}`
  )
  process.exitCode = 1
}
