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
import { Lock, TrendingUp, Wallet } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Skeleton } from '@/components/ui/skeleton'
import { cny } from '../lib'
import type { EarningsSummary } from '../types'

interface Props {
  summary: EarningsSummary
  loading?: boolean
}

export function EarningsSummaryCards({ summary, loading }: Props) {
  const { t } = useTranslation()

  if (loading) {
    return (
      <div className='overflow-hidden rounded-lg border'>
        <div className='divide-border/60 grid grid-cols-1 divide-y sm:grid-cols-3 sm:divide-x sm:divide-y-0'>
          {Array.from({ length: 3 }).map((_, i) => (
            <div key={i} className='px-3 py-3 sm:px-5 sm:py-4'>
              <Skeleton className='h-3.5 w-20' />
              <Skeleton className='mt-2 h-7 w-28' />
            </div>
          ))}
        </div>
      </div>
    )
  }

  const cards = [
    {
      label: t('Withdrawable'),
      value: cny(summary.withdrawable_cny),
      icon: Wallet,
      testid: 'withdrawable-amount',
      emphasis: true,
    },
    {
      label: t('Frozen'),
      value: cny(summary.frozen_cny),
      icon: Lock,
      testid: 'frozen-amount',
    },
    {
      label: t('Total Earned'),
      value: cny(summary.total_earned_cny),
      icon: TrendingUp,
      testid: 'total-earned-amount',
    },
  ]

  return (
    <div className='overflow-hidden rounded-lg border'>
      <div className='divide-border/60 grid grid-cols-1 divide-y sm:grid-cols-3 sm:divide-x sm:divide-y-0'>
        {cards.map((item) => (
          <div key={item.label} className='px-3 py-3 sm:px-5 sm:py-4'>
            <div className='flex items-center gap-2'>
              <item.icon className='text-muted-foreground/60 size-3.5 shrink-0' />
              <div className='text-muted-foreground truncate text-xs font-medium tracking-wider uppercase'>
                {item.label}
              </div>
            </div>
            <div
              data-testid={item.testid}
              className={
                'mt-1.5 font-mono text-base font-bold tracking-tight break-all tabular-nums sm:mt-2 sm:text-2xl ' +
                (item.emphasis ? 'text-emerald-600' : 'text-foreground')
              }
            >
              {item.value}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
