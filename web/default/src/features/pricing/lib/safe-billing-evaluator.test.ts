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
import { describe, expect, it } from 'vitest'

import {
  convertRawBillingCostToQuota,
  createInitialVisualEditorState,
  evalExprLocally,
  generateExprFromVisualConfig,
  normalizeVisualTier,
  tryParseVisualConfig,
  type ExtraTokenValues,
} from './tier-expr'

const noExtras: ExtraTokenValues = {
  cacheReadTokens: 0,
  cacheCreateTokens: 0,
  cacheCreate1hTokens: 0,
  imageTokens: 0,
  imageOutputTokens: 0,
  audioInputTokens: 0,
  audioOutputTokens: 0,
}

describe('safe billing expression evaluator', () => {
  it('evaluates supported arithmetic, functions, versions, and one tier branch', () => {
    expect(
      evalExprLocally(
        'v1:len <= 20 ? tier("short", max(p * 2, c * 3)) : tier("long", floor(p / 2))',
        10,
        2,
        12,
        noExtras
      )
    ).toEqual({ cost: 20, matchedTier: 'short', error: null })

    expect(
      evalExprLocally(
        'len < 5 ? tier("short", 1) : tier("long", ceil((p + c) / 3))',
        6,
        2,
        8,
        noExtras
      )
    ).toEqual({ cost: 3, matchedTier: 'long', error: null })
  })

  it('preserves all supported token variables', () => {
    const result = evalExprLocally(
      'tier("media", p + c + cr + cc + cc1h + img + img_o + ai + ao)',
      1,
      2,
      13,
      {
        cacheReadTokens: 3,
        cacheCreateTokens: 4,
        cacheCreate1hTokens: 5,
        imageTokens: 6,
        imageOutputTokens: 7,
        audioInputTokens: 8,
        audioOutputTokens: 9,
      }
    )

    expect(result).toEqual({ cost: 45, matchedTier: 'media', error: null })
  })

  it('rejects executable JavaScript without invoking it', () => {
    const globalState = globalThis as typeof globalThis & {
      billingEvaluatorOwned?: boolean
    }
    delete globalState.billingEvaluatorOwned
    const payloads = [
      'globalThis.billingEvaluatorOwned = true',
      'tier.constructor("globalThis.billingEvaluatorOwned=true")()',
      'p; globalThis.billingEvaluatorOwned = true',
      '({}).constructor.constructor("globalThis.billingEvaluatorOwned=true")()',
      'tier("x", (() => 1)())',
    ]

    for (const payload of payloads) {
      const result = evalExprLocally(payload, 1, 1, 1, noExtras)
      expect(result.error).not.toBeNull()
      expect(globalState.billingEvaluatorOwned).toBeUndefined()
    }
  })

  it('rejects unknown capabilities, invalid arithmetic, and excessive depth', () => {
    for (const expression of [
      'param("secret")',
      'unknown + 1',
      'p / 0',
      `${'('.repeat(140)}p${')'.repeat(140)}`,
      'v2:p + c',
      ' p % 2',
      ' v1:p + c',
      'tier("unsafe", 9007199254740993 - 9007199254740992)',
      'true ? tier("ok", 1) : unknown()',
      'true ? tier("ok", 1) : tier("bad")',
      'true ? tier("ok", 1) : "wrong type"',
    ]) {
      expect(
        evalExprLocally(expression, 1, 1, 1, noExtras).error
      ).not.toBeNull()
    }
  })

  it('quotes visual tier labels as data rather than expression source', () => {
    const expression = generateExprFromVisualConfig({
      tiers: [
        normalizeVisualTier({
          label: 'base", globalThis.billingEvaluatorOwned = true, "',
          input_unit_cost: 2,
          output_unit_cost: 3,
        }),
      ],
    })

    expect(expression).toContain('\\"')
    expect(tryParseVisualConfig(expression)?.tiers[0].label).toBe(
      'base", globalThis.billingEvaluatorOwned = true, "'
    )
    expect(evalExprLocally(expression, 2, 3, 2, noExtras)).toEqual({
      cost: 13,
      matchedTier: 'base", globalThis.billingEvaluatorOwned = true, "',
      error: null,
    })
  })

  it('uses caller-supplied normalized p, c, and len values exactly', () => {
    expect(
      evalExprLocally(
        'tier("normalized", p + c * 10 + len * 100 + cr * 1000)',
        2,
        3,
        4,
        { ...noExtras, cacheReadTokens: 5 }
      )
    ).toEqual({ cost: 5432, matchedTier: 'normalized', error: null })
  })

  it('converts raw expression output to backend quota units', () => {
    expect(convertRawBillingCostToQuota(2_500_000, 500_000)).toBe(1_250_000)
    expect(convertRawBillingCostToQuota(2_500_000, 0)).toBe(0)
  })

  it('only enables visual mode for an equivalent finite expression', () => {
    const canonical = 'v1:tier("base\\\"tier", p * 2.5 + c * 15 + cr * 0.25)'
    const parsed = tryParseVisualConfig(canonical)
    expect(parsed).not.toBeNull()
    expect(generateExprFromVisualConfig(parsed)).toBe(canonical)

    for (const expression of [
      'tier("base", p * 1+2 + c * 3)',
      'tier("base", p * 1-2 + c * 3)',
      'tier("base", p * 1e999 + c * 3)',
      'v2:tier("base", p * 1 + c * 3)',
    ]) {
      expect(tryParseVisualConfig(expression)).toBeNull()
      expect(createInitialVisualEditorState(expression)).toEqual({
        mode: 'raw',
        visualConfig: null,
      })
    }
  })

  it('fails closed for structurally invalid visual tier chains', () => {
    const fallback = normalizeVisualTier({
      label: 'fallback',
      input_unit_cost: 2,
      output_unit_cost: 3,
    })
    expect(() =>
      generateExprFromVisualConfig({
        tiers: [
          normalizeVisualTier({
            label: 'missing_condition',
            input_unit_cost: 1,
            output_unit_cost: 1,
          }),
          fallback,
        ],
      })
    ).toThrow('Invalid visual tier configuration')

    expect(() =>
      generateExprFromVisualConfig({
        tiers: [
          normalizeVisualTier({
            label: 'first',
            conditions: [{ var: 'len', op: '<=', value: 100 }],
            input_unit_cost: 1,
            output_unit_cost: 1,
          }),
          normalizeVisualTier({
            ...fallback,
            conditions: [{ var: 'len', op: '>', value: 100 }],
          }),
        ],
      })
    ).toThrow('Invalid visual tier configuration')
  })
})
