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
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'

// ============================================================================
// Reply composer. Empty replies are blocked client-side (backend also enforces
// TICKET_REPLY_EMPTY). On success the parent clears the field via the resolved
// promise; on failure the text is kept so the author can retry.
// ============================================================================

export function TicketReplyBox(props: {
  onSubmit: (content: string) => Promise<boolean>
  submitLabel?: string
}) {
  const { t } = useTranslation()
  const [content, setContent] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const handleSubmit = async () => {
    const trimmed = content.trim()
    if (!trimmed) return
    setSubmitting(true)
    try {
      const ok = await props.onSubmit(trimmed)
      if (ok) setContent('')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className='flex flex-col gap-2' data-testid='ticket-reply-box'>
      <Textarea
        value={content}
        onChange={(e) => setContent(e.target.value)}
        placeholder={t('Write a reply...')}
        className='min-h-24'
        data-testid='ticket-reply-input'
      />
      <div className='flex justify-end'>
        <Button
          type='button'
          disabled={submitting || !content.trim()}
          onClick={handleSubmit}
          data-testid='ticket-reply-submit'
        >
          {submitting
            ? t('Sending...')
            : (props.submitLabel ?? t('Send Reply'))}
        </Button>
      </div>
    </div>
  )
}
