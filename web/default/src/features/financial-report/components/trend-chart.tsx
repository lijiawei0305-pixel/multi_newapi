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
import { useMemo } from 'react'
import { VChart } from '@visactor/react-vchart'
import type { TFunction } from 'i18next'
import { TrendingUp } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Skeleton } from '@/components/ui/skeleton'
import { useChartTheme } from '@/lib/use-chart-theme'
import { VCHART_OPTION } from '@/lib/vchart'
import { cny, lensLabel } from '../lib'
import type { Granularity, Lens, TrendPoint } from '../types'

// ============================================================================
// Multi-series trend line chart (VChart, mirrors the dashboard's chart usage).
// Each lens maps to one or more CNY metric fields (contract §1.2); the chart
// is melted to long-format `{ bucket, series, value }` rows. The y-axis is
// always CNY so currencies never mix. SCOPE-AGNOSTIC: admin/agent feed the same
// `series` shape. Do NOT add recharts — VChart only (contract §5).
// ============================================================================

export interface TrendChartProps {
  lens: Lens
  granularity: Granularity
  series: TrendPoint[]
  loading?: boolean
}

interface MetricDef {
  key: string
  label: string
}

function metricsForLens(lens: Lens, t: TFunction): MetricDef[] {
  switch (lens) {
    case 'recharge':
      return [
        { key: 'recharge_paid_cny', label: t('Recharge Paid') },
        { key: 'subscription_paid_cny', label: t('Subscription Paid') },
        { key: 'subscription_cost_cny', label: t('Subscription Cost') },
        { key: 'subscription_spread_cny', label: t('Subscription Spread') },
      ]
    case 'withdrawals':
      return [
        { key: 'pending_cny', label: t('Pending') },
        { key: 'withdrawn_cny', label: t('Withdrawn') },
        { key: 'rejected_cny', label: t('Rejected') },
      ]
    case 'consumption':
      return [{ key: 'used_cost_cny', label: t('Consumption Cost') }]
    case 'earnings':
    default:
      return [{ key: 'amount_cny', label: t('Earnings') }]
  }
}

export function TrendChart({
  lens,
  granularity,
  series,
  loading,
}: TrendChartProps) {
  const { t } = useTranslation()
  const { resolvedTheme, themeReady } = useChartTheme()

  const metrics = useMemo(() => metricsForLens(lens, t), [lens, t])

  const values = useMemo(() => {
    const out: { bucket: string; series: string; value: number }[] = []
    for (const point of series) {
      const rec = point as unknown as Record<string, number | string>
      for (const metric of metrics) {
        out.push({
          bucket: String(point.bucket),
          series: metric.label,
          value: Number(rec[metric.key]) || 0,
        })
      }
    }
    return out
  }, [series, metrics])

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
      data: [{ id: 'finance-trend', values }],
      xField: 'bucket',
      yField: 'value',
      seriesField: 'series',
      legends: { visible: metrics.length > 1 },
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
    [values, metrics, chartTextColor, chartGridColor]
  )

  const chartKey = `finance-trend-${lens}-${granularity}-${resolvedTheme}-${series.length}`

  return (
    <section
      className='bg-card flex h-full flex-col overflow-hidden rounded-lg border'
      data-testid='trend-chart'
    >
      <header className='flex items-center gap-2 border-b px-4 py-3'>
        <TrendingUp className='text-muted-foreground/60 size-4 shrink-0' />
        <h3 className='text-sm font-semibold'>
          {t('{{lens}} Trend', { lens: lensLabel(lens, t) })}
        </h3>
      </header>
      <div className='h-64 p-2 sm:h-72'>
        {loading || !themeReady ? (
          <Skeleton className='h-full w-full' />
        ) : values.length === 0 ? (
          <div className='text-muted-foreground/80 flex h-full items-center justify-center text-xs'>
            {t('No data available')}
          </div>
        ) : (
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
