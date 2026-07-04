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

// ============================================================================
// Admin withdrawal review — backed by:
//   GET  /api/admin/withdrawals
//   POST /api/admin/withdrawals/:id/approve
//   POST /api/admin/withdrawals/:id/reject   { reason }
// Field contract (snake_case, aligned with backend Worker):
//   id / tenant_id / agent_name / amount_cny / status / created_at / reviewed_at
// ============================================================================

/** Lifecycle of a withdrawal request. Tolerant of extra backend values. */
export const withdrawalStatusValues = [
  'pending',
  'approved',
  'rejected',
  'paid',
] as const
export type WithdrawalStatus = (typeof withdrawalStatusValues)[number]

export interface Withdrawal {
  id: number
  tenant_id: number
  agent_name: string
  /** 提现金额 (¥). */
  amount_cny: number
  status: string
  /** Review/reject remark (payout closure #3 — standardized field name). */
  remark?: string
  /** Payout destination snapshot taken at request time (empty until set). */
  payout_method?: string
  payout_account?: string
  payout_name?: string
  payout_bank?: string
  /** Payout reference/receipt — set once marked paid via mark-paid. */
  payout_ref?: string
  /** Paid time — set once marked paid (empty string until then). */
  paid_at?: number | string
  /** Request time — unix seconds, unix ms, or ISO string (formatter tolerant). */
  created_at?: number | string
  /** Review time — set once approved/rejected. */
  reviewed_at?: number | string
}

/** Unified new-api control-plane envelope: `{ success, message, data }`. */
export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  code?: string
  data?: T
}

export type WithdrawalAction = 'approve' | 'reject' | 'mark-paid'
