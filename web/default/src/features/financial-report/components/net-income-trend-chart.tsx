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
import { VChart } from '@visactor/react-vchart'
import { TrendingUp } from 'lucide-react'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { Skeleton } from '@/components/ui/skeleton'
import { cny } from '@/features/financial-report/lib'
import type { NetIncomeTrendPoint } from '@/features/financial-report/types'
import { useChartTheme } from '@/lib/use-chart-theme'
import { VCHART_OPTION } from '@/lib/vchart'

// ============================================================================
// Dedicated 3-line trend chart for the simplified ADMIN 财务报表 page
// (doc/admin-finance-report-simplify.md §三): 套餐净收入 (tokenplan_net_cny) /
// api净收入 (api_net_cny) / 总净收入 (their sum, computed here — the wire contract
// never carries a combined field). Same structure/theme/tooltip as the agent's
// earnings-trend-chart (VChart only — no recharts). Its window total reconciles
// with the 6 overview cards: Σ套餐净 = 卡1+卡2−卡5, Σapi净 = 卡3+卡4−卡6.
// ============================================================================

export interface NetIncomeTrendChartProps {
  series: NetIncomeTrendPoint[]
  loading?: boolean
}

export function NetIncomeTrendChart({
  series,
  loading,
}: NetIncomeTrendChartProps) {
  const { t } = useTranslation()
  const { resolvedTheme, themeReady } = useChartTheme()

  const tokenplanLabel = t('Tokenplan Net Income', {
    defaultValue: '套餐净收入',
  })
  const apiLabel = t('Apikey Net Income', { defaultValue: 'api净收入' })
  // NOT the bare 'Total' key — zh.json already binds it to '总计' (a different
  // existing feature), which would silently shadow this chart's defaultValue.
  // Use a distinct, unclaimed key so the Chinese defaultValue always applies.
  const totalLabel = t('Total Net Income', { defaultValue: '总净收入' })

  const values = useMemo(() => {
    const out: { bucket: string; series: string; value: number }[] = []
    for (const point of series) {
      const tokenplan = Number(point.tokenplan_net_cny) || 0
      const apiNet = Number(point.api_net_cny) || 0
      out.push({
        bucket: point.bucket,
        series: tokenplanLabel,
        value: tokenplan,
      })
      out.push({ bucket: point.bucket, series: apiLabel, value: apiNet })
      out.push({
        bucket: point.bucket,
        series: totalLabel,
        value: tokenplan + apiNet,
      })
    }
    return out
  }, [series, tokenplanLabel, apiLabel, totalLabel])

  const chartTextColor =
    resolvedTheme === 'dark'
      ? 'rgba(255, 255, 255, 0.68)'
      : 'rgba(15, 23, 42, 0.58)'
  const chartGridColor =
    resolvedTheme === 'dark'
      ? 'rgba(255, 255, 255, 0.12)'
      : 'rgba(15, 23, 42, 0.12)'

  const spec = useMemo(
    () => ({
      type: 'line' as const,
      data: [{ id: 'admin-net-income-trend', values }],
      xField: 'bucket',
      yField: 'value',
      seriesField: 'series',
      legends: { visible: true },
      point: { visible: false },
      line: { style: { lineWidth: 2, curveType: 'monotone' } },
      axes: [
        {
          orient: 'bottom',
          type: 'band',
          label: {
            style: { fill: chartTextColor, fontSize: 10 },
            autoHide: true,
          },
          tick: { visible: false },
        },
        {
          orient: 'left',
          type: 'linear',
          label: {
            formatMethod: (value: number | string) => cny(Number(value)),
            style: { fill: chartTextColor, fontSize: 10 },
          },
          grid: {
            visible: true,
            style: { lineDash: [3, 3], stroke: chartGridColor },
          },
        },
      ],
      tooltip: {
        mark: {
          content: [
            {
              key: (datum: Record<string, unknown>) =>
                String(datum?.series ?? ''),
              value: (datum: Record<string, unknown>) =>
                cny(Number(datum?.value) || 0),
            },
          ],
        },
        dimension: {
          title: {
            value: (datum: Record<string, unknown>) =>
              String(datum?.bucket ?? ''),
          },
          content: [
            {
              key: (datum: Record<string, unknown>) =>
                String(datum?.series ?? ''),
              value: (datum: Record<string, unknown>) =>
                cny(Number(datum?.value) || 0),
            },
          ],
        },
      },
      background: { fill: 'transparent' },
    }),
    [values, chartTextColor, chartGridColor]
  )

  const chartKey = `admin-net-income-trend-${resolvedTheme}-${series.length}`

  return (
    <section
      className='bg-card flex h-full flex-col overflow-hidden rounded-lg border'
      data-testid='net-income-trend-chart'
    >
      <header className='flex items-center gap-2 border-b px-4 py-3'>
        <TrendingUp className='text-muted-foreground/60 size-4 shrink-0' />
        <h3 className='text-sm font-semibold'>
          {t('Net Income Trend', { defaultValue: '净收入趋势' })}
        </h3>
      </header>
      <div className='h-64 p-2 sm:h-72'>
        {(loading || !themeReady) && <Skeleton className='h-full w-full' />}
        {!loading && themeReady && values.length === 0 && (
          <div className='text-muted-foreground/80 flex h-full items-center justify-center text-xs'>
            {t('No data available')}
          </div>
        )}
        {!loading && themeReady && values.length > 0 && (
          <VChart
            key={chartKey}
            spec={{
              ...spec,
              theme: resolvedTheme === 'dark' ? 'dark' : 'light',
              background: 'transparent',
            }}
            option={VCHART_OPTION}
          />
        )}
      </div>
    </section>
  )
}
