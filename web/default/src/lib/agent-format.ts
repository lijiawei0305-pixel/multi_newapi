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
// Shared formatters for the agent self-service pages (P1-UI-04).
// Currency convention (api-contract §1): `*_cny` are CNY ¥, `*_usd` are USD.
// new-api quota uses 500000 quota = $1.
// ============================================================================
import dayjs from '@/lib/dayjs'

/** Tokens-per-USD: 500000 raw quota = $1 (new-api default). */
export const QUOTA_PER_USD = 500000

/** ¥ amount with 2 decimals. */
export const cny = (v: number | undefined | null) =>
  `¥${Number(v ?? 0).toFixed(2)}`

/** $ amount with 2 decimals. */
export const usd = (v: number | undefined | null) =>
  `$${Number(v ?? 0).toFixed(2)}`

/** Raw quota → USD ($) display, per the page spec `quota / 500000`. */
export const quotaToUsd = (quota: number | undefined | null) =>
  usd(Number(quota ?? 0) / QUOTA_PER_USD)

/** Tolerant timestamp formatter — accepts unix seconds, ms, or ISO string. */
export function fmtDateTime(value: number | string | undefined | null): string {
  if (value === undefined || value === null || value === '') return '-'
  if (typeof value === 'number') {
    const ms = value < 1e12 ? value * 1000 : value
    return dayjs(ms).tz().format('YYYY-MM-DD HH:mm')
  }
  const d = dayjs(value)
  return d.isValid() ? d.tz().format('YYYY-MM-DD HH:mm') : String(value)
}
