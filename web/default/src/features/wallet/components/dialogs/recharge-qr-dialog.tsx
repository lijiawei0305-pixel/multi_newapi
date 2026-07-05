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
import { QRCodeSVG } from 'qrcode.react'
import { useTranslation } from 'react-i18next'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

interface RechargeQrDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** QR payload — WeChat Native code_url (e.g. weixin://wxpay/...). */
  qr: string | null
  orderNo?: string
  amountUsd?: number
  amountCny?: number
}

/**
 * RechargeQrDialog renders the WeChat Native pay QR code for a pending recharge
 * order. The QR encodes the real `code_url` returned by the in-process WeChat
 * SDK; the user scans it in the WeChat app to pay. Payment settles via the async
 * notify callback (verified server-side) — this dialog only displays the QR.
 */
export function RechargeQrDialog({
  open,
  onOpenChange,
  qr,
  orderNo,
  amountUsd,
  amountCny,
}: RechargeQrDialogProps) {
  const { t } = useTranslation()
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-sm'>
        <DialogHeader>
          <DialogTitle>
            {t('Scan to pay with WeChat', { defaultValue: '请扫码支付' })}
          </DialogTitle>
          <DialogDescription>
            {amountUsd != null
              ? t('Recharge ${{usd}} (pay ¥{{cny}})', {
                  usd: amountUsd,
                  cny: amountCny,
                  defaultValue: '充值 ${{usd}}（支付 ¥{{cny}}）',
                })
              : t('Scan the QR code with WeChat to complete payment.', {
                  defaultValue: '请扫描二维码完成支付。',
                })}
          </DialogDescription>
        </DialogHeader>

        <div className='flex flex-col items-center gap-4 py-2'>
          {qr ? (
            <div
              data-testid='pay-qr'
              data-order-no={orderNo}
              data-qr-value={qr}
              className='rounded-lg border bg-white p-4'
            >
              <QRCodeSVG value={qr} size={196} marginSize={2} />
            </div>
          ) : null}

          {orderNo ? (
            <p className='text-muted-foreground text-center text-xs'>
              {t('Order', { defaultValue: '订单号' })}: <code>{orderNo}</code>
            </p>
          ) : null}

          {qr ? (
            // On mobile, the weixin:// code_url can open the WeChat app directly.
            <a
              href={qr}
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
        </div>
      </DialogContent>
    </Dialog>
  )
}
