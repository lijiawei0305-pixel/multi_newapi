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
import { beforeEach, describe, expect, it, vi } from 'vitest'

import {
  createPlan,
  getAdminPlansOrThrow,
  patchPlanStatus,
  updatePlan,
} from './api'

const apiGet = vi.hoisted(() => vi.fn())
const apiPost = vi.hoisted(() => vi.fn())
const apiPut = vi.hoisted(() => vi.fn())
const apiPatch = vi.hoisted(() => vi.fn())

vi.mock('@/lib/api', () => ({
  api: {
    get: apiGet,
    post: apiPost,
    put: apiPut,
    patch: apiPatch,
  },
}))

describe('admin subscription plan list', () => {
  beforeEach(() => {
    apiGet.mockReset()
    apiPost.mockReset()
    apiPut.mockReset()
    apiPatch.mockReset()
  })

  it('rejects an HTTP 200 response whose API envelope reports failure', async () => {
    apiGet.mockResolvedValue({
      data: {
        success: false,
        message: 'Plan ownership is temporarily unavailable.',
        error_code: 'PLAN_OWNERSHIP_UNAVAILABLE',
      },
    })

    await expect(getAdminPlansOrThrow('Failed to load')).rejects.toThrow(
      'Plan ownership is temporarily unavailable.'
    )
    expect(apiGet).toHaveBeenCalledWith('/api/subscription/admin/plans', {
      skipBusinessError: true,
    })
  })

  it('uses the localized fallback when a failed envelope omits its message', async () => {
    apiGet.mockResolvedValue({ data: { success: false } })

    await expect(getAdminPlansOrThrow('Failed to load')).rejects.toThrow(
      'Failed to load'
    )
  })

  it('returns records only from a successful envelope', async () => {
    const records = [{ plan: { id: 7, title: 'Native' } }]
    apiGet.mockResolvedValue({ data: { success: true, data: records } })

    await expect(getAdminPlansOrThrow('Failed to load')).resolves.toBe(records)
  })

  it('suppresses the global business-error toast for locally handled mutations', async () => {
    const response = { data: { success: false, message: 'Rejected' } }
    apiPost.mockResolvedValue(response)
    apiPut.mockResolvedValue(response)
    apiPatch.mockResolvedValue(response)
    const payload = { plan: { title: 'Native plan' } }

    await createPlan(payload)
    await updatePlan(7, payload)
    await patchPlanStatus(7, false)

    expect(apiPost).toHaveBeenCalledWith(
      '/api/subscription/admin/plans',
      payload,
      { skipBusinessError: true }
    )
    expect(apiPut).toHaveBeenCalledWith(
      '/api/subscription/admin/plans/7',
      payload,
      { skipBusinessError: true }
    )
    expect(apiPatch).toHaveBeenCalledWith(
      '/api/subscription/admin/plans/7',
      { enabled: false },
      { skipBusinessError: true }
    )
  })
})
