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
import type { AlertLevel } from '../types'

export function clampPct(v: number | undefined): number {
  const n = Number(v || 0)
  if (Number.isNaN(n)) return 0
  return Math.min(100, Math.max(0, n))
}

/** Tolerant timestamp formatter — accepts unix seconds, ms, or ISO string. */
export function formatDate(value: number | string | undefined): string {
  if (value === undefined || value === null || value === '') return '-'
  if (typeof value === 'number') {
    const ms = value < 1e12 ? value * 1000 : value
    return dayjs(ms).tz().format('YYYY-MM-DD')
  }
  const d = dayjs(value)
  return d.isValid() ? d.tz().format('YYYY-MM-DD') : String(value)
}

/** Badge styling + i18n label for an alert level. */
export function alertLevelMeta(
  level: AlertLevel | '' | null | undefined,
  t: TFunction
): { variant: StatusVariant; label: string } {
  switch (level) {
    case 'warn':
      return { variant: 'warning', label: t('Warning') }
    case 'critical':
      return { variant: 'danger', label: t('Critical') }
    case 'exhausted':
      return { variant: 'danger', label: t('Exhausted') }
    default:
      return { variant: 'success', label: t('Normal') }
  }
}

export function usageBarColor(pct: number): string {
  if (pct >= 90) return 'bg-destructive'
  if (pct >= 75) return 'bg-warning'
  return 'bg-primary'
}
