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

const { chmodSync, existsSync, mkdirSync } = require('node:fs')
const { dirname, join } = require('node:path')
const { spawnSync } = require('node:child_process')
const { downloadArtifact } = require('@electron/get')
const { path7za } = require('7zip-bin')

const electronDir = dirname(require.resolve('electron/package.json'))
const distDir = join(electronDir, 'dist')
const electronPackage = require(join(electronDir, 'package.json'))
const licenseNames = new Set(['LICENSE', 'LICENSES.chromium.html'])

function extractLicenseFiles(zipPath) {
  if (process.platform !== 'win32') {
    chmodSync(path7za, 0o755)
  }
  const extraction = spawnSync(
    path7za,
    [
      'e',
      zipPath,
      ...licenseNames,
      `-o${distDir}`,
      '-y',
      '-bso0',
      '-bsp0',
    ],
    { stdio: 'inherit' }
  )
  if (extraction.status !== 0) {
    throw new Error(
      `Could not extract Electron runtime licenses (${extraction.error?.message || `exit ${extraction.status ?? 'unknown'}`})`
    )
  }
}

async function main() {
  mkdirSync(distDir, { recursive: true })
  const licenseFiles = [...licenseNames].map((name) => join(distDir, name))
  if (!licenseFiles.every(existsSync)) {
    const zipPath = await downloadArtifact({
      version: electronPackage.version,
      artifactName: 'electron',
      checksums: require(join(electronDir, 'checksums.json')),
      platform: process.env.npm_config_platform || process.platform,
      arch: process.env.npm_config_arch || process.arch,
    })
    extractLicenseFiles(zipPath)
  }

  for (const file of licenseFiles) {
    if (!existsSync(file)) {
      throw new Error(`Electron runtime license is missing: ${file}`)
    }
  }
  console.log('Electron runtime license inputs are ready.')
}

const keepAlive = setInterval(() => {}, 1000)
main()
  .catch((error) => {
    console.error(error)
    process.exitCode = 1
  })
  .finally(() => clearInterval(keepAlive))
