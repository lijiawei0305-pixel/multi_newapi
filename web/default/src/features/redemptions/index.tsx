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
import { Ticket } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { SectionPageLayout } from '@/components/layout'
import { StatusBadge, type StatusVariant } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { fmtDateTime, usd } from '@/lib/agent-format'

import { createRedemptions, getRedemptions } from './api'
import type { Redemption } from './types'

/** Tolerant status → badge styling. */
function statusMeta(
  status: string | undefined,
  t: (k: string) => string
): { variant: StatusVariant; label: string } {
  switch (status) {
    case 'unused':
    case 'available':
    case 'enabled':
      return { variant: 'success', label: t('Unused') }
    case 'used':
      return { variant: 'neutral', label: t('Used') }
    case 'disabled':
      return { variant: 'danger', label: t('Disabled') }
    case 'expired':
      return { variant: 'warning', label: t('Expired') }
    default:
      return { variant: 'neutral', label: status || '-' }
  }
}

function CreateRedemptionDrawer({
  open,
  onOpenChange,
  onSuccess,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSuccess: () => void
}) {
  const { t } = useTranslation()
  const [amount, setAmount] = useState<number>(0)
  const [count, setCount] = useState<number>(1)
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (open) {
      setAmount(0)
      setCount(1)
    }
  }, [open])

  const total = (amount || 0) * (count || 0)
  const invalid = !(amount > 0) || !(count > 0)

  const handleSubmit = async () => {
    if (invalid) {
      toast.error(t('Please enter a valid amount and count'))
      return
    }
    setSubmitting(true)
    try {
      const res = await createRedemptions({ amount_usd: amount, count })
      if (res.success) {
        toast.success(t('Redemption codes generated'))
        onOpenChange(false)
        onSuccess()
      }
    } catch {
      toast.error(t('Request failed'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className={sideDrawerContentClassName('sm:max-w-[460px]')}>
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle>{t('Generate Redemption Codes')}</SheetTitle>
          <SheetDescription>
            {t('Codes are pre-deducted from your quota as count × amount_usd.')}
          </SheetDescription>
        </SheetHeader>

        <div className={sideDrawerFormClassName()}>
          <div className='flex flex-col gap-2'>
            <Label htmlFor='redeem-amount'>{t('Amount per code ($)')}</Label>
            <Input
              id='redeem-amount'
              data-testid='redeem-amount-input'
              type='number'
              step='0.01'
              min={0}
              value={amount || ''}
              onChange={(e) => {
                const parsed = Number.parseFloat(e.target.value)
                setAmount(Number.isNaN(parsed) ? 0 : parsed)
              }}
              placeholder='0.00'
            />
          </div>

          <div className='flex flex-col gap-2'>
            <Label htmlFor='redeem-count'>{t('Count')}</Label>
            <Input
              id='redeem-count'
              data-testid='redeem-count-input'
              type='number'
              min={1}
              step='1'
              value={count || ''}
              onChange={(e) => {
                const parsed = Number.parseInt(e.target.value, 10)
                setCount(Number.isNaN(parsed) ? 0 : parsed)
              }}
              placeholder='1'
            />
          </div>

          <p className='text-muted-foreground text-sm'>
            {t('This will pre-deduct {{total}} from your quota.', {
              total: usd(total),
            })}
          </p>
        </div>

        <SheetFooter className={sideDrawerFooterClassName()}>
          <SheetClose render={<Button variant='outline' />}>
            {t('Cancel')}
          </SheetClose>
          <Button
            type='button'
            disabled={submitting || invalid}
            onClick={handleSubmit}
            data-testid='redeem-create-submit'
          >
            {submitting ? t('Saving...') : t('Generate')}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}

export function Redemptions() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [createOpen, setCreateOpen] = useState(false)

  const { data, isLoading } = useQuery({
    queryKey: ['tenant-redemptions'],
    queryFn: async () => {
      const res = await getRedemptions()
      return res.data || []
    },
    placeholderData: (prev) => prev,
  })

  const rows = data || []

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Redemption Codes')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          size='sm'
          onClick={() => setCreateOpen(true)}
          data-testid='redeem-create-btn'
        >
          <Ticket className='h-4 w-4' />
          {t('Generate Codes')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='flex flex-col gap-4' data-testid='redemptions-page'>
          <div className='overflow-hidden rounded-lg border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('Code')}</TableHead>
                  <TableHead>{t('Amount ($)')}</TableHead>
                  <TableHead>{t('Status')}</TableHead>
                  <TableHead>{t('Created At')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {isLoading && (
                  <TableRow>
                    <TableCell
                      colSpan={4}
                      className='text-muted-foreground text-center'
                    >
                      {t('Loading...')}
                    </TableCell>
                  </TableRow>
                )}
                {!isLoading && rows.length === 0 && (
                  <TableRow>
                    <TableCell
                      colSpan={4}
                      className='text-muted-foreground text-center'
                    >
                      {t('No redemption codes yet')}
                    </TableCell>
                  </TableRow>
                )}
                {!isLoading &&
                  rows.length > 0 &&
                  rows.map((row: Redemption) => {
                    const meta = statusMeta(row.status, t)
                    return (
                      <TableRow
                        key={row.id}
                        data-testid={`redeem-row-${row.code}`}
                      >
                        <TableCell className='font-mono text-sm'>
                          {row.code}
                        </TableCell>
                        <TableCell className='tabular-nums'>
                          {usd(row.amount_usd)}
                        </TableCell>
                        <TableCell>
                          <StatusBadge
                            variant={meta.variant}
                            label={meta.label}
                            copyable={false}
                          />
                        </TableCell>
                        <TableCell className='text-muted-foreground text-sm'>
                          {fmtDateTime(row.created_at)}
                        </TableCell>
                      </TableRow>
                    )
                  })}
              </TableBody>
            </Table>
          </div>
        </div>
      </SectionPageLayout.Content>

      <CreateRedemptionDrawer
        open={createOpen}
        onOpenChange={setCreateOpen}
        onSuccess={() =>
          queryClient.invalidateQueries({ queryKey: ['tenant-redemptions'] })
        }
      />
    </SectionPageLayout>
  )
}
