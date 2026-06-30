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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
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
import { listStuckOrders, runReconcile } from './api'

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

  const [lastResult, setLastResult] = useState('')
  const runMut = useMutation({
    mutationFn: runReconcile,
    onSuccess: (res) => {
      const r = res.rcg
      const s = res.sub
      setLastResult(
        `RCG ${t('scanned')}${r?.scanned ?? 0}/${t('credited')}${r?.credited?.length ?? 0}/${t('failed')}${Object.keys(r?.failed ?? {}).length} · ` +
          `SUB ${t('scanned')}${s?.scanned ?? 0}/${t('activated')}${s?.activated?.length ?? 0}/${t('unpaid')}${s?.unpaid?.length ?? 0}/${t('failed')}${Object.keys(s?.failed ?? {}).length}`
      )
      qc.invalidateQueries({ queryKey: ['admin-reconcile-stuck'] })
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
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
