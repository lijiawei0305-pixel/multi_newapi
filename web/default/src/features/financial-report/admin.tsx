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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { computeTimeRange } from '@/lib/time'

import {
  getAdminFinanceAgents,
  getAdminFinanceNetTrend,
  getAdminFinanceSummary,
} from './api'
import { AgentRankingTable } from './components/agent-ranking-table'
import { NetIncomeTrendChart } from './components/net-income-trend-chart'
import { ReportControls } from './components/report-controls'
import { OverviewCards } from './components/summary-cards'
import type { AgentSortBy, Granularity, RangeParams, SortOrder } from './types'

// ============================================================================
// 精简后的管理员财务报表（doc/admin-finance-report-simplify.md）：控件(区间+粒度+刷新，
// 去掉 lens 透镜) → v3 概览 6 卡 (OverviewCards scope='admin') → 净收入趋势图 3 线
// (NetIncomeTrendChart) → 跨代理收益排行 (AgentRankingTable，保留原样)。原通用 9 卡/旧
// lens 趋势图/分项来源/逐笔明细/导出均已移除。Auth 在路由层强制（role<ADMIN → /403）；
// 端点均 AdminAuth 跨租户，无租户作用域。金额/时间遵循 FROZEN 契约（CNY 后缀、epoch 秒请求）。
// ============================================================================

const DEFAULT_PAGE_SIZE = 20

export function AdminFinancialReport() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const [range, setRange] = useState<RangeParams>(() => computeTimeRange(30))
  const [granularity, setGranularity] = useState<Granularity>('day')
  const [rankPage, setRankPage] = useState(1)
  const [sortBy, setSortBy] = useState<AgentSortBy>('total_earned_cny')
  const [order, setOrder] = useState<SortOrder>('desc')

  // v3 概览 6 卡数据源（管理端 overview）。
  const summaryQuery = useQuery({
    queryKey: ['admin-finance-summary', range],
    queryFn: () => getAdminFinanceSummary(range),
    select: (res) => res.data,
    placeholderData: (prev) => prev,
  })

  // 净收入趋势（套餐净/api净；总净由图组件相加）。区间合计与 6 卡对账：
  // Σ套餐净=卡1+卡2−卡5，Σapi净=卡3+卡4−卡6（rebate 两块后端已排除主站平台租户）。
  const netTrendParams = { ...range, granularity }
  const netTrendQuery = useQuery({
    queryKey: ['admin-finance-net-trend', netTrendParams],
    queryFn: () => getAdminFinanceNetTrend(netTrendParams),
    select: (res) => res.data,
    placeholderData: (prev) => prev,
  })

  const rankParams = {
    ...range,
    sort_by: sortBy,
    order,
    page: rankPage,
    page_size: DEFAULT_PAGE_SIZE,
  }
  const rankQuery = useQuery({
    queryKey: ['admin-finance-agents', rankParams],
    queryFn: () => getAdminFinanceAgents(rankParams),
    select: (res) => res.data,
    placeholderData: (prev) => prev,
  })

  const refreshAll = () => {
    queryClient.invalidateQueries({ queryKey: ['admin-finance-summary'] })
    queryClient.invalidateQueries({ queryKey: ['admin-finance-net-trend'] })
    queryClient.invalidateQueries({ queryKey: ['admin-finance-agents'] })
  }

  const handleRangeChange = (next: RangeParams) => {
    setRange(next)
    setRankPage(1)
  }
  const handleSortChange = (nextSortBy: AgentSortBy, nextOrder: SortOrder) => {
    setSortBy(nextSortBy)
    setOrder(nextOrder)
    setRankPage(1)
  }

  const summary = summaryQuery.data
  const ranking = rankQuery.data

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Financial Report')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div
          className='flex flex-col gap-6'
          data-testid='admin-finance-report-page'
        >
          <ReportControls
            range={range}
            onRangeChange={handleRangeChange}
            granularity={granularity}
            onGranularityChange={setGranularity}
            onRefresh={refreshAll}
            refreshing={summaryQuery.isFetching}
          />

          <OverviewCards
            scope='admin'
            overview={summary?.overview}
            loading={summaryQuery.isLoading}
          />

          <NetIncomeTrendChart
            series={netTrendQuery.data?.series ?? []}
            loading={netTrendQuery.isLoading}
          />

          <section className='flex flex-col gap-2'>
            <h3 className='text-sm font-semibold'>{t('Agent Ranking')}</h3>
            <AgentRankingTable
              items={ranking?.items ?? []}
              loading={rankQuery.isLoading}
              sortBy={sortBy}
              order={order}
              onSortChange={handleSortChange}
              page={rankPage}
              pageSize={ranking?.page_size ?? DEFAULT_PAGE_SIZE}
              total={ranking?.total ?? 0}
              onPageChange={setRankPage}
            />
          </section>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
