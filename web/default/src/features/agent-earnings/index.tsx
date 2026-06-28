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
import { useMemo, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Wallet } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { getMyWithdrawals, getTenantEarnings } from './api'
import { EarningsSummaryCards } from './components/earnings-summary-cards'
import { EarningsTable } from './components/earnings-table'
import { MyWithdrawalsTable } from './components/my-withdrawals-table'
import { WithdrawDialog } from './components/withdraw-dialog'
import { parseEarnings } from './lib'

export function AgentEarnings() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [withdrawOpen, setWithdrawOpen] = useState(false)

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

  const { summary, items } = useMemo(
    () => parseEarnings(earningsRes?.data),
    [earningsRes]
  )

  const refreshAll = () => {
    queryClient.invalidateQueries({ queryKey: ['tenant-earnings'] })
    queryClient.invalidateQueries({ queryKey: ['tenant-withdrawals'] })
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('My Earnings & Withdrawals')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          size='sm'
          onClick={() => setWithdrawOpen(true)}
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

          <section className='flex flex-col gap-2'>
            <h3 className='text-sm font-semibold'>{t('Earnings Details')}</h3>
            <div className='overflow-hidden rounded-lg border'>
              <EarningsTable items={items} loading={earningsLoading} />
            </div>
          </section>

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
      />
    </SectionPageLayout>
  )
}
