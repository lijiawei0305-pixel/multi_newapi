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
import type {
  Granularity,
  Lens,
  SourceType,
  WithdrawalStatus,
} from '../types'

// ============================================================================
// Financial Reporting — presentation helpers. Money/number formatting follows
// the FROZEN wire rule (contract §3): `*_cny` → ¥, `*_usd` → $, both rounded
// to 2 decimals; raw counts (quota/tokens/calls) stay integer. All user-facing
// strings funnel through `t('English sentence')` keys (nsSeparator:false, so
// `¥` and `:` inside keys are safe).
// ============================================================================

function toFinite(v: number | string | null | undefined): number {
  const n = typeof v === 'string' ? Number(v) : (v ?? 0)
  return Number.isFinite(n) ? (n as number) : 0
}

/** Format a CNY amount: `¥` + thousands + 2dp. Tolerates negatives (manual_adjustment). */
export const cny = (v: number | string | null | undefined): string => {
  const n = toFinite(v)
  const sign = n < 0 ? '-' : ''
  return `${sign}¥${Math.abs(n).toLocaleString(undefined, {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })}`
}

/** Format a USD amount: `$` + thousands + 2dp. */
export const usd = (v: number | string | null | undefined): string => {
  const n = toFinite(v)
  const sign = n < 0 ? '-' : ''
  return `${sign}$${Math.abs(n).toLocaleString(undefined, {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })}`
}

/** Format an exact integer count (quota / tokens / calls) with thousands. */
export const count = (v: number | string | null | undefined): string =>
  toFinite(v).toLocaleString(undefined, { maximumFractionDigits: 0 })

/**
 * Tolerant timestamp formatter — accepts ISO-8601 strings, unix seconds, or
 * unix milliseconds. Response timestamps are ISO (contract §3) but trend
 * `bucket_ts` and some rows arrive as unix seconds.
 */
export function formatDateTime(
  value: number | string | null | undefined
): string {
  if (value === undefined || value === null || value === '') return '-'
  if (typeof value === 'number') {
    const ms = value < 1e12 ? value * 1000 : value
    return dayjs(ms).format('YYYY-MM-DD HH:mm')
  }
  const d = dayjs(value)
  return d.isValid() ? d.format('YYYY-MM-DD HH:mm') : String(value)
}

// ---------------------------------------------------------------------------
// Enum → i18n label helpers
// ---------------------------------------------------------------------------

/** i18n label for an earning ledger `source_type` (tolerant of unknowns). */
export function sourceTypeLabel(
  source: SourceType | string | undefined,
  t: TFunction
): string {
  switch (source) {
    case 'recharge_spread':
      return t('Recharge Spread')
    case 'consume_commission':
      return t('Consumption Commission')
    case 'tokenplan_spread':
      return t('Subscription Spread')
    case 'tokenplan_commission':
      return t('Subscription Commission')
    case 'manual_adjustment':
      return t('Manual Adjustment')
    default:
      return source || '-'
  }
}

/** i18n label for one of the four data lenses. */
export function lensLabel(lens: Lens | string | undefined, t: TFunction): string {
  switch (lens) {
    case 'earnings':
      return t('Earnings')
    case 'recharge':
      return t('Recharge')
    case 'consumption':
      return t('Consumption')
    case 'withdrawals':
      return t('Withdrawals')
    default:
      return lens || '-'
  }
}

/** i18n label for a trend granularity. */
export function granularityLabel(
  granularity: Granularity | string | undefined,
  t: TFunction
): string {
  switch (granularity) {
    case 'day':
      return t('Daily')
    case 'week':
      return t('Weekly')
    case 'month':
      return t('Monthly')
    default:
      return granularity || '-'
  }
}

/** Badge styling + i18n label for a withdrawal status (FROZEN: 3 values). */
export function withdrawalStatusMeta(
  status: WithdrawalStatus | string | undefined,
  t: TFunction
): { variant: StatusVariant; label: string; pulse?: boolean } {
  switch (status) {
    case 'pending':
      return { variant: 'warning', label: t('Pending'), pulse: true }
    case 'approved':
      return { variant: 'success', label: t('Approved') }
    case 'rejected':
      return { variant: 'danger', label: t('Rejected') }
    default:
      return { variant: 'neutral', label: status || '-' }
  }
}

// ---------------------------------------------------------------------------
// Granularity / bucket helpers
// ---------------------------------------------------------------------------

/** The selectable granularity values, in chart-friendly order. */
export const GRANULARITY_OPTIONS: Granularity[] = ['day', 'week', 'month']

/** The four lenses in canonical display order. */
export const LENS_OPTIONS: Lens[] = [
  'earnings',
  'recharge',
  'consumption',
  'withdrawals',
]

/**
 * dayjs token used to render a `bucket_ts` for a given granularity, so the
 * chart axis label matches the backend `bucket` calendar label.
 */
export function bucketFormatToken(granularity: Granularity): string {
  switch (granularity) {
    case 'month':
      return 'YYYY-MM'
    case 'week':
    case 'day':
    default:
      return 'YYYY-MM-DD'
  }
}

/** Render an epoch-seconds `bucket_ts` into its calendar label. */
export function formatBucketTs(
  bucketTs: number | null | undefined,
  granularity: Granularity
): string {
  const n = toFinite(bucketTs)
  if (n <= 0) return '-'
  return dayjs(n * 1000).format(bucketFormatToken(granularity))
}
