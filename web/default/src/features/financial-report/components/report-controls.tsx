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
import { CalendarDays, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import dayjs from '@/lib/dayjs'
import { computeTimeRange } from '@/lib/time'
import {
  GRANULARITY_OPTIONS,
  LENS_OPTIONS,
  granularityLabel,
  lensLabel,
} from '../lib'
import type { Granularity, Lens, RangeParams } from '../types'

// ============================================================================
// Report toolbar: lens tabs + date-range (preset + custom from/to) + trend
// granularity. SCOPE-AGNOSTIC and fully controlled — the admin and agent pages
// render the identical control. Time is epoch-seconds on the wire (contract §3);
// the native date inputs are normalized to start/end-of-day UTC-local seconds.
// ============================================================================

const RANGE_PRESETS: { value: string; labelKey: string; days: number }[] = [
  { value: '7d', labelKey: 'Last 7 days', days: 7 },
  { value: '30d', labelKey: 'Last 30 days', days: 30 },
  { value: '90d', labelKey: 'Last 90 days', days: 90 },
  { value: '180d', labelKey: 'Last 6 months', days: 180 },
  { value: '365d', labelKey: 'Last 12 months', days: 365 },
]

export interface ReportControlsProps {
  range: RangeParams
  onRangeChange: (range: RangeParams) => void
  /**
   * Lens tabs are OPTIONAL: rendered only when both `lens` and `onLensChange`
   * are supplied (the agent/admin lens-driven views). The simplified admin
   * finance report has no lens switch, so it omits them and only shows
   * range + granularity + refresh.
   */
  lens?: Lens
  onLensChange?: (lens: Lens) => void
  granularity: Granularity
  onGranularityChange: (granularity: Granularity) => void
  /** Optional manual refresh affordance. */
  onRefresh?: () => void
  refreshing?: boolean
}

const dateInputClass =
  'border-input bg-transparent dark:bg-input/30 h-7 rounded-md border px-2 text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50'

export function ReportControls(props: ReportControlsProps) {
  const { t } = useTranslation()
  const [preset, setPreset] = useState<string>('30d')

  const startStr = dayjs(props.range.start_timestamp * 1000).format('YYYY-MM-DD')
  const endStr = dayjs(props.range.end_timestamp * 1000).format('YYYY-MM-DD')

  const applyPreset = (value: string | null) => {
    if (!value) return
    setPreset(value)
    const found = RANGE_PRESETS.find((p) => p.value === value)
    if (found) props.onRangeChange(computeTimeRange(found.days))
  }

  const setStart = (value: string) => {
    if (!value) return
    setPreset('custom')
    props.onRangeChange({
      start_timestamp: dayjs(value).startOf('day').unix(),
      end_timestamp: props.range.end_timestamp,
    })
  }

  const setEnd = (value: string) => {
    if (!value) return
    setPreset('custom')
    props.onRangeChange({
      start_timestamp: props.range.start_timestamp,
      end_timestamp: dayjs(value).endOf('day').unix(),
    })
  }

  const { lens, onLensChange } = props

  return (
    <div className='flex flex-col gap-3' data-testid='report-controls'>
      {lens && onLensChange && (
        <Tabs
          value={lens}
          onValueChange={(value) => onLensChange(value as Lens)}
        >
          <TabsList aria-label={t('Data lens')}>
            {LENS_OPTIONS.map((l) => (
              <TabsTrigger
                key={l}
                value={l}
                data-testid={`lens-tab-${l}`}
                className='px-3 text-xs'
              >
                {lensLabel(l, t)}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      )}

      <div className='flex flex-wrap items-end gap-3'>
        <div className='flex flex-col gap-1.5'>
          <span className='text-muted-foreground text-xs font-medium'>
            {t('Date range')}
          </span>
          <Select value={preset} onValueChange={applyPreset}>
            <SelectTrigger size='sm' data-testid='range-preset'>
              <CalendarDays className='size-3.5' />
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {RANGE_PRESETS.map((p) => (
                <SelectItem key={p.value} value={p.value}>
                  {t(p.labelKey)}
                </SelectItem>
              ))}
              <SelectItem value='custom'>{t('Custom')}</SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div className='flex flex-col gap-1.5'>
          <span className='text-muted-foreground text-xs font-medium'>
            {t('From')}
          </span>
          <input
            type='date'
            value={startStr}
            max={endStr}
            onChange={(e) => setStart(e.target.value)}
            data-testid='range-start'
            className={dateInputClass}
          />
        </div>

        <div className='flex flex-col gap-1.5'>
          <span className='text-muted-foreground text-xs font-medium'>
            {t('To')}
          </span>
          <input
            type='date'
            value={endStr}
            min={startStr}
            onChange={(e) => setEnd(e.target.value)}
            data-testid='range-end'
            className={dateInputClass}
          />
        </div>

        <div className='flex flex-col gap-1.5'>
          <span className='text-muted-foreground text-xs font-medium'>
            {t('Granularity')}
          </span>
          <Select
            value={props.granularity}
            onValueChange={(value) =>
              props.onGranularityChange(value as Granularity)
            }
          >
            <SelectTrigger size='sm' data-testid='granularity-select'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {GRANULARITY_OPTIONS.map((g) => (
                <SelectItem key={g} value={g}>
                  {granularityLabel(g, t)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        {props.onRefresh && (
          <Button
            variant='outline'
            size='sm'
            onClick={props.onRefresh}
            data-testid='report-refresh'
          >
            <RefreshCw className={props.refreshing ? 'animate-spin' : undefined} />
            {t('Refresh')}
          </Button>
        )}
      </div>
    </div>
  )
}
