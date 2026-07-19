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

import { auditI18nSource } from './i18n-source-audit.mjs'

const PROJECT_ROOT = process.cwd()
const LOCALES_ROOT = path.resolve(PROJECT_ROOT, 'src', 'i18n', 'locales')
const REPORTS_ROOT = path.join(LOCALES_ROOT, '_reports')
const UNCHANGED_VALUE_ALLOWLIST_PATH = path.join(
  PROJECT_ROOT,
  'scripts',
  'i18n-unchanged-value-allowlist.json'
)
const BASE_LOCALE = 'en'
const LOCALES = ['en', 'zh', 'fr', 'ja', 'ru', 'vi']
const OBFUSCATED_KEYS = [
  {
    runtime: ['footer', 'new' + 'api', 'projectAttributionSuffix'].join('.'),
    serialized: 'footer.new\\u0061pi.projectAttributionSuffix',
  },
]
const BRAND_AND_LITERAL_KEYS = new Set([
  'AI Proxy',
  'AIGC2D',
  'Alipay',
  'Alipay (PC Web)',
  'Alipay App ID',
  'Alibaba Cloud',
  'Anthropic',
  'API URL',
  'API2GPT',
  'AccessKey / SecretAccessKey',
  'App ID',
  'AZURE_OPENAI_ENDPOINT *',
  'Azure',
  'Baidu',
  'Baidu V2',
  'ChatGPT',
  'ChatGPT Subscription (Codex)',
  'Claude',
  'Client ID',
  'Client Secret',
  'Cloudflare',
  'Cohere',
  'Codex',
  'Coze',
  'CC Switch',
  'DeepSeek',
  'Dify',
  'Discord',
  'DoubaoVideo',
  'Doubao',
  'FastGPT',
  'Gemini',
  'Gemini Image 4K',
  'GitHub',
  'Grok',
  'Jimeng',
  'Jina',
  'JustSong',
  'Kling',
  'LingYiWanWu',
  'LinuxDO',
  'MiniMax',
  'Mistral',
  'MjProxy',
  'MjProxyPlus',
  'MokaAI',
  'Moonshot',
  'Moonshot AI',
  'New API',
  'New API &lt;noreply@example.com&gt;',
  'NewAPI',
  'OAuth Client Secret',
  'OhMyGPT',
  'Ollama',
  'One API',
  'OpenAI',
  'OpenAIMax',
  'OpenRouter',
  'PaLM',
  'Pancake',
  'Passkey',
  'Perplexity',
  'Playground',
  'QuantumNous',
  'Quota:',
  'Replicate',
  'SiliconFlow',
  'Sora',
  'Stripe',
  'Submodel',
  'SunoAPI',
  'Telegram',
  'Tencent',
  'TTFT P50',
  'TTFT P95',
  'TTFT P99',
  'Uptime Kuma',
  'Uptime Kuma URL',
  'Vertex AI',
  'Vidu',
  'VolcEngine',
  'Waffo Pancake Dashboard',
  'Waffo Pancake MoR',
  'Waffo',
  'WeChat',
  'WeChat:',
  'WeChat Pay',
  'WeChat Pay (Native)',
  'Webhook URL',
  'Webhook URL:',
  'WeDream Core',
  'Well-Known URL',
  'Worker URL',
  'Xinference',
  'Xunfei',
  'xAI',
  'Zhipu',
  'Zhipu V4',
  'Zhipu AI',
  'Zoom',
  'ByteDance',
  '"default": "us-central1", "claude-3-5-sonnet-20240620": "europe-west1"',
  'edit_this',
  'footer.columns.related.links.midjourney',
  'footer.columns.related.links.newApiKeyTool',
  'my-status',
  'new-api-key-tool',
  'price_xxx',
  'whsec_xxx',
  '_copy',
  'ms',
  'vip',
  '{{method}} {{route}}',
])

function stableStringify(value) {
  let text = JSON.stringify(value, null, 2)
  for (const key of OBFUSCATED_KEYS) {
    text = text.replaceAll(`"${key.runtime}":`, `"${key.serialized}":`)
  }
  return `${text}\n`
}

function placeholders(value) {
  if (typeof value !== 'string') return []
  return [...value.matchAll(/{{\s*([^{}]+?)\s*}}/g)]
    .map((match) => match[1])
    .sort((a, b) => a.localeCompare(b))
}

function isSafeUnchangedValue(english) {
  if (typeof english !== 'string') return true
  const text = english.trim()
  const containsExampleMarker = /^e\.g\.,?(?:\s|$)/i.test(text)
  if (
    BRAND_AND_LITERAL_KEYS.has(text) ||
    (!/[A-Za-z]{2,}/.test(text) && !containsExampleMarker) ||
    /^https?:\/\//.test(text) ||
    /^\/[\w/-]+/.test(text) ||
    /^[\w.-]+@[\w.-]+$/.test(text) ||
    /^smtp\./i.test(text) ||
    /^socks5:/i.test(text) ||
    text.startsWith('org-') ||
    /^gpt-/i.test(text) ||
    text.startsWith('checkout.') ||
    text.startsWith('footer.') ||
    /\.(?:png|mp4)$/i.test(text) ||
    /^[A-Z0-9_ *./:-]+$/.test(text) ||
    (/^\{[\s\S]*\}$/.test(text) && !text.includes('{{')) ||
    (/^\[[\s\S]*\]$/.test(text) && !text.includes('{{')) ||
    text.includes('&#10;')
  ) {
    return true
  }
  return false
}

async function main() {
  const catalogs = {}
  for (const locale of LOCALES) {
    const localePath = path.join(LOCALES_ROOT, `${locale}.json`)
    const parsed = JSON.parse(await fs.readFile(localePath, 'utf8'))
    if (!parsed.translation || typeof parsed.translation !== 'object') {
      throw new Error(`${locale}.json does not contain a translation object`)
    }
    catalogs[locale] = parsed
  }

  const sourceAudit = await auditI18nSource(PROJECT_ROOT)
  const unchangedValueAllowlist = JSON.parse(
    await fs.readFile(UNCHANGED_VALUE_ALLOWLIST_PATH, 'utf8')
  )
  if (
    !unchangedValueAllowlist ||
    typeof unchangedValueAllowlist !== 'object' ||
    Array.isArray(unchangedValueAllowlist)
  ) {
    throw new Error('The unchanged-value i18n allowlist must be an object')
  }
  for (const locale of LOCALES.filter((locale) => locale !== BASE_LOCALE)) {
    const entries = unchangedValueAllowlist[locale]
    if (
      !Array.isArray(entries) ||
      entries.some((entry) => typeof entry !== 'string') ||
      new Set(entries).size !== entries.length
    ) {
      throw new Error(
        `The unchanged-value i18n allowlist for ${locale} must be a duplicate-free string array`
      )
    }
  }
  const unexpectedAllowlistLocales = Object.keys(
    unchangedValueAllowlist
  ).filter((locale) => locale === BASE_LOCALE || !LOCALES.includes(locale))
  if (unexpectedAllowlistLocales.length > 0) {
    throw new Error(
      `Unexpected unchanged-value allowlist locales: ${unexpectedAllowlistLocales.join(', ')}`
    )
  }
  const baseTranslation = catalogs[BASE_LOCALE].translation
  const baseKeys = Object.keys(baseTranslation)
  const baseKeySet = new Set(baseKeys)
  const report = {
    base: `${BASE_LOCALE}.json`,
    source: {
      sourceFileCount: sourceAudit.sourceFileCount,
      literalCallCount: sourceAudit.literalCallCount,
      literalSourceKeyCount: sourceAudit.literalSourceKeyCount,
      staticAllowlistKeyCount: sourceAudit.staticAllowlistKeyCount,
      requiredKeyCount: sourceAudit.requiredKeyCount,
      dynamicCallCount: sourceAudit.dynamicCallCount,
      dynamicUniqueCallCount: sourceAudit.dynamicUniqueCallCount,
      dynamicAllowlistCount: sourceAudit.dynamicAllowlistCount,
      unexpectedDynamicCalls: sourceAudit.unexpectedDynamicCalls,
      staleDynamicAllowlist: sourceAudit.staleDynamicAllowlist,
    },
    locales: {},
  }
  let hasErrors =
    sourceAudit.unexpectedDynamicCalls.length > 0 ||
    sourceAudit.staleDynamicAllowlist.length > 0

  for (const locale of LOCALES) {
    const translation = catalogs[locale].translation
    const keys = Object.keys(translation)
    const keySet = new Set(keys)
    const missingSourceKeys = sourceAudit.requiredKeys.filter(
      (key) => !keySet.has(key)
    )
    const missingBaseKeys = baseKeys.filter((key) => !keySet.has(key))
    const extraKeys = keys.filter((key) => !baseKeySet.has(key))
    const placeholderMismatches = baseKeys.filter(
      (key) =>
        keySet.has(key) &&
        JSON.stringify(placeholders(translation[key])) !==
          JSON.stringify(placeholders(baseTranslation[key]))
    )
    const allowedUnchangedKeys = new Set(
      locale === BASE_LOCALE ? [] : unchangedValueAllowlist[locale]
    )
    const untranslatedKeys =
      locale === BASE_LOCALE
        ? []
        : baseKeys.filter(
            (key) =>
              keySet.has(key) &&
              translation[key] === baseTranslation[key] &&
              !isSafeUnchangedValue(baseTranslation[key]) &&
              !allowedUnchangedKeys.has(key)
          )
    const staleUnchangedValueAllowlist =
      locale === BASE_LOCALE
        ? []
        : [...allowedUnchangedKeys].filter(
            (key) =>
              !keySet.has(key) ||
              !baseKeySet.has(key) ||
              translation[key] !== baseTranslation[key] ||
              isSafeUnchangedValue(baseTranslation[key])
          )

    report.locales[locale] = {
      keyCount: keys.length,
      unchangedValueAllowlistCount: allowedUnchangedKeys.size,
      missingSourceKeys,
      missingBaseKeys,
      extraKeys,
      placeholderMismatches,
      untranslatedKeys,
      staleUnchangedValueAllowlist,
    }
    if (
      missingSourceKeys.length > 0 ||
      missingBaseKeys.length > 0 ||
      extraKeys.length > 0 ||
      placeholderMismatches.length > 0 ||
      untranslatedKeys.length > 0 ||
      staleUnchangedValueAllowlist.length > 0
    ) {
      hasErrors = true
    }
  }

  await fs.mkdir(REPORTS_ROOT, { recursive: true })
  const reportPath = path.join(REPORTS_ROOT, '_sync-report.json')
  await fs.writeFile(reportPath, stableStringify(report), 'utf8')

  if (hasErrors) {
    throw new Error(`i18n catalogs are out of sync; see ${reportPath}`)
  }

  for (const locale of LOCALES) {
    const translation = catalogs[locale].translation
    catalogs[locale].translation = Object.fromEntries(
      baseKeys.map((key) => [key, translation[key]])
    )
    await fs.writeFile(
      path.join(LOCALES_ROOT, `${locale}.json`),
      stableStringify(catalogs[locale]),
      'utf8'
    )
  }

  console.log(
    `i18n sync passed: ${sourceAudit.requiredKeyCount} source/static keys, ${baseKeys.length} catalog keys, ${LOCALES.length} locales. Report: ${reportPath}`
  )
}

main().catch((error) => {
  console.error(error)
  process.exitCode = 1
})
