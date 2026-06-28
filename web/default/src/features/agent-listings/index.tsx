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
import { toast } from 'sonner'
import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { cny, usd } from '@/lib/agent-format'
import { getAgentListings, updateAgentListing } from './api'
import type { AgentListing } from './types'

/** Editable row: local draft of retail price + enabled flag, saved per row. */
function ListingRow({ row, onSaved }: { row: AgentListing; onSaved: () => void }) {
  const { t } = useTranslation()
  const [retail, setRetail] = useState<number>(Number(row.retail_price_cny ?? 0))
  const [enabled, setEnabled] = useState<boolean>(Boolean(row.enabled))
  const [saving, setSaving] = useState(false)

  const floor = Number(row.min_price_cny ?? 0)
  const belowFloor = retail < floor

  const handleSave = async () => {
    if (belowFloor) {
      toast.error(t('Retail price must not be lower than {{floor}}', { floor: cny(floor) }))
      return
    }
    setSaving(true)
    try {
      const res = await updateAgentListing(row.plan_id, {
        retail_price_cny: retail,
        enabled,
      })
      if (res.success) {
        toast.success(t('Saved'))
        onSaved()
      }
    } catch {
      toast.error(t('Request failed'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <TableRow data-testid={`listing-row-${row.code}`}>
      <TableCell className='font-mono text-sm'>{row.code}</TableCell>
      <TableCell>{row.name}</TableCell>
      <TableCell className='tabular-nums'>{cny(row.base_price_cny)}</TableCell>
      <TableCell className='tabular-nums'>{cny(row.min_price_cny)}</TableCell>
      <TableCell className='tabular-nums'>{usd(row.month_limit_usd)}</TableCell>
      <TableCell>
        <Input
          type='number'
          step='0.01'
          min={0}
          className='h-8 w-28'
          value={Number.isFinite(retail) ? retail : ''}
          aria-invalid={belowFloor}
          onChange={(e) => {
            const parsed = parseFloat(e.target.value)
            setRetail(Number.isNaN(parsed) ? 0 : parsed)
          }}
        />
        {belowFloor && (
          <p className='text-destructive mt-1 text-xs'>
            {t('Must not be lower than {{floor}}', { floor: cny(floor) })}
          </p>
        )}
      </TableCell>
      <TableCell>
        <Switch checked={enabled} onCheckedChange={setEnabled} />
      </TableCell>
      <TableCell className='text-right'>
        <Button
          size='sm'
          onClick={handleSave}
          disabled={saving || belowFloor}
          data-testid={`listing-save-${row.code}`}
        >
          {saving ? t('Saving...') : t('Save')}
        </Button>
      </TableCell>
    </TableRow>
  )
}

export function AgentListings() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const { data, isLoading } = useQuery({
    queryKey: ['tenant-listings'],
    queryFn: async () => {
      const res = await getAgentListings()
      return res.data || []
    },
    placeholderData: (prev) => prev,
  })

  const rows = data || []
  const refresh = () =>
    queryClient.invalidateQueries({ queryKey: ['tenant-listings'] })

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Plan Listings & Pricing')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='flex flex-col gap-4' data-testid='listings-page'>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Set your retail price for each plan and toggle whether it is listed. Retail price must not be lower than the protection floor (min_price_cny).'
            )}
          </p>
          <div className='overflow-hidden rounded-lg border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('Code')}</TableHead>
                  <TableHead>{t('Name')}</TableHead>
                  <TableHead>{t('Base Price (¥)')}</TableHead>
                  <TableHead>{t('Floor (¥)')}</TableHead>
                  <TableHead>{t('Monthly Limit ($)')}</TableHead>
                  <TableHead>{t('Retail Price (¥)')}</TableHead>
                  <TableHead>{t('Listed')}</TableHead>
                  <TableHead className='text-right'>{t('Actions')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {isLoading ? (
                  <TableRow>
                    <TableCell
                      colSpan={8}
                      className='text-muted-foreground text-center'
                    >
                      {t('Loading...')}
                    </TableCell>
                  </TableRow>
                ) : rows.length === 0 ? (
                  <TableRow>
                    <TableCell
                      colSpan={8}
                      className='text-muted-foreground text-center'
                    >
                      {t('No listings yet')}
                    </TableCell>
                  </TableRow>
                ) : (
                  rows.map((row) => (
                    <ListingRow key={row.plan_id} row={row} onSaved={refresh} />
                  ))
                )}
              </TableBody>
            </Table>
          </div>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
