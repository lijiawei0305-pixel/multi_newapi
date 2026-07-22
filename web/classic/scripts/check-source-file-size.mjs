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
import { readFile, readdir } from 'node:fs/promises';
import path from 'node:path';

const SOURCE_ROOT = path.resolve('src');
const DEFAULT_MAX_LINES = 1500;
const SOURCE_EXTENSIONS = new Set(['.js', '.jsx', '.ts', '.tsx']);

function isGeneratedSource(fileName) {
  return (
    fileName.endsWith('.d.ts') ||
    fileName.includes('.gen.') ||
    fileName.includes('.generated.')
  );
}

async function collectSourceFiles(directory) {
  const files = [];
  const entries = await readdir(directory, { withFileTypes: true });

  for (const entry of entries) {
    const absolutePath = path.join(directory, entry.name);
    if (entry.isDirectory()) {
      files.push(...(await collectSourceFiles(absolutePath)));
      continue;
    }
    if (
      entry.isFile() &&
      SOURCE_EXTENSIONS.has(path.extname(entry.name)) &&
      !isGeneratedSource(entry.name)
    ) {
      files.push(absolutePath);
    }
  }

  return files;
}

function countLines(source) {
  if (source.length === 0) return 0;
  const lineCount = source.split(/\r?\n/).length;
  return source.endsWith('\n') ? lineCount - 1 : lineCount;
}

const sourceFiles = await collectSourceFiles(SOURCE_ROOT);
const failures = [];

for (const absolutePath of sourceFiles) {
  const relativePath = path
    .relative(process.cwd(), absolutePath)
    .split(path.sep)
    .join('/');
  const lineCount = countLines(await readFile(absolutePath, 'utf8'));
  if (lineCount > DEFAULT_MAX_LINES) {
    failures.push(
      `${relativePath}: ${lineCount} lines (budget ${DEFAULT_MAX_LINES})`,
    );
  }
}

if (failures.length > 0) {
  console.error('Classic source file size check failed:');
  for (const failure of failures) console.error(`  - ${failure}`);
  console.error(
    `Split oversized files by stable domain responsibility; the default budget is ${DEFAULT_MAX_LINES} lines.`,
  );
  process.exitCode = 1;
} else {
  console.log(
    `Classic source file size check passed for ${sourceFiles.length} files (budget: ${DEFAULT_MAX_LINES} lines; legacy allowances: 0).`,
  );
}
