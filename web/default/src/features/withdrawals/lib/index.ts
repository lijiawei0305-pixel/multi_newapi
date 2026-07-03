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

export const isPending = (status: string | undefined) => status === 'pending'

/** Badge styling + i18n label for a withdrawal status. */
export function withdrawalStatusMeta(
  status: string | undefined,
  t: TFunction
): { variant: StatusVariant; label: string; pulse?: boolean } {
  switch (status) {
    case 'pending':
      return { variant: 'warning', label: t('Pending'), pulse: true }
    case 'approved':
      return { variant: 'success', label: t('Approved') }
    case 'paid':
      return { variant: 'success', label: t('Paid') }
    case 'rejected':
      return { variant: 'danger', label: t('Rejected') }
    default:
      return { variant: 'neutral', label: status || '-' }
  }
}
