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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Copy, Globe, Link2, RefreshCw, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Spinner } from '@/components/ui/spinner'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { getApiErrorCode } from '@/lib/api'

import {
  bindCustomDomain,
  getCustomDomain,
  unbindCustomDomain,
  verifyCustomDomain,
} from './api'
import { CertificateSeal } from './certificate-seal'
import type { DnsRecord } from './types'

// Map the stable backend error code to a localized message (skipErrorHandler path).
function domainErrorMessage(t: (k: string) => string, err: unknown): string {
  const code = getApiErrorCode(err)
  const map: Record<string, string> = {
    DOMAIN_INVALID: t('Invalid domain format'),
    DOMAIN_RESERVED: t('This domain is reserved and cannot be bound'),
    DOMAIN_TAKEN: t('This domain is already in use'),
    DOMAIN_LIMIT: t('Each site can bind only one custom domain'),
    DNS_VERIFY_FAILED: t(
      'DNS TXT verification failed. Make sure the record is live, then retry.'
    ),
  }
  return (code && map[code]) || t('Request failed')
}

function DnsRow({ label, record }: { label: string; record: DnsRecord }) {
  const { t } = useTranslation()
  const { copyToClipboard } = useCopyToClipboard()
  return (
    <div className='flex flex-col gap-2 rounded-md border p-3'>
      <div className='flex items-center gap-2'>
        <span className='text-muted-foreground text-xs font-medium'>
          {label}
        </span>
        <Badge variant='outline'>{record.type}</Badge>
      </div>
      <Field
        label={t('Host / Name')}
        value={record.name}
        onCopy={() => copyToClipboard(record.name)}
      />
      <Field
        label={t('Value')}
        value={record.value}
        onCopy={() => copyToClipboard(record.value)}
      />
    </div>
  )
}

function Field({
  label,
  value,
  onCopy,
}: {
  label: string
  value: string
  onCopy: () => void
}) {
  return (
    <div className='flex items-center gap-2'>
      <span className='text-muted-foreground w-20 shrink-0 text-xs'>
        {label}
      </span>
      <code className='bg-muted min-w-0 flex-1 truncate rounded px-2 py-1 text-xs'>
        {value}
      </code>
      <Button size='icon' variant='ghost' onClick={onCopy} title={label}>
        <Copy className='h-4 w-4' />
      </Button>
    </div>
  )
}

export function CustomDomain() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [domainInput, setDomainInput] = useState('')
  const [binding, setBinding] = useState(false)
  const [verifying, setVerifying] = useState(false)
  const [unbinding, setUnbinding] = useState(false)

  const { data, isLoading } = useQuery({
    queryKey: ['tenant-custom-domain'],
    queryFn: async () => {
      const res = await getCustomDomain()
      return res.data
    },
    // Poll while ownership/cert issuance is in progress so the UI advances to active.
    refetchInterval: (query) => {
      const s = query.state.data?.status
      return s === 'verifying' || s === 'dns_verified' ? 5000 : false
    },
    placeholderData: (prev) => prev,
  })

  const refresh = () =>
    queryClient.invalidateQueries({ queryKey: ['tenant-custom-domain'] })

  const handleBind = async () => {
    const d = domainInput.trim().toLowerCase()
    if (!d) {
      toast.error(t('Please enter a domain'))
      return
    }
    setBinding(true)
    try {
      const res = await bindCustomDomain(d)
      if (res.success) {
        toast.success(
          t('Domain bound. Add the DNS records below, then verify.')
        )
        setDomainInput('')
        refresh()
      }
    } catch (err) {
      toast.error(domainErrorMessage(t, err))
    } finally {
      setBinding(false)
    }
  }

  const handleVerify = async () => {
    setVerifying(true)
    try {
      const res = await verifyCustomDomain()
      if (res.success) {
        toast.success(t('Ownership verified. Issuing certificate...'))
      }
    } catch (err) {
      toast.error(domainErrorMessage(t, err))
    } finally {
      setVerifying(false)
      refresh()
    }
  }

  const handleUnbind = async () => {
    if (!window.confirm(t('Unbind this custom domain?'))) return
    setUnbinding(true)
    try {
      const res = await unbindCustomDomain()
      if (res.success) {
        toast.success(t('Custom domain unbound'))
        refresh()
      }
    } finally {
      setUnbinding(false)
    }
  }

  const bound = data?.bound
  const status = data?.status
  const showLoading = isLoading && !data

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Custom Domain')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='mx-auto flex w-full max-w-2xl flex-col gap-4'>
          {showLoading && (
            <div className='flex justify-center py-10'>
              <Spinner />
            </div>
          )}
          {!showLoading && !bound && (
            <Card>
              <CardHeader>
                <CardTitle className='flex items-center gap-2'>
                  <Globe className='h-5 w-5' />
                  {t('Bind a custom domain')}
                </CardTitle>
                <CardDescription>
                  {t(
                    'Point your domain at this site to run it under your own brand. After binding, add the shown DNS records and verify.'
                  )}
                </CardDescription>
              </CardHeader>
              <CardContent className='flex flex-col gap-3'>
                <Label htmlFor='custom-domain-input'>{t('Domain')}</Label>
                <div className='flex gap-2'>
                  <Input
                    id='custom-domain-input'
                    value={domainInput}
                    onChange={(e) => setDomainInput(e.target.value)}
                    placeholder='proxy.example.com'
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') handleBind()
                    }}
                  />
                  <Button onClick={handleBind} disabled={binding}>
                    {binding ? t('Saving...') : t('Bind')}
                  </Button>
                </div>
              </CardContent>
            </Card>
          )}
          {!showLoading && bound && (
            <>
              <Card>
                <CardHeader>
                  <CardTitle className='flex min-w-0 items-center gap-2'>
                    <Globe className='h-5 w-5 shrink-0' />
                    <span className='truncate'>{data?.domain}</span>
                  </CardTitle>
                </CardHeader>
                <CardContent className='flex flex-col items-center gap-6 pt-2'>
                  <CertificateSeal
                    status={status ?? 'pending_dns'}
                    lastError={data?.last_error}
                  />
                  {/* SSL 到期提醒(P3 #9):active 且 ≤30 天时提示;系统 acme 自动续期,仅供知会。 */}
                  {status === 'active' &&
                    data?.cert_expires_at &&
                    (() => {
                      const d = Math.ceil(
                        (Date.parse(data.cert_expires_at) - Date.now()) /
                          86400000
                      )
                      if (d > 30) return null
                      return (
                        <p className='text-muted-foreground -mt-3 text-center text-xs'>
                          {t(
                            'Certificate expires in {{days}} day(s); it renews automatically — contact the admin if this persists.',
                            {
                              defaultValue:
                                '证书将于 {{days}} 天后到期,系统会自动续期;若持续临期请联系管理员。',
                              days: Math.max(d, 0),
                            }
                          )}
                        </p>
                      )
                    })()}
                  <div className='flex flex-wrap justify-center gap-2'>
                    {status !== 'active' && (
                      <Button onClick={handleVerify} disabled={verifying}>
                        <RefreshCw className='h-4 w-4' />
                        {verifying
                          ? t('Verifying...')
                          : t('I have configured DNS — verify')}
                      </Button>
                    )}
                    <Button
                      variant='outline'
                      onClick={handleUnbind}
                      disabled={unbinding}
                    >
                      <Trash2 className='h-4 w-4' />
                      {t('Unbind')}
                    </Button>
                  </div>
                </CardContent>
              </Card>

              {status !== 'active' && data?.dns && (
                <Card>
                  <CardHeader>
                    <CardTitle className='flex items-center gap-2 text-base'>
                      <Link2 className='h-4 w-4' />
                      {t('Add these DNS records at your registrar')}
                    </CardTitle>
                    <CardDescription>
                      {t(
                        'Add the A record so traffic reaches us, and the TXT record so we can verify ownership. Do not proxy through Cloudflare (orange cloud).'
                      )}
                    </CardDescription>
                  </CardHeader>
                  <CardContent className='flex flex-col gap-3'>
                    <DnsRow label={t('A record')} record={data.dns.a_record} />
                    <DnsRow
                      label={t('TXT record (ownership)')}
                      record={data.dns.txt_record}
                    />
                  </CardContent>
                </Card>
              )}
            </>
          )}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
