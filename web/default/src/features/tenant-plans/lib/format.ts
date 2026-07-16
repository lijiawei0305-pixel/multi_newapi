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
import dayjs from '@/lib/dayjs'

/** Format a CNY amount: `¥12.00`. */
export const cny = (v: number | undefined) => `¥${Number(v || 0).toFixed(2)}`

/** Format a USD amount: `$12.00`. */
export const usd = (v: number | undefined) => `$${Number(v || 0).toFixed(2)}`

/** Clamp a usage percentage into the displayable [0, 100] range. */
export function clampPct(v: number | undefined): number {
  const n = Number(v || 0)
  if (Number.isNaN(n)) return 0
  return Math.min(100, Math.max(0, n))
}

/**
 * Tolerant timestamp formatter. Accepts unix seconds, unix milliseconds, or an
 * ISO string, since the tenant subscription contract does not pin the unit.
 */
export function formatDate(value: number | string | undefined): string {
  if (value === undefined || value === null || value === '') return '-'
  if (typeof value === 'number') {
    // Heuristic: < 1e12 is almost certainly seconds, not milliseconds.
    const ms = value < 1e12 ? value * 1000 : value
    return dayjs(ms).tz().format('YYYY-MM-DD')
  }
  const d = dayjs(value)
  return d.isValid() ? d.tz().format('YYYY-MM-DD') : String(value)
}
