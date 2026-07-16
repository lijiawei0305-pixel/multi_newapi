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
import { Fragment, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  NativeSelect,
  NativeSelectOption,
} from '@/components/ui/native-select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { cn } from '@/lib/utils'
import {
  getPaymentOverview,
  listHistory,
  listStuckOrders,
  runReconcile,
  type PaymentStatus,
} from './api'

// ============================================================================
// 支付对账页（doc/payment-overview.md）：顶部「支付概览」= 筛选栏 + 4 状态卡 +
// 可筛订单列表；下方「对账运维」（卡单/运行记录/立即对账）默认折叠。数据全查
// payment_orders（充值+套餐同表，共用 created/paid/credited/failed 四态）。
// ============================================================================

const PAGE_SIZE = 20
const STATUS_ORDER: PaymentStatus[] = ['created', 'paid', 'credited', 'failed']

/** 4 态的中文名 + 卡片/徽章配色。 */
function statusMeta(
  status: string,
  t: (key: string, opts?: Record<string, unknown>) => string
): { label: string; badge: string } {
  switch (status) {
    case 'created':
      return { label: t('Awaiting Payment', { defaultValue: '待支付' }), badge: 'bg-amber-100 text-amber-700 dark:bg-amber-950 dark:text-amber-300' }
    case 'paid':
      return { label: t('Received, Pending Credit', { defaultValue: '已收款待入账' }), badge: 'bg-blue-100 text-blue-700 dark:bg-blue-950 dark:text-blue-300' }
    case 'credited':
      return { label: t('Payment Successful', { defaultValue: '支付成功' }), badge: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300' }
    case 'failed':
      return { label: t('Payment Failed', { defaultValue: '支付失败' }), badge: 'bg-red-100 text-red-700 dark:bg-red-950 dark:text-red-300' }
    default:
      return { label: status, badge: 'bg-muted text-muted-foreground' }
  }
}

const cny = (v: number) => `¥${(Number(v) || 0).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`
const fmtTime = (ts: number) =>
  new Date(ts * 1000).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai' })

/** 预设区间（epoch 秒）；今日=本地零点起，其余=滚动 N 天。 */
function presetRange(preset: string): { start: number; end: number } {
  const end = Math.floor(Date.now() / 1000)
  if (preset === 'today') {
    const d = new Date()
    d.setHours(0, 0, 0, 0)
    return { start: Math.floor(d.getTime() / 1000), end }
  }
  const days = preset === '7d' ? 7 : preset === '90d' ? 90 : 30
  return { start: end - days * 86400, end }
}

const PRESETS: { key: string }[] = [
  { key: 'today' },
  { key: '7d' },
  { key: '30d' },
  { key: '90d' },
]

/** Localized label for a date-range preset key. */
function presetLabel(
  key: string,
  t: (k: string, opts?: Record<string, unknown>) => string
): string {
  switch (key) {
    case 'today':
      return t('Today Only', { defaultValue: '今日' })
    case '7d':
      return t('Last 7 Days', { defaultValue: '近7天' })
    case '30d':
      return t('Last 30 Days', { defaultValue: '近30天' })
    case '90d':
      return t('Last 90 Days', { defaultValue: '近90天' })
    default:
      return key
  }
}

/** relTime returns a short "5m"/"2h"/"3d" from a unix-seconds timestamp. */
function relTime(unixSecs: number): string {
  const diff = Math.max(0, Math.floor(Date.now() / 1000 - unixSecs))
  if (diff < 60) return `${diff}s`
  if (diff < 3600) return `${Math.floor(diff / 60)}m`
  if (diff < 86400) return `${Math.floor(diff / 3600)}h`
  return `${Math.floor(diff / 86400)}d`
}

export function PaymentReconcile() {
  const { t } = useTranslation()
  const qc = useQueryClient()

  // ---- 支付概览筛选状态 ----
  const [preset, setPreset] = useState('30d')
  const [range, setRange] = useState(() => presetRange('30d'))
  const [provider, setProvider] = useState('')
  const [type, setType] = useState('')
  const [statusFilter, setStatusFilter] = useState('')
  const [page, setPage] = useState(1)

  const pickPreset = (key: string) => {
    setPreset(key)
    setRange(presetRange(key))
    setPage(1)
  }
  const pickStatus = (s: string) => {
    setStatusFilter((cur) => (cur === s ? '' : s))
    setPage(1)
  }

  const overviewParams = {
    start_timestamp: range.start,
    end_timestamp: range.end,
    ...(provider ? { provider } : {}),
    ...(type ? { type } : {}),
    ...(statusFilter ? { status: statusFilter } : {}),
    page,
    page_size: PAGE_SIZE,
  }
  const { data: overview, isLoading: ovLoading } = useQuery({
    queryKey: ['admin-payment-overview', overviewParams],
    queryFn: () => getPaymentOverview(overviewParams),
    placeholderData: (prev) => prev,
  })
  const summaryByStatus = new Map(
    (overview?.summary ?? []).map((s) => [s.status, s])
  )
  const orders = overview?.orders
  const totalPages = orders ? Math.max(1, Math.ceil(orders.total / PAGE_SIZE)) : 1

  // ---- 对账运维（折叠区）----
  const [opsOpen, setOpsOpen] = useState(false)
  const { data: stuckData } = useQuery({
    queryKey: ['admin-reconcile-stuck'],
    queryFn: listStuckOrders,
    placeholderData: (prev) => prev,
  })
  const stuck = stuckData?.stuck || []
  const { data: history = [] } = useQuery({
    queryKey: ['admin-reconcile-history'],
    queryFn: () => listHistory(50),
    placeholderData: (prev) => prev,
  })
  const [expanded, setExpanded] = useState<Record<number, boolean>>({})

  const hb = stuckData?.heartbeat
  const failedCount = hb?.last_failed_count ?? 0
  const statusKind = failedCount > 0 ? 'fail' : stuck.length > 0 ? 'stuck' : 'ok'
  const statusLabel =
    statusKind === 'fail'
      ? t('Has failures', { defaultValue: '有失败' })
      : statusKind === 'stuck'
        ? t('Has stuck orders', { defaultValue: '有卡单' })
        : t('Normal', { defaultValue: '正常' })
  const statusClass =
    statusKind === 'fail'
      ? 'text-red-600'
      : statusKind === 'stuck'
        ? 'text-yellow-600'
        : 'text-green-600'

  const [lastResult, setLastResult] = useState('')
  const runMut = useMutation({
    mutationFn: runReconcile,
    onSuccess: (res) => {
      const r = res.rcg
      const s = res.sub
      const c = res.rcg_created
      const g = res.agt
      const rcgFailed = Object.keys(r?.failed ?? {}).length
      const rcgCreatedFailed = Object.keys(c?.failed ?? {}).length
      const subFailed = Object.keys(s?.failed ?? {}).length
      const agtFailed = Object.keys(g?.failed ?? {}).length
      setLastResult(
        [
          t('RCG scanned {{scanned}}/credited {{credited}}/failed {{failed}}', {
            scanned: r?.scanned ?? 0,
            credited: r?.credited?.length ?? 0,
            failed: rcgFailed,
            defaultValue: `RCG 扫${r?.scanned ?? 0}/入账${r?.credited?.length ?? 0}/失败${rcgFailed}`,
          }),
          t('RCG-created scanned {{scanned}}/credited {{credited}}/expired {{expired}}/failed {{failed}}', {
            scanned: c?.scanned ?? 0,
            credited: c?.credited?.length ?? 0,
            expired: c?.expired?.length ?? 0,
            failed: rcgCreatedFailed,
            defaultValue: `RCG-created 扫${c?.scanned ?? 0}/入账${c?.credited?.length ?? 0}/过期${c?.expired?.length ?? 0}/失败${rcgCreatedFailed}`,
          }),
          t('SUB scanned {{scanned}}/activated {{activated}}/unpaid {{unpaid}}/failed {{failed}}', {
            scanned: s?.scanned ?? 0,
            activated: s?.activated?.length ?? 0,
            unpaid: s?.unpaid?.length ?? 0,
            failed: subFailed,
            defaultValue: `SUB 扫${s?.scanned ?? 0}/激活${s?.activated?.length ?? 0}/未付${s?.unpaid?.length ?? 0}/失败${subFailed}`,
          }),
          t('AGT scanned {{scanned}}/activated {{activated}}/unpaid {{unpaid}}/expired {{expired}}/failed {{failed}}', {
            scanned: g?.scanned ?? 0,
            activated: g?.activated?.length ?? 0,
            unpaid: g?.unpaid?.length ?? 0,
            expired: g?.expired?.length ?? 0,
            failed: agtFailed,
            defaultValue: `AGT 扫${g?.scanned ?? 0}/激活${g?.activated?.length ?? 0}/未付${g?.unpaid?.length ?? 0}/过期${g?.expired?.length ?? 0}/失败${agtFailed}`,
          }),
        ].join(' · ')
      )
      qc.invalidateQueries({ queryKey: ['admin-reconcile-stuck'] })
      qc.invalidateQueries({ queryKey: ['admin-reconcile-history'] })
    },
  })

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Payment Reconcile', { defaultValue: '支付对账' })}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        {/* ---- Filter bar ---- */}
        <div className='mb-3 flex flex-wrap items-center gap-2'>
          <div className='flex items-center gap-1'>
            {PRESETS.map((p) => (
              <Button
                key={p.key}
                size='sm'
                variant={preset === p.key ? 'default' : 'outline'}
                onClick={() => pickPreset(p.key)}
              >
                {presetLabel(p.key, t)}
              </Button>
            ))}
          </div>
          <NativeSelect
            className='w-32'
            value={provider}
            onChange={(e) => {
              setProvider(e.target.value)
              setPage(1)
            }}
          >
            <NativeSelectOption value=''>{t('All Methods', { defaultValue: '全部方式' })}</NativeSelectOption>
            <NativeSelectOption value='wxpay'>{t('WeChat', { defaultValue: '微信' })}</NativeSelectOption>
            <NativeSelectOption value='alipay'>{t('Alipay', { defaultValue: '支付宝' })}</NativeSelectOption>
          </NativeSelect>
          <NativeSelect
            className='w-32'
            value={type}
            onChange={(e) => {
              setType(e.target.value)
              setPage(1)
            }}
          >
            <NativeSelectOption value=''>{t('All Order Types', { defaultValue: '全部类型' })}</NativeSelectOption>
            <NativeSelectOption value='recharge'>{t('Recharge', { defaultValue: '充值' })}</NativeSelectOption>
            <NativeSelectOption value='subscription'>{t('Subscription Plan', { defaultValue: '套餐订阅' })}</NativeSelectOption>
          </NativeSelect>
        </div>

        {/* ---- 4 status cards ---- */}
        <div className='mb-4 grid grid-cols-2 gap-3 lg:grid-cols-4'>
          {STATUS_ORDER.map((s) => {
            const meta = statusMeta(s, t)
            const row = summaryByStatus.get(s)
            const selected = statusFilter === s
            return (
              <button
                key={s}
                type='button'
                onClick={() => pickStatus(s)}
                data-testid={`pay-card-${s}`}
                className={cn(
                  'rounded-lg border px-4 py-3 text-left transition',
                  selected
                    ? 'border-primary ring-primary/40 ring-2'
                    : 'hover:border-primary/50'
                )}
              >
                <div className='flex items-center justify-between'>
                  <span
                    className={cn(
                      'rounded px-1.5 py-0.5 text-xs font-medium',
                      meta.badge
                    )}
                  >
                    {meta.label}
                  </span>
                  <span className='text-muted-foreground text-xs tabular-nums'>
                    {t('{{count}} orders', { count: row?.count ?? 0, defaultValue: '{{count}} 笔' })}
                  </span>
                </div>
                <div className='mt-2 font-mono text-lg font-bold tabular-nums'>
                  {cny(row?.amount_cny ?? 0)}
                </div>
              </button>
            )
          })}
        </div>

        {/* ---- Order list ---- */}
        <div className='overflow-hidden rounded-lg border' data-testid='pay-orders'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Order No', { defaultValue: '订单号' })}</TableHead>
                <TableHead>{t('Type', { defaultValue: '类型' })}</TableHead>
                <TableHead>{t('Method', { defaultValue: '方式' })}</TableHead>
                <TableHead>{t('Amount', { defaultValue: '金额' })}</TableHead>
                <TableHead>{t('Status', { defaultValue: '状态' })}</TableHead>
                <TableHead>{t('User / Tenant', { defaultValue: '用户/租户' })}</TableHead>
                <TableHead>{t('Time', { defaultValue: '时间' })}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {ovLoading ? (
                <TableRow>
                  <TableCell colSpan={7} className='text-muted-foreground text-center'>
                    {t('Loading...', { defaultValue: '加载中…' })}
                  </TableCell>
                </TableRow>
              ) : !orders || orders.items.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={7} className='text-muted-foreground text-center'>
                    {t('No Orders', { defaultValue: '无订单' })}
                  </TableCell>
                </TableRow>
              ) : (
                orders.items.map((o) => {
                  const meta = statusMeta(o.status, t)
                  return (
                    <TableRow key={o.order_no} data-testid={`pay-row-${o.order_no}`}>
                      <TableCell className='font-mono text-xs'>{o.order_no}</TableCell>
                      <TableCell>
                        {o.type === 'recharge'
                          ? t('Recharge', { defaultValue: '充值' })
                          : t('Plan', { defaultValue: '套餐' })}
                      </TableCell>
                      <TableCell>
                        {o.provider === 'wxpay'
                          ? t('WeChat', { defaultValue: '微信' })
                          : o.provider === 'alipay'
                            ? t('Alipay', { defaultValue: '支付宝' })
                            : o.provider}
                      </TableCell>
                      <TableCell className='tabular-nums'>{cny(o.amount_cny)}</TableCell>
                      <TableCell>
                        <span className={cn('rounded px-1.5 py-0.5 text-xs font-medium', meta.badge)}>
                          {meta.label}
                        </span>
                      </TableCell>
                      <TableCell className='tabular-nums'>
                        {o.user_id}
                        <span className='text-muted-foreground'> / {o.tenant_id}</span>
                      </TableCell>
                      <TableCell className='text-muted-foreground text-xs'>
                        {fmtTime(o.created_at_ts)}
                      </TableCell>
                    </TableRow>
                  )
                })
              )}
            </TableBody>
          </Table>
        </div>
        {orders && orders.total > PAGE_SIZE && (
          <div className='mt-2 flex items-center justify-end gap-3 text-sm'>
            <span className='text-muted-foreground'>
              {t('{{total}} orders total · page {{page}}/{{totalPages}}', {
                total: orders.total,
                page,
                totalPages,
                defaultValue: '共 {{total}} 笔 · 第 {{page}}/{{totalPages}} 页',
              })}
            </span>
            <Button
              size='sm'
              variant='outline'
              disabled={page <= 1}
              onClick={() => setPage((p) => Math.max(1, p - 1))}
            >
              {t('Previous page', { defaultValue: '上一页' })}
            </Button>
            <Button
              size='sm'
              variant='outline'
              disabled={page >= totalPages}
              onClick={() => setPage((p) => p + 1)}
            >
              {t('Next page', { defaultValue: '下一页' })}
            </Button>
          </div>
        )}

        {/* ---- Reconcile ops (collapsible) ---- */}
        <div className='mt-6 overflow-hidden rounded-lg border'>
          <button
            type='button'
            className='hover:bg-muted/40 flex w-full items-center gap-2 px-4 py-3 text-left text-sm font-medium'
            onClick={() => setOpsOpen((v) => !v)}
            data-testid='ops-toggle'
          >
            {opsOpen ? (
              <ChevronDown className='size-4' />
            ) : (
              <ChevronRight className='size-4' />
            )}
            {t('Reconcile Ops (Stuck Orders / Run History / Run Now)', {
              defaultValue: '对账运维（卡单 / 运行记录 / 立即对账）',
            })}
            <span className={cn('ml-2 text-xs', statusClass)}>· {statusLabel}</span>
          </button>
          {opsOpen && (
            <div className='space-y-3 border-t p-4'>
              <div className='bg-muted/40 flex flex-wrap items-center gap-x-4 gap-y-1 rounded-md border p-3 text-sm'>
                <span>
                  {t('Last Reconciled:', { defaultValue: '上次对账：' })}
                  {hb?.last_run_at
                    ? t('{{time}} ago', { time: relTime(hb.last_run_at), defaultValue: '{{time}} 前' })
                    : t('Never Run', { defaultValue: '从未' })}
                </span>
                <span>
                  {t('Runs Today:', { defaultValue: '今日运行：' })}
                  <span className='tabular-nums'>{hb?.today_runs ?? 0}</span>
                </span>
                <span>
                  {t('Status:', { defaultValue: '状态：' })}
                  <span className={statusClass}>{statusLabel}</span>
                </span>
                <Button
                  size='sm'
                  className='ml-auto'
                  onClick={() => runMut.mutate()}
                  disabled={runMut.isPending}
                  data-testid='reconcile-run'
                >
                  {runMut.isPending
                    ? t('Reconciling…', { defaultValue: '对账中…' })
                    : t('Run Reconcile Now', { defaultValue: '立即对账' })}
                </Button>
              </div>
              <p className='text-muted-foreground text-sm'>
                {t(
                  'Auto-reconcile runs every 5 minutes; only orders still stuck past the threshold are listed here.',
                  {
                    defaultValue: '自动对账每 5 分钟一次；只有超过阈值仍卡住的订单才列在这里。',
                  }
                )}
              </p>
              {lastResult && (
                <div className='bg-muted/40 rounded-md border p-3 text-sm' data-testid='reconcile-result'>
                  {t('Last Run:', { defaultValue: '上次运行：' })}
                  {lastResult}
                </div>
              )}
              <div className='overflow-hidden rounded-lg border' data-testid='stuck-table'>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t('Type', { defaultValue: '类型' })}</TableHead>
                      <TableHead>{t('Order No', { defaultValue: '订单号' })}</TableHead>
                      <TableHead>{t('Tenant', { defaultValue: '租户' })}</TableHead>
                      <TableHead>{t('User', { defaultValue: '用户' })}</TableHead>
                      <TableHead>{t('Amount', { defaultValue: '金额' })}</TableHead>
                      <TableHead>{t('Status', { defaultValue: '状态' })}</TableHead>
                      <TableHead>{t('Stuck For', { defaultValue: '卡住' })}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {stuck.length === 0 ? (
                      <TableRow>
                        <TableCell colSpan={7} className='text-muted-foreground text-center'>
                          {t('No stuck orders', { defaultValue: '无卡单' })}
                        </TableCell>
                      </TableRow>
                    ) : (
                      stuck.map((o) => (
                        <TableRow key={o.order_no} data-testid={`stuck-row-${o.order_no}`}>
                          <TableCell>
                            <Badge variant={o.kind === 'RCG' ? 'secondary' : o.kind === 'AGT' ? 'destructive' : 'outline'}>{o.kind}</Badge>
                          </TableCell>
                          <TableCell className='font-mono text-xs'>{o.order_no}</TableCell>
                          <TableCell className='tabular-nums'>{o.tenant_id}</TableCell>
                          <TableCell className='tabular-nums'>{o.user_id}</TableCell>
                          <TableCell className='tabular-nums'>¥{o.amount}</TableCell>
                          <TableCell>{o.status}</TableCell>
                          <TableCell className='text-muted-foreground text-sm'>
                            {Math.round(o.stuck_secs / 60)}m
                          </TableCell>
                        </TableRow>
                      ))
                    )}
                  </TableBody>
                </Table>
              </div>
              <h3 className='mt-4 mb-1 text-sm font-medium'>
                {t('Reconcile Run History', { defaultValue: '对账运行记录' })}
              </h3>
              <div className='overflow-hidden rounded-lg border' data-testid='history-table'>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className='w-8'></TableHead>
                      <TableHead>{t('Time', { defaultValue: '时间' })}</TableHead>
                      <TableHead>{t('Trigger', { defaultValue: '触发' })}</TableHead>
                      <TableHead>{t('Summary', { defaultValue: '摘要' })}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {history.length === 0 ? (
                      <TableRow>
                        <TableCell colSpan={4} className='text-muted-foreground text-center'>
                          {t('No history yet', { defaultValue: '暂无记录' })}
                        </TableCell>
                      </TableRow>
                    ) : (
                      history.map((run) => (
                        <Fragment key={run.id}>
                          <TableRow
                            className='cursor-pointer'
                            onClick={() => setExpanded((m) => ({ ...m, [run.id]: !m[run.id] }))}
                            data-testid={`history-row-${run.id}`}
                          >
                            <TableCell>
                              {expanded[run.id] ? (
                                <ChevronDown className='size-4' />
                              ) : (
                                <ChevronRight className='size-4' />
                              )}
                            </TableCell>
                            <TableCell className='text-sm'>{fmtTime(run.ran_at)}</TableCell>
                            <TableCell>
                              <Badge variant={run.trigger === 'manual' ? 'default' : 'secondary'}>
                                {run.trigger === 'manual'
                                  ? t('Manual', { defaultValue: '手动' })
                                  : t('Scheduled', { defaultValue: '定时' })}
                              </Badge>
                            </TableCell>
                            <TableCell className='text-sm'>{run.summary}</TableCell>
                          </TableRow>
                          {expanded[run.id] && (
                            <TableRow data-testid={`history-detail-${run.id}`}>
                              <TableCell colSpan={4} className='bg-muted/30'>
                                <pre className='overflow-x-auto text-xs'>
                                  {JSON.stringify(run.detail, null, 2)}
                                </pre>
                              </TableCell>
                            </TableRow>
                          )}
                        </Fragment>
                      ))
                    )}
                  </TableBody>
                </Table>
              </div>
            </div>
          )}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
