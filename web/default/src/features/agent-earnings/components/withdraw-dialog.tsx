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
import { getApiErrorCode } from '@/lib/api'

import { requestWithdrawal } from '../api'
import { cny } from '../lib'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Maximum withdrawable balance (¥). */
  max: number
  onSuccess: () => void
  /** Fired when the request bounces with PAYOUT_ACCOUNT_REQUIRED so the
   * parent page can surface the payout-account settings entry. */
  onPayoutAccountRequired?: () => void
}

export function WithdrawDialog({
  open,
  onOpenChange,
  max,
  onSuccess,
  onPayoutAccountRequired,
}: Props) {
  const { t } = useTranslation()
  const [amount, setAmount] = useState<number>(0)
  const [submitting, setSubmitting] = useState(false)
  const [requestKey, setRequestKey] = useState('')

  useEffect(() => {
    if (open) {
      setAmount(0)
      setRequestKey(window.crypto.randomUUID())
    }
  }, [open])

  const amountInCents = amount * 100
  const invalid =
    !(amount > 0) ||
    amount > max ||
    Math.abs(amountInCents - Math.round(amountInCents)) > 1e-8

  const handleSubmit = async () => {
    if (invalid) {
      toast.error(t('Please enter a valid amount'))
      return
    }
    setSubmitting(true)
    try {
      const res = await requestWithdrawal(
        amount,
        requestKey || window.crypto.randomUUID()
      )
      if (res.success) {
        toast.success(t('Withdrawal request submitted'))
        onOpenChange(false)
        onSuccess()
      }
    } catch (err) {
      const code = getApiErrorCode(err)
      if (code === 'PAYOUT_ACCOUNT_REQUIRED') {
        toast.error(
          t('Please set your payout account before requesting a withdrawal', {
            defaultValue: '请先设置收款账户，再申请提现',
          })
        )
        onOpenChange(false)
        onPayoutAccountRequired?.()
      } else if (code === 'WITHDRAW_INSUFFICIENT') {
        toast.error(
          t('Withdrawal amount exceeds withdrawable balance', {
            defaultValue: '提现金额超过可提现余额',
          })
        )
      } else if (code === 'WITHDRAW_AMOUNT_INVALID') {
        toast.error(t('Please enter a valid amount'))
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
          <DialogTitle>{t('Apply for Withdrawal')}</DialogTitle>
          <DialogDescription>
            {t('Withdrawable balance: {{amount}}', { amount: cny(max) })}
          </DialogDescription>
        </DialogHeader>

        <div className='flex flex-col gap-2'>
          <Label htmlFor='withdraw-amount'>{t('Withdrawal Amount (¥)')}</Label>
          <Input
            id='withdraw-amount'
            data-testid='withdraw-amount'
            type='number'
            step='0.01'
            min={0}
            max={max}
            value={amount || ''}
            onChange={(e) => {
              const parsed = Number.parseFloat(e.target.value)
              setAmount(Number.isNaN(parsed) ? 0 : parsed)
            }}
            placeholder='0.00'
          />
        </div>

        <DialogFooter>
          <DialogClose render={<Button variant='outline' />}>
            {t('Cancel')}
          </DialogClose>
          <Button
            type='button'
            disabled={submitting || invalid}
            onClick={handleSubmit}
            data-testid='withdraw-submit'
          >
            {submitting ? t('Saving...') : t('Submit')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
