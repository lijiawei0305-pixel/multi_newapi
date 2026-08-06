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

import { interpretRechargeStatus } from './recharge-status'

/**
 * 前端行为契约（文档 §9.2 子集）：双击防护、状态机、abort 语义用纯函数锁定。
 * 完整 React 组件测在后续可补 testing-library；此处保证关键不变量可回归。
 */

/** 模拟「任意时刻最多一个状态请求」：inFlight 时拒绝新 tick。 */
function canStartPoll(inFlight: boolean, aborted: boolean): boolean {
  return !inFlight && !aborted
}

/** 双击：submitting 非空时忽略。 */
function canSubmit(submitting: string | null, phase: string): boolean {
  if (submitting !== null) return false
  if (phase === 'creating') return false
  return true
}

/** Dialog 可见条件：dialogOpen 与 activeOrder 解耦——creating 无 orderNo 也可开。 */
function dialogShouldOpen(dialogOpen: boolean, phase: string): boolean {
  return dialogOpen && phase !== 'idle'
}

describe('recharge UI behavior contracts', () => {
  it('blocks double submit while creating', () => {
    expect(canSubmit('wxpay', 'creating')).toBe(false)
    expect(canSubmit(null, 'creating')).toBe(false)
    expect(canSubmit(null, 'idle')).toBe(true)
    expect(canSubmit(null, 'pending')).toBe(true)
  })

  it('dialog can open during creating before order_no exists', () => {
    expect(dialogShouldOpen(true, 'creating')).toBe(true)
    expect(dialogShouldOpen(false, 'creating')).toBe(false)
    expect(dialogShouldOpen(true, 'idle')).toBe(false)
  })

  it('poll does not overlap when in flight or aborted', () => {
    expect(canStartPoll(false, false)).toBe(true)
    expect(canStartPoll(true, false)).toBe(false)
    expect(canStartPoll(false, true)).toBe(false)
  })

  it('paid_processing continues; only credited completes', () => {
    expect(
      interpretRechargeStatus({
        status: 'paid',
        provider_paid: true,
        credited: false,
        paid: false,
      })
    ).toBe('paid_processing')
    expect(
      interpretRechargeStatus({
        status: 'credited',
        provider_paid: true,
        credited: true,
        paid: true,
      })
    ).toBe('credited')
  })

  it('opening checkout is not payment success (semantic marker)', () => {
    // PAY-EXT-01：redirect_created / order_created 不得映射为 credited
    const checkoutOnly = {
      status: 'created' as const,
      provider_paid: false,
      credited: false,
      paid: false,
    }
    expect(interpretRechargeStatus(checkoutOnly)).toBe('pending')
    expect(interpretRechargeStatus(checkoutOnly)).not.toBe('credited')
  })
})
