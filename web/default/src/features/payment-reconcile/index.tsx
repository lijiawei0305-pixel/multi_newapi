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
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { listHistory, listStuckOrders, runReconcile } from './api'

/** relTime returns a short language-neutral "5m" / "2h" / "3d" string from a unix-seconds timestamp. */
function relTime(unixSecs: number): string {
  const diff = Math.max(0, Math.floor(Date.now() / 1000 - unixSecs))
  if (diff < 60) return `${diff}s`
  if (diff < 3600) return `${Math.floor(diff / 60)}m`
  if (diff < 86400) return `${Math.floor(diff / 3600)}h`
  return `${Math.floor(diff / 86400)}d`
}

/** Admin page: payment stuck-order reconciliation — view stuck orders + trigger an immediate sweep. */
export function PaymentReconcile() {
  const { t } = useTranslation()
  const qc = useQueryClient()

  const { data, isLoading } = useQuery({
    queryKey: ['admin-reconcile-stuck'],
    queryFn: listStuckOrders,
    placeholderData: (prev) => prev,
  })
  const stuck = data?.stuck || []

  const { data: history = [] } = useQuery({
    queryKey: ['admin-reconcile-history'],
    queryFn: () => listHistory(50),
    placeholderData: (prev) => prev,
  })
  const [expanded, setExpanded] = useState<Record<number, boolean>>({})

  const hb = data?.heartbeat
  const failedCount = hb?.last_failed_count ?? 0
  const statusKind = failedCount > 0 ? 'fail' : stuck.length > 0 ? 'stuck' : 'ok'
  const statusLabel =
    statusKind === 'fail'
      ? t('Has failures')
      : statusKind === 'stuck'
        ? t('Has stuck orders')
        : t('Normal')
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
      setLastResult(
        `RCG ${t('scanned')}${r?.scanned ?? 0}/${t('credited')}${r?.credited?.length ?? 0}/${t('failed')}${Object.keys(r?.failed ?? {}).length} · ` +
          `RCG-created ${t('scanned')}${c?.scanned ?? 0}/${t('credited')}${c?.credited?.length ?? 0}/${t('failed')}${Object.keys(c?.failed ?? {}).length} · ` +
          `SUB ${t('scanned')}${s?.scanned ?? 0}/${t('activated')}${s?.activated?.length ?? 0}/${t('unpaid')}${s?.unpaid?.length ?? 0}/${t('failed')}${Object.keys(s?.failed ?? {}).length}`
      )
      qc.invalidateQueries({ queryKey: ['admin-reconcile-stuck'] })
      qc.invalidateQueries({ queryKey: ['admin-reconcile-history'] })
    },
  })

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Payment Reconcile')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          onClick={() => runMut.mutate()}
          disabled={runMut.isPending}
          data-testid='reconcile-run'
        >
          {runMut.isPending ? t('Reconciling...') : t('Reconcile Now')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div
          className='bg-muted/40 mb-3 flex flex-wrap items-center gap-x-4 gap-y-1 rounded-md border p-3 text-sm'
          data-testid='reconcile-heartbeat'
        >
          <span>
            {t('Last reconcile')}:{' '}
            {hb?.last_run_at ? `${relTime(hb.last_run_at)} ${t('ago')}` : t('Never')}
          </span>
          <span>
            {t('Runs today')}: <span className='tabular-nums'>{hb?.today_runs ?? 0}</span>
          </span>
          <span>
            {t('Status')}: <span className={statusClass}>{statusLabel}</span>
          </span>
        </div>
        <p className='text-muted-foreground mb-3 text-sm'>
          {t('Auto-reconcile runs every 5 minutes; only orders stuck past the threshold appear here.')}
        </p>
        {lastResult && (
          <div
            className='bg-muted/40 mb-3 rounded-md border p-3 text-sm'
            data-testid='reconcile-result'
          >
            {t('Last run')}: {lastResult}
          </div>
        )}
        <div className='overflow-hidden rounded-lg border' data-testid='stuck-table'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Type')}</TableHead>
                <TableHead>{t('Order No')}</TableHead>
                <TableHead>{t('Tenant')}</TableHead>
                <TableHead>{t('User')}</TableHead>
                <TableHead>{t('Amount')}</TableHead>
                <TableHead>{t('Status')}</TableHead>
                <TableHead>{t('Stuck')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading ? (
                <TableRow>
                  <TableCell colSpan={7} className='text-muted-foreground text-center'>
                    {t('Loading...')}
                  </TableCell>
                </TableRow>
              ) : stuck.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={7} className='text-muted-foreground text-center'>
                    {t('No stuck orders')}
                  </TableCell>
                </TableRow>
              ) : (
                stuck.map((o) => (
                  <TableRow key={o.order_no} data-testid={`stuck-row-${o.order_no}`}>
                    <TableCell>
                      <Badge variant={o.kind === 'RCG' ? 'secondary' : 'outline'}>{o.kind}</Badge>
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
        <h3 className='mt-6 mb-2 text-sm font-medium'>{t('Reconcile History')}</h3>
        <div className='overflow-hidden rounded-lg border' data-testid='history-table'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className='w-8'></TableHead>
                <TableHead>{t('Time')}</TableHead>
                <TableHead>{t('Trigger')}</TableHead>
                <TableHead>{t('Summary')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {history.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={4} className='text-muted-foreground text-center'>
                    {t('No history yet')}
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
                      <TableCell className='text-sm'>
                        {new Date(run.ran_at * 1000).toLocaleString()}
                      </TableCell>
                      <TableCell>
                        <Badge variant={run.trigger === 'manual' ? 'default' : 'secondary'}>
                          {run.trigger === 'manual' ? t('Manual') : t('Scheduled')}
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
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
