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
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { getApiErrorCode } from '@/lib/api'
import { getTenantGroups, updateTenantGroup } from './api'
import type { TenantGroup } from './types'

/** Editable row: local draft of the model-group multiplier, saved per row. */
function GroupRow({ row, onSaved }: { row: TenantGroup; onSaved: () => void }) {
  const { t } = useTranslation()
  const [ratio, setRatio] = useState<number>(Number(row.ratio ?? 0))
  const [saving, setSaving] = useState(false)

  const floor = Number(row.floor ?? row.platform_ratio ?? 0)
  const platform = Number(row.platform_ratio ?? floor)
  const belowFloor = Number.isFinite(ratio) && ratio < floor

  const handleSave = async () => {
    // Front-end floor guard: only mark up at or above the platform baseline.
    if (belowFloor) {
      toast.error(t('Ratio must not be lower than {{floor}}', { floor }))
      return
    }
    setSaving(true)
    try {
      const res = await updateTenantGroup(row.group_name, { ratio })
      if (res.success) {
        toast.success(t('Saved'))
        onSaved()
      }
    } catch (err) {
      // Back-end backstop for the same floor rule + non-model groups.
      const code = getApiErrorCode(err)
      if (code === 'RATIO_BELOW_FLOOR') {
        toast.error(t('Ratio is below the platform baseline'))
      } else if (code === 'AGENT_GROUP_NOT_MODEL') {
        toast.error(t('Only model-group multipliers can be adjusted'))
      } else {
        toast.error(t('Request failed'))
      }
    } finally {
      setSaving(false)
    }
  }

  return (
    <TableRow data-testid={`group-row-${row.group_name}`}>
      <TableCell className='font-medium'>{row.group_name}</TableCell>
      <TableCell className='tabular-nums'>{platform}</TableCell>
      <TableCell className='tabular-nums'>{floor}</TableCell>
      <TableCell>
        <Input
          type='number'
          step='0.01'
          min={floor}
          className='h-8 w-28'
          value={Number.isFinite(ratio) ? ratio : ''}
          aria-invalid={belowFloor}
          onChange={(e) => {
            const parsed = parseFloat(e.target.value)
            setRatio(Number.isNaN(parsed) ? 0 : parsed)
          }}
        />
        {belowFloor && (
          <p className='text-destructive mt-1 text-xs'>
            {t('Must not be lower than {{floor}}', { floor })}
          </p>
        )}
      </TableCell>
      <TableCell>
        {row.has_override ? (
          <Badge variant='default'>{t('Overridden')}</Badge>
        ) : (
          <Badge variant='secondary'>{t('Baseline')}</Badge>
        )}
      </TableCell>
      <TableCell className='text-right'>
        <Button
          size='sm'
          onClick={handleSave}
          disabled={saving || belowFloor}
          data-testid={`group-save-${row.group_name}`}
        >
          {saving ? t('Saving...') : t('Save')}
        </Button>
      </TableCell>
    </TableRow>
  )
}

export function MyGroups() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const { data, isLoading } = useQuery({
    queryKey: ['tenant-groups'],
    queryFn: async () => {
      const res = await getTenantGroups()
      return res.data || []
    },
    placeholderData: (prev) => prev,
  })

  const rows = data || []
  const refresh = () =>
    queryClient.invalidateQueries({ queryKey: ['tenant-groups'] })

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Model Group Multipliers')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='flex flex-col gap-4' data-testid='groups-page'>
          <p className='text-muted-foreground text-sm'>
            {t(
              'You can only set a multiplier at or above the platform baseline (mark up to earn the spread).'
            )}
          </p>
          <div className='overflow-hidden rounded-lg border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('Group')}</TableHead>
                  <TableHead>{t('Platform Baseline')}</TableHead>
                  <TableHead>{t('Floor')}</TableHead>
                  <TableHead>{t('Your Ratio')}</TableHead>
                  <TableHead>{t('Status')}</TableHead>
                  <TableHead className='text-right'>{t('Actions')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {isLoading ? (
                  <TableRow>
                    <TableCell
                      colSpan={6}
                      className='text-muted-foreground text-center'
                    >
                      {t('Loading...')}
                    </TableCell>
                  </TableRow>
                ) : rows.length === 0 ? (
                  <TableRow>
                    <TableCell
                      colSpan={6}
                      className='text-muted-foreground text-center'
                    >
                      {t('No groups yet')}
                    </TableCell>
                  </TableRow>
                ) : (
                  rows.map((row: TenantGroup) => (
                    <GroupRow
                      key={row.group_name}
                      row={row}
                      onSaved={refresh}
                    />
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
