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
import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { SectionPageLayout } from '@/components/layout'
import { Skeleton } from '@/components/ui/skeleton'
import { RechargeQrDialog } from '@/features/wallet/components/dialogs/recharge-qr-dialog'
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
    // Default to WeChat (QR dialog); Alipay would redirect, same as recharge.
    mutationFn: (plan: TenantPlan) =>
      purchaseTokenPlan(plan.id ?? plan.code, 'wxpay'),
    onMutate: (plan) => setPurchasingCode(plan.code),
    onSettled: () => setPurchasingCode(null),
    onSuccess: (res) => {
      if (!res.success) return // global interceptor already toasted the error
      const data = res.data
      queryClient.invalidateQueries({ queryKey: ['tenant-subscriptions'] })

      // WeChat: pop a scannable QR for the auth-service mock pay page.
      const wxQr = data?.pay?.wxpay_qr
      if (wxQr) {
        setQrState({
          orderNo: data?.order_no,
          qr: wxQr,
          amountCny: data?.amount_cny,
        })
        return
      }
      // Alipay: redirect the browser to the gateway / mock confirm page.
      const aliUrl = data?.pay?.alipay_url
      if (aliUrl) {
        window.location.href = aliUrl
        return
      }
      // Fallback (legacy shape): open whatever pay URL is present in a new tab.
      const payUrl = extractPayUrl(data)
      if (payUrl) {
        toast.success(t('Order created. Redirecting to payment...'))
        window.open(payUrl, '_blank', 'noopener,noreferrer')
      } else {
        toast.success(res.message || t('Order created successfully'))
      }
    },
  })

  const plans = useMemo(() => {
    const list = plansData || []
    return [...list].sort((a, b) => (a.sort ?? 0) - (b.sort ?? 0))
  }, [plansData])

  const subscriptions = useMemo(() => subsData || [], [subsData])

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
          qr={qrState?.qr ?? null}
          orderNo={qrState?.orderNo}
          amountCny={qrState?.amountCny}
        />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

export function TenantPlans() {
  return <TenantPlansContent />
}
