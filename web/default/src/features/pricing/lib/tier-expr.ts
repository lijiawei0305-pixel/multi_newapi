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
import { BILLING_CACHE_VAR_MAP } from './billing-expr'
import { evaluateSafeBillingExpression } from './safe-billing-evaluator'

export const CACHE_MODE_TIMED = 'timed'
export const CACHE_MODE_GENERIC = 'generic'
export type CacheMode = typeof CACHE_MODE_TIMED | typeof CACHE_MODE_GENERIC

export type TierConditionInput = {
  var: 'p' | 'c' | 'len'
  op: '<' | '<=' | '>' | '>='
  value: number | string
}

export type VisualTier = {
  label: string
  conditions: TierConditionInput[]
  input_unit_cost: number
  output_unit_cost: number
  cache_mode: CacheMode
  cache_read_unit_cost?: number
  cache_create_unit_cost?: number
  cache_create_1h_unit_cost?: number
  image_unit_cost?: number
  image_output_unit_cost?: number
  audio_input_unit_cost?: number
  audio_output_unit_cost?: number
  [field: string]: unknown
}

export type VisualConfig = {
  tiers: VisualTier[]
  version?: 1
}

export function getTierCacheMode(
  tier: Partial<VisualTier> | null | undefined
): CacheMode {
  if (tier?.cache_mode === CACHE_MODE_TIMED) return CACHE_MODE_TIMED
  if (tier?.cache_mode === CACHE_MODE_GENERIC) return CACHE_MODE_GENERIC
  return Number(tier?.cache_create_1h_unit_cost) > 0
    ? CACHE_MODE_TIMED
    : CACHE_MODE_GENERIC
}

export function normalizeVisualTier(
  tier: Partial<VisualTier> = {}
): VisualTier {
  return {
    label: tier.label ?? '',
    input_unit_cost: Number(tier.input_unit_cost) || 0,
    output_unit_cost: Number(tier.output_unit_cost) || 0,
    cache_mode: getTierCacheMode(tier),
    conditions: Array.isArray(tier.conditions) ? tier.conditions : [],
    ...tier,
    cache_read_unit_cost: Number(tier.cache_read_unit_cost) || 0,
    cache_create_unit_cost: Number(tier.cache_create_unit_cost) || 0,
    cache_create_1h_unit_cost: Number(tier.cache_create_1h_unit_cost) || 0,
    image_unit_cost: Number(tier.image_unit_cost) || 0,
    image_output_unit_cost: Number(tier.image_output_unit_cost) || 0,
    audio_input_unit_cost: Number(tier.audio_input_unit_cost) || 0,
    audio_output_unit_cost: Number(tier.audio_output_unit_cost) || 0,
  }
}

export function createDefaultVisualConfig(): VisualConfig {
  return {
    tiers: [
      normalizeVisualTier({
        conditions: [],
        input_unit_cost: 0,
        output_unit_cost: 0,
        label: 'base',
        cache_mode: CACHE_MODE_GENERIC,
      }),
    ],
  }
}

export function normalizeVisualConfig(
  config: VisualConfig | null | undefined
): VisualConfig {
  if (!config || !Array.isArray(config.tiers) || config.tiers.length === 0) {
    return createDefaultVisualConfig()
  }
  return {
    ...config,
    tiers: config.tiers.map((tier) => normalizeVisualTier(tier)),
  }
}

function buildConditionStr(conditions: TierConditionInput[]): string {
  if (!conditions || conditions.length === 0) return ''
  return conditions
    .map((condition) => {
      const value = Number(condition.value)
      if (
        !['p', 'c', 'len'].includes(condition.var) ||
        !['<', '<=', '>', '>='].includes(condition.op) ||
        condition.value === '' ||
        !Number.isFinite(value)
      ) {
        throw new Error('Invalid visual tier configuration')
      }
      return `${condition.var} ${condition.op} ${value}`
    })
    .join(' && ')
}

function buildTierBodyExpr(tier: VisualTier): string {
  const parts: string[] = []
  const ic = Number(tier.input_unit_cost)
  const oc = Number(tier.output_unit_cost)
  if (!Number.isFinite(ic) || !Number.isFinite(oc)) {
    throw new Error('Invalid visual tier configuration')
  }
  parts.push(`p * ${ic}`)
  parts.push(`c * ${oc}`)
  for (const cv of BILLING_CACHE_VAR_MAP) {
    const rawValue = (tier as Record<string, unknown>)[cv.field]
    const v = rawValue == null || rawValue === '' ? 0 : Number(rawValue)
    if (!Number.isFinite(v)) {
      throw new Error('Invalid visual tier configuration')
    }
    if (v !== 0) parts.push(`${cv.exprVar} * ${v}`)
  }
  return parts.join(' + ')
}

export function generateExprFromVisualConfig(
  config: VisualConfig | null | undefined
): string {
  if (!config || !config.tiers || config.tiers.length === 0) {
    return 'p * 0 + c * 0'
  }
  const tiers = config.tiers

  if (tiers.length > 1) {
    for (let index = 0; index < tiers.length - 1; index += 1) {
      if (!buildConditionStr(tiers[index].conditions)) {
        throw new Error('Invalid visual tier configuration')
      }
    }
    if (buildConditionStr(tiers.at(-1)?.conditions ?? [])) {
      throw new Error('Invalid visual tier configuration')
    }
  }

  if (tiers.length === 1) {
    const tier = tiers[0]
    const label = tier.label || 'default'
    const body = `tier(${JSON.stringify(label)}, ${buildTierBodyExpr(tier)})`
    const cond = buildConditionStr(tier.conditions)
    if (cond) {
      return applyVisualExpressionVersion(
        config,
        `${cond} ? ${body} : p * 0 + c * 0`
      )
    }
    return applyVisualExpressionVersion(config, body)
  }

  const parts: string[] = []
  for (let i = 0; i < tiers.length; i++) {
    const tier = tiers[i]
    const label = tier.label || `tier_${i + 1}`
    const body = `tier(${JSON.stringify(label)}, ${buildTierBodyExpr(tier)})`
    const cond = buildConditionStr(tier.conditions)

    if (i < tiers.length - 1 && cond) {
      parts.push(`${cond} ? ${body}`)
    } else {
      parts.push(body)
    }
  }
  return applyVisualExpressionVersion(config, parts.join(' : '))
}

function applyVisualExpressionVersion(
  config: VisualConfig,
  expression: string
): string {
  return config.version === 1 ? `v1:${expression}` : expression
}

export function tryParseVisualConfig(
  exprStr: string | null | undefined
): VisualConfig | null {
  if (!exprStr) return null
  try {
    let body = exprStr
    let version: 1 | undefined
    const versionMatch = body.match(/^v(\d+):([\s\S]*)$/)
    if (versionMatch) {
      if (versionMatch[1] !== '1') return null
      version = 1
      body = versionMatch[2]
    }
    const cacheVarNames = BILLING_CACHE_VAR_MAP.map((cv) => cv.exprVar)
    const numberPattern = '[+-]?(?:\\d+(?:\\.\\d*)?|\\.\\d+)(?:[eE][+-]?\\d+)?'
    const optCacheStr = cacheVarNames
      .map((v) => `(?:\\s*\\+\\s*${v}\\s*\\*\\s*(${numberPattern}))?`)
      .join('')

    const bodyPat = `p\\s*\\*\\s*(${numberPattern})\\s*\\+\\s*c\\s*\\*\\s*(${numberPattern})${optCacheStr}`
    const labelPattern = '"((?:\\\\.|[^"\\\\])*)"'

    const singleRe = new RegExp(`^tier\\(${labelPattern},\\s*${bodyPat}\\)$`)
    const simple = body.match(singleRe)
    if (simple) {
      const tier: Record<string, unknown> = {
        conditions: [],
        input_unit_cost: parseFiniteVisualNumber(simple[2]),
        output_unit_cost: parseFiniteVisualNumber(simple[3]),
        label: decodeTierLabel(simple[1]),
      }
      BILLING_CACHE_VAR_MAP.forEach((cv, i) => {
        const val = simple[4 + i]
        if (val != null) tier[cv.field] = parseFiniteVisualNumber(val)
      })
      const config = normalizeVisualConfig({
        tiers: [normalizeVisualTier(tier as Partial<VisualTier>)],
        version,
      })
      if (!visualExpressionMatches(exprStr, config)) return null
      return config
    }

    const condGroup =
      `((?:(?:p|c|len)\\s*(?:<|<=|>|>=)\\s*${numberPattern})` +
      `(?:\\s*&&\\s*(?:p|c|len)\\s*(?:<|<=|>|>=)\\s*${numberPattern})*)`
    const tierRe = new RegExp(
      `(?:${condGroup}\\s*\\?\\s*)?tier\\(${labelPattern},\\s*${bodyPat}\\)`,
      'g'
    )
    const tiers: VisualTier[] = []
    let match: RegExpExecArray | null
    while ((match = tierRe.exec(body)) !== null) {
      const condStr = match[1] || ''
      const conditions: TierConditionInput[] = []
      if (condStr) {
        for (const cp of condStr.split(/\s*&&\s*/)) {
          const cm = cp
            .trim()
            .match(
              new RegExp(`^(p|c|len)\\s*(<|<=|>|>=)\\s*(${numberPattern})$`)
            )
          if (cm) {
            conditions.push({
              var: cm[1] as TierConditionInput['var'],
              op: cm[2] as TierConditionInput['op'],
              value: parseFiniteVisualNumber(cm[3]),
            })
          }
        }
      }
      const tier: Record<string, unknown> = {
        conditions,
        input_unit_cost: parseFiniteVisualNumber(match[3]),
        output_unit_cost: parseFiniteVisualNumber(match[4]),
        label: decodeTierLabel(match[2]),
      }
      const m = match
      BILLING_CACHE_VAR_MAP.forEach((cv, i) => {
        const val = m[5 + i]
        if (val != null) tier[cv.field] = parseFiniteVisualNumber(val)
      })
      tiers.push(normalizeVisualTier(tier as Partial<VisualTier>))
    }
    if (tiers.length === 0) return null

    const cfg = normalizeVisualConfig({ tiers, version })
    if (!visualExpressionMatches(exprStr, cfg)) return null
    return cfg
  } catch {
    return null
  }
}

export function createInitialVisualEditorState(exprStr: string): {
  mode: 'visual' | 'raw'
  visualConfig: VisualConfig | null
} {
  const visualConfig = tryParseVisualConfig(exprStr)
  if (visualConfig) return { mode: 'visual', visualConfig }
  if (exprStr) return { mode: 'raw', visualConfig: null }
  return { mode: 'visual', visualConfig: createDefaultVisualConfig() }
}

function decodeTierLabel(encodedBody: string): string {
  const value: unknown = JSON.parse(`"${encodedBody}"`)
  if (typeof value !== 'string') throw new Error('Tier label is not a string')
  return value
}

function parseFiniteVisualNumber(value: string): number {
  const parsed = Number(value)
  if (!Number.isFinite(parsed)) {
    throw new Error('Invalid visual tier configuration')
  }
  return parsed
}

function visualExpressionMatches(
  expression: string,
  config: VisualConfig
): boolean {
  const regenerated = generateExprFromVisualConfig(config)
  return (
    regenerated.replaceAll(/\s+/g, '') === expression.replaceAll(/\s+/g, '')
  )
}

// ---------------------------------------------------------------------------
// Local cost evaluator (for the estimator preview)
// ---------------------------------------------------------------------------

const ESTIMATOR_VARS = [
  { var: 'cr', stateKey: 'cacheReadTokens' },
  { var: 'cc', stateKey: 'cacheCreateTokens' },
  { var: 'cc1h', stateKey: 'cacheCreate1hTokens' },
  { var: 'img', stateKey: 'imageTokens' },
  { var: 'img_o', stateKey: 'imageOutputTokens' },
  { var: 'ai', stateKey: 'audioInputTokens' },
  { var: 'ao', stateKey: 'audioOutputTokens' },
] as const

export type ExtraTokenValues = Record<
  (typeof ESTIMATOR_VARS)[number]['stateKey'],
  number
>

export type EvalResult = {
  cost: number
  matchedTier: string
  error: string | null
}

export function evalExprLocally(
  exprStr: string,
  billableInputTokens: number,
  billableOutputTokens: number,
  fullInputLength: number,
  extraTokenValues: ExtraTokenValues
): EvalResult {
  try {
    if (!exprStr || !exprStr.trim()) {
      return { cost: 0, matchedTier: '', error: null }
    }
    const env: Record<string, number> = {
      p: billableInputTokens,
      c: billableOutputTokens,
      len: fullInputLength,
    }
    for (const field of ESTIMATOR_VARS) {
      env[field.var] = extraTokenValues[field.stateKey] || 0
    }
    const result = evaluateSafeBillingExpression(exprStr, env)
    return { cost: result.value, matchedTier: result.matchedTier, error: null }
  } catch (e) {
    const message = e instanceof Error ? e.message : String(e)
    return { cost: 0, matchedTier: '', error: message }
  }
}

export function convertRawBillingCostToQuota(
  rawCost: number,
  quotaPerUnit: number
): number {
  if (!Number.isFinite(rawCost) || !Number.isFinite(quotaPerUnit)) {
    throw new Error('Billing cost conversion requires finite values')
  }
  return (rawCost / 1_000_000) * quotaPerUnit
}

export function exprUsesExtraVars(exprStr: string): boolean {
  if (!exprStr) return false
  const varNames = ESTIMATOR_VARS.map((f) => f.var).join('|')
  return new RegExp(`\\b(${varNames})\\b`).test(exprStr)
}

export const ESTIMATOR_EXTRA_FIELDS = ESTIMATOR_VARS
