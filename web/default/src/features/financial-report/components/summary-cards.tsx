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
import type { LucideIcon } from 'lucide-react'
import {
  Activity,
  Banknote,
  Coins,
  CreditCard,
  HandCoins,
  Package,
  Percent,
  TrendingUp,
  Wallet,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'
import { cny, usd } from '../lib'
import type { FinanceSummary } from '../types'

// ============================================================================
// KPI cards summarising all four lenses of a finance report. SCOPE-AGNOSTIC:
// admin (cross-tenant rollup) and agent (single tenant) feed the same
// `FinanceSummary` shape, so the same card grid renders for both. Each money
// value is rendered with its own currency symbol (`cny`/`usd`) — never mixed.
// ============================================================================

export interface SummaryCardsProps {
  summary?: FinanceSummary
  loading?: boolean
}

interface KpiCard {
  label: string
  value: string
  icon: LucideIcon
  testid: string
  emphasis?: boolean
}

const CARD_COUNT = 9

export function SummaryCards({ summary, loading }: SummaryCardsProps) {
  const { t } = useTranslation()

  if (loading || !summary) {
    return (
      <div
        className='grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-4'
        data-testid='summary-cards'
      >
        {Array.from({ length: CARD_COUNT }).map((_, i) => (
          <div key={i} className='rounded-lg border px-3 py-3 sm:px-4'>
            <Skeleton className='h-3.5 w-20' />
            <Skeleton className='mt-2 h-6 w-24' />
          </div>
        ))}
      </div>
    )
  }

  const cards: KpiCard[] = [
    {
      label: t('Total Earned'),
      value: cny(summary.earnings.total_earned_cny),
      icon: TrendingUp,
      testid: 'kpi-total-earned',
      emphasis: true,
    },
    {
      label: t('Withdrawable'),
      value: cny(summary.earnings.wallet_total.withdrawable_cny),
      icon: Wallet,
      testid: 'kpi-withdrawable',
    },
    {
      label: t('API Balance'),
      value: usd(summary.earnings.wallet_total.api_balance_usd),
      icon: Coins,
      testid: 'kpi-api-balance',
    },
    {
      label: t('Recharge Paid'),
      value: cny(summary.recharge.recharge_paid_cny),
      icon: CreditCard,
      testid: 'kpi-recharge-paid',
    },
    {
      label: t('Subscription Paid'),
      value: cny(summary.recharge.subscription_paid_cny),
      icon: Package,
      testid: 'kpi-subscription-paid',
    },
    {
      label: t('Subscription Spread'),
      value: cny(summary.recharge.subscription_spread_cny),
      icon: Percent,
      testid: 'kpi-subscription-spread',
    },
    {
      label: t('Consumption Cost'),
      value: cny(summary.consumption.used_cost_cny),
      icon: Activity,
      testid: 'kpi-consumption-cost',
    },
    {
      label: t('Pending Withdrawals'),
      value: cny(summary.withdrawals.pending_cny),
      icon: HandCoins,
      testid: 'kpi-pending-withdraw',
    },
    {
      label: t('Withdrawn'),
      value: cny(summary.withdrawals.withdrawn_cny),
      icon: Banknote,
      testid: 'kpi-withdrawn',
    },
  ]

  return (
    <div
      className='grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-4'
      data-testid='summary-cards'
    >
      {cards.map((card) => (
        <div
          key={card.label}
          className='rounded-lg border px-3 py-3 sm:px-4'
        >
          <div className='flex items-center gap-2'>
            <card.icon className='text-muted-foreground/60 size-3.5 shrink-0' />
            <div className='text-muted-foreground truncate text-xs font-medium tracking-wider uppercase'>
              {card.label}
            </div>
          </div>
          <div
            data-testid={card.testid}
            className={cn(
              'mt-1.5 font-mono text-lg font-bold tracking-tight break-all tabular-nums sm:text-xl',
              card.emphasis ? 'text-emerald-600' : 'text-foreground'
            )}
          >
            {card.value}
          </div>
        </div>
      ))}
    </div>
  )
}
