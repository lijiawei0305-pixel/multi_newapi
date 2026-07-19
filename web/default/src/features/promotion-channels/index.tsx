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
import { Copy, Plus } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
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
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { fmtDateTime } from '@/lib/agent-format'

import { createPromotionChannel, getPromotionChannels } from './api'

/** Build the agent's dedicated sign-up link for a channel on the current host. */
function signupLink(code: string): string {
  const origin =
    typeof window !== 'undefined' && window.location?.origin
      ? window.location.origin
      : ''
  return `${origin}/sign-up?channel=${code}`
}

function CreateChannelDialog({
  open,
  onOpenChange,
  onSuccess,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSuccess: () => void
}) {
  const { t } = useTranslation()
  const [name, setName] = useState('')
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (open) setName('')
  }, [open])

  const handleSubmit = async () => {
    if (!name.trim()) {
      toast.error(t('Please enter a channel name'))
      return
    }
    setSubmitting(true)
    try {
      const res = await createPromotionChannel(name.trim())
      if (res.success) {
        toast.success(t('Channel created'))
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
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('New Channel')}</DialogTitle>
          <DialogDescription>
            {t(
              'Create a promotion channel to attribute sign-ups from your link.'
            )}
          </DialogDescription>
        </DialogHeader>
        <div className='flex flex-col gap-2'>
          <Label htmlFor='channel-name'>{t('Channel Name')}</Label>
          <Input
            id='channel-name'
            data-testid='channel-name-input'
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={t('e.g. WeChat group')}
          />
        </div>
        <DialogFooter>
          <DialogClose render={<Button variant='outline' />}>
            {t('Cancel')}
          </DialogClose>
          <Button
            type='button'
            disabled={submitting}
            onClick={handleSubmit}
            data-testid='channel-create-submit'
          >
            {submitting ? t('Saving...') : t('Create')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function PromotionChannels() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { copyToClipboard } = useCopyToClipboard()
  const [createOpen, setCreateOpen] = useState(false)

  const { data, isLoading } = useQuery({
    queryKey: ['tenant-promotion-channels'],
    queryFn: async () => {
      const res = await getPromotionChannels()
      return res.data || []
    },
    placeholderData: (prev) => prev,
  })

  const rows = data || []

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Promotion Channels')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          size='sm'
          onClick={() => setCreateOpen(true)}
          data-testid='channel-create-btn'
        >
          <Plus className='h-4 w-4' />
          {t('New Channel')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='flex flex-col gap-4' data-testid='channels-page'>
          <div className='overflow-hidden rounded-lg border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('Code')}</TableHead>
                  <TableHead>{t('Name')}</TableHead>
                  <TableHead>{t('Registered')}</TableHead>
                  <TableHead>{t('Created At')}</TableHead>
                  <TableHead>{t('Promotion Link')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {isLoading && (
                  <TableRow>
                    <TableCell
                      colSpan={5}
                      className='text-muted-foreground text-center'
                    >
                      {t('Loading...')}
                    </TableCell>
                  </TableRow>
                )}
                {!isLoading && rows.length === 0 && (
                  <TableRow>
                    <TableCell
                      colSpan={5}
                      className='text-muted-foreground text-center'
                    >
                      {t('No channels yet')}
                    </TableCell>
                  </TableRow>
                )}
                {!isLoading &&
                  rows.length > 0 &&
                  rows.map((row) => {
                    const link = signupLink(row.code)
                    return (
                      <TableRow
                        key={row.id}
                        data-testid={`channel-row-${row.code}`}
                      >
                        <TableCell className='font-mono text-sm'>
                          {row.code}
                        </TableCell>
                        <TableCell>{row.name}</TableCell>
                        <TableCell className='tabular-nums'>
                          {row.registered_count}
                        </TableCell>
                        <TableCell className='text-muted-foreground text-sm'>
                          {fmtDateTime(row.created_at)}
                        </TableCell>
                        <TableCell>
                          <div className='flex items-center gap-2'>
                            <code className='bg-muted max-w-[280px] truncate rounded px-1.5 py-0.5 text-xs'>
                              {link}
                            </code>
                            <Button
                              variant='outline'
                              size='icon'
                              className='h-7 w-7'
                              onClick={() => copyToClipboard(link)}
                              aria-label={t('Copy link')}
                            >
                              <Copy className='h-3.5 w-3.5' />
                            </Button>
                          </div>
                        </TableCell>
                      </TableRow>
                    )
                  })}
              </TableBody>
            </Table>
          </div>
        </div>
      </SectionPageLayout.Content>

      <CreateChannelDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onSuccess={() =>
          queryClient.invalidateQueries({
            queryKey: ['tenant-promotion-channels'],
          })
        }
      />
    </SectionPageLayout>
  )
}
