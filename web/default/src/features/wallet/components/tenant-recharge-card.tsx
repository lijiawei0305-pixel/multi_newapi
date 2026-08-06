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
import { Loader2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useSystemConfig } from '@/hooks/use-system-config'
import { cn } from '@/lib/utils'

import {
  useTenantRecharge,
  type RechargeCreditInfo,
  type RechargeProvider,
} from '../hooks/use-tenant-recharge'
import { getPaymentIcon } from '../lib'
import { RechargeQrDialog } from './dialogs/recharge-qr-dialog'

// 人民币整数预设档位（元）。官方微信/支付宝以 ¥ 结算，中国用户按整数元充值最直观——
// 所见即所付：选 ¥100 → 微信扣 ¥100 → 后端按汇率折美元入原生额度（$1 = 500k quota）。
const CNY_PRESETS = [10, 30, 50, 100, 300, 500]

type TenantRechargeCardProps = {
  /**
   * Official channels to show — the enabled && configured set (wxpay/alipay)
   * from the recharge methods endpoint. Single-gate: once the admin fills creds
   * and enables a channel under the WeChat/Alipay tabs it shows here
   * automatically (no PayMethods entry needed). The parent owns the fetch so the
   * page can reuse the result. Empty → the whole card is hidden.
   */
  providers: RechargeProvider[]
  /**
   * 仅在 credited 时调用一次；可携带 current_quota 立即更新余额 UI。
   * 不再使用 window CustomEvent 桥接（PAY-UI-01）。
   */
  onCredited?: (info: RechargeCreditInfo) => void
}

/**
 * TenantRechargeCard is the multi-tenant recharge section (official WeChat /
 * Alipay, in-process real SDK).
 *
 * Amount is in CNY (minimum derived from $1 × exchange rate). WeChat shows a QR
 * modal (opens immediately on click while creating); Alipay redirects. Distinct
 * from the Epay/Stripe flow.
 */
export function TenantRechargeCard(props: TenantRechargeCardProps) {
  const { t } = useTranslation()
  const { currency } = useSystemConfig()
  // 汇率取系统「货币显示」配置的 usd_exchange_rate（¥/USD）；仅用于推算 ¥ 下限，实付以后端为准。
  const rate = Math.max(Number(currency?.usdExchangeRate) || 1, 0.0001)
  const minCny = Math.max(1, Math.ceil(rate)) // 后端要求 usd≥$1 → ¥ 下限 = ⌈汇率⌉
  const [amount, setAmount] = useState<string>('100')
  const [provider, setProvider] = useState<RechargeProvider>('wxpay')

  const {
    submitting,
    phase,
    dialogOpen,
    setDialogOpen,
    activeOrder,
    errorMessage,
    submit,
    closeDialog,
    dismissOrder,
    retryCreate,
  } = useTenantRecharge({
    onCredited: props.onCredited,
  })

  // Keep the selected provider within the configured set (default = first).
  useEffect(() => {
    if (props.providers.length > 0 && !props.providers.includes(provider)) {
      setProvider(props.providers[0])
    }
  }, [props.providers, provider])

  // No enabled && configured channel → hide the whole card.
  if (props.providers.length === 0) return null

  const amountNum = Number.parseFloat(amount) || 0
  const belowMin = amountNum < minCny
  const busy = submitting !== null || phase === 'creating'

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
        {t('Recharge (WeChat / Alipay)', {
          defaultValue: '充值（微信 / 支付宝）',
        })}
      </Label>

      {/* 人民币整数预设档位（所见即所付） */}
      <div className='grid grid-cols-3 gap-2'>
        {CNY_PRESETS.map((v) => (
          <Button
            key={v}
            type='button'
            variant='outline'
            data-testid={`recharge-preset-${v}`}
            aria-pressed={amountNum === v}
            onClick={() => setAmount(String(v))}
            disabled={busy}
            className={cn(
              'min-h-11',
              amountNum === v
                ? 'border-foreground bg-foreground/5 dark:bg-foreground/10'
                : 'border-muted'
            )}
          >
            ¥{v}
          </Button>
        ))}
      </div>

      <div className='space-y-2'>
        <Label
          htmlFor='tenant-recharge-amount'
          className='text-muted-foreground text-xs'
        >
          {t('Amount (CNY), minimum ¥{{amount}}', {
            amount: minCny,
            defaultValue: '金额（元），最低 ¥{{amount}}',
          })}
        </Label>
        <Input
          id='tenant-recharge-amount'
          data-testid='recharge-amount'
          type='number'
          min={minCny}
          step='1'
          inputMode='decimal'
          value={amount}
          onChange={(e) => setAmount(e.target.value)}
          placeholder={`¥${minCny}`}
          className='h-9 sm:h-10'
        />
      </div>

      <div className='flex gap-2'>
        {props.providers.includes('wxpay') &&
          providerButton('wxpay', t('WeChat Pay'))}
        {props.providers.includes('alipay') &&
          providerButton('alipay', t('Alipay'))}
      </div>

      <Button
        type='button'
        data-testid='recharge-submit'
        disabled={busy || belowMin}
        onClick={() => {
          void submit(amountNum, provider)
        }}
        className='w-full'
      >
        {busy ? <Loader2 className='mr-2 h-4 w-4 animate-spin' /> : null}
        {belowMin
          ? t('Minimum recharge amount is ¥{{amount}}', {
              amount: minCny,
              defaultValue: '最低充值金额为 ¥{{amount}}',
            })
          : t('Recharge ¥{{amount}}', {
              amount: amountNum,
              defaultValue: '充值 ¥{{amount}}',
            })}
      </Button>

      <RechargeQrDialog
        open={dialogOpen}
        onOpenChange={(o) => {
          if (!o) {
            // 关闭弹窗只藏 UI；终态可 dismiss 清监控，否则保持 activeOrder 轮询
            if (
              phase === 'credited' ||
              phase === 'failed' ||
              phase === 'expired' ||
              phase === 'poll_timeout' ||
              phase === 'creating_error' ||
              phase === 'idle'
            ) {
              dismissOrder()
            } else {
              closeDialog()
            }
          } else {
            setDialogOpen(true)
          }
        }}
        phase={phase}
        order={activeOrder}
        errorMessage={errorMessage}
        onRetry={retryCreate}
        onDismiss={dismissOrder}
      />
    </div>
  )
}
