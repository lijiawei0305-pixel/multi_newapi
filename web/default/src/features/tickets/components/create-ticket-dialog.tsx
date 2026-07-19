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
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'

import { PRIORITY_VALUES, priorityMeta, ticketErrorMessage } from '../lib'
import type { CreateTicketPayload, TicketPriority } from '../types'

const TITLE_MAX = 255

// ============================================================================
// User-only "New Ticket" dialog. Client-side guards mirror the backend
// (title/content required, title length, priority enum) but the server remains
// authoritative — its TICKET_* code is mapped to a localized toast on failure.
// ============================================================================

export function CreateTicketDialog(props: {
  onCreate: (payload: CreateTicketPayload) => Promise<number | null>
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [title, setTitle] = useState('')
  const [content, setContent] = useState('')
  const [priority, setPriority] = useState<TicketPriority>('normal')
  const [submitting, setSubmitting] = useState(false)

  const reset = () => {
    setTitle('')
    setContent('')
    setPriority('normal')
  }

  const handleOpenChange = (next: boolean) => {
    setOpen(next)
    if (!next) reset()
  }

  const handleSubmit = async () => {
    const trimmedTitle = title.trim()
    const trimmedContent = content.trim()
    if (!trimmedTitle || !trimmedContent) {
      toast.error(t('Please provide a title and message'))
      return
    }
    setSubmitting(true)
    try {
      const id = await props.onCreate({
        title: trimmedTitle,
        content: trimmedContent,
        priority,
      })
      if (id != null) {
        toast.success(t('Ticket created'))
        handleOpenChange(false)
      }
    } catch (err) {
      toast.error(ticketErrorMessage(err, t))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger render={<Button data-testid='ticket-create-open' />}>
        {t('New Ticket')}
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('New Ticket')}</DialogTitle>
          <DialogDescription>
            {t('Describe your issue and our team will get back to you.')}
          </DialogDescription>
        </DialogHeader>

        <div className='flex flex-col gap-4'>
          <div className='flex flex-col gap-2'>
            <Label htmlFor='ticket-title'>{t('Title')}</Label>
            <Input
              id='ticket-title'
              value={title}
              maxLength={TITLE_MAX}
              onChange={(e) => setTitle(e.target.value)}
              placeholder={t('Brief summary of your issue')}
              data-testid='ticket-title-input'
            />
          </div>

          <div className='flex flex-col gap-2'>
            <Label htmlFor='ticket-priority'>{t('Priority')}</Label>
            <Select
              value={priority}
              onValueChange={(v) => setPriority(v as TicketPriority)}
            >
              <SelectTrigger
                id='ticket-priority'
                className='w-full'
                data-testid='ticket-priority-select'
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {PRIORITY_VALUES.map((p) => (
                  <SelectItem key={p} value={p}>
                    {priorityMeta(p, t).label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className='flex flex-col gap-2'>
            <Label htmlFor='ticket-content'>{t('Message')}</Label>
            <Textarea
              id='ticket-content'
              value={content}
              onChange={(e) => setContent(e.target.value)}
              placeholder={t('Describe your issue in detail')}
              className='min-h-32'
              data-testid='ticket-content-input'
            />
          </div>
        </div>

        <DialogFooter>
          <DialogClose render={<Button variant='outline' />}>
            {t('Cancel')}
          </DialogClose>
          <Button
            type='button'
            disabled={submitting}
            onClick={handleSubmit}
            data-testid='ticket-create-submit'
          >
            {submitting ? t('Submitting...') : t('Submit')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
