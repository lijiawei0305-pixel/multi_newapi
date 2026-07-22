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
import type { Row } from '@tanstack/react-table'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'

import type { PlanRecord } from '../types'
import { DataTableRowActions } from './data-table-row-actions'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock('./subscriptions-provider', () => ({
  useSubscriptions: () => ({
    setOpen: vi.fn(),
    setCurrentRow: vi.fn(),
    complianceConfirmed: true,
  }),
}))

const plan = {
  id: 1,
  title: 'Plan',
  price_amount: 10,
  currency: 'USD',
  duration_unit: 'month' as const,
  duration_value: 1,
  quota_reset_period: 'monthly' as const,
  enabled: true,
  sort_order: 1,
  allow_balance_pay: true,
  allow_wallet_overflow: true,
  max_purchase_per_user: 1,
  total_amount: 100,
}

function renderActions(record: PlanRecord): string {
  return renderToStaticMarkup(
    <DataTableRowActions row={{ original: record } as Row<PlanRecord>} />
  )
}

describe('subscription plan ownership actions', () => {
  it('disables edit and status actions for token-plan managed records', () => {
    const markup = renderActions({
      plan,
      managed_by: 'token_plan',
      read_only: true,
    })
    const actionButtons = markup
      .match(/<button\b[^>]*>/g)
      ?.filter((button) => /aria-label="(?:Edit|Disable)"/.test(button))

    expect(actionButtons).toHaveLength(2)
    expect(
      actionButtons?.every((button) => button.includes('disabled=""'))
    ).toBe(true)
  })

  it('keeps actions available for native subscription records', () => {
    const markup = renderActions({
      plan,
      managed_by: 'native_subscription',
      read_only: false,
    })
    const actionButtons = markup
      .match(/<button\b[^>]*>/g)
      ?.filter((button) => /aria-label="(?:Edit|Disable)"/.test(button))

    expect(actionButtons).toHaveLength(2)
    expect(
      actionButtons?.some((button) => button.includes('disabled=""'))
    ).toBe(false)
  })
})
