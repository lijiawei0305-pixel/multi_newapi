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
import * as React from 'react'
import { useTranslation } from 'react-i18next'
import { SiAlipay, SiWechat } from 'react-icons/si'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import {
  Card,
  CardAction,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import {
  getPaymentProviders,
  updatePaymentProvider,
  type PaymentProvider,
  type PaymentProvidersData,
} from './payment-providers-api'

type ProviderMeta = {
  id: PaymentProvider
  title: string
  callback: string
  icon: React.ReactNode
}

/**
 * PaymentProvidersStatusSection shows the WeChat / Alipay channel cards.
 *
 * Credentials live in the independent auth-service (configured flag is
 * read-only here); the enabled switch is the admin's main-site toggle that
 * gates buyer availability (available = enabled && configured).
 */
export function PaymentProvidersStatusSection() {
  const { t } = useTranslation()
  const [data, setData] = React.useState<PaymentProvidersData | null>(null)
  const [loading, setLoading] = React.useState(true)
  const [error, setError] = React.useState(false)
  const [pending, setPending] = React.useState<PaymentProvider | null>(null)

  React.useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError(false)
    getPaymentProviders()
      .then((res) => {
        if (cancelled) return
        if (res.success === true && res.data) {
          setData(res.data)
        } else {
          setError(true)
        }
      })
      .catch(() => {
        if (!cancelled) setError(true)
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [])

  const handleToggle = React.useCallback(
    async (provider: PaymentProvider, next: boolean) => {
      // Optimistic update.
      setData((prev) =>
        prev
          ? { ...prev, [provider]: { ...prev[provider], enabled: next } }
          : prev
      )
      setPending(provider)
      try {
        const res = await updatePaymentProvider(provider, next)
        if (res.success !== true) {
          throw new Error(res.message || 'update failed')
        }
        toast.success(t('Provider settings updated'))
      } catch {
        // Roll back on failure.
        setData((prev) =>
          prev
            ? { ...prev, [provider]: { ...prev[provider], enabled: !next } }
            : prev
        )
        toast.error(t('Failed to update payment provider'))
      } finally {
        setPending(null)
      }
    },
    [t]
  )

  const providers: ProviderMeta[] = [
    {
      id: 'wxpay',
      title: t('WeChat Pay (Native)'),
      callback: '/pay/wxpay/notify',
      icon: <SiWechat className='h-5 w-5' style={{ color: '#07C160' }} />,
    },
    {
      id: 'alipay',
      title: t('Alipay (PC Web)'),
      callback: '/auth/alipay/notify',
      icon: <SiAlipay className='h-5 w-5' style={{ color: '#1677FF' }} />,
    },
  ]

  return (
    <div className='mb-6 space-y-3'>
      <div>
        <h3 className='text-lg font-medium'>{t('Payment Providers')}</h3>
        <p className='text-muted-foreground text-sm'>
          {t(
            'Credentials are set in auth-service (environment variables / config.yaml), not here.'
          )}
        </p>
      </div>

      {loading ? (
        <div className='grid gap-4 md:grid-cols-2'>
          {[0, 1].map((i) => (
            <Card key={i} size='sm'>
              <CardHeader>
                <Skeleton className='h-5 w-40' />
              </CardHeader>
              <CardContent className='space-y-2'>
                <Skeleton className='h-4 w-24' />
                <Skeleton className='h-4 w-56' />
              </CardContent>
            </Card>
          ))}
        </div>
      ) : error || !data ? (
        <p className='text-destructive text-sm'>
          {t('Failed to load payment providers')}
        </p>
      ) : (
        <div className='grid gap-4 md:grid-cols-2'>
          {providers.map((p) => {
            const state = data[p.id]
            return (
              <Card key={p.id} size='sm'>
                <CardHeader className='border-b'>
                  <CardTitle className='flex items-center gap-2'>
                    {p.icon}
                    <span>{p.title}</span>
                    <Badge
                      variant={state.configured ? 'default' : 'secondary'}
                      className={
                        state.configured
                          ? 'bg-emerald-500 text-white'
                          : undefined
                      }
                    >
                      {state.configured
                        ? t('Configured')
                        : t('Not configured')}
                    </Badge>
                  </CardTitle>
                  <CardAction>
                    <div className='flex items-center gap-2'>
                      <Label
                        htmlFor={`provider-${p.id}-enabled`}
                        className='text-muted-foreground text-xs'
                      >
                        {t('Enabled')}
                      </Label>
                      <Switch
                        id={`provider-${p.id}-enabled`}
                        checked={state.enabled}
                        disabled={pending === p.id}
                        onCheckedChange={(checked) =>
                          handleToggle(p.id, checked)
                        }
                      />
                    </div>
                  </CardAction>
                </CardHeader>
                <CardContent className='text-muted-foreground space-y-1 text-xs'>
                  <p>
                    {t('Callback address')}:{' '}
                    <code className='bg-muted text-foreground rounded px-1 py-0.5'>
                      {p.callback}
                    </code>
                  </p>
                </CardContent>
              </Card>
            )
          })}
        </div>
      )}
    </div>
  )
}
