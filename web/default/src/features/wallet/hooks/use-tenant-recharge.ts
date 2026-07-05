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
import { useCallback, useEffect, useRef, useState } from 'react'
import i18next from 'i18next'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { createTenantRecharge, isApiSuccess } from '../api'

// $1 minimum recharge (USD); native quota credit is $1 = 500k units.
export const MIN_RECHARGE_USD = 1

// Order-status poll cadence / cap. Fix for "付完款不跳转": WeChat Native pay has
// no server-side redirect, so once the QR is shown the frontend must actively
// poll GET /api/tenant/wallet/recharge/status to learn the order was paid.
const STATUS_POLL_INTERVAL_MS = 3000
const STATUS_POLL_TIMEOUT_MS = 180000

export type RechargeProvider = 'wxpay' | 'alipay'

interface QrState {
  orderNo: string
  qr: string
  amountUsd: number
  amountCny: number
}

interface RechargeStatusResponse {
  success: boolean
  data?: { paid: boolean; status: string }
}

interface UseTenantRechargeOptions {
  /**
   * Invoked once when order-status polling confirms the pending QR order was
   * paid. Wire this to refresh the wallet balance (e.g. re-fetch the user).
   */
  onPaid?: () => void
}

/**
 * useTenantRecharge drives the multi-tenant recharge flow:
 *   - submit(provider): POST /api/tenant/wallet/recharge
 *   - WeChat -> opens a QR modal (qrState set) and polls order-status until
 *     paid or timeout (WeChat Native has no server redirect, so this is the
 *     only way the UI learns payment landed).
 *   - Alipay -> redirects the browser to the returned pay URL
 */
export function useTenantRecharge(opts: UseTenantRechargeOptions = {}) {
  const { onPaid } = opts
  const [submitting, setSubmitting] = useState<RechargeProvider | null>(null)
  const [qrState, setQrState] = useState<QrState | null>(null)
  const pollTimerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  // Keep the latest onPaid without making it a poll-effect dependency — a new
  // callback identity on every parent render must not reset the poll deadline.
  const onPaidRef = useRef(onPaid)
  useEffect(() => {
    onPaidRef.current = onPaid
  }, [onPaid])

  const stopPolling = useCallback(() => {
    if (pollTimerRef.current !== null) {
      clearInterval(pollTimerRef.current)
      pollTimerRef.current = null
    }
  }, [])

  const closeQr = useCallback(() => {
    stopPolling()
    setQrState(null)
  }, [stopPolling])

  // Poll while a QR is showing; stop on paid, timeout, closeQr, or unmount.
  useEffect(() => {
    if (!qrState) return undefined
    const orderNo = qrState.orderNo
    const deadline = Date.now() + STATUS_POLL_TIMEOUT_MS

    const tick = async () => {
      try {
        const res = await api.get('/api/tenant/wallet/recharge/status', {
          params: { order_no: orderNo },
          skipBusinessError: true,
          skipErrorHandler: true,
        } as Record<string, unknown>)
        const body = res.data as RechargeStatusResponse
        if (body?.success && body.data?.paid) {
          stopPolling()
          setQrState(null)
          toast.success(i18next.t('Operation successful'))
          onPaidRef.current?.()
          return
        }
      } catch {
        // Transient (network blip / momentary backend hiccup) — keep polling;
        // the timeout branch below is the backstop.
      }
      if (Date.now() >= deadline) {
        stopPolling()
        toast.info(i18next.t('Please try again later.'))
      }
    }

    pollTimerRef.current = setInterval(tick, STATUS_POLL_INTERVAL_MS)
    return () => stopPolling()
  }, [qrState, stopPolling])

  const submit = useCallback(
    async (amountUsd: number, provider: RechargeProvider) => {
      if (!Number.isFinite(amountUsd) || amountUsd < MIN_RECHARGE_USD) {
        toast.error(
          i18next.t('Minimum recharge amount is ${{amount}}', {
            amount: MIN_RECHARGE_USD,
            defaultValue: '最低充值金额为 ${{amount}}',
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
        // Alipay: redirect to the official PC web payment gateway page.
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
