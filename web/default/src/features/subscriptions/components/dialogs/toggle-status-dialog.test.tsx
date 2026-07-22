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
import { renderToStaticMarkup } from 'react-dom/server'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ToggleStatusDialog } from './toggle-status-dialog'

type ConfirmHandler = () => Promise<void>

const testState = vi.hoisted(() => ({
  response: {
    success: false,
    message: 'This plan is managed in Token Plans.',
  } as { success: boolean; message?: string },
  handleConfirm: null as ConfirmHandler | null,
  errorToast: vi.fn(),
  successToast: vi.fn(),
  setOpen: vi.fn(),
  triggerRefresh: vi.fn(),
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock('sonner', () => ({
  toast: {
    error: testState.errorToast,
    success: testState.successToast,
  },
}))

vi.mock('@/components/confirm-dialog', () => ({
  ConfirmDialog: (props: { handleConfirm: ConfirmHandler }) => {
    testState.handleConfirm = props.handleConfirm
    return <button type='button'>Confirm</button>
  },
}))

vi.mock('../../api', () => ({
  patchPlanStatus: vi.fn(async () => testState.response),
}))

vi.mock('../subscriptions-provider', () => ({
  useSubscriptions: () => ({
    open: 'toggle-status',
    setOpen: testState.setOpen,
    triggerRefresh: testState.triggerRefresh,
    currentRow: {
      managed_by: 'native_subscription',
      read_only: false,
      plan: { id: 1, enabled: true },
    },
  }),
}))

describe('toggle subscription plan status errors', () => {
  beforeEach(() => {
    testState.response = {
      success: false,
      message: 'This plan is managed in Token Plans.',
    }
    testState.handleConfirm = null
    testState.errorToast.mockClear()
    testState.successToast.mockClear()
    testState.setOpen.mockClear()
    testState.triggerRefresh.mockClear()
  })

  it('shows a localized backend rejection returned with HTTP 200', async () => {
    renderToStaticMarkup(<ToggleStatusDialog />)

    await testState.handleConfirm?.()

    expect(testState.errorToast).toHaveBeenCalledWith(
      'This plan is managed in Token Plans.'
    )
    expect(testState.successToast).not.toHaveBeenCalled()
    expect(testState.triggerRefresh).not.toHaveBeenCalled()
  })

  it('shows the localized fallback when the rejection has no message', async () => {
    testState.response = { success: false }
    renderToStaticMarkup(<ToggleStatusDialog />)

    await testState.handleConfirm?.()

    expect(testState.errorToast).toHaveBeenCalledWith('Operation failed')
  })
})
