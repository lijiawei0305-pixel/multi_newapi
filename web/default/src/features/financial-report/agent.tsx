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
import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { SectionPageLayout } from '@/components/layout'
import { computeTimeRange } from '@/lib/time'
import {
  getTenantFinanceDetail,
  getTenantFinanceSummary,
  getTenantFinanceTrend,
} from './api'
import { BreakdownBySource } from './components/breakdown-by-source'
import { DetailTable } from './components/detail-table'
import { ExportButtons } from './components/export-buttons'
import { ReportControls } from './components/report-controls'
import { SummaryCards } from './components/summary-cards'
import { TrendChart } from './components/trend-chart'
import { lensLabel } from './lib'
import type { Granularity, Lens, RangeParams } from './types'

// ============================================================================
// Agent self-service financial report. Auth is enforced at the route
// (agentContextQueryOptions -> /403 if !is_agent_owner); the tenant endpoints
// take their tenant_id ONLY from agentTenantID(c) server-side — the client
// never sends one. This page REUSES the shared, prop-driven components from
// admin.tsx (stage 2) with showTenant=false and WITHOUT the cross-tenant agent
// ranking table. Money/time follow the FROZEN contract (§3): CNY/USD by suffix,
// epoch-seconds requests, ISO responses.
// ============================================================================

const DEFAULT_PAGE_SIZE = 20

export function AgentFinancialReport() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const [range, setRange] = useState<RangeParams>(() => computeTimeRange(30))
  const [lens, setLens] = useState<Lens>('earnings')
  const [granularity, setGranularity] = useState<Granularity>('day')
  const [detailPage, setDetailPage] = useState(1)

  const summaryQuery = useQuery({
    queryKey: ['tenant-finance-summary', range],
    queryFn: () => getTenantFinanceSummary(range),
    select: (res) => res.data,
    placeholderData: (prev) => prev,
  })

  const trendParams = { ...range, lens, granularity }
  const trendQuery = useQuery({
    queryKey: ['tenant-finance-trend', trendParams],
    queryFn: () => getTenantFinanceTrend(trendParams),
    select: (res) => res.data,
    placeholderData: (prev) => prev,
  })

  const detailParams = {
    ...range,
    lens,
    page: detailPage,
    page_size: DEFAULT_PAGE_SIZE,
  }
  const detailQuery = useQuery({
    queryKey: ['tenant-finance-detail', detailParams],
    queryFn: () => getTenantFinanceDetail(detailParams),
    select: (res) => res.data,
    placeholderData: (prev) => prev,
  })

  const refreshAll = () => {
    queryClient.invalidateQueries({ queryKey: ['tenant-finance-summary'] })
    queryClient.invalidateQueries({ queryKey: ['tenant-finance-trend'] })
    queryClient.invalidateQueries({ queryKey: ['tenant-finance-detail'] })
  }

  const handleRangeChange = (next: RangeParams) => {
    setRange(next)
    setDetailPage(1)
  }
  const handleLensChange = (next: Lens) => {
    setLens(next)
    setDetailPage(1)
  }

  const summary = summaryQuery.data
  const detail = detailQuery.data

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Financial Report')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <ExportButtons
          scope='tenant'
          params={detailParams}
          disabled={detailQuery.isLoading}
        />
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div
          className='flex flex-col gap-6'
          data-testid='agent-finance-report-page'
        >
          <ReportControls
            range={range}
            onRangeChange={handleRangeChange}
            lens={lens}
            onLensChange={handleLensChange}
            granularity={granularity}
            onGranularityChange={setGranularity}
            onRefresh={refreshAll}
            refreshing={summaryQuery.isFetching}
          />

          <SummaryCards summary={summary} loading={summaryQuery.isLoading} />

          <div className='grid grid-cols-1 gap-4 xl:grid-cols-3'>
            <div className='xl:col-span-2'>
              <TrendChart
                lens={lens}
                granularity={granularity}
                series={trendQuery.data?.lens === lens ? (trendQuery.data.series ?? []) : []}
                loading={trendQuery.isLoading}
              />
            </div>
            <BreakdownBySource
              bySource={summary?.earnings.by_source ?? []}
              total={summary?.earnings.total_earned_cny}
              loading={summaryQuery.isLoading}
            />
          </div>

          <section className='flex flex-col gap-2'>
            <h3 className='text-sm font-semibold'>
              {t('{{lens}} Details', { lens: lensLabel(lens, t) })}
            </h3>
            <DetailTable
              lens={lens}
              items={detail?.items ?? []}
              loading={detailQuery.isLoading}
              showTenant={false}
              page={detailPage}
              pageSize={detail?.page_size ?? DEFAULT_PAGE_SIZE}
              total={detail?.total ?? 0}
              onPageChange={setDetailPage}
            />
          </section>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
