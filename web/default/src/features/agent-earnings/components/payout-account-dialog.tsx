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
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { getApiErrorCode } from '@/lib/api'

import { updatePayoutAccount } from '../api'
import type { PayoutAccount } from '../types'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  account?: PayoutAccount
  onSaved: () => void
}

/**
 * Agent self-service payout (收款) settings form — task #1 of the payout
 * closure. Loaded from GET, saved via PUT `/api/tenant/payout-account`.
 * Setting this is a prerequisite for withdrawal requests (see WithdrawDialog).
 */
export function PayoutAccountDialog({
  open,
  onOpenChange,
  account,
  onSaved,
}: Props) {
  const { t } = useTranslation()
  const [method, setMethod] = useState<'alipay' | 'bank'>('alipay')
  const [accountNo, setAccountNo] = useState('')
  const [name, setName] = useState('')
  const [bank, setBank] = useState('')
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (open) {
      setMethod(account?.payout_method === 'bank' ? 'bank' : 'alipay')
      setAccountNo(account?.payout_account || '')
      setName(account?.payout_name || '')
      setBank(account?.payout_bank || '')
    }
  }, [open, account])

  const invalid =
    !accountNo.trim() || !name.trim() || (method === 'bank' && !bank.trim())

  const handleSubmit = async () => {
    if (invalid) {
      toast.error(
        t('Please complete the payout account information', {
          defaultValue: '请完整填写收款账户信息',
        })
      )
      return
    }
    setSubmitting(true)
    try {
      const res = await updatePayoutAccount({
        payout_method: method,
        payout_account: accountNo.trim(),
        payout_name: name.trim(),
        payout_bank: method === 'bank' ? bank.trim() : '',
      })
      if (res.success) {
        toast.success(t('Saved'))
        onOpenChange(false)
        onSaved()
      }
    } catch (err) {
      const code = getApiErrorCode(err)
      if (code === 'PAYOUT_ACCOUNT_INVALID') {
        toast.error(
          t('Invalid payout account information — please check every field', {
            defaultValue: '收款账户信息不完整或不合法，请检查各字段',
          })
        )
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
          <DialogTitle>
            {t('Payout Account', { defaultValue: '收款账户' })}
          </DialogTitle>
          <DialogDescription>
            {t(
              'Where your withdrawals get paid out. Set this before requesting a withdrawal.',
              { defaultValue: '提现的打款目标；申请提现前请先完善此信息。' }
            )}
          </DialogDescription>
        </DialogHeader>

        <div className='flex flex-col gap-4'>
          <div className='flex flex-col gap-2'>
            <Label>{t('Payout Method', { defaultValue: '收款方式' })}</Label>
            <RadioGroup
              value={method}
              onValueChange={(value) => setMethod(value)}
              className='flex gap-4'
            >
              <div className='flex items-center gap-2'>
                <RadioGroupItem value='alipay' id='payout-method-alipay' />
                <Label
                  htmlFor='payout-method-alipay'
                  className='cursor-pointer font-normal'
                >
                  {t('Alipay', { defaultValue: '支付宝' })}
                </Label>
              </div>
              <div className='flex items-center gap-2'>
                <RadioGroupItem value='bank' id='payout-method-bank' />
                <Label
                  htmlFor='payout-method-bank'
                  className='cursor-pointer font-normal'
                >
                  {t('Bank Card', { defaultValue: '银行卡' })}
                </Label>
              </div>
            </RadioGroup>
          </div>

          <div className='flex flex-col gap-2'>
            <Label htmlFor='payout-account-no'>
              {method === 'bank'
                ? t('Bank Card Number', { defaultValue: '银行卡号' })
                : t('Alipay Account', {
                    defaultValue: '支付宝账号（邮箱/手机号）',
                  })}
            </Label>
            <Input
              id='payout-account-no'
              data-testid='payout-account-no'
              value={accountNo}
              onChange={(e) => setAccountNo(e.target.value)}
              placeholder={
                method === 'bank' ? '6222 0000 0000 0000' : 'alice@example.com'
              }
            />
          </div>

          <div className='flex flex-col gap-2'>
            <Label htmlFor='payout-account-name'>
              {t('Real Name', { defaultValue: '实名' })}
            </Label>
            <Input
              id='payout-account-name'
              data-testid='payout-account-name'
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t('Payee real name', {
                defaultValue: '收款人真实姓名',
              })}
            />
          </div>

          {method === 'bank' && (
            <div className='flex flex-col gap-2'>
              <Label htmlFor='payout-account-bank'>
                {t('Bank Name', { defaultValue: '开户行' })}
              </Label>
              <Input
                id='payout-account-bank'
                data-testid='payout-account-bank'
                value={bank}
                onChange={(e) => setBank(e.target.value)}
                placeholder={t('e.g. ICBC Beijing Branch', {
                  defaultValue: '如中国工商银行北京分行',
                })}
              />
            </div>
          )}
        </div>

        <DialogFooter>
          <DialogClose render={<Button variant='outline' />}>
            {t('Cancel')}
          </DialogClose>
          <Button
            type='button'
            disabled={submitting || invalid}
            onClick={handleSubmit}
            data-testid='payout-account-submit'
          >
            {submitting ? t('Saving...') : t('Save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
