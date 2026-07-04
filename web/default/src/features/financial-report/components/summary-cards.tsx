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
  Building2,
  HandCoins,
  KeyRound,
  Landmark,
  Package,
  PiggyBank,
  Split,
  Store,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'
import { cny } from '../lib'
import type { AdminFinanceOverview, AgentFinanceOverview } from '../types'

// ============================================================================
// v3 概览 KPI 卡（doc/finance-model-report-v3.md §二 + admin-finance-report-simplify）：
// 代理 4 卡 / 管理端 6 卡，两侧字段形状不同，故由 `OverviewCards` 按 `scope` 分支渲染
// （见下）。每个金额用 `cny` 渲染。
// ============================================================================

interface KpiCard {
  label: string
  value: string
  icon: LucideIcon
  testid: string
  emphasis?: boolean
  /** Small muted caveat line rendered under the value (v3 upper-bound notes). */
  footnote?: string
}

/** One KPI tile — shared render used by `OverviewCards`. */
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

// ============================================================================
// v3 概览 (doc/finance-model-report-v3.md §二) — scope-aware headline KPI
// section. NOT scope-agnostic: the agent (4-field) and admin (6-field) overview
// shapes don't share fields (see `FinanceOverview` in ../types), so callers pass
// an explicit `scope` discriminant to pick the branch.
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

    // 顺序 + 显示名对齐 doc/admin-finance-report-simplify.md §二（6 卡重命名+重排）。
    const cards: KpiCard[] = [
      {
        label: t('Mainsite Tokenplan Revenue', { defaultValue: '主站套餐收入' }),
        value: cny(overview.mainsite_tokenplan_revenue_cny),
        icon: Landmark,
        testid: 'kpi-overview-mainsite-tokenplan-revenue',
      },
      {
        label: t('Agent Tokenplan Revenue', { defaultValue: '代理套餐收入' }),
        value: cny(overview.agent_tokenplan_revenue_cny),
        icon: Store,
        testid: 'kpi-overview-agent-tokenplan-revenue',
      },
      {
        label: t('Mainsite Api Consumption Revenue', {
          defaultValue: '主站api消耗收入',
        }),
        value: cny(overview.mainsite_wallet_consumption_cny),
        icon: Activity,
        testid: 'kpi-overview-mainsite-wallet-consumption',
      },
      {
        label: t('Agent Api Consumption Revenue', {
          defaultValue: '代理api消耗收入',
        }),
        value: cny(overview.agent_wallet_consumption_cny),
        icon: Building2,
        testid: 'kpi-overview-agent-wallet-consumption',
      },
      {
        label: t('Agent Tokenplan Withdrawable', {
          defaultValue: '代理套餐可提现',
        }),
        value: cny(overview.tokenplan_rebate_cny),
        icon: HandCoins,
        testid: 'kpi-overview-tokenplan-rebate',
      },
      {
        label: t('Agent Api Withdrawable', {
          defaultValue: '代理api消耗可提现',
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
      label: t('Consumption Withdrawable', { defaultValue: 'apikey消费可提现' }),
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
