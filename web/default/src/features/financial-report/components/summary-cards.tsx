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
  Building2,
  Coins,
  CreditCard,
  HandCoins,
  KeyRound,
  Landmark,
  Package,
  Percent,
  PiggyBank,
  Split,
  Store,
  TrendingUp,
  Wallet,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'
import { cny, usd } from '../lib'
import type {
  AdminFinanceOverview,
  AgentFinanceOverview,
  FinanceSummary,
} from '../types'

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
  /** Small muted caveat line rendered under the value (v3 upper-bound notes). */
  footnote?: string
}

const CARD_COUNT = 9

/** One KPI tile — shared render used by both `SummaryCards` and `OverviewCards`. */
function KpiTile({ card }: { card: KpiCard }) {
  return (
    <div className='rounded-lg border px-3 py-3 sm:px-4'>
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
      {card.footnote ? (
        <div className='text-muted-foreground/60 mt-1 text-[11px] leading-snug'>
          {card.footnote}
        </div>
      ) : null}
    </div>
  )
}

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
        <KpiTile key={card.testid} card={card} />
      ))}
    </div>
  )
}

// ============================================================================
// v3 概览 (doc/finance-model-report-v3.md §二) — scope-aware headline KPI
// section, rendered ABOVE `SummaryCards`. Unlike `SummaryCards`, this is NOT
// scope-agnostic: the agent (4-field) and admin (6-field) overview shapes
// don't share fields (see `FinanceOverview` in ../types), so callers pass an
// explicit `scope` discriminant — mirrors how `scope` already threads through
// `ExportButtons` for the same admin/tenant split.
// ============================================================================

export type OverviewCardsProps =
  | { scope: 'agent'; overview?: AgentFinanceOverview; loading?: boolean }
  | { scope: 'admin'; overview?: AdminFinanceOverview; loading?: boolean }

function OverviewSkeletonGrid({
  count,
  gridCols,
}: {
  count: number
  gridCols: string
}) {
  return (
    <div className={cn('grid grid-cols-2 gap-3', gridCols)} data-testid='overview-cards'>
      {Array.from({ length: count }).map((_, i) => (
        <div key={i} className='rounded-lg border px-3 py-3 sm:px-4'>
          <Skeleton className='h-3.5 w-20' />
          <Skeleton className='mt-2 h-6 w-24' />
        </div>
      ))}
    </div>
  )
}

export function OverviewCards(props: OverviewCardsProps) {
  const { t } = useTranslation()

  const heading = (
    <h3 className='text-base font-semibold'>
      {t('Finance Report V3 Overview', { defaultValue: 'v3 概览' })}
    </h3>
  )

  if (props.scope === 'admin') {
    const { overview, loading } = props

    if (loading || !overview) {
      return (
        <section className='flex flex-col gap-2' data-testid='overview-section'>
          {heading}
          <OverviewSkeletonGrid count={6} gridCols='sm:grid-cols-3 xl:grid-cols-6' />
        </section>
      )
    }

    const cards: KpiCard[] = [
      {
        label: t('Mainsite Tokenplan Revenue', { defaultValue: '主站套餐收益' }),
        value: cny(overview.mainsite_tokenplan_revenue_cny),
        icon: Landmark,
        testid: 'kpi-overview-mainsite-tokenplan-revenue',
      },
      {
        label: t('Agent Tokenplan Revenue', { defaultValue: '代理站套餐收益' }),
        value: cny(overview.agent_tokenplan_revenue_cny),
        icon: Store,
        testid: 'kpi-overview-agent-tokenplan-revenue',
      },
      {
        label: t('Tokenplan Rebate To Agents', {
          defaultValue: '给代理的套餐返现',
        }),
        value: cny(overview.tokenplan_rebate_cny),
        icon: HandCoins,
        testid: 'kpi-overview-tokenplan-rebate',
      },
      {
        label: t('Mainsite Wallet Consumption', {
          defaultValue: '主站钱包消耗',
        }),
        value: cny(overview.mainsite_wallet_consumption_cny),
        icon: Activity,
        testid: 'kpi-overview-mainsite-wallet-consumption',      },
      {
        label: t('Agent Wallet Consumption', { defaultValue: '代理站钱包消耗' }),
        value: cny(overview.agent_wallet_consumption_cny),
        icon: Building2,
        testid: 'kpi-overview-agent-wallet-consumption',      },
      {
        label: t('Agent Api Rebate', {
          defaultValue: '需返现代理的 api 消耗金额',
        }),
        value: cny(overview.agent_api_rebate_cny),
        icon: Split,
        testid: 'kpi-overview-agent-api-rebate',
      },
    ]

    return (
      <section className='flex flex-col gap-2' data-testid='overview-section'>
        {heading}
        <div
          className='grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-6'
          data-testid='overview-cards'
        >
          {cards.map((card) => (
            <KpiTile key={card.testid} card={card} />
          ))}
        </div>
      </section>
    )
  }

  const { overview, loading } = props

  if (loading || !overview) {
    return (
      <section className='flex flex-col gap-2' data-testid='overview-section'>
        {heading}
        <OverviewSkeletonGrid count={4} gridCols='sm:grid-cols-4' />
      </section>
    )
  }

  const cards: KpiCard[] = [
    {
      label: t('Tokenplan Revenue', { defaultValue: '套餐收益(用户支付金额)' }),
      value: cny(overview.tokenplan_revenue_cny),
      icon: Package,
      testid: 'kpi-overview-tokenplan-revenue',
    },
    {
      label: t('Tokenplan Withdrawable', { defaultValue: '套餐可提现' }),
      value: cny(overview.tokenplan_withdrawable_cny),
      icon: PiggyBank,
      testid: 'kpi-overview-tokenplan-withdrawable',
    },
    {
      label: t('Apikey Consumption Revenue', { defaultValue: 'apikey 消费收益' }),
      value: cny(overview.apikey_consumption_cny),
      icon: KeyRound,
      testid: 'kpi-overview-apikey-consumption',
    },
    {
      label: t('Consumption Withdrawable', { defaultValue: '消耗可提现' }),
      value: cny(overview.consumption_withdrawable_cny),
      icon: HandCoins,
      testid: 'kpi-overview-consumption-withdrawable',
    },
  ]

  return (
    <section className='flex flex-col gap-2' data-testid='overview-section'>
      {heading}
      <div
        className='grid grid-cols-2 gap-3 sm:grid-cols-4'
        data-testid='overview-cards'
      >
        {cards.map((card) => (
          <KpiTile key={card.testid} card={card} />
        ))}
      </div>
    </section>
  )
}
