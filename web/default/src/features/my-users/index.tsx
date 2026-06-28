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
import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { SectionPageLayout } from '@/components/layout'
import { StatusBadge, type StatusVariant } from '@/components/status-badge'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { fmtDateTime, quotaToUsd } from '@/lib/agent-format'
import { getApiErrorCode } from '@/lib/api'
import { getTenantUsers, setTenantUserTier } from './api'
import type { TenantUser, UserTier } from './types'

/** Tolerant user-status → badge styling (new-api: 1 enabled, 2 disabled). */
function statusMeta(
  status: number | string | undefined,
  t: (k: string) => string
): { variant: StatusVariant; label: string } {
  const s = typeof status === 'number' ? String(status) : status
  switch (s) {
    case '1':
    case 'enabled':
    case 'active':
      return { variant: 'success', label: t('Enabled') }
    case '2':
    case '3':
    case 'disabled':
    case 'banned':
      return { variant: 'danger', label: t('Disabled') }
    default:
      return { variant: 'neutral', label: s || '-' }
  }
}

/** Normalize an arbitrary `group` value to a tier the agent can manage. */
function asTier(group: string | undefined): UserTier {
  return group === 'vip' ? 'vip' : 'default'
}

/** Tier cell: shows the current tier when known, otherwise a neutral dash. */
function TierCell({ tier }: { tier: string | undefined }) {
  const { t } = useTranslation()
  if (!tier) {
    return <span className='text-muted-foreground'>—</span>
  }
  const label =
    tier === 'vip' ? t('VIP') : tier === 'default' ? t('Default') : tier
  return (
    <Badge variant={tier === 'vip' ? 'default' : 'secondary'}>{label}</Badge>
  )
}

/** Per-user dialog to set the membership tier (default / vip). */
function SetTierDialog({
  user,
  open,
  onOpenChange,
  onSuccess,
}: {
  user: TenantUser | null
  open: boolean
  onOpenChange: (open: boolean) => void
  onSuccess: (id: number, tier: UserTier) => void
}) {
  const { t } = useTranslation()
  const [tier, setTier] = useState<UserTier>('default')
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (open && user) setTier(asTier(user.group))
  }, [open, user])

  const handleSubmit = async () => {
    if (!user) return
    setSubmitting(true)
    try {
      const res = await setTenantUserTier(user.id, { tier })
      if (res.success) {
        toast.success(t('Tier updated'))
        onSuccess(user.id, tier)
        onOpenChange(false)
      }
    } catch (err) {
      const code = getApiErrorCode(err)
      if (code === 'AGENT_TIER_INVALID') {
        toast.error(t('Invalid tier'))
      } else if (code === 'AGENT_FORBIDDEN') {
        toast.error(t('This user is not in your tenant'))
      } else {
        toast.error(t('Request failed'))
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('Set Tier')}</DialogTitle>
          <DialogDescription>
            {t('Set the membership tier for this user.')}
          </DialogDescription>
        </DialogHeader>
        <div className='flex flex-col gap-2'>
          <Label htmlFor='tier-select'>{t('Tier')}</Label>
          <Select
            value={tier}
            onValueChange={(value) => {
              if (value === 'default' || value === 'vip') setTier(value)
            }}
          >
            <SelectTrigger
              id='tier-select'
              className='w-full'
              data-testid='tier-select'
            >
              <SelectValue>
                {tier === 'vip' ? t('VIP') : t('Default')}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='default'>{t('Default')}</SelectItem>
              <SelectItem value='vip'>{t('VIP')}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <DialogFooter>
          <DialogClose render={<Button variant='outline' />}>
            {t('Cancel')}
          </DialogClose>
          <Button
            type='button'
            disabled={submitting}
            onClick={handleSubmit}
            data-testid='tier-save-submit'
          >
            {submitting ? t('Saving...') : t('Save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function MyUsers() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  // Freshly-set tiers, by user id. The list endpoint does not return `group`,
  // so we reflect the agent's own changes optimistically within the session.
  const [tierOverrides, setTierOverrides] = useState<Record<number, UserTier>>(
    {}
  )
  const [dialogUser, setDialogUser] = useState<TenantUser | null>(null)
  const [dialogOpen, setDialogOpen] = useState(false)

  const { data, isLoading } = useQuery({
    queryKey: ['tenant-users'],
    queryFn: async () => {
      const res = await getTenantUsers()
      return res.data || []
    },
    placeholderData: (prev) => prev,
  })

  const rows = data || []

  const openTierDialog = (user: TenantUser) => {
    setDialogUser(user)
    setDialogOpen(true)
  }

  const handleTierSaved = (id: number, tier: UserTier) => {
    setTierOverrides((prev) => ({ ...prev, [id]: tier }))
    queryClient.invalidateQueries({ queryKey: ['tenant-users'] })
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('My Users')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='flex flex-col gap-4' data-testid='my-users-page'>
          <div className='overflow-hidden rounded-lg border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('ID')}</TableHead>
                  <TableHead>{t('Username')}</TableHead>
                  <TableHead>{t('Display Name')}</TableHead>
                  <TableHead>{t('Balance ($)')}</TableHead>
                  <TableHead>{t('Used ($)')}</TableHead>
                  <TableHead>{t('Tier')}</TableHead>
                  <TableHead>{t('Status')}</TableHead>
                  <TableHead>{t('Created At')}</TableHead>
                  <TableHead className='text-right'>{t('Actions')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {isLoading ? (
                  <TableRow>
                    <TableCell
                      colSpan={9}
                      className='text-muted-foreground text-center'
                    >
                      {t('Loading...')}
                    </TableCell>
                  </TableRow>
                ) : rows.length === 0 ? (
                  <TableRow>
                    <TableCell
                      colSpan={9}
                      className='text-muted-foreground text-center'
                    >
                      {t('No users yet')}
                    </TableCell>
                  </TableRow>
                ) : (
                  rows.map((row: TenantUser) => {
                    const meta = statusMeta(row.status, t)
                    const tier = tierOverrides[row.id] ?? row.group
                    return (
                      <TableRow key={row.id} data-testid={`user-row-${row.id}`}>
                        <TableCell className='tabular-nums'>{row.id}</TableCell>
                        <TableCell className='font-medium'>
                          {row.username}
                        </TableCell>
                        <TableCell>{row.display_name || '-'}</TableCell>
                        <TableCell className='tabular-nums'>
                          {quotaToUsd(row.quota)}
                        </TableCell>
                        <TableCell className='tabular-nums'>
                          {quotaToUsd(row.used_quota)}
                        </TableCell>
                        <TableCell>
                          <TierCell tier={tier} />
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
                        <TableCell className='text-right'>
                          <Button
                            variant='outline'
                            size='sm'
                            onClick={() => openTierDialog(row)}
                            data-testid={`set-tier-${row.id}`}
                          >
                            {t('Set Tier')}
                          </Button>
                        </TableCell>
                      </TableRow>
                    )
                  })
                )}
              </TableBody>
            </Table>
          </div>
        </div>
      </SectionPageLayout.Content>

      <SetTierDialog
        user={dialogUser}
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        onSuccess={handleTierSaved}
      />
    </SectionPageLayout>
  )
}
