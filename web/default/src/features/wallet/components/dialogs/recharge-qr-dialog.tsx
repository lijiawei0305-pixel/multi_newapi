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
  /** QR payload (WeChat code_url / mock confirm URL). */
  qr: string | null
  orderNo?: string
  amountUsd?: number
  amountCny?: number
}

/**
 * RechargeQrDialog renders the WeChat-pay QR code for a pending recharge order.
 * In mock mode the QR encodes the auth-service confirm page URL (also shown as a
 * clickable link), so a payment can be completed without a real scanner.
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
          <DialogTitle>{t('Scan to pay with WeChat')}</DialogTitle>
          <DialogDescription>
            {amountUsd != null
              ? t('Recharge ${{usd}} (pay ¥{{cny}})', {
                  usd: amountUsd,
                  cny: amountCny,
                })
              : t('Scan the QR code with WeChat to complete payment.')}
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
              {t('Order')}: <code>{orderNo}</code>
            </p>
          ) : null}

          {qr ? (
            // Mock fallback: open the confirm page directly (real WeChat replaces this).
            <a
              href={qr}
              target='_blank'
              rel='noopener noreferrer'
              data-testid='pay-qr-link'
              className='text-xs underline underline-offset-4'
            >
              {t('Trouble scanning? Open the payment page')}
            </a>
          ) : null}
        </div>
      </DialogContent>
    </Dialog>
  )
}
