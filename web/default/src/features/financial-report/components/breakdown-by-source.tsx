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
import { PieChart } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'
import { cny, sourceTypeLabel } from '../lib'
import type { EarningSourceSum } from '../types'

// ============================================================================
// Earnings split by ledger `source_type` (contract §1.1 `earnings.by_source`).
// Bar widths are scaled to the largest ABSOLUTE amount so negative buckets
// (`manual_adjustment` may be < 0, contract §2 lens a) still render a bar, in a
// danger tint. SCOPE-AGNOSTIC: identical for admin rollup and single tenant.
// ============================================================================

export interface BreakdownBySourceProps {
  bySource: EarningSourceSum[]
  /** Total earned, shown in the header for context. */
  total?: number
  loading?: boolean
}

export function BreakdownBySource({
  bySource,
  total,
  loading,
}: BreakdownBySourceProps) {
  const { t } = useTranslation()

  const maxAbs = Math.max(
    1,
    ...bySource.map((s) => Math.abs(Number(s.amount_cny) || 0))
  )

  return (
    <section
      className='bg-card flex flex-col overflow-hidden rounded-lg border'
      data-testid='breakdown-by-source'
    >
      <header className='flex items-center justify-between gap-2 border-b px-4 py-3'>
        <div className='flex items-center gap-2'>
          <PieChart className='text-muted-foreground/60 size-4 shrink-0' />
          <h3 className='text-sm font-semibold'>{t('Earnings by Source')}</h3>
        </div>
        {total !== undefined && (
          <span className='text-muted-foreground font-mono text-xs tabular-nums'>
            {cny(total)}
          </span>
        )}
      </header>

      <div className='flex flex-1 flex-col gap-3 p-4'>
        {loading ? (
          Array.from({ length: 4 }).map((_, i) => (
            <div key={i} className='flex flex-col gap-1.5'>
              <Skeleton className='h-3 w-32' />
              <Skeleton className='h-2 w-full' />
            </div>
          ))
        ) : bySource.length === 0 ? (
          <div className='text-muted-foreground/80 flex flex-1 items-center justify-center py-8 text-xs'>
            {t('No data available')}
          </div>
        ) : (
          bySource.map((source) => {
            const amount = Number(source.amount_cny) || 0
            const width = Math.min(100, (Math.abs(amount) / maxAbs) * 100)
            const negative = amount < 0
            return (
              <div
                key={source.source_type}
                className='flex flex-col gap-1'
                data-testid={`source-${source.source_type}`}
              >
                <div className='flex items-center justify-between gap-2 text-xs'>
                  <span className='truncate font-medium'>
                    {sourceTypeLabel(source.source_type, t)}
                  </span>
                  <span
                    className={cn(
                      'font-mono tabular-nums',
                      negative ? 'text-destructive' : 'text-foreground'
                    )}
                  >
                    {cny(amount)}
                  </span>
                </div>
                <div className='bg-muted h-2 overflow-hidden rounded-full'>
                  <div
                    className={cn(
                      'h-full rounded-full',
                      negative ? 'bg-destructive' : 'bg-emerald-500'
                    )}
                    style={{ width: `${width}%` }}
                  />
                </div>
              </div>
            )
          })
        )}
      </div>
    </section>
  )
}
