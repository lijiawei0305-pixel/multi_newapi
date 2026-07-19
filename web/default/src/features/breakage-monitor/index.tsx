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
import { useQuery } from '@tanstack/react-query'
import { VChart } from '@visactor/react-vchart'
import {
  AlertTriangle,
  Download,
  PiggyBank,
  TrendingUp,
  Wallet,
} from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { useChartTheme } from '@/lib/use-chart-theme'
import { cn } from '@/lib/utils'
import { VCHART_OPTION } from '@/lib/vchart'

import {
  exportBreakageDetailCsv,
  getBreakageDetail,
  getBreakageOverview,
  getBreakageSnapshots,
} from './api'
import {
  ALERT_LEVEL_FILTER_OPTIONS,
  alertLevelMeta,
  clampPct,
  count,
  formatDate,
  formatSnapshotTs,
  unusedBarColor,
  usd,
} from './lib'
import type {
  AlertLevel,
  BreakageDetailParams,
  BreakageSnapshotPoint,
} from './types'

// ============================================================================
// Breakage 监控页（P2-BRK-01）：4 指标卡（活跃剩余 / 到期未用 / 钱包未消耗 / 系统异常）
// + 明细表（筛选：档位 / 时间 / 告警级 + 分页 + CSV 导出）+ 历史快照趋势图。
// 全端点走 /api/admin/breakage（AdminAuth + TenantMiddleware）：主站看全平台，代理
// 隔离本租户 —— 租户作用域由后端从鉴权解析，前端绝不传 tenant_id。文案全中文（W5）。
// ============================================================================

const PAGE_SIZE = 20
const DAY = 86400
const EMPTY_SNAPSHOT_SERIES: BreakageSnapshotPoint[] = []

/** 预设区间（epoch 秒）：滚动 N 天，end = 现在。 */
function presetRange(days: number): { start: number; end: number } {
  const end = Math.floor(Date.now() / 1000)
  return { start: end - days * DAY, end }
}

const PRESETS: { key: string; label: string; days: number }[] = [
  { key: '30d', label: '近30天', days: 30 },
  { key: '90d', label: '近90天', days: 90 },
  { key: '180d', label: '近半年', days: 180 },
  { key: '365d', label: '近一年', days: 365 },
]

export function BreakageMonitor() {
  const { t } = useTranslation()
  const { resolvedTheme, themeReady } = useChartTheme()

  // ---- 明细筛选状态 ----
  const [planCode, setPlanCode] = useState('')
  const [alertLevel, setAlertLevel] = useState<'' | AlertLevel>('')
  const [detailPreset, setDetailPreset] = useState('') // '' = 全部时间
  const [detailRange, setDetailRange] = useState<{
    start?: number
    end?: number
  }>({})
  const [page, setPage] = useState(1)
  const [exporting, setExporting] = useState(false)

  // ---- 快照趋势区间（独立于明细筛选）----
  const [trendPreset, setTrendPreset] = useState('90d')
  const [trendRange, setTrendRange] = useState(() => presetRange(90))

  const resetPage = () => setPage(1)

  // ---- 1) 概览 4 卡 ----
  const overviewQuery = useQuery({
    queryKey: ['admin-breakage-overview'],
    queryFn: getBreakageOverview,
    select: (res) => res.data,
    placeholderData: (prev) => prev,
  })
  const overview = overviewQuery.data

  // ---- 2) 明细表 ----
  const detailParams: BreakageDetailParams = {
    page,
    page_size: PAGE_SIZE,
    ...(planCode.trim() ? { plan_code: planCode.trim() } : {}),
    ...(alertLevel ? { alert_level: alertLevel } : {}),
    ...(detailRange.start ? { start_timestamp: detailRange.start } : {}),
    ...(detailRange.end ? { end_timestamp: detailRange.end } : {}),
  }
  const detailQuery = useQuery({
    queryKey: ['admin-breakage-detail', detailParams],
    queryFn: () => getBreakageDetail(detailParams),
    select: (res) => res.data,
    placeholderData: (prev) => prev,
  })
  const detail = detailQuery.data
  const rows = detail?.items ?? []
  // 明细表是否包含租户列：跨租户(主站)时后端回带 tenant_id；代理作用域省略。
  const crossTenant = rows.some((r) => r.tenant_id !== undefined)
  const totalPages = detail
    ? Math.max(1, Math.ceil(detail.total / PAGE_SIZE))
    : 1

  // ---- 3) 快照趋势 ----
  const snapshotsQuery = useQuery({
    queryKey: ['admin-breakage-snapshots', trendRange],
    queryFn: () =>
      getBreakageSnapshots({
        start_timestamp: trendRange.start,
        end_timestamp: trendRange.end,
      }),
    select: (res) => res.data,
    placeholderData: (prev) => prev,
  })
  const series = snapshotsQuery.data?.series ?? EMPTY_SNAPSHOT_SERIES

  const pickDetailPreset = (key: string) => {
    setDetailPreset(key)
    resetPage()
    if (!key) {
      setDetailRange({})
      return
    }
    const found = PRESETS.find((p) => p.key === key)
    if (found) {
      const r = presetRange(found.days)
      setDetailRange({ start: r.start, end: r.end })
    }
  }

  const pickTrendPreset = (key: string) => {
    setTrendPreset(key)
    const found = PRESETS.find((p) => p.key === key)
    if (found) setTrendRange(presetRange(found.days))
  }

  const onExport = async () => {
    setExporting(true)
    try {
      await exportBreakageDetailCsv({
        ...(planCode.trim() ? { plan_code: planCode.trim() } : {}),
        ...(alertLevel ? { alert_level: alertLevel } : {}),
        ...(detailRange.start ? { start_timestamp: detailRange.start } : {}),
        ...(detailRange.end ? { end_timestamp: detailRange.end } : {}),
      })
      toast.success(t('Export started', { defaultValue: '已开始导出' }))
    } catch {
      toast.error(t('Export failed', { defaultValue: '导出失败' }))
    } finally {
      setExporting(false)
    }
  }

  // ---- 指标卡定义 ----
  const cards = [
    {
      key: 'active',
      label: '套餐剩余额度',
      hint: '活跃订阅未消耗的额度合计',
      value: usd(overview?.active_remaining_usd),
      icon: PiggyBank,
      tone: 'text-foreground',
      testid: 'breakage-card-active',
    },
    {
      key: 'expired',
      label: '到期未使用余额',
      hint: '已到期订阅里未消耗的额度（按到期时间判定）',
      value: usd(overview?.expired_unused_usd),
      icon: TrendingUp,
      tone: 'text-amber-600',
      testid: 'breakage-card-expired',
    },
    {
      key: 'wallet',
      label: '钱包未消耗余额',
      hint: '用户钱包里尚未消耗的余额合计',
      value: usd(overview?.wallet_unused_usd),
      icon: Wallet,
      tone: 'text-foreground',
      testid: 'breakage-card-wallet',
    },
    {
      key: 'anomaly',
      label: '系统异常',
      hint: '需要关注的异常订阅条数',
      value: count(overview?.anomaly_count),
      icon: AlertTriangle,
      tone:
        (overview?.anomaly_count ?? 0) > 0
          ? 'text-destructive'
          : 'text-foreground',
      testid: 'breakage-card-anomaly',
    },
  ]

  // ---- 趋势图 spec（VChart 双线：到期未用 / 活跃剩余）----
  const chartTextColor =
    resolvedTheme === 'dark'
      ? 'rgba(255, 255, 255, 0.68)'
      : 'rgba(15, 23, 42, 0.58)'
  const chartGridColor =
    resolvedTheme === 'dark'
      ? 'rgba(255, 255, 255, 0.12)'
      : 'rgba(15, 23, 42, 0.12)'

  const expiredLabel = '到期未使用'
  const activeLabel = '活跃剩余'

  const chartValues = useMemo(() => {
    const out: { bucket: string; series: string; value: number }[] = []
    for (const p of series) {
      const bucket = formatSnapshotTs(p.period_end)
      out.push({
        bucket,
        series: expiredLabel,
        value: Number(p.expired_unused_usd) || 0,
      })
      out.push({
        bucket,
        series: activeLabel,
        value: Number(p.active_remaining_usd) || 0,
      })
    }
    return out
  }, [series])

  const chartSpec = useMemo(
    () => ({
      type: 'line' as const,
      data: [{ id: 'breakage-trend', values: chartValues }],
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
            formatMethod: (value: number | string) => usd(Number(value)),
            style: { fill: chartTextColor, fontSize: 10 },
          },
          grid: {
            visible: true,
            style: { lineDash: [3, 3], stroke: chartGridColor },
          },
        },
      ],
      tooltip: {
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
                usd(Number(datum?.value) || 0),
            },
          ],
        },
      },
      background: { fill: 'transparent' },
    }),
    [chartValues, chartTextColor, chartGridColor]
  )
  const chartKey = `breakage-trend-${resolvedTheme}-${series.length}`

  const detailColSpan = crossTenant ? 9 : 8

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Breakage Monitor', { defaultValue: '额度沉淀监控' })}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div
          className='flex flex-col gap-6'
          data-testid='breakage-monitor-page'
        >
          {/* ---- 4 指标卡 ---- */}
          <div
            className='grid grid-cols-2 gap-3 lg:grid-cols-4'
            data-testid='breakage-cards'
          >
            {cards.map((c) => (
              <div
                key={c.key}
                className='rounded-lg border px-3 py-3 sm:px-4'
                data-testid={c.testid}
              >
                <div className='flex items-center gap-2'>
                  <c.icon className='text-muted-foreground/60 size-3.5 shrink-0' />
                  <div className='text-muted-foreground truncate text-xs font-medium'>
                    {c.label}
                  </div>
                </div>
                {overviewQuery.isLoading && !overview ? (
                  <Skeleton className='mt-2 h-6 w-24' />
                ) : (
                  <div
                    className={cn(
                      'mt-1.5 font-mono text-lg font-bold tracking-tight tabular-nums sm:text-xl',
                      c.tone
                    )}
                  >
                    {c.value}
                  </div>
                )}
                <div className='text-muted-foreground/60 mt-1 text-[11px] leading-snug'>
                  {c.hint}
                </div>
              </div>
            ))}
          </div>

          {/* ---- 明细区：筛选栏 + 导出 ---- */}
          <section className='flex flex-col gap-3'>
            <div className='flex flex-wrap items-center justify-between gap-2'>
              <h3 className='text-sm font-semibold'>额度沉淀明细</h3>
              <Button
                size='sm'
                variant='outline'
                onClick={onExport}
                disabled={exporting}
                data-testid='breakage-export'
              >
                <Download className='size-3.5' />
                {exporting ? '导出中…' : '导出 CSV'}
              </Button>
            </div>

            <div className='flex flex-wrap items-center gap-2'>
              <div className='flex items-center gap-1'>
                <Button
                  size='sm'
                  variant={detailPreset === '' ? 'default' : 'outline'}
                  onClick={() => pickDetailPreset('')}
                >
                  全部时间
                </Button>
                {PRESETS.map((p) => (
                  <Button
                    key={p.key}
                    size='sm'
                    variant={detailPreset === p.key ? 'default' : 'outline'}
                    onClick={() => pickDetailPreset(p.key)}
                  >
                    {p.label}
                  </Button>
                ))}
              </div>

              <Input
                className='h-8 w-40'
                placeholder='档位（plan_code）'
                value={planCode}
                onChange={(e) => {
                  setPlanCode(e.target.value)
                  resetPage()
                }}
                data-testid='breakage-plan-filter'
              />

              <NativeSelect
                className='w-36'
                value={alertLevel}
                onChange={(e) => {
                  setAlertLevel(e.target.value as '' | AlertLevel)
                  resetPage()
                }}
                data-testid='breakage-alert-filter'
              >
                <NativeSelectOption value=''>全部告警级</NativeSelectOption>
                {ALERT_LEVEL_FILTER_OPTIONS.map((lv) => (
                  <NativeSelectOption key={lv} value={lv}>
                    {alertLevelMeta(lv, t).label}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </div>

            {/* ---- 明细表 ---- */}
            <div
              className='overflow-hidden rounded-lg border'
              data-testid='breakage-detail-table'
            >
              <Table>
                <TableHeader>
                  <TableRow>
                    {crossTenant && <TableHead>租户</TableHead>}
                    <TableHead>用户</TableHead>
                    <TableHead>档位</TableHead>
                    <TableHead>状态</TableHead>
                    <TableHead>使用情况</TableHead>
                    <TableHead>未使用</TableHead>
                    <TableHead>告警级</TableHead>
                    <TableHead>到期时间</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {detailQuery.isLoading && rows.length === 0 && (
                    <TableRow>
                      <TableCell
                        colSpan={detailColSpan}
                        className='text-muted-foreground text-center'
                      >
                        加载中…
                      </TableCell>
                    </TableRow>
                  )}
                  {!detailQuery.isLoading && rows.length === 0 && (
                    <TableRow>
                      <TableCell
                        colSpan={detailColSpan}
                        className='text-muted-foreground text-center'
                      >
                        无数据
                      </TableCell>
                    </TableRow>
                  )}
                  {rows.length > 0 &&
                    rows.map((row) => {
                      const pct = clampPct(row.usage_pct)
                      const meta = alertLevelMeta(row.alert_level, t)
                      const unusedPctVal = clampPct(100 - pct)
                      return (
                        <TableRow
                          key={`${row.user_id}-${row.plan_code}-${row.period_end}`}
                          data-testid={`breakage-row-${row.user_id}`}
                        >
                          {crossTenant && (
                            <TableCell className='text-muted-foreground'>
                              {row.tenant_name || `#${row.tenant_id ?? '-'}`}
                            </TableCell>
                          )}
                          <TableCell>
                            <span className='font-medium'>{row.username}</span>
                            <span className='text-muted-foreground tabular-nums'>
                              {' '}
                              #{row.user_id}
                            </span>
                          </TableCell>
                          <TableCell className='font-mono text-sm'>
                            {row.plan_code}
                          </TableCell>
                          <TableCell className='text-muted-foreground'>
                            {row.status}
                          </TableCell>
                          <TableCell>
                            <div className='flex min-w-[140px] flex-col gap-1'>
                              <div className='flex items-center justify-between text-xs'>
                                <span className='text-muted-foreground'>
                                  {usd(row.used_usd)} / {usd(row.limit_usd)}
                                </span>
                                <span className='tabular-nums'>
                                  {pct.toFixed(0)}%
                                </span>
                              </div>
                              <div className='bg-muted h-1.5 w-full overflow-hidden rounded-full'>
                                <div
                                  className={cn(
                                    'h-full rounded-full',
                                    unusedBarColor(unusedPctVal)
                                  )}
                                  style={{ width: `${unusedPctVal}%` }}
                                />
                              </div>
                            </div>
                          </TableCell>
                          <TableCell className='tabular-nums'>
                            {usd(row.unused_usd)}
                          </TableCell>
                          <TableCell>
                            <StatusBadge
                              label={meta.label}
                              variant={meta.variant}
                              copyable={false}
                            />
                          </TableCell>
                          <TableCell className='text-muted-foreground text-sm'>
                            {formatDate(row.period_end)}
                          </TableCell>
                        </TableRow>
                      )
                    })}
                </TableBody>
              </Table>
            </div>

            {detail && detail.total > PAGE_SIZE && (
              <div className='flex items-center justify-end gap-3 text-sm'>
                <span className='text-muted-foreground'>
                  共 {detail.total} 条 · 第 {page}/{totalPages} 页
                </span>
                <Button
                  size='sm'
                  variant='outline'
                  disabled={page <= 1}
                  onClick={() => setPage((p) => Math.max(1, p - 1))}
                >
                  上一页
                </Button>
                <Button
                  size='sm'
                  variant='outline'
                  disabled={page >= totalPages}
                  onClick={() => setPage((p) => p + 1)}
                >
                  下一页
                </Button>
              </div>
            )}
          </section>

          {/* ---- 快照趋势 ---- */}
          <section
            className='bg-card flex flex-col overflow-hidden rounded-lg border'
            data-testid='breakage-trend'
          >
            <header className='flex flex-wrap items-center gap-2 border-b px-4 py-3'>
              <TrendingUp className='text-muted-foreground/60 size-4 shrink-0' />
              <h3 className='text-sm font-semibold'>历史沉淀趋势</h3>
              <div className='ml-auto flex items-center gap-1'>
                {PRESETS.map((p) => (
                  <Button
                    key={p.key}
                    size='sm'
                    variant={trendPreset === p.key ? 'default' : 'outline'}
                    onClick={() => pickTrendPreset(p.key)}
                  >
                    {p.label}
                  </Button>
                ))}
              </div>
            </header>
            <div className='h-64 p-2 sm:h-72'>
              {(snapshotsQuery.isLoading || !themeReady) && (
                <Skeleton className='h-full w-full' />
              )}
              {!snapshotsQuery.isLoading &&
                themeReady &&
                chartValues.length === 0 && (
                  <div className='text-muted-foreground/80 flex h-full items-center justify-center text-xs'>
                    暂无快照数据
                  </div>
                )}
              {!snapshotsQuery.isLoading &&
                themeReady &&
                chartValues.length > 0 && (
                  <VChart
                    key={chartKey}
                    spec={{
                      ...chartSpec,
                      theme: resolvedTheme === 'dark' ? 'dark' : 'light',
                      background: 'transparent',
                    }}
                    option={VCHART_OPTION}
                  />
                )}
            </div>
          </section>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
