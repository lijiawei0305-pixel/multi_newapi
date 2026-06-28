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
import { useCallback, useState } from 'react'
import i18next from 'i18next'
import { toast } from 'sonner'
import { createTenantRecharge, isApiSuccess } from '../api'

// $1 minimum recharge (USD); native quota credit is $1 = 500k units.
export const MIN_RECHARGE_USD = 1

export type RechargeProvider = 'wxpay' | 'alipay'

interface QrState {
  orderNo: string
  qr: string
  amountUsd: number
  amountCny: number
}

/**
 * useTenantRecharge drives the multi-tenant recharge flow:
 *   - submit(provider): POST /api/tenant/wallet/recharge
 *   - WeChat -> opens a QR modal (qrState set)
 *   - Alipay -> redirects the browser to the returned pay URL
 */
export function useTenantRecharge() {
  const [submitting, setSubmitting] = useState<RechargeProvider | null>(null)
  const [qrState, setQrState] = useState<QrState | null>(null)

  const closeQr = useCallback(() => setQrState(null), [])

  const submit = useCallback(
    async (amountUsd: number, provider: RechargeProvider) => {
      if (!Number.isFinite(amountUsd) || amountUsd < MIN_RECHARGE_USD) {
        toast.error(
          i18next.t('Minimum recharge amount is ${{amount}}', {
            amount: MIN_RECHARGE_USD,
          })
        )
        return false
      }
      try {
        setSubmitting(provider)
        const res = await createTenantRecharge({
          amount_usd: amountUsd,
          provider,
        })
        if (!isApiSuccess(res) || !res.data) {
          toast.error(res.message || i18next.t('Payment request failed'))
          return false
        }
        const { order_no, amount_usd, amount_cny, pay } = res.data
        if (provider === 'wxpay') {
          const qr = pay?.wxpay_qr
          if (!qr) {
            toast.error(i18next.t('Payment request failed'))
            return false
          }
          setQrState({
            orderNo: order_no,
            qr,
            amountUsd: amount_usd,
            amountCny: amount_cny,
          })
          return true
        }
        // Alipay: redirect to the gateway / mock confirm page.
        const url = pay?.alipay_url
        if (!url) {
          toast.error(i18next.t('Payment request failed'))
          return false
        }
        window.location.href = url
        return true
      } catch (_error) {
        toast.error(i18next.t('Payment request failed'))
        return false
      } finally {
        setSubmitting(null)
      }
    },
    []
  )

  return { submitting, qrState, submit, closeQr }
}
