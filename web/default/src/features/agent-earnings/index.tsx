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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Wallet } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import {
  getTenantFinanceSummary,
  getTenantFinanceTrend,
} from '@/features/financial-report/api'
import { OverviewCards } from '@/features/financial-report/components/summary-cards'
import type {
  EarningsTrendPoint,
  RangeParams,
} from '@/features/financial-report/types'
import { computeTimeRange } from '@/lib/time'

import { getMyWithdrawals, getPayoutAccount, getTenantEarnings } from './api'
import { EarningsSummaryCards } from './components/earnings-summary-cards'
import { EarningsTrendChart } from './components/earnings-trend-chart'
import { MyWithdrawalsTable } from './components/my-withdrawals-table'
import { PayoutAccountCard } from './components/payout-account-card'
import { PayoutAccountDialog } from './components/payout-account-dialog'
import { WithdrawDialog } from './components/withdraw-dialog'
import { parseEarnings } from './lib'

// ============================================================================
// 「我的收益」是代理站唯一的收益/报表入口（doc/agent-earnings-simplify.md）：钱包 3 卡
// (EarningsSummaryCards) + v3 概览 4 卡 (OverviewCards scope='agent') + 3 线趋势图
// (EarningsTrendChart) + 收款账户/提现历史/dialog。原「财务报表」代理页已删除，其
// OverviewCards/getTenantFinanceSummary/getTenantFinanceTrend 在此复用（管理端页面不受影响）。
// ============================================================================

export function AgentEarnings() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [withdrawOpen, setWithdrawOpen] = useState(false)
  const [payoutOpen, setPayoutOpen] = useState(false)

  const { data: earningsRes, isLoading: earningsLoading } = useQuery({
    queryKey: ['tenant-earnings'],
    queryFn: getTenantEarnings,
    placeholderData: (prev) => prev,
  })

  const { data: withdrawals, isLoading: wdLoading } = useQuery({
    queryKey: ['tenant-withdrawals'],
    queryFn: async () => {
      const res = await getMyWithdrawals()
      return res.data || []
    },
    placeholderData: (prev) => prev,
  })

  const { data: payoutRes, isLoading: payoutLoading } = useQuery({
    queryKey: ['tenant-payout-account'],
    queryFn: getPayoutAccount,
    placeholderData: (prev) => prev,
  })
  const payoutAccount = payoutRes?.data

  const { summary } = useMemo(
    () => parseEarnings(earningsRes?.data),
    [earningsRes]
  )

  // v3 概览卡 + 3 线趋势图：固定近 30 天（与 financial-report 页一致的默认口径），本页精简后不
  // 提供交互式区间选择器（doc/agent-earnings-simplify.md §一）。
  const [range] = useState<RangeParams>(() => computeTimeRange(30))

  const financeSummaryQuery = useQuery({
    queryKey: ['tenant-finance-summary', range],
    queryFn: () => getTenantFinanceSummary(range),
    select: (res) => res.data,
    placeholderData: (prev) => prev,
  })

  const trendParams = {
    ...range,
    lens: 'earnings' as const,
    granularity: 'day' as const,
  }
  const financeTrendQuery = useQuery({
    queryKey: ['tenant-finance-trend', trendParams],
    queryFn: () => getTenantFinanceTrend(trendParams),
    select: (res) => res.data,
    placeholderData: (prev) => prev,
  })
  const trendSeries = (
    financeTrendQuery.data?.lens === 'earnings'
      ? financeTrendQuery.data.series
      : []
  ) as EarningsTrendPoint[]

  const refreshAll = () => {
    queryClient.invalidateQueries({ queryKey: ['tenant-earnings'] })
    queryClient.invalidateQueries({ queryKey: ['tenant-withdrawals'] })
  }
  const refreshPayoutAccount = () =>
    queryClient.invalidateQueries({ queryKey: ['tenant-payout-account'] })

  // Proactive guard (payout closure #1): if the payout account isn't set yet,
  // redirect straight to that settings dialog instead of opening the
  // withdrawal form the request would just bounce off of. WithdrawDialog
  // itself still handles the reactive PAYOUT_ACCOUNT_REQUIRED error as a
  // defense-in-depth fallback (e.g. stale query data).
  const handleOpenWithdraw = () => {
    if (payoutAccount && !payoutAccount.configured) {
      toast.error(
        t('Please set your payout account before requesting a withdrawal', {
          defaultValue: '请先设置收款账户，再申请提现',
        })
      )
      setPayoutOpen(true)
      return
    }
    setWithdrawOpen(true)
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('My Earnings & Withdrawals')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          size='sm'
          onClick={handleOpenWithdraw}
          disabled={!(summary.withdrawable_cny > 0)}
          data-testid='withdraw-btn'
        >
          <Wallet className='h-4 w-4' />
          {t('Apply for Withdrawal')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='flex flex-col gap-6' data-testid='agent-earnings-page'>
          <EarningsSummaryCards summary={summary} loading={earningsLoading} />

          <OverviewCards
            scope='agent'
            overview={financeSummaryQuery.data?.overview}
            loading={financeSummaryQuery.isLoading}
          />

          <EarningsTrendChart
            series={trendSeries}
            loading={financeTrendQuery.isLoading}
          />

          <PayoutAccountCard
            account={payoutAccount}
            loading={payoutLoading}
            onEdit={() => setPayoutOpen(true)}
          />

          <section className='flex flex-col gap-2'>
            <h3 className='text-sm font-semibold'>{t('My Withdrawals')}</h3>
            <div className='overflow-hidden rounded-lg border'>
              <MyWithdrawalsTable
                items={withdrawals || []}
                loading={wdLoading}
              />
            </div>
          </section>
        </div>
      </SectionPageLayout.Content>

      <WithdrawDialog
        open={withdrawOpen}
        onOpenChange={setWithdrawOpen}
        max={summary.withdrawable_cny}
        onSuccess={refreshAll}
        onPayoutAccountRequired={() => setPayoutOpen(true)}
      />

      <PayoutAccountDialog
        open={payoutOpen}
        onOpenChange={setPayoutOpen}
        account={payoutAccount}
        onSaved={refreshPayoutAccount}
      />
    </SectionPageLayout>
  )
}
