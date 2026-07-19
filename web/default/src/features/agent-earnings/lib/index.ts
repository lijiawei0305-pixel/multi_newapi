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
import type { TFunction } from 'i18next'

import type { StatusVariant } from '@/components/status-badge'
import dayjs from '@/lib/dayjs'

import type { Earning, EarningsSummary } from '../types'

export const cny = (v: number | undefined) => `¥${Number(v || 0).toFixed(2)}`

/** Tolerant timestamp formatter — accepts unix seconds, ms, or ISO string. */
export function formatDateTime(value: number | string | undefined): string {
  if (value === undefined || value === null || value === '') return '-'
  if (typeof value === 'number') {
    const ms = value < 1e12 ? value * 1000 : value
    return dayjs(ms).tz().format('YYYY-MM-DD HH:mm')
  }
  const d = dayjs(value)
  return d.isValid() ? d.tz().format('YYYY-MM-DD HH:mm') : String(value)
}

const EMPTY_SUMMARY: EarningsSummary = {
  withdrawable_cny: 0,
  frozen_cny: 0,
  total_earned_cny: 0,
}

/**
 * Normalize the `/api/tenant/earnings` envelope `data` into a `{ summary,
 * items }` pair. Tolerates two backend shapes so integration stays trivial:
 *   1. `data: Earning[]`                       → summary defaults to 0
 *   2. `data: { ...summary, items: Earning[] }` (or earnings/list/logs)
 */
export function parseEarnings(data: unknown): {
  summary: EarningsSummary
  items: Earning[]
} {
  if (Array.isArray(data)) {
    return { summary: { ...EMPTY_SUMMARY }, items: data as Earning[] }
  }
  if (data && typeof data === 'object') {
    const d = data as Record<string, unknown>
    const raw =
      (d.items as unknown) ??
      (d.earnings as unknown) ??
      (d.list as unknown) ??
      (d.logs as unknown) ??
      []
    return {
      summary: {
        withdrawable_cny: Number(d.withdrawable_cny ?? 0),
        frozen_cny: Number(d.frozen_cny ?? 0),
        total_earned_cny: Number(d.total_earned_cny ?? 0),
      },
      items: Array.isArray(raw) ? (raw as Earning[]) : [],
    }
  }
  return { summary: { ...EMPTY_SUMMARY }, items: [] }
}

/** i18n label for an earning source type (tolerant of unknown values). */
export function sourceTypeLabel(
  source: string | undefined,
  t: TFunction
): string {
  switch (source) {
    case 'commission':
      return t('Commission')
    case 'recharge_diff':
      return t('Recharge Spread')
    case 'consumption':
      return t('Consumption Share')
    default:
      return source || '-'
  }
}

/** Badge styling + i18n label for a withdrawal status. Each of the 4 statuses
 * gets a visually distinct variant (approved=info vs. paid=success) so the
 * (now non-terminal) `approved` state reads clearly different from `paid`. */
export function withdrawalStatusMeta(
  status: string | undefined,
  t: TFunction
): { variant: StatusVariant; label: string; pulse?: boolean } {
  switch (status) {
    case 'pending':
      return { variant: 'warning', label: t('Pending'), pulse: true }
    case 'approved':
      return { variant: 'info', label: t('Approved') }
    case 'paid':
      return { variant: 'success', label: t('Paid') }
    case 'rejected':
      return { variant: 'danger', label: t('Rejected') }
    default:
      return { variant: 'neutral', label: status || '-' }
  }
}

/** i18n label for a payout method (tolerant of unknown/empty values). */
export function payoutMethodLabel(
  method: string | undefined,
  t: TFunction
): string {
  switch (method) {
    case 'alipay':
      return t('Alipay', { defaultValue: '支付宝' })
    case 'bank':
      return t('Bank Card', { defaultValue: '银行卡' })
    default:
      return '-'
  }
}
