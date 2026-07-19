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
import fs from 'node:fs/promises'
import path from 'node:path'

const SOURCE_EXTENSIONS = new Set(['.ts', '.tsx', '.js', '.jsx'])
const LITERAL_CALL_PATTERN =
  /\b(?:i18n(?:next)?\.t|t)\s*(?:\?\.)?\(\s*(?:'((?:\\.|[^'\\])*)'|"((?:\\.|[^"\\])*)"|`((?:\\.|[^`\\])*)`)/g
const STRING_PATTERN =
  /(?:'((?:\\.|[^'\\])*)'|"((?:\\.|[^"\\])*)"|`((?:\\.|[^`\\])*)`)/g

function decodeString(raw) {
  return raw
    .replaceAll(/\\u\{([0-9a-fA-F]+)\}/g, (_, hex) =>
      String.fromCodePoint(Number.parseInt(hex, 16))
    )
    .replaceAll(/\\u([0-9a-fA-F]{4})/g, (_, hex) =>
      String.fromCharCode(Number.parseInt(hex, 16))
    )
    .replaceAll(/\\x([0-9a-fA-F]{2})/g, (_, hex) =>
      String.fromCharCode(Number.parseInt(hex, 16))
    )
    .replaceAll('\\n', '\n')
    .replaceAll('\\r', '\r')
    .replaceAll('\\t', '\t')
    .replaceAll('\\b', '\b')
    .replaceAll('\\f', '\f')
    .replaceAll('\\v', '\v')
    .replaceAll(/\\(['"`\\])/g, '$1')
}

function lineNumberAt(text, index) {
  return text.slice(0, index).split('\n').length
}

// Preserve offsets while excluding comments. This avoids treating examples in
// documentation comments as live translation calls or corrupting URL strings.
function maskComments(text) {
  // Regex/string offsets use UTF-16 code units. `split('')` preserves those
  // offsets even when a file contains astral symbols (for example emoji);
  // spreading by code point would shift every later source slice.
  // oxlint-disable-next-line unicorn/prefer-spread -- spread intentionally has different Unicode offset semantics here.
  const characters = text.split('')
  let state = 'code'
  for (let index = 0; index < characters.length; index += 1) {
    const character = characters[index]
    const next = characters[index + 1]
    if (state === 'line-comment') {
      if (character === '\n') state = 'code'
      else characters[index] = ' '
      continue
    }
    if (state === 'block-comment') {
      if (character === '*' && next === '/') {
        characters[index] = ' '
        characters[index + 1] = ' '
        index += 1
        state = 'code'
      } else if (character !== '\n') {
        characters[index] = ' '
      }
      continue
    }
    if (state !== 'code') {
      if (character === '\\') {
        index += 1
        continue
      }
      if (
        (state === 'single-quote' && character === "'") ||
        (state === 'double-quote' && character === '"') ||
        (state === 'template' && character === '`')
      ) {
        state = 'code'
      }
      continue
    }
    if (character === '/' && next === '/') {
      characters[index] = ' '
      characters[index + 1] = ' '
      index += 1
      state = 'line-comment'
    } else if (character === '/' && next === '*') {
      characters[index] = ' '
      characters[index + 1] = ' '
      index += 1
      state = 'block-comment'
    } else if (character === "'") {
      state = 'single-quote'
    } else if (character === '"') {
      state = 'double-quote'
    } else if (character === '`') {
      state = 'template'
    }
  }
  return characters.join('')
}

function extractFirstArguments(text) {
  const searchableText = maskComments(text)
  const callPattern = /\b(?:i18n(?:next)?\.t|t)\s*(?:\?\.)?\(\s*/g
  const calls = []

  for (const match of searchableText.matchAll(callPattern)) {
    const argumentStart = match.index + match[0].length
    let quote = ''
    let roundDepth = 0
    let squareDepth = 0
    let curlyDepth = 0
    let argumentEnd = searchableText.length

    for (let index = argumentStart; index < searchableText.length; index += 1) {
      const character = searchableText[index]
      if (quote) {
        if (character === '\\') {
          index += 1
        } else if (character === quote) {
          quote = ''
        }
        continue
      }
      if (character === "'" || character === '"' || character === '`') {
        quote = character
      } else if (character === '(') {
        roundDepth += 1
      } else if (character === ')') {
        if (roundDepth === 0 && squareDepth === 0 && curlyDepth === 0) {
          argumentEnd = index
          break
        }
        roundDepth -= 1
      } else if (character === '[') {
        squareDepth += 1
      } else if (character === ']') {
        squareDepth -= 1
      } else if (character === '{') {
        curlyDepth += 1
      } else if (character === '}') {
        curlyDepth -= 1
      } else if (
        character === ',' &&
        roundDepth === 0 &&
        squareDepth === 0 &&
        curlyDepth === 0
      ) {
        argumentEnd = index
        break
      }
    }

    calls.push({
      index: match.index,
      line: lineNumberAt(text, match.index),
      argument: text.slice(argumentStart, argumentEnd).trim(),
    })
  }
  return calls
}

function isStandaloneLiteralArgument(argument) {
  const quote = argument[0]
  if (quote !== "'" && quote !== '"' && quote !== '`') return false

  let hasInterpolation = false
  for (let index = 1; index < argument.length; index += 1) {
    const character = argument[index]
    if (character === '\\') {
      index += 1
      continue
    }
    if (quote === '`' && character === '$' && argument[index + 1] === '{') {
      hasInterpolation = true
    }
    if (character === quote) {
      return index === argument.length - 1 && !hasInterpolation
    }
  }
  return false
}

async function listSourceFiles(directory, localesRoot) {
  const entries = await fs.readdir(directory, { withFileTypes: true })
  const files = []
  for (const entry of entries) {
    const fullPath = path.join(directory, entry.name)
    if (entry.isDirectory()) {
      if (fullPath === localesRoot) continue
      files.push(...(await listSourceFiles(fullPath, localesRoot)))
    } else if (
      entry.isFile() &&
      SOURCE_EXTENSIONS.has(path.extname(entry.name))
    ) {
      files.push(fullPath)
    }
  }
  return files
}

function extractLiteralCalls(text, relativePath) {
  const searchableText = maskComments(text)
  const argumentsByCallIndex = new Map(
    extractFirstArguments(text).map((call) => [call.index, call.argument])
  )
  const occurrences = []
  LITERAL_CALL_PATTERN.lastIndex = 0
  for (const match of searchableText.matchAll(LITERAL_CALL_PATTERN)) {
    if (
      !isStandaloneLiteralArgument(argumentsByCallIndex.get(match.index) ?? '')
    ) {
      continue
    }
    const raw = match[1] ?? match[2] ?? match[3] ?? ''
    const key = decodeString(raw)
    if (!key) continue
    occurrences.push({
      key,
      file: relativePath,
      line: lineNumberAt(text, match.index),
    })
  }
  return occurrences
}

function extractStaticKeys(text) {
  const searchableText = maskComments(text)
  const assignment = searchableText.indexOf('STATIC_I18N_KEYS')
  const arrayStart = searchableText.indexOf('[', assignment)
  const arrayEnd = searchableText.lastIndexOf(']')
  if (assignment < 0 || arrayStart < 0 || arrayEnd <= arrayStart) {
    throw new Error('Unable to locate the STATIC_I18N_KEYS allowlist')
  }
  const keys = []
  STRING_PATTERN.lastIndex = 0
  for (const match of searchableText
    .slice(arrayStart + 1, arrayEnd)
    .matchAll(STRING_PATTERN)) {
    const raw = match[1] ?? match[2] ?? match[3] ?? ''
    if (match[3] !== undefined && raw.includes('${')) continue
    keys.push(decodeString(raw))
  }
  return [...new Set(keys)].sort((a, b) => a.localeCompare(b))
}

function extractDynamicCalls(text, relativePath) {
  return extractFirstArguments(text).flatMap((call) => {
    if (!call.argument || isStandaloneLiteralArgument(call.argument)) {
      return []
    }
    return [
      {
        file: relativePath,
        line: call.line,
        // Keep the complete first argument. Truncating a preview would let two
        // distinct long or multiline expressions share one allowlist ID.
        expression: call.argument.replaceAll(/\s+/g, ' ').trim(),
      },
    ]
  })
}

export async function auditI18nSource(projectRoot = process.cwd()) {
  const sourceRoot = path.join(projectRoot, 'src')
  const localesRoot = path.join(sourceRoot, 'i18n', 'locales')
  const staticKeysPath = path.join(sourceRoot, 'i18n', 'static-keys.ts')
  const dynamicAllowlistPath = path.join(
    projectRoot,
    'scripts',
    'i18n-dynamic-call-allowlist.json'
  )
  const files = (await listSourceFiles(sourceRoot, localesRoot)).sort((a, b) =>
    a.localeCompare(b)
  )
  const occurrences = []
  const dynamicCalls = []
  for (const file of files) {
    const text = await fs.readFile(file, 'utf8')
    const relativePath = path.relative(projectRoot, file)
    occurrences.push(...extractLiteralCalls(text, relativePath))
    dynamicCalls.push(...extractDynamicCalls(text, relativePath))
  }

  const staticKeys = extractStaticKeys(
    await fs.readFile(staticKeysPath, 'utf8')
  )
  const literalKeys = [...new Set(occurrences.map(({ key }) => key))].sort(
    (a, b) => a.localeCompare(b)
  )
  const requiredKeys = [...new Set([...literalKeys, ...staticKeys])].sort(
    (a, b) => a.localeCompare(b)
  )
  const dynamicCallIds = [
    ...new Set(
      dynamicCalls.map(({ file, expression }) => `${file}::${expression}`)
    ),
  ].sort((a, b) => a.localeCompare(b))
  const parsedDynamicAllowlist = JSON.parse(
    await fs.readFile(dynamicAllowlistPath, 'utf8')
  )
  if (
    !Array.isArray(parsedDynamicAllowlist) ||
    parsedDynamicAllowlist.some((entry) => typeof entry !== 'string')
  ) {
    throw new Error('The dynamic i18n call allowlist must be a string array')
  }
  const dynamicAllowlist = [...new Set(parsedDynamicAllowlist)].sort((a, b) =>
    a.localeCompare(b)
  )
  if (dynamicAllowlist.length !== parsedDynamicAllowlist.length) {
    throw new Error('The dynamic i18n call allowlist contains duplicates')
  }
  const dynamicCallSet = new Set(dynamicCallIds)
  const dynamicAllowlistSet = new Set(dynamicAllowlist)
  const unexpectedDynamicCalls = dynamicCallIds.filter(
    (call) => !dynamicAllowlistSet.has(call)
  )
  const staleDynamicAllowlist = dynamicAllowlist.filter(
    (call) => !dynamicCallSet.has(call)
  )

  return {
    sourceFileCount: files.length,
    literalCallCount: occurrences.length,
    literalSourceKeyCount: literalKeys.length,
    staticAllowlistKeyCount: staticKeys.length,
    requiredKeyCount: requiredKeys.length,
    dynamicCallCount: dynamicCalls.length,
    dynamicUniqueCallCount: dynamicCallIds.length,
    dynamicAllowlistCount: dynamicAllowlist.length,
    unexpectedDynamicCalls,
    staleDynamicAllowlist,
    requiredKeys,
    occurrences,
    dynamicCalls,
  }
}
