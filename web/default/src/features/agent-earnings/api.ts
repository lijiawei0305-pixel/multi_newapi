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

import type {
  ApiResponse,
  MyWithdrawal,
  PayoutAccount,
  PayoutAccountInput,
} from './types'

// ============================================================================
// Agent self-service endpoints. Auth is carried by new-api's shared axios
// instance (session cookie + New-Api-User header); the backend scopes the
// response to the calling owner.
// ============================================================================

/**
 * Earnings envelope. `data` may be either a bare `Earning[]` or an object
 * carrying both the wallet summary and an `items`/`earnings` array — the
 * page parses both shapes (see `parseEarnings` in ./lib). Returned raw so the
 * caller can extract summary + items together.
 */
export async function getTenantEarnings(): Promise<ApiResponse<unknown>> {
  const res = await api.get('/api/tenant/earnings')
  return res.data
}

export async function getMyWithdrawals(): Promise<ApiResponse<MyWithdrawal[]>> {
  const res = await api.get('/api/tenant/withdrawals')
  return res.data
}

/**
 * `skipErrorHandler` so `WithdrawDialog` can special-case
 * `PAYOUT_ACCOUNT_REQUIRED` (surface the payout-account settings entry)
 * instead of just toasting the raw backend message (see getApiErrorCode).
 */
export async function requestWithdrawal(
  amountCny: number
): Promise<ApiResponse> {
  const res = await api.post(
    '/api/tenant/withdrawals',
    { amount_cny: amountCny },
    { skipErrorHandler: true }
  )
  return res.data
}

export async function getPayoutAccount(): Promise<ApiResponse<PayoutAccount>> {
  const res = await api.get('/api/tenant/payout-account')
  return res.data
}

/** `skipErrorHandler` so the dialog can map PAYOUT_ACCOUNT_INVALID to a localized message. */
export async function updatePayoutAccount(
  input: PayoutAccountInput
): Promise<ApiResponse<PayoutAccount>> {
  const res = await api.put('/api/tenant/payout-account', input, {
    skipErrorHandler: true,
  })
  return res.data
}
