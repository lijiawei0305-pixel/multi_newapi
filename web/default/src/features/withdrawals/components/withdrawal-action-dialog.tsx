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
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { approveWithdrawal, rejectWithdrawal } from '../api'
import { cny } from '../lib'
import { useWithdrawals } from './withdrawals-provider'

export function WithdrawalActionDialog() {
  const { t } = useTranslation()
  const { action, currentRow, closeAction, triggerRefresh } = useWithdrawals()
  const [reason, setReason] = useState('')
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (action) setReason('')
  }, [action, currentRow])

  if (!action || !currentRow) return null

  const isReject = action === 'reject'
  const title = isReject ? t('Reject withdrawal') : t('Approve withdrawal')
  const desc = isReject
    ? t('Reject the withdrawal of {{amount}} from {{agent}}?', {
        amount: cny(currentRow.amount_cny),
        agent: currentRow.agent_name,
      })
    : t('Approve the withdrawal of {{amount}} from {{agent}}?', {
        amount: cny(currentRow.amount_cny),
        agent: currentRow.agent_name,
      })

  const handleConfirm = async () => {
    setLoading(true)
    try {
      const res = isReject
        ? await rejectWithdrawal(currentRow.id, reason.trim())
        : await approveWithdrawal(currentRow.id)
      if (res.success) {
        toast.success(isReject ? t('Has been rejected') : t('Has been approved'))
        triggerRefresh()
        closeAction()
      }
    } catch {
      toast.error(t('Operation failed'))
    } finally {
      setLoading(false)
    }
  }

  return (
    <ConfirmDialog
      open
      onOpenChange={(v) => !v && closeAction()}
      title={title}
      desc={desc}
      handleConfirm={handleConfirm}
      isLoading={loading}
      confirmText={isReject ? t('Reject') : t('Approve')}
      destructive={isReject}
    >
      {isReject && (
        <div className='flex flex-col gap-2'>
          <Label htmlFor='wd-reject-reason'>{t('Reason (optional)')}</Label>
          <Textarea
            id='wd-reject-reason'
            data-testid='wd-reject-reason'
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            placeholder={t('Reason for rejection')}
            rows={3}
          />
        </div>
      )}
    </ConfirmDialog>
  )
}
