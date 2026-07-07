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
import { computeUsageAlert } from './usage-alert'
import type { UserSubscriptionRecord } from '../types'

const rec = (
  o: Partial<UserSubscriptionRecord['subscription']> & { next_reset_time?: number }
): UserSubscriptionRecord =>
  ({ subscription: {
    id: 1, user_id: 1, plan_id: 1, status: 'active',
    start_time: 0, end_time: 0, amount_total: 100, amount_used: 0, ...o,
  } })

describe('computeUsageAlert', () => {
  it('低于 80% → none', () => {
    expect(computeUsageAlert([rec({ amount_used: 50 })]).level).toBe('none')
  })
  it('恰好 80% → warn', () => {
    const r = computeUsageAlert([rec({ amount_used: 80 })])
    expect(r.level).toBe('warn'); expect(r.ratio).toBeCloseTo(0.8)
  })
  it('达到 100% → exhausted', () => {
    expect(computeUsageAlert([rec({ amount_used: 100 })]).level).toBe('exhausted')
  })
  it('status=exhausted 即使 ratio 略低也算 exhausted', () => {
    expect(computeUsageAlert([rec({ status: 'exhausted', amount_used: 99 })]).level).toBe('exhausted')
  })
  it('多订阅取最严重那条并带其 id', () => {
    const r = computeUsageAlert([rec({ id: 1, amount_used: 50 }), rec({ id: 2, amount_used: 90 })])
    expect(r.level).toBe('warn'); expect(r.subscriptionId).toBe(2)
  })
  it('status=expired 跳过（时间到期非满额）', () => {
    expect(computeUsageAlert([rec({ status: 'expired', amount_used: 100 })]).level).toBe('none')
  })
  it('amount_total<=0（无限额）跳过', () => {
    expect(computeUsageAlert([rec({ amount_total: 0, amount_used: 100 })]).level).toBe('none')
  })
  it('空/undefined → none', () => {
    expect(computeUsageAlert([]).level).toBe('none')
    expect(computeUsageAlert(undefined).level).toBe('none')
  })
  it('exhausted 即使 ratio 更低也压过 ratio 更高的 warn（等级优先于比例）', () => {
    const r = computeUsageAlert([
      rec({ id: 1, amount_used: 95 }), // ratio .95 → warn
      rec({ id: 2, status: 'exhausted', amount_used: 85 }), // ratio .85 → exhausted
    ])
    expect(r.level).toBe('exhausted'); expect(r.subscriptionId).toBe(2); expect(r.ratio).toBeCloseTo(0.85)
  })
  it('同一档位内 ratio 更高者胜出，并带其 subscriptionId', () => {
    const r = computeUsageAlert([
      rec({ id: 5, amount_used: 82 }), // ratio .82 → warn
      rec({ id: 7, amount_used: 95 }), // ratio .95 → warn
    ])
    expect(r.level).toBe('warn'); expect(r.subscriptionId).toBe(7); expect(r.ratio).toBeCloseTo(0.95)
  })
  it('resetMarker 取自胜出记录的 next_reset_time', () => {
    const r = computeUsageAlert([
      rec({ id: 1, amount_used: 50, next_reset_time: 111 }), // none，不参与比较
      rec({ id: 2, amount_used: 90, next_reset_time: 222 }), // warn，胜出
    ])
    expect(r.subscriptionId).toBe(2); expect(r.resetMarker).toBe(222)
  })
  it('resetMarker 缺省（未提供 next_reset_time）时为 0', () => {
    expect(computeUsageAlert([rec({ amount_used: 90 })]).resetMarker).toBe(0)
  })
})
