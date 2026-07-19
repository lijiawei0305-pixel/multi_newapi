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
import { describe, it, expect } from 'vitest'

import type { UserSubscriptionRecord } from '../types'
import { computeUsageAlert } from './usage-alert'

/** 固定"现在"（epoch 秒），保证时间维用例确定性。 */
const NOW = 1_800_000_000
const DAY = 86400

const rec = (
  o: Partial<UserSubscriptionRecord['subscription']> & {
    next_reset_time?: number
  }
): UserSubscriptionRecord => ({
  subscription: {
    id: 1,
    user_id: 1,
    plan_id: 1,
    status: 'active',
    start_time: 0,
    end_time: 0,
    amount_total: 100,
    amount_used: 0,
    ...o,
  },
})

describe('computeUsageAlert · 用量维', () => {
  it('低于 80% → none', () => {
    expect(computeUsageAlert([rec({ amount_used: 50 })], NOW).level).toBe(
      'none'
    )
  })
  it('恰好 80% → warn', () => {
    const r = computeUsageAlert([rec({ amount_used: 80 })], NOW)
    expect(r.level).toBe('warn')
    expect(r.ratio).toBeCloseTo(0.8)
  })
  it('达到 100% → exhausted', () => {
    expect(computeUsageAlert([rec({ amount_used: 100 })], NOW).level).toBe(
      'exhausted'
    )
  })
  it('status=exhausted 即使 ratio 略低也算 exhausted', () => {
    expect(
      computeUsageAlert([rec({ status: 'exhausted', amount_used: 99 })], NOW)
        .level
    ).toBe('exhausted')
  })
  it('多订阅取最严重那条并带其 id', () => {
    const r = computeUsageAlert(
      [rec({ id: 1, amount_used: 50 }), rec({ id: 2, amount_used: 90 })],
      NOW
    )
    expect(r.level).toBe('warn')
    expect(r.subscriptionId).toBe(2)
  })
  it('status=expired 且无 end_time 信息 → none（时间维无从计算，用量维跳过）', () => {
    expect(
      computeUsageAlert([rec({ status: 'expired', amount_used: 100 })], NOW)
        .level
    ).toBe('none')
  })
  it('amount_total<=0（无限额）无用量档', () => {
    expect(
      computeUsageAlert([rec({ amount_total: 0, amount_used: 100 })], NOW).level
    ).toBe('none')
  })
  it('空/undefined → none', () => {
    expect(computeUsageAlert([], NOW).level).toBe('none')
    expect(computeUsageAlert(undefined, NOW).level).toBe('none')
  })
  it('exhausted 即使 ratio 更低也压过 ratio 更高的 warn（等级优先于比例）', () => {
    const r = computeUsageAlert(
      [
        rec({ id: 1, amount_used: 95 }), // ratio .95 → warn
        rec({ id: 2, status: 'exhausted', amount_used: 85 }), // ratio .85 → exhausted
      ],
      NOW
    )
    expect(r.level).toBe('exhausted')
    expect(r.subscriptionId).toBe(2)
    expect(r.ratio).toBeCloseTo(0.85)
  })
  it('同一档位内 ratio 更高者胜出，并带其 subscriptionId', () => {
    const r = computeUsageAlert(
      [rec({ id: 5, amount_used: 82 }), rec({ id: 7, amount_used: 95 })],
      NOW
    )
    expect(r.level).toBe('warn')
    expect(r.subscriptionId).toBe(7)
    expect(r.ratio).toBeCloseTo(0.95)
  })
  it('resetMarker 取自胜出记录的 next_reset_time', () => {
    const r = computeUsageAlert(
      [
        rec({ id: 1, amount_used: 50, next_reset_time: 111 }),
        rec({ id: 2, amount_used: 90, next_reset_time: 222 }),
      ],
      NOW
    )
    expect(r.subscriptionId).toBe(2)
    expect(r.resetMarker).toBe(222)
  })
  it('resetMarker 缺省（未提供 next_reset_time）时为 0', () => {
    expect(computeUsageAlert([rec({ amount_used: 90 })], NOW).resetMarker).toBe(
      0
    )
  })
})

describe('computeUsageAlert · 时间维（P3-RNW 到期提醒）', () => {
  it('距到期 3 天 → expiring，daysLeft=3，planId 随胜出记录带出', () => {
    const r = computeUsageAlert(
      [rec({ plan_id: 42, end_time: NOW + 3 * DAY })],
      NOW
    )
    expect(r.level).toBe('expiring')
    expect(r.daysLeft).toBe(3)
    expect(r.planId).toBe(42)
  })
  it('边界：恰好 7 天 → expiring(7)；7 天零 1 秒 → none', () => {
    expect(
      computeUsageAlert([rec({ end_time: NOW + 7 * DAY })], NOW).level
    ).toBe('expiring')
    expect(
      computeUsageAlert([rec({ end_time: NOW + 7 * DAY })], NOW).daysLeft
    ).toBe(7)
    expect(
      computeUsageAlert([rec({ end_time: NOW + 7 * DAY + 1 })], NOW).level
    ).toBe('none')
  })
  it('不足 1 天按 1 天计（daysLeft 最小为 1）', () => {
    expect(computeUsageAlert([rec({ end_time: NOW + 60 })], NOW).daysLeft).toBe(
      1
    )
  })
  it('已过期 1 天（status 仍 active，惰性未翻转）→ expired', () => {
    expect(
      computeUsageAlert([rec({ end_time: NOW - 1 * DAY })], NOW).level
    ).toBe('expired')
  })
  it('status=expired 且 end_time 在 7 天宽限内 → expired（不再被跳过）', () => {
    const r = computeUsageAlert(
      [rec({ status: 'expired', end_time: NOW - 2 * DAY, amount_used: 100 })],
      NOW
    )
    expect(r.level).toBe('expired')
  })
  it('过期超 7 天 → none（不再纠缠）', () => {
    expect(
      computeUsageAlert(
        [rec({ status: 'expired', end_time: NOW - 8 * DAY })],
        NOW
      ).level
    ).toBe('none')
  })
  it('无限额套餐（amount_total=0）时间维照常生效', () => {
    const r = computeUsageAlert(
      [rec({ amount_total: 0, end_time: NOW + 2 * DAY })],
      NOW
    )
    expect(r.level).toBe('expiring')
    expect(r.daysLeft).toBe(2)
  })
  it('严重度：exhausted 压过 expired；expired 压过 warn/expiring', () => {
    const a = computeUsageAlert(
      [
        rec({ id: 1, end_time: NOW - DAY }), // expired
        rec({ id: 2, status: 'exhausted', amount_used: 100 }), // exhausted
      ],
      NOW
    )
    expect(a.level).toBe('exhausted')
    expect(a.subscriptionId).toBe(2)
    const b = computeUsageAlert(
      [
        rec({ id: 3, amount_used: 95 }), // warn
        rec({ id: 4, end_time: NOW - DAY }), // expired
      ],
      NOW
    )
    expect(b.level).toBe('expired')
    expect(b.subscriptionId).toBe(4)
    const c = computeUsageAlert(
      [
        rec({ id: 5, end_time: NOW + 2 * DAY }), // expiring
        rec({ id: 6, end_time: NOW - DAY }), // expired
      ],
      NOW
    )
    expect(c.level).toBe('expired')
    expect(c.subscriptionId).toBe(6)
  })
  it('同一记录 用量 warn + 时间 expiring → 取更严重的 expiring', () => {
    const r = computeUsageAlert(
      [rec({ amount_used: 85, end_time: NOW + 2 * DAY })],
      NOW
    )
    expect(r.level).toBe('expiring')
  })
  it('end_time=0（无到期语义）不产生时间档', () => {
    expect(
      computeUsageAlert([rec({ end_time: 0, amount_used: 50 })], NOW).level
    ).toBe('none')
  })
})
