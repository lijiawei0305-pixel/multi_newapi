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
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { getPaymentIcon } from '../lib'
import {
  useTenantRecharge,
  MIN_RECHARGE_USD,
  type RechargeProvider,
} from '../hooks/use-tenant-recharge'
import { useRechargeMethods } from '../hooks/use-recharge-methods'
import { RechargeQrDialog } from './dialogs/recharge-qr-dialog'

/**
 * TenantRechargeCard is the multi-tenant recharge section (WeChat / Alipay).
 *
 * Amount is in USD ($1 minimum, credited as native quota at $1 = 500k). WeChat
 * shows a QR modal; Alipay redirects. Distinct from the native epay/Stripe flow
 * — it calls POST /api/tenant/wallet/recharge and settles via the auth-service.
 */
export function TenantRechargeCard() {
  const { t } = useTranslation()
  const [amount, setAmount] = useState<string>(String(MIN_RECHARGE_USD))
  const [provider, setProvider] = useState<RechargeProvider>('wxpay')
  const { submitting, qrState, submit, closeQr } = useTenantRecharge()
  const { methods } = useRechargeMethods()

  // Keep the selected provider within the available set (default = first).
  useEffect(() => {
    if (methods && methods.length > 0 && !methods.includes(provider)) {
      setProvider(methods[0])
    }
  }, [methods, provider])

  // Wait until availability is known to avoid a flash of all buttons.
  if (methods === null) return null
  // No enabled && configured channel → hide the whole card.
  if (methods.length === 0) return null

  const amountNum = parseFloat(amount) || 0
  const belowMin = amountNum < MIN_RECHARGE_USD
  const busy = submitting !== null

  const providerButton = (value: RechargeProvider, label: string) => (
    <Button
      type='button'
      variant='outline'
      data-testid={value === 'wxpay' ? 'recharge-wxpay' : 'recharge-alipay'}
      aria-pressed={provider === value}
      onClick={() => setProvider(value)}
      disabled={busy}
      className={cn(
        'min-h-11 flex-1 justify-center gap-2',
        provider === value
          ? 'border-foreground bg-foreground/5 dark:bg-foreground/10'
          : 'border-muted'
      )}
    >
      {getPaymentIcon(value, 'h-4 w-4')}
      <span>{label}</span>
    </Button>
  )

  return (
    <div className='space-y-3 border-b pb-4 sm:pb-6'>
      <Label className='text-muted-foreground text-xs font-medium tracking-wider uppercase'>
        {t('Recharge (WeChat / Alipay)')}
      </Label>

      <div className='space-y-2'>
        <Label
          htmlFor='tenant-recharge-amount'
          className='text-muted-foreground text-xs'
        >
          {t('Amount (USD), minimum ${{amount}}', { amount: MIN_RECHARGE_USD })}
        </Label>
        <Input
          id='tenant-recharge-amount'
          data-testid='recharge-amount'
          type='number'
          min={MIN_RECHARGE_USD}
          step='1'
          inputMode='decimal'
          value={amount}
          onChange={(e) => setAmount(e.target.value)}
          placeholder={`$${MIN_RECHARGE_USD}`}
          className='h-9 sm:h-10'
        />
      </div>

      <div className='flex gap-2'>
        {methods.includes('wxpay') && providerButton('wxpay', t('WeChat Pay'))}
        {methods.includes('alipay') && providerButton('alipay', t('Alipay'))}
      </div>

      <Button
        type='button'
        data-testid='recharge-submit'
        disabled={busy || belowMin}
        onClick={() => submit(amountNum, provider)}
        className='w-full'
      >
        {busy ? <Loader2 className='mr-2 h-4 w-4 animate-spin' /> : null}
        {belowMin
          ? t('Minimum recharge amount is ${{amount}}', {
              amount: MIN_RECHARGE_USD,
            })
          : t('Recharge ${{amount}}', { amount: amountNum })}
      </Button>

      <RechargeQrDialog
        open={qrState !== null}
        onOpenChange={(o) => {
          if (!o) closeQr()
        }}
        qr={qrState?.qr ?? null}
        orderNo={qrState?.orderNo}
        amountUsd={qrState?.amountUsd}
        amountCny={qrState?.amountCny}
      />
    </div>
  )
}
