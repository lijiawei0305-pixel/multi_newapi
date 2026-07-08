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
import { getApiErrorCode } from '@/lib/api'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import {
  approveWithdrawal,
  markPaidWithdrawal,
  rejectWithdrawal,
} from '../api'
import { cny } from '../lib'
import { useWithdrawals } from './withdrawals-provider'

export function WithdrawalActionDialog() {
  const { t } = useTranslation()
  const { action, currentRow, closeAction, triggerRefresh } = useWithdrawals()
  const [reason, setReason] = useState('')
  const [payoutRef, setPayoutRef] = useState('')
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (action) {
      setReason('')
      setPayoutRef('')
    }
  }, [action, currentRow])

  if (!action || !currentRow) return null

  const isReject = action === 'reject'
  const isMarkPaid = action === 'mark-paid'

  const title = isMarkPaid
    ? t('Mark as paid', { defaultValue: '标记已打款' })
    : isReject
      ? t('Reject withdrawal')
      : t('Approve withdrawal')
  const desc = isMarkPaid
    ? t(
        'Confirm {{amount}} has been paid offline to {{agent}}, and record the payout reference number.',
        {
          amount: cny(currentRow.amount_cny),
          agent: currentRow.agent_name,
          defaultValue: '确认已向 {{agent}} 线下打款 {{amount}}，并登记打款单号/凭证。',
        }
      )
    : isReject
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
      if (isMarkPaid) {
        const ref = payoutRef.trim()
        if (!ref) {
          toast.error(
            t('Please enter the payout reference number', {
              defaultValue: '请填写打款单号/凭证',
            })
          )
          return
        }
        const res = await markPaidWithdrawal(currentRow.id, ref)
        if (res.success) {
          toast.success(t('Marked as paid', { defaultValue: '已标记为已打款' }))
          triggerRefresh()
          closeAction()
        }
        return
      }
      const res = isReject
        ? await rejectWithdrawal(currentRow.id, reason.trim())
        : await approveWithdrawal(currentRow.id)
      if (res.success) {
        toast.success(isReject ? t('Has been rejected') : t('Has been approved'))
        triggerRefresh()
        closeAction()
      }
    } catch (err) {
      if (isMarkPaid) {
        const code = getApiErrorCode(err)
        if (code === 'PAYOUT_REF_REQUIRED') {
          toast.error(
            t('Please enter the payout reference number', {
              defaultValue: '请填写打款单号/凭证',
            })
          )
        } else if (code === 'WITHDRAW_NOT_APPROVED') {
          toast.error(
            t(
              'This withdrawal is no longer in approved status and cannot be marked as paid',
              {
                defaultValue: '该提现单状态已发生变化（非「已通过」），无法标记已打款',
              }
            )
          )
          triggerRefresh()
          closeAction()
        } else {
          toast.error(t('Operation failed'))
        }
      } else {
        toast.error(t('Operation failed'))
      }
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
      confirmText={
        isMarkPaid
          ? t('Confirm payment', { defaultValue: '确认已打款' })
          : isReject
            ? t('Reject')
            : t('Approve')
      }
      destructive={isReject}
      disabled={isMarkPaid && !payoutRef.trim()}
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
      {isMarkPaid && (
        <div className='flex flex-col gap-2'>
          <Label htmlFor='wd-payout-ref'>
            {t('Payout Reference', { defaultValue: '打款单号/凭证' })}
          </Label>
          <Input
            id='wd-payout-ref'
            data-testid='wd-payout-ref'
            value={payoutRef}
            onChange={(e) => setPayoutRef(e.target.value)}
            placeholder={t('e.g. bank transfer serial number', {
              defaultValue: '如银行转账流水号',
            })}
          />
        </div>
      )}
    </ConfirmDialog>
  )
}
