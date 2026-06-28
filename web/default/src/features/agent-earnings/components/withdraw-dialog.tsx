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
import { requestWithdrawal } from '../api'
import { cny } from '../lib'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Maximum withdrawable balance (¥). */
  max: number
  onSuccess: () => void
}

export function WithdrawDialog({ open, onOpenChange, max, onSuccess }: Props) {
  const { t } = useTranslation()
  const [amount, setAmount] = useState<number>(0)
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (open) setAmount(0)
  }, [open])

  const invalid = !(amount > 0) || amount > max

  const handleSubmit = async () => {
    if (invalid) {
      toast.error(t('Please enter a valid amount'))
      return
    }
    setSubmitting(true)
    try {
      const res = await requestWithdrawal(amount)
      if (res.success) {
        toast.success(t('Withdrawal request submitted'))
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
              const parsed = parseFloat(e.target.value)
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
