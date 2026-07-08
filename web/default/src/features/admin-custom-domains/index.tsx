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
import { RefreshCw, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { fmtDateTime } from '@/lib/agent-format'
import { adminUnbindCustomDomain, getAdminCustomDomains } from './api'

type BadgeVariant = 'default' | 'secondary' | 'destructive' | 'outline'

function statusVariant(status: string): BadgeVariant {
  if (status === 'active') return 'default'
  if (status === 'failed') return 'destructive'
  return 'secondary'
}

export function AdminCustomDomains() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const { data, isLoading } = useQuery({
    queryKey: ['admin-custom-domains'],
    queryFn: async () => {
      const res = await getAdminCustomDomains()
      return res.data || []
    },
    placeholderData: (prev) => prev,
  })

  const rows = data || []

  const handleUnbind = async (id: number, domain: string) => {
    if (!window.confirm(t('Force unbind {{domain}}?', { domain }))) return
    try {
      const res = await adminUnbindCustomDomain(id)
      if (res.success) {
        toast.success(t('Custom domain unbound'))
        queryClient.invalidateQueries({ queryKey: ['admin-custom-domains'] })
      }
    } catch {
      /* global interceptor toasts the error */
    }
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Custom Domains')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          size='sm'
          variant='outline'
          onClick={() =>
            queryClient.invalidateQueries({ queryKey: ['admin-custom-domains'] })
          }
        >
          <RefreshCw className='h-4 w-4' />
          {t('Refresh')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='overflow-hidden rounded-lg border'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Tenant')}</TableHead>
                <TableHead>{t('Domain')}</TableHead>
                <TableHead>{t('Status')}</TableHead>
                <TableHead>{t('Cert')}</TableHead>
                <TableHead>{t('Created At')}</TableHead>
                <TableHead className='text-right'>{t('Actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading ? (
                <TableRow>
                  <TableCell
                    colSpan={6}
                    className='text-muted-foreground text-center'
                  >
                    {t('Loading...')}
                  </TableCell>
                </TableRow>
              ) : rows.length === 0 ? (
                <TableRow>
                  <TableCell
                    colSpan={6}
                    className='text-muted-foreground text-center'
                  >
                    {t('No custom domains yet')}
                  </TableCell>
                </TableRow>
              ) : (
                rows.map((row) => (
                  <TableRow key={row.id}>
                    <TableCell>
                      <div className='flex flex-col'>
                        <span>{row.tenant_name || '-'}</span>
                        <span className='text-muted-foreground text-xs'>
                          {row.tenant_slug}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell className='font-mono text-sm'>
                      {row.domain}
                    </TableCell>
                    <TableCell>
                      <Badge variant={statusVariant(row.status)}>
                        {row.status}
                      </Badge>
                      {row.status === 'failed' && row.last_error && (
                        <div className='text-destructive mt-1 max-w-48 truncate text-xs'>
                          {row.last_error}
                        </div>
                      )}
                    </TableCell>
                    <TableCell className='text-sm'>
                      {row.cert_status ? (
                        <div className='flex flex-col'>
                          <span>{row.cert_status}</span>
                          {row.cert_expires_at && (
                            <span className='text-muted-foreground text-xs'>
                              {t('Expires')}: {fmtDateTime(row.cert_expires_at)}
                            </span>
                          )}
                          {/* SSL 到期提醒(P3 #9):≤30 天琥珀、已过期红。acme 每日自动续期,
                              cert-loop 每 ~20h 回刷 DB 到期时间,正常不会真过期。 */}
                          {row.cert_expires_at &&
                            (() => {
                              const d = Math.ceil(
                                (Date.parse(row.cert_expires_at) - Date.now()) /
                                  86400000
                              )
                              if (d > 30) return null
                              return d <= 0 ? (
                                <Badge variant='destructive' className='mt-0.5 w-fit'>
                                  {t('Cert expired, awaiting auto-renew check', {
                                    defaultValue: '证书已过期(待自动续期核查)',
                                  })}
                                </Badge>
                              ) : (
                                <Badge
                                  variant='secondary'
                                  className='mt-0.5 w-fit bg-amber-500/15 text-amber-700 dark:text-amber-400'
                                >
                                  {t('Cert expires in {{days}} day(s)', {
                                    defaultValue: '证书 {{days}} 天后到期',
                                    days: d,
                                  })}
                                </Badge>
                              )
                            })()}
                        </div>
                      ) : (
                        <span className='text-muted-foreground'>-</span>
                      )}
                    </TableCell>
                    <TableCell className='text-muted-foreground text-sm'>
                      {fmtDateTime(row.created_at)}
                    </TableCell>
                    <TableCell className='text-right'>
                      <Button
                        size='sm'
                        variant='outline'
                        onClick={() => handleUnbind(row.id, row.domain)}
                      >
                        <Trash2 className='h-4 w-4' />
                        {t('Unbind')}
                      </Button>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
