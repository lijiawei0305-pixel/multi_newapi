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
import i18next from 'i18next'
import { useCallback, useEffect, useRef, useState } from 'react'
import { toast } from 'sonner'

import { api } from '@/lib/api'
import { normalizeHttpNavigationUrl } from '@/lib/safe-navigation'
import { useAuthStore } from '@/stores/auth-store'

import { createTenantRecharge, isApiSuccess } from '../api'
import { interpretRechargeStatus } from '../lib/recharge-status'

// $1 minimum recharge (USD); native quota credit is $1 = 500k units.
export const MIN_RECHARGE_USD = 1

/** 串行状态轮询间隔（PAY-REC-01：约 2s；禁止 setInterval(async)）。 */
const STATUS_POLL_INTERVAL_MS = 2000
/** 单次状态请求超时。 */
const STATUS_REQUEST_TIMEOUT_MS = 8000
/**
 * 前端监控上限：对齐微信 Native 二维码约 2h 有效期；真正终态由 status/failed/expired 决定。
 * 关闭 Dialog 不停止 activeOrder 监控。
 */
const STATUS_POLL_MAX_MS = 2 * 60 * 60 * 1000

/** sessionStorage 支付意图键（不存二维码/签名 URL）。 */
const RECHARGE_INTENT_STORAGE_KEY = 'wallet.recharge.intent.v1'
const RECHARGE_INTENT_TTL_MS = 2 * 60 * 60 * 1000

type StoredRechargeIntent = {
  orderNo: string
  rootOrderNo?: string
  activeOrderNo?: string
  idempotencyKey: string
  provider: RechargeProvider
  amountCny: number
  startedAt: number
  userId?: number | string
  /** origin+host 隔离，换租户清理 */
  origin?: string
  /** 支付宝恢复跳转仅一次 */
  alipayRedirected?: boolean
}

function saveRechargeIntent(intent: StoredRechargeIntent) {
  try {
    sessionStorage.setItem(
      RECHARGE_INTENT_STORAGE_KEY,
      JSON.stringify({
        ...intent,
        origin: window.location.origin,
        savedAt: Date.now(),
      })
    )
  } catch {
    /* private mode */
  }
}

function loadRechargeIntent(): StoredRechargeIntent | null {
  try {
    const raw = sessionStorage.getItem(RECHARGE_INTENT_STORAGE_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as StoredRechargeIntent & { savedAt?: number }
    if (
      parsed.savedAt &&
      Date.now() - parsed.savedAt > RECHARGE_INTENT_TTL_MS
    ) {
      sessionStorage.removeItem(RECHARGE_INTENT_STORAGE_KEY)
      return null
    }
    if (parsed.origin && parsed.origin !== window.location.origin) {
      sessionStorage.removeItem(RECHARGE_INTENT_STORAGE_KEY)
      return null
    }
    if (!parsed.idempotencyKey || !parsed.provider) return null
    return parsed
  } catch {
    return null
  }
}

function clearRechargeIntent() {
  try {
    sessionStorage.removeItem(RECHARGE_INTENT_STORAGE_KEY)
  } catch {
    /* ignore */
  }
}

export type RechargeProvider = 'wxpay' | 'alipay'

/**
 * 充值前端状态机（PAY-LAT-01 / PAY-STA-01）：
 * idle → creating → pending → paid_processing → credited
 * 异常：creating_error | failed | expired | poll_timeout
 */
export type RechargePhase =
  | 'idle'
  | 'creating'
  | 'pending'
  | 'paid_processing'
  | 'credited'
  | 'creating_error'
  | 'failed'
  | 'expired'
  | 'poll_timeout'

export interface ActiveRechargeOrder {
  orderNo: string
  qr: string | null
  amountUsd: number
  amountCny: number
  provider: RechargeProvider
  expiresAt: string | null
  /** 下单意图开始时间（用于 poll 超时与 User Timing）。 */
  startedAt: number
  /** 同一支付意图复用；retry 不得重新生成（P0-4）。 */
  idempotencyKey: string
}

export interface RechargeCreditInfo {
  orderNo: string
  amountCny: number
  amountUsd: number
  creditedQuota: number
  /** 主库权威余额；有则立即同步 UI。 */
  currentQuota: number | null
}

interface RechargeStatusData {
  order_no?: string
  root_order_no?: string
  active_order_no?: string
  status?: string
  create_state?: string
  provider_trade_state?: string
  provider_paid?: boolean
  credited?: boolean
  /** 兼容字段：服务端现定义为仅 credited 时为 true。 */
  paid?: boolean
  amount_cny?: number
  amount_usd?: number
  credited_quota?: number
  current_quota?: number
  expires_at?: string
  pay?: {
    wxpay_qr?: string
    alipay_url?: string
  }
}

interface RechargeStatusResponse {
  success: boolean
  data?: RechargeStatusData
}

interface UseTenantRechargeOptions {
  /**
   * 仅在 status=credited 时调用一次。携带权威 current_quota 供 Wallet/Dashboard 同步。
   */
  onCredited?: (info: RechargeCreditInfo) => void
}

function markTiming(name: string) {
  try {
    if (typeof performance !== 'undefined' && performance.mark) {
      performance.mark(name)
    }
  } catch {
    // User Timing 不可用不影响主流程
  }
}

/**
 * useTenantRecharge 驱动多租户官方微信/支付宝充值：
 * - 点击即 creating（Dialog 可立即打开，不等待创建接口）
 * - 微信：pending 展示 QR，串行轮询本站 status；仅 credited 完成
 * - 支付宝：跳转收银台（不视为付款成功）
 * - dialogOpen 与 activeOrder 分离：关弹窗不停止未终结订单监控
 */
export function useTenantRecharge(opts: UseTenantRechargeOptions = {}) {
  const { onCredited } = opts
  const [submitting, setSubmitting] = useState<RechargeProvider | null>(null)
  const [phase, setPhase] = useState<RechargePhase>('idle')
  const [dialogOpen, setDialogOpen] = useState(false)
  const [activeOrder, setActiveOrder] = useState<ActiveRechargeOrder | null>(
    null
  )
  const [errorMessage, setErrorMessage] = useState<string | null>(null)
  const [createNonce, setCreateNonce] = useState(0)

  const onCreditedRef = useRef(onCredited)
  useEffect(() => {
    onCreditedRef.current = onCredited
  }, [onCredited])

  const creditedHandledRef = useRef<string | null>(null)
  const pollAbortRef = useRef<AbortController | null>(null)
  const pollInFlightRef = useRef(false)
  const activeOrderRef = useRef(activeOrder)
  useEffect(() => {
    activeOrderRef.current = activeOrder
  }, [activeOrder])

  // 页面 reload：从 sessionStorage 恢复支付意图并继续轮询 status
  useEffect(() => {
    if (activeOrder) return
    const stored = loadRechargeIntent()
    if (!stored) return
    const userId = useAuthStore.getState().auth.user?.id
    if (
      stored.userId != null &&
      userId != null &&
      String(stored.userId) !== String(userId)
    ) {
      clearRechargeIntent()
      return
    }
    setActiveOrder({
      orderNo: stored.orderNo || '',
      qr: null,
      amountUsd: 0,
      amountCny: stored.amountCny,
      provider: stored.provider,
      expiresAt: null,
      startedAt: stored.startedAt || Date.now(),
      idempotencyKey: stored.idempotencyKey,
    })
    setPhase(stored.orderNo ? 'creating' : 'creating_error')
    if (stored.orderNo) {
      setDialogOpen(true)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- mount-only restore
  }, [])

  const stopPolling = useCallback(() => {
    if (pollAbortRef.current) {
      pollAbortRef.current.abort()
      pollAbortRef.current = null
    }
    pollInFlightRef.current = false
  }, [])

  const applyCredited = useCallback((data: RechargeStatusData, orderNo: string) => {
    if (creditedHandledRef.current === orderNo) {
      return
    }
    creditedHandledRef.current = orderNo
    markTiming('recharge_credited_seen')

    const currentQuota =
      typeof data.current_quota === 'number' ? data.current_quota : null
    if (currentQuota != null) {
      const auth = useAuthStore.getState().auth
      if (auth.user) {
        auth.setUser({ ...auth.user, quota: currentQuota })
      }
      markTiming('recharge_balance_rendered')
    }

    const info: RechargeCreditInfo = {
      orderNo,
      amountCny: Number(data.amount_cny) || 0,
      amountUsd: Number(data.amount_usd) || 0,
      creditedQuota: Number(data.credited_quota) || 0,
      currentQuota,
    }
    setPhase('credited')
    toast.success(i18next.t('Operation successful'))
    onCreditedRef.current?.(info)
  }, [])

  // 有待终结订单时串行轮询：立即首查，之后每 2s；任意时刻最多一个请求。
  // 依赖 orderNo + startedAt + phase；订单详情经 activeOrderRef 读取，避免 expiresAt 更新重置轮询。
  const pollOrderNo = activeOrder?.orderNo ?? ''
  const pollStartedAt = activeOrder?.startedAt ?? 0
  useEffect(() => {
    if (!pollOrderNo) return undefined
    // creating 且已有 order_no：允许轮询（202 unknown 恢复等待 QR）
    if (
      phase === 'credited' ||
      phase === 'failed' ||
      phase === 'expired' ||
      phase === 'poll_timeout' ||
      phase === 'creating_error' ||
      phase === 'idle'
    ) {
      return undefined
    }
    if (phase === 'creating' && !pollOrderNo) {
      return undefined
    }

    const orderNo = pollOrderNo
    const deadline = pollStartedAt + STATUS_POLL_MAX_MS
    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | null = null
    const ac = new AbortController()
    pollAbortRef.current = ac

    const schedule = (delayMs: number) => {
      if (cancelled) return
      timer = setTimeout(() => {
        void tick()
      }, delayMs)
    }

    const tick = async () => {
      if (cancelled || pollInFlightRef.current) {
        schedule(STATUS_POLL_INTERVAL_MS)
        return
      }
      if (Date.now() >= deadline) {
        stopPolling()
        setPhase('poll_timeout')
        toast.info(
          i18next.t('Payment status check timed out', {
            defaultValue: '支付状态查询已超时，请稍后在账单中确认',
          })
        )
        return
      }

      pollInFlightRef.current = true
      try {
        const res = await api.get('/api/tenant/wallet/recharge/status', {
          params: { order_no: orderNo },
          skipBusinessError: true,
          skipErrorHandler: true,
          timeout: STATUS_REQUEST_TIMEOUT_MS,
          signal: ac.signal,
        } as Record<string, unknown>)
        if (cancelled) return

        const body = res.data as RechargeStatusResponse
        const data = body?.data
        if (body?.success && data) {
          if (data.expires_at && activeOrderRef.current?.orderNo === orderNo) {
            setActiveOrder((prev) =>
              prev && prev.orderNo === orderNo
                ? { ...prev, expiresAt: data.expires_at ?? prev.expiresAt }
                : prev
            )
          }

          // 跟随 active_order_no（replacement）
          const activeNo = data.active_order_no || data.order_no || orderNo
          if (activeNo && activeNo !== orderNo) {
            setActiveOrder((prev) =>
              prev
                ? {
                    ...prev,
                    orderNo: activeNo,
                  }
                : prev
            )
            saveRechargeIntent({
              orderNo: activeNo,
              rootOrderNo: data.root_order_no,
              activeOrderNo: activeNo,
              idempotencyKey:
                activeOrderRef.current?.idempotencyKey || '',
              provider: activeOrderRef.current?.provider || 'wxpay',
              amountCny: activeOrderRef.current?.amountCny || 0,
              startedAt: activeOrderRef.current?.startedAt || Date.now(),
              userId: useAuthStore.getState().auth.user?.id,
            })
            // 下轮 tick 用新 order_no（依赖 activeOrder 更新 effect）
            schedule(STATUS_POLL_INTERVAL_MS)
            return
          }

          // status 带回二维码时原地渲染（unknown → pending）
          const qrFromStatus = data.pay?.wxpay_qr
          if (
            qrFromStatus &&
            activeOrderRef.current &&
            !activeOrderRef.current.qr
          ) {
            setActiveOrder((prev) =>
              prev ? { ...prev, qr: qrFromStatus } : prev
            )
            markTiming('recharge_qr_rendered')
          }

          // 支付宝：status 返回 alipay_url 时仅安全跳转一次
          const aliFromStatus = data.pay?.alipay_url
          if (
            aliFromStatus &&
            activeOrderRef.current?.provider === 'alipay'
          ) {
            const stored = loadRechargeIntent()
            if (!stored?.alipayRedirected) {
              const safeUrl = normalizeHttpNavigationUrl(aliFromStatus)
              if (safeUrl) {
                if (stored) {
                  saveRechargeIntent({ ...stored, alipayRedirected: true })
                }
                window.location.href = safeUrl
                return
              }
            }
          }

          const interp = interpretRechargeStatus(data)
          if (interp === 'credited') {
            stopPolling()
            clearRechargeIntent()
            applyCredited(data, orderNo)
            return
          }
          if (interp === 'failed') {
            stopPolling()
            clearRechargeIntent()
            setPhase('failed')
            toast.error(
              i18next.t('Payment failed', { defaultValue: '支付失败' })
            )
            return
          }
          if (interp === 'expired') {
            stopPolling()
            clearRechargeIntent()
            setPhase('expired')
            toast.info(
              i18next.t('Payment QR expired', {
                defaultValue: '支付二维码已过期，请重新下单',
              })
            )
            return
          }
          if (interp === 'paid_processing') {
            markTiming('recharge_provider_paid_seen')
            setPhase('paid_processing')
          } else if (
            activeOrderRef.current?.qr ||
            qrFromStatus ||
            data.status === 'created'
          ) {
            // 有 QR 或仍在 created：有 QR → pending；无 QR 确认中 → 保持 creating
            setPhase(
              activeOrderRef.current?.qr || qrFromStatus
                ? 'pending'
                : 'creating'
            )
          } else {
            setPhase('pending')
          }
        }
      } catch (err) {
        // abort / 瞬时网络错误：继续轮询
        if (ac.signal.aborted || cancelled) return
        void err
      } finally {
        pollInFlightRef.current = false
      }
      schedule(STATUS_POLL_INTERVAL_MS)
    }

    // 立即首查（不等待 interval）
    void tick()

    const onFocus = () => {
      if (!cancelled && !pollInFlightRef.current) {
        void tick()
      }
    }
    if (typeof window !== 'undefined') {
      window.addEventListener('focus', onFocus)
    }

    return () => {
      cancelled = true
      if (timer) clearTimeout(timer)
      stopPolling()
      if (typeof window !== 'undefined') {
        window.removeEventListener('focus', onFocus)
      }
    }
  }, [pollOrderNo, pollStartedAt, phase, stopPolling, applyCredited])

  const closeDialog = useCallback(() => {
    // 只关 UI，不清理 activeOrder / 不停止轮询（未终结订单继续监控）
    setDialogOpen(false)
  }, [])

  const dismissOrder = useCallback(() => {
    stopPolling()
    setActiveOrder(null)
    setPhase('idle')
    setErrorMessage(null)
    setDialogOpen(false)
    setSubmitting(null)
    creditedHandledRef.current = null
  }, [stopPolling])

  const submit = useCallback(
    async (amountCny: number, provider: RechargeProvider) => {
      if (!Number.isFinite(amountCny) || amountCny <= 0) {
        toast.error(
          i18next.t('Please enter a valid amount', {
            defaultValue: '请输入有效的充值金额',
          })
        )
        return false
      }
      // 防双击：创建中忽略
      if (submitting !== null) {
        return false
      }

      markTiming('recharge_click')
      creditedHandledRef.current = null
      setSubmitting(provider)
      setErrorMessage(null)
      setPhase('creating')
      setDialogOpen(true)
      markTiming('recharge_dialog_open')
      // 同一支付意图复用 idempotency key（retry / 202 unknown 恢复不得新开 key）
      const existingKey =
        activeOrderRef.current?.idempotencyKey &&
        activeOrderRef.current.amountCny === amountCny &&
        activeOrderRef.current.provider === provider
          ? activeOrderRef.current.idempotencyKey
          : ''
      const idempotencyKey =
        existingKey ||
        (typeof crypto !== 'undefined' && crypto.randomUUID
          ? crypto.randomUUID()
          : `rcg-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`)

      const startedAt = Date.now()
      setActiveOrder({
        orderNo: activeOrderRef.current?.orderNo || '',
        qr: null,
        amountUsd: 0,
        amountCny,
        provider,
        expiresAt: null,
        startedAt,
        idempotencyKey,
      })
      // POST 前持久化意图（reload 恢复）；不存 QR/签名 URL
      saveRechargeIntent({
        orderNo: activeOrderRef.current?.orderNo || '',
        idempotencyKey,
        provider,
        amountCny,
        startedAt,
        userId: useAuthStore.getState().auth.user?.id,
      })
      setCreateNonce((n) => n + 1)

      try {
        markTiming('recharge_create_request_start')
        const res = await createTenantRecharge({
          amount_cny: amountCny,
          provider,
          idempotency_key: idempotencyKey,
        })
        markTiming('recharge_create_response')
        const data = res.data
        // 202 / PAY_CREATE_UNKNOWN：有 order_no 则进入确认中并轮询
        const code = (res as { code?: string }).code
        const isUnknown =
          code === 'PAY_CREATE_UNKNOWN' ||
          code === 'PAY_URL_PERSIST_FAILED' ||
          code === 'PAY_URL_MISSING'
        if ((!isApiSuccess(res) || !data) && !(isUnknown && data?.order_no)) {
          const msg = res.message || i18next.t('Payment request failed')
          setErrorMessage(msg)
          setPhase('creating_error')
          toast.error(msg)
          return false
        }
        if (!data) {
          const msg = res.message || i18next.t('Payment request failed')
          setErrorMessage(msg)
          setPhase('creating_error')
          toast.error(msg)
          return false
        }
        const {
          order_no,
          amount_usd,
          amount_cny,
          pay,
          expires_at,
          idempotency_key,
        } = data
        const keyOut = idempotency_key || idempotencyKey
        saveRechargeIntent({
          orderNo: order_no,
          idempotencyKey: keyOut,
          provider,
          amountCny: amount_cny,
          startedAt: Date.now(),
          userId: useAuthStore.getState().auth.user?.id,
        })
        const baseOrder = {
          orderNo: order_no,
          amountUsd: amount_usd,
          amountCny: amount_cny,
          provider,
          expiresAt: expires_at ?? null,
          startedAt: Date.now(),
          idempotencyKey: keyOut,
        } as const

        // Phase E：先判断 unknown，再按 provider 分支；禁止用 wxpay_qr 判断支付宝成功。
        if (isUnknown) {
          setActiveOrder({ ...baseOrder, qr: null })
          setPhase('creating')
          toast.info(
            i18next.t('Confirming payment order', {
              defaultValue: '正在确认订单，请稍候…',
            })
          )
          return true
        }

        if (provider === 'wxpay') {
          const qr = pay?.wxpay_qr
          if (!qr) {
            setActiveOrder({ ...baseOrder, qr: null })
            setPhase('creating')
            toast.info(
              i18next.t('Confirming payment order', {
                defaultValue: '正在确认订单，请稍候…',
              })
            )
            return true
          }
          setActiveOrder({
            ...baseOrder,
            qr,
          })
          setPhase('pending')
          markTiming('recharge_qr_rendered')
          return true
        }
        // Alipay: 打开收银台 ≠ 付款成功；仅表示 redirect_created
        const url = pay?.alipay_url
        if (!url) {
          setActiveOrder({
            orderNo: order_no,
            qr: null,
            amountUsd: amount_usd,
            amountCny: amount_cny,
            provider,
            expiresAt: expires_at ?? null,
            startedAt: Date.now(),
            idempotencyKey: keyOut,
          })
          setPhase('creating')
          return true
        }
        const safeUrl = normalizeHttpNavigationUrl(url)
        if (!safeUrl) {
          const msg = i18next.t('Invalid payment redirect URL')
          setErrorMessage(msg)
          setPhase('creating_error')
          toast.error(msg)
          return false
        }
        setActiveOrder({
          orderNo: order_no,
          qr: null,
          amountUsd: amount_usd,
          amountCny: amount_cny,
          provider,
          expiresAt: expires_at ?? null,
          startedAt: Date.now(),
          idempotencyKey: keyOut,
        })
        setPhase('pending')
        setDialogOpen(false)
        window.location.href = safeUrl
        return true
      } catch {
        const msg = i18next.t('Payment service busy, please retry', {
          defaultValue: '支付服务网络繁忙，请稍后重试',
        })
        setErrorMessage(msg)
        setPhase('creating_error')
        toast.error(msg)
        return false
      } finally {
        setSubmitting(null)
      }
    },
    [submitting]
  )

  const retryCreate = useCallback(() => {
    const order = activeOrder
    if (!order || submitting !== null) return
    // 复用原支付意图（同一 amount/provider/idempotencyKey），不得生成新 key
    void submit(order.amountCny, order.provider)
  }, [activeOrder, submit, submitting])

  return {
    submitting,
    phase,
    dialogOpen,
    setDialogOpen,
    activeOrder,
    errorMessage,
    createNonce,
    submit,
    closeDialog,
    dismissOrder,
    retryCreate,
  }
}
