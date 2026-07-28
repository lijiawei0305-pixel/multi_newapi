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
import type { ReactNode } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'

import type { WithdrawalAction } from '../types'
import { WithdrawalActionDialog } from './withdrawal-action-dialog'

const dialogState = vi.hoisted(() => ({
  action: 'approve',
  currentRow: {
    id: 321,
    tenant_id: 9,
    agent_name: 'Agent Nine',
    amount_cny: 1234.5,
    status: 'pending',
    payout_method: 'bank',
    payout_account: '6222021234567890123',
    payout_name: 'Alice Chen',
    payout_bank: 'Example Main Branch',
    created_at: '2026-07-28T08:30:00+08:00',
    reviewed_at: '2026-07-28T09:45:00+08:00',
  },
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
  }),
}))

vi.mock('sonner', () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}))

vi.mock('@/components/confirm-dialog', () => ({
  ConfirmDialog: (props: {
    title: ReactNode
    desc: ReactNode
    children?: ReactNode
    className?: string
  }) => (
    <section data-dialog-class={props.className}>
      <h1>{props.title}</h1>
      <div>{props.desc}</div>
      {props.children}
    </section>
  ),
}))

vi.mock('./withdrawals-provider', () => ({
  useWithdrawals: () => ({
    action: dialogState.action,
    currentRow: dialogState.currentRow,
    closeAction: vi.fn(),
    triggerRefresh: vi.fn(),
  }),
}))

describe('withdrawal action confirmation details', () => {
  for (const action of ['approve', 'reject', 'mark-paid'] as const) {
    it(`shows the complete payout target for ${action}`, () => {
      dialogState.action = action
      const markup = renderToStaticMarkup(<WithdrawalActionDialog />)

      expect(markup).toContain('data-testid="withdrawal-review-details"')
      expect(markup).toContain('data-testid="withdrawal-review-id"')
      expect(markup).toContain('#321')
      expect(markup).toContain('2026-07-28 08:30')
      expect(markup).toContain('2026-07-28 09:45')
      expect(markup).toContain('¥1234.50')
      expect(markup).toContain('Bank Card')
      expect(markup).toContain('6222021234567890123')
      expect(markup).toContain('Alice Chen')
      expect(markup).toContain('Example Main Branch')
      expect(markup).toContain('max-h-[calc(100dvh_-_2rem)]')
      expect(markup).toContain('data-[size=default]:max-w-[calc(100%_-_2rem)]')
    })
  }

  it('keeps the full account visible instead of truncating it', () => {
    dialogState.action = 'approve' satisfies WithdrawalAction
    const markup = renderToStaticMarkup(<WithdrawalActionDialog />)

    expect(markup).toContain('break-all')
    expect(markup).toContain('select-all')
    expect(markup).not.toContain('truncate')
  })
})
