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
import { api } from '@/lib/api'

import type { ApiResponse, Withdrawal, WithdrawalPage } from './types'

// ============================================================================
// Admin withdrawal review. Auth is carried by new-api's shared axios instance
// (session cookie + New-Api-User header), as on every admin page.
// ============================================================================

export async function getAdminWithdrawals(
  page: number,
  pageSize: number
): Promise<ApiResponse<WithdrawalPage>> {
  const res = await api.get('/api/admin/withdrawals', {
    params: { page, page_size: pageSize },
  })
  const payload = res.data as ApiResponse<WithdrawalPage | Withdrawal[]>
  if (Array.isArray(payload.data)) {
    const start = (page - 1) * pageSize
    return {
      ...payload,
      data: {
        items: payload.data.slice(start, start + pageSize),
        total: payload.data.length,
        page,
        page_size: pageSize,
      },
    }
  }
  return payload as ApiResponse<WithdrawalPage>
}

export async function approveWithdrawal(id: number): Promise<ApiResponse> {
  const res = await api.post(`/api/admin/withdrawals/${id}/approve`)
  return res.data
}

// Field aligned to what the backend binds (payout closure #3): `remark`, not
// `reason` — the mismatch used to silently drop the rejection reason.
export async function rejectWithdrawal(
  id: number,
  remark: string
): Promise<ApiResponse> {
  const res = await api.post(`/api/admin/withdrawals/${id}/reject`, { remark })
  return res.data
}

/**
 * Mark an approved withdrawal as paid (approved -> paid, CAS on the backend).
 * `skipErrorHandler` so the dialog can special-case `PAYOUT_REF_REQUIRED`
 * (400) and `WITHDRAW_NOT_APPROVED` (409 — status changed concurrently).
 */
export async function markPaidWithdrawal(
  id: number,
  payoutRef: string
): Promise<ApiResponse> {
  const res = await api.post(
    `/api/admin/withdrawals/${id}/mark-paid`,
    { payout_ref: payoutRef },
    { skipErrorHandler: true }
  )
  return res.data
}
