import { describe, it, expect } from 'vitest'
import { computeUsageAlert } from './usage-alert'
import type { UserSubscriptionRecord } from '../types'

const rec = (o: Partial<UserSubscriptionRecord['subscription']>): UserSubscriptionRecord =>
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
})
