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
import { useCallback, useEffect, useState } from 'react'
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
import { RechargeQrDialog } from './dialogs/recharge-qr-dialog'

/**
 * Fired on `window` once a pending QR order is confirmed paid (see
 * use-tenant-recharge's status polling). TenantRechargeCard is mounted a couple
 * of layers below the Wallet page (inside RechargeFormCard, which is out of
 * scope for this fix), so a DOM event is the least invasive way to let the page
 * refresh the balance without threading a prop through that intermediate file.
 * `index.tsx` listens for this event and re-fetches the current user.
 */
export const TENANT_RECHARGE_PAID_EVENT = 'mt:tenant-recharge-paid'

type TenantRechargeCardProps = {
  /**
   * Official channels to show — the enabled && configured set (wxpay/alipay)
   * from the recharge methods endpoint. Single-gate: once the admin fills creds
   * and enables a channel under the WeChat/Alipay tabs it shows here
   * automatically (no PayMethods entry needed). The parent owns the fetch so the
   * page can reuse the result. Empty → the whole card is hidden.
   */
  providers: RechargeProvider[]
  /** Optional: called once when a pending order is confirmed paid (balance refresh hook). */
  onPaid?: () => void
}

/**
 * TenantRechargeCard is the multi-tenant recharge section (official WeChat /
 * Alipay, in-process real SDK).
 *
 * Amount is in USD ($1 minimum, credited as native quota at $1 = 500k). WeChat
 * shows a QR modal; Alipay redirects. Distinct from the Epay/Stripe flow — it
 * calls POST /api/tenant/wallet/recharge and settles in-process via the real
 * WeChat/Alipay SDK (notify verify / active query).
 */
export function TenantRechargeCard({
  providers,
  onPaid,
}: TenantRechargeCardProps) {
  const { t } = useTranslation()
  const [amount, setAmount] = useState<string>(String(MIN_RECHARGE_USD))
  const [provider, setProvider] = useState<RechargeProvider>('wxpay')

  // Bridge payment confirmation up to the Wallet page: call the caller's
  // onPaid (if wired) and always dispatch the window event, since the current
  // parent (RechargeFormCard) doesn't pass onPaid through.
  const handlePaid = useCallback(() => {
    onPaid?.()
    if (typeof window !== 'undefined') {
      window.dispatchEvent(new CustomEvent(TENANT_RECHARGE_PAID_EVENT))
    }
  }, [onPaid])

  const { submitting, qrState, submit, closeQr } = useTenantRecharge({
    onPaid: handlePaid,
  })

  // Keep the selected provider within the configured set (default = first).
  useEffect(() => {
    if (providers.length > 0 && !providers.includes(provider)) {
      setProvider(providers[0])
    }
  }, [providers, provider])

  // No enabled && configured channel → hide the whole card.
  if (providers.length === 0) return null

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
        {t('Recharge (WeChat / Alipay)', { defaultValue: '充值（微信 / 支付宝）' })}
      </Label>

      <div className='space-y-2'>
        <Label
          htmlFor='tenant-recharge-amount'
          className='text-muted-foreground text-xs'
        >
          {t('Amount (USD), minimum ${{amount}}', {
            amount: MIN_RECHARGE_USD,
            defaultValue: '金额（美元），最低 ${{amount}}',
          })}
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
        {providers.includes('wxpay') &&
          providerButton('wxpay', t('WeChat Pay'))}
        {providers.includes('alipay') &&
          providerButton('alipay', t('Alipay'))}
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
              defaultValue: '最低充值金额为 ${{amount}}',
            })
          : t('Recharge ${{amount}}', {
              amount: amountNum,
              defaultValue: '充值 ${{amount}}',
            })}
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
