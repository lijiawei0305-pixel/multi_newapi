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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useSearch } from '@tanstack/react-router'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { RechargeQrDialog } from '@/features/wallet/components/dialogs/recharge-qr-dialog'
import { useRechargeMethods } from '@/features/wallet/hooks/use-recharge-methods'
import type { RechargeProvider } from '@/features/wallet/hooks/use-tenant-recharge'
import { getPaymentIcon } from '@/features/wallet/lib'
import {
  normalizeHttpNavigationUrl,
  openHttpUrlInNewTab,
} from '@/lib/safe-navigation'
import { cn } from '@/lib/utils'

import {
  getTenantSubscriptions,
  getTenantTokenPlans,
  purchaseTokenPlan,
} from './api'
import { MySubscriptions } from './components/my-subscriptions'
import { PlanCard } from './components/plan-card'
import type { PurchaseResult, TenantPlan } from './types'

function extractPayUrl(data?: PurchaseResult): string | undefined {
  if (!data) return undefined
  return data.pay_url || data.pay_link || data.payment_url || data.url
}

/** Pending WeChat QR state for a created purchase order (mirrors recharge). */
interface PurchaseQrState {
  orderNo?: string
  qr: string
  amountCny?: number
}

function TenantPlansContent() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [purchasingCode, setPurchasingCode] = useState<string | null>(null)
  const [qrState, setQrState] = useState<PurchaseQrState | null>(null)
  const [provider, setProvider] = useState<RechargeProvider>('wxpay')

  // Tokenplans settle only via the official in-process WeChat/Alipay SDK (no
  // Epay/Stripe path here). Single-gate: the buyer picks among exactly the
  // channels that are enabled && configured under the WeChat/Alipay tabs.
  const { methods: officialMethods, loading: methodsLoading } =
    useRechargeMethods()
  const officialProviders = useMemo<RechargeProvider[]>(
    () => officialMethods ?? [],
    [officialMethods]
  )
  // Keep the selected provider within the configured set (default = first).
  useEffect(() => {
    if (officialProviders.length > 0 && !officialProviders.includes(provider)) {
      setProvider(officialProviders[0])
    }
  }, [officialProviders, provider])

  const noPaymentConfigured = !methodsLoading && officialProviders.length === 0
  const buyDisabled = methodsLoading || noPaymentConfigured

  const { data: plansData, isLoading: plansLoading } = useQuery({
    queryKey: ['tenant-token-plans'],
    queryFn: async () => {
      const res = await getTenantTokenPlans()
      return res.data || []
    },
    placeholderData: (prev) => prev,
  })

  const { data: subsData, isLoading: subsLoading } = useQuery({
    queryKey: ['tenant-subscriptions'],
    queryFn: async () => {
      const res = await getTenantSubscriptions()
      return res.data || []
    },
    placeholderData: (prev) => prev,
  })

  const purchaseMutation = useMutation({
    // Provider = the buyer-selected official channel (WeChat → QR, Alipay → redirect).
    mutationFn: (plan: TenantPlan) =>
      purchaseTokenPlan(plan.id ?? plan.code, provider),
    onMutate: (plan) => setPurchasingCode(plan.code),
    onSettled: () => setPurchasingCode(null),
    onSuccess: (res) => {
      if (!res.success) return // global interceptor already toasted the error
      const data = res.data
      queryClient.invalidateQueries({ queryKey: ['tenant-subscriptions'] })

      // WeChat: pop a scannable QR for the in-process pay order.
      const wxQr = data?.pay?.wxpay_qr
      if (wxQr) {
        setQrState({
          orderNo: data?.order_no,
          qr: wxQr,
          amountCny: data?.amount_cny,
        })
        return
      }
      // Alipay: redirect the browser to the gateway page.
      const aliUrl = data?.pay?.alipay_url
      if (aliUrl) {
        const safeAliUrl = normalizeHttpNavigationUrl(aliUrl)
        if (!safeAliUrl) {
          toast.error(t('Invalid payment redirect URL'))
          return
        }
        window.location.href = safeAliUrl
        return
      }
      // Fallback (legacy shape): open whatever pay URL is present in a new tab.
      const payUrl = extractPayUrl(data)
      if (payUrl) {
        if (!openHttpUrlInNewTab(payUrl)) {
          toast.error(t('Invalid payment redirect URL'))
          return
        }
        toast.success(t('Order created. Redirecting to payment...'))
      } else {
        toast.success(res.message || t('Order created successfully'))
      }
    },
  })

  const plans = useMemo(() => {
    const list = plansData || []
    return [...list].sort((a, b) => (a.sort ?? 0) - (b.sort ?? 0))
  }, [plansData])

  // 一键续费深链（/plans?renew=<套餐id>，来自满额/到期横幅）：套餐与支付渠道就绪后
  // 自动对该套餐发起一次购买（直接弹二维码/跳转支付），仅触发一次（P3-RNW 降级版）。
  const search = useSearch({ strict: false }) as { renew?: number }
  const renewFired = useRef(false)
  useEffect(() => {
    if (renewFired.current) return
    if (!search?.renew || plansLoading || methodsLoading) return
    if (officialProviders.length === 0) return // 未配支付渠道：页面已有提示，不自动下单
    const target = (plansData || []).find((p) => p.id === search.renew)
    if (!target) return // 套餐已下架/不在本租户：静默降级为普通购买页
    renewFired.current = true
    purchaseMutation.mutate(target)
  }, [
    search?.renew,
    plansLoading,
    methodsLoading,
    officialProviders,
    plansData,
    purchaseMutation,
  ])

  const subscriptions = useMemo(() => subsData || [], [subsData])

  const providerButton = (value: RechargeProvider, label: string) => (
    <Button
      type='button'
      size='sm'
      variant='outline'
      data-testid={value === 'wxpay' ? 'plan-pay-wxpay' : 'plan-pay-alipay'}
      aria-pressed={provider === value}
      onClick={() => setProvider(value)}
      className={cn(
        'gap-1.5',
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
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Buy Plans')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div
          data-testid='buyer-plans-page'
          className='flex flex-col gap-6 pb-4'
        >
          <section className='flex flex-col gap-3'>
            <h3 className='text-sm font-semibold'>{t('Available Plans')}</h3>

            {/* Payment method selector — official WeChat/Alipay (single-gate). */}
            {!plansLoading &&
              plans.length > 0 &&
              officialProviders.length > 0 && (
                <div className='flex flex-wrap items-center gap-2'>
                  <span className='text-muted-foreground text-xs font-medium tracking-wider uppercase'>
                    {t('Payment Method')}
                  </span>
                  {officialProviders.includes('wxpay') &&
                    providerButton('wxpay', t('WeChat Pay'))}
                  {officialProviders.includes('alipay') &&
                    providerButton('alipay', t('Alipay'))}
                </div>
              )}

            {/* No official channel configured → tokenplans can't be paid for. */}
            {!plansLoading && plans.length > 0 && noPaymentConfigured && (
              <Alert>
                <AlertDescription>
                  {t(
                    'Online payment is not configured yet. Please contact the administrator.'
                  )}
                </AlertDescription>
              </Alert>
            )}

            {plansLoading && (
              <div className='grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4'>
                <Skeleton className='h-56 w-full' />
                <Skeleton className='h-56 w-full' />
                <Skeleton className='h-56 w-full' />
              </div>
            )}
            {!plansLoading && plans.length === 0 && (
              <p className='text-muted-foreground text-sm'>
                {t('No plans are available right now')}
              </p>
            )}
            {!plansLoading && plans.length > 0 && (
              <div className='grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4'>
                {plans.map((plan) => (
                  <PlanCard
                    key={plan.code}
                    plan={plan}
                    purchasing={purchasingCode === plan.code}
                    disabled={buyDisabled}
                    onBuy={(p) => purchaseMutation.mutate(p)}
                  />
                ))}
              </div>
            )}
          </section>

          <MySubscriptions
            subscriptions={subscriptions}
            isLoading={subsLoading}
          />
        </div>

        <RechargeQrDialog
          open={qrState !== null}
          onOpenChange={(o) => {
            if (!o) setQrState(null)
          }}
          phase={qrState ? 'pending' : 'idle'}
          order={
            qrState
              ? {
                  orderNo: qrState.orderNo ?? '',
                  qr: qrState.qr,
                  amountUsd: 0,
                  amountCny: qrState.amountCny ?? 0,
                  provider: 'wxpay',
                  expiresAt: null,
                  startedAt: Date.now(),
                  idempotencyKey: '',
                }
              : null
          }
        />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

export function TenantPlans() {
  return <TenantPlansContent />
}
