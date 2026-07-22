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

import { getSubscriptionPlanMutationError } from './plan-mutation'

describe('subscription plan mutation errors', () => {
  it('preserves the localized backend message for a rejected mutation', () => {
    expect(
      getSubscriptionPlanMutationError(
        {
          success: false,
          message: 'This plan is managed in Token Plans.',
        },
        'Operation failed'
      )
    ).toBe('This plan is managed in Token Plans.')
  })

  it('uses a localized fallback when the backend omits its message', () => {
    expect(
      getSubscriptionPlanMutationError(
        { success: false, message: '   ' },
        'Operation failed'
      )
    ).toBe('Operation failed')
    expect(
      getSubscriptionPlanMutationError(
        { success: true, message: 'ignored' },
        'Operation failed'
      )
    ).toBeNull()
  })
})
