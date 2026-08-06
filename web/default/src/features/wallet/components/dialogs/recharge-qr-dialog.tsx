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
import { QRCodeSVG } from 'qrcode.react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { normalizePaymentQrNavigationUrl } from '@/lib/safe-navigation'

import type {
  ActiveRechargeOrder,
  RechargePhase,
} from '../../hooks/use-tenant-recharge'

interface RechargeQrDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  phase: RechargePhase
  order: ActiveRechargeOrder | null
  errorMessage?: string | null
  onRetry?: () => void
  onDismiss?: () => void
}

function formatCountdown(expiresAt: string | null | undefined): string | null {
  if (!expiresAt) return null
  const end = Date.parse(expiresAt)
  if (!Number.isFinite(end)) return null
  const remain = Math.max(0, Math.floor((end - Date.now()) / 1000))
  const m = Math.floor(remain / 60)
  const s = remain % 60
  return `${m}:${String(s).padStart(2, '0')}`
}

/**
 * RechargeQrDialog：微信 Native 支付弹窗，覆盖 creating / pending / paid_processing /
 * credited / 错误 / 过期 各相。dialogOpen 与订单监控分离——关闭只藏 UI。
 */
export function RechargeQrDialog(props: RechargeQrDialogProps) {
  const { t } = useTranslation()
  const safeQrLink = normalizePaymentQrNavigationUrl(props.order?.qr ?? null)
  const amountCny = props.order?.amountCny
  const [countdown, setCountdown] = useState<string | null>(() =>
    formatCountdown(props.order?.expiresAt)
  )

  useEffect(() => {
    setCountdown(formatCountdown(props.order?.expiresAt))
    if (!props.order?.expiresAt) return undefined
    const id = setInterval(() => {
      setCountdown(formatCountdown(props.order?.expiresAt))
    }, 1000)
    return () => clearInterval(id)
  }, [props.order?.expiresAt])

  const title = (() => {
    switch (props.phase) {
      case 'creating':
        return t('Creating payment order', { defaultValue: '正在创建支付订单' })
      case 'paid_processing':
        return t('Payment confirmed', { defaultValue: '支付已确认' })
      case 'credited':
        return t('Recharge successful', { defaultValue: '充值成功' })
      case 'creating_error':
      case 'failed':
        return t('Payment failed', { defaultValue: '支付失败' })
      case 'expired':
      case 'poll_timeout':
        return t('Payment expired or timed out', {
          defaultValue: '支付已过期或超时',
        })
      default:
        return t('Scan to pay with WeChat', { defaultValue: '请扫码支付' })
    }
  })()

  const description = (() => {
    if (amountCny != null && amountCny > 0) {
      return t('Pay ¥{{cny}}', {
        cny: Number(amountCny).toFixed(2),
        defaultValue: '支付 ¥{{cny}}',
      })
    }
    return t('Scan the QR code with WeChat to complete payment.', {
      defaultValue: '请扫描二维码完成支付。',
    })
  })()

  const liveMessage = (() => {
    switch (props.phase) {
      case 'creating':
        return t('Contacting payment provider, please wait…', {
          defaultValue: '正在连接支付渠道，请稍候…',
        })
      case 'pending':
        return t('Waiting for payment…', { defaultValue: '等待扫码支付…' })
      case 'paid_processing':
        return t('Payment confirmed, crediting balance…', {
          defaultValue: '支付已确认，余额入账中…',
        })
      case 'credited':
        return t('Balance updated', { defaultValue: '余额已更新' })
      case 'creating_error':
        return (
          props.errorMessage ||
          t('Failed to create payment order', {
            defaultValue: '创建支付订单失败',
          })
        )
      case 'failed':
        return t('Payment failed', { defaultValue: '支付失败' })
      case 'expired':
        return t('Payment QR expired', {
          defaultValue: '支付二维码已过期，请重新下单',
        })
      case 'poll_timeout':
        return t('Payment status check timed out', {
          defaultValue: '支付状态查询已超时，请稍后在账单中确认',
        })
      default:
        return ''
    }
  })()

  // creating 全页 Spinner；paid_processing 保留二维码并在 live 区提示「入账中」
  const showSpinner = props.phase === 'creating'
  const showQr =
    (props.phase === 'pending' || props.phase === 'paid_processing') &&
    !!safeQrLink
  const showRetry =
    props.phase === 'creating_error' ||
    props.phase === 'failed' ||
    props.phase === 'expired' ||
    props.phase === 'poll_timeout'

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='sm:max-w-sm'>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>

        <div className='flex flex-col items-center gap-4 py-2'>
          <div
            role='status'
            aria-live='polite'
            className='text-muted-foreground w-full text-center text-sm'
            data-testid='pay-status-live'
          >
            {liveMessage}
          </div>

          {showSpinner ? (
            <div
              className='flex flex-col items-center gap-2 py-6'
              data-testid='pay-creating'
            >
              <Loader2 className='text-muted-foreground h-8 w-8 animate-spin' />
            </div>
          ) : null}

          {showQr && safeQrLink ? (
            <div
              data-testid='pay-qr'
              data-order-no={props.order?.orderNo}
              data-qr-value={safeQrLink}
              className='rounded-lg border bg-white p-4'
            >
              <QRCodeSVG value={safeQrLink} size={196} marginSize={2} />
            </div>
          ) : null}

          {props.order?.orderNo ? (
            <p className='text-muted-foreground text-center text-xs'>
              {t('Order', { defaultValue: '订单号' })}:{' '}
              <code>{props.order.orderNo}</code>
            </p>
          ) : null}

          {countdown && showQr ? (
            <p className='text-muted-foreground text-center text-xs'>
              {t('Expires in {{time}}', {
                time: countdown,
                defaultValue: '剩余 {{time}}',
              })}
            </p>
          ) : null}

          {showQr && safeQrLink ? (
            <a
              href={safeQrLink}
              target='_blank'
              rel='noopener noreferrer'
              data-testid='pay-qr-link'
              className='text-xs underline underline-offset-4'
            >
              {t('Trouble scanning? Open the payment page', {
                defaultValue: '扫码有问题？打开支付页面',
              })}
            </a>
          ) : null}

          {showRetry ? (
            <div className='flex w-full gap-2'>
              {props.onRetry ? (
                <Button
                  type='button'
                  className='flex-1'
                  data-testid='pay-retry'
                  onClick={props.onRetry}
                >
                  {t('Retry', { defaultValue: '重试' })}
                </Button>
              ) : null}
              {props.onDismiss ? (
                <Button
                  type='button'
                  variant='outline'
                  className='flex-1'
                  onClick={props.onDismiss}
                >
                  {t('Close', { defaultValue: '关闭' })}
                </Button>
              ) : null}
            </div>
          ) : null}
        </div>
      </DialogContent>
    </Dialog>
  )
}
