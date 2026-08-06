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

describe('interpretRechargeStatus (PAY-STA-01)', () => {
  it('treats intermediate paid as paid_processing, not credited', () => {
    expect(
      interpretRechargeStatus({
        status: 'paid',
        provider_paid: true,
        credited: false,
        paid: false,
      })
    ).toBe('paid_processing')
  })

  it('completes only on credited', () => {
    expect(
      interpretRechargeStatus({
        status: 'credited',
        provider_paid: true,
        credited: true,
        paid: true,
      })
    ).toBe('credited')
  })

  it('uses legacy paid=true as credited for compatibility', () => {
    expect(
      interpretRechargeStatus({
        status: 'credited',
        paid: true,
      })
    ).toBe('credited')
  })

  it('keeps created as pending', () => {
    expect(
      interpretRechargeStatus({
        status: 'created',
        provider_paid: false,
        credited: false,
        paid: false,
      })
    ).toBe('pending')
  })

  it('marks failed', () => {
    expect(interpretRechargeStatus({ status: 'failed' })).toBe('failed')
  })

  it('marks expired when expires_at passed and not provider-paid', () => {
    expect(
      interpretRechargeStatus(
        {
          status: 'created',
          provider_paid: false,
          expires_at: '2020-01-01T00:00:00Z',
        },
        Date.parse('2026-08-06T00:00:00Z')
      )
    ).toBe('expired')
  })

  it('does not expire when provider already paid', () => {
    expect(
      interpretRechargeStatus(
        {
          status: 'paid',
          provider_paid: true,
          credited: false,
          paid: false,
          expires_at: '2020-01-01T00:00:00Z',
        },
        Date.parse('2026-08-06T00:00:00Z')
      )
    ).toBe('paid_processing')
  })
})
