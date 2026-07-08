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
import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { QRCodeSVG } from 'qrcode.react'
import { Check, Globe, KeyRound } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'
import { agentContextQueryOptions } from '@/lib/agent-context'
import { getPaymentIcon } from '@/features/wallet/lib'
import { useRechargeMethods } from '@/features/wallet/hooks/use-recharge-methods'
import type { RechargeProvider } from '@/features/wallet/hooks/use-tenant-recharge'

import { type AgentPlan, getPublicAgentPlans, purchaseAgentPlan } from './api'

interface QrState {
  orderNo?: string
  qr: string
  amountCny?: number
}

export function AgentPlansPurchase() {
  const { t } = useTranslation()
  const [provider, setProvider] = useState<RechargeProvider>('wxpay')
  const [slug, setSlug] = useState('')
  const [name, setName] = useState('')
  const [qrState, setQrState] = useState<QrState | null>(null)
  const [buyingId, setBuyingId] = useState<number | null>(null)

  const { methods: officialMethods, loading: methodsLoading } =
    useRechargeMethods()
  const officialProviders = useMemo<RechargeProvider[]>(
    () => officialMethods ?? [],
    [officialMethods]
  )
  useEffect(() => {
    if (officialProviders.length > 0 && !officialProviders.includes(provider)) {
      setProvider(officialProviders[0])
    }
  }, [officialProviders, provider])

  const noPaymentConfigured = !methodsLoading && officialProviders.length === 0

  const { data: plans, isLoading } = useQuery({
    queryKey: ['become-agent', 'plans'],
    queryFn: getPublicAgentPlans,
    placeholderData: (prev) => prev,
  })

  // 已是代理 → 购买走「升级/续期」分支,后端忽略 slug/name(见 agent_plan_bridge.go 升级契约),
  // 故隐藏首开输入、换成升级说明——否则买家填了没反应会误以为坏了(2026-07-08 用户反馈)。
  const { data: agentCtx } = useQuery(agentContextQueryOptions)
  const isExistingAgent = !!agentCtx?.is_agent_owner

  const purchase = useMutation({
    mutationFn: (plan: AgentPlan) =>
      purchaseAgentPlan(plan.id, {
        provider: provider as 'wxpay' | 'alipay',
        slug: slug.trim(),
        name: name.trim(),
      }),
    onMutate: (plan) => setBuyingId(plan.id),
    onSettled: () => setBuyingId(null),
    onSuccess: (res) => {
      if (!res.success) return
      const data = res.data
      const wxQr = data?.pay?.wxpay_qr
      if (wxQr) {
        setQrState({ orderNo: data?.order_no, qr: wxQr, amountCny: data?.amount_cny })
        return
      }
      const aliUrl = data?.pay?.alipay_url
      if (aliUrl) {
        window.location.href = aliUrl
        return
      }
      toast.success(res.message || t('Order created successfully'))
    },
  })

  const sorted = useMemo(
    () => [...(plans ?? [])].sort((a, b) => a.sort - b.sort),
    [plans]
  )

  const providerButton = (value: RechargeProvider, label: string) => (
    <Button
      type='button'
      size='sm'
      variant='outline'
      aria-pressed={provider === value}
      onClick={() => setProvider(value)}
      className={cn(
        'gap-1.5',
        provider === value
          ? 'border-foreground bg-foreground/5 dark:bg-foreground/10'
          : 'border-muted'
      )}
    >
      {getPaymentIcon(value, 'h-4 w-4')}
      <span>{label}</span>
    </Button>
  )

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Become Agent', { defaultValue: '开通代理' })}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='flex flex-col gap-6 pb-4'>
          <p className='text-muted-foreground text-sm'>
            {t('Become Agent Intro', {
              defaultValue:
                '选择一档代理套餐,支付开通后即成为对应档位代理(一次性付费,含有效期)。首次开通请填写你的专属子域名与站点名;已是代理则为升级,可留空。',
            })}
          </p>

          {noPaymentConfigured ? (
            <Alert>
              <AlertDescription>
                {t('Become Agent No Payment', {
                  defaultValue: '管理员尚未配置支付渠道,暂无法在线开通,请联系客服。',
                })}
              </AlertDescription>
            </Alert>
          ) : null}

          {/* 首次开通信息(已是代理 → 升级/续期,后端忽略 slug/name → 隐藏输入,免得填了没反应) */}
          {isExistingAgent ? (
            <Alert>
              <AlertDescription>
                {t('Become Agent Upgrade Hint', {
                  defaultValue:
                    '你已是代理:购买将升级/续期你现有的代理站(档位、批发折扣与有效期),不更改站点标识与子域名;升级到 OEM/API 档会自动开通你的独立站点。',
                })}
              </AlertDescription>
            </Alert>
          ) : (
            <div className='grid gap-3 sm:grid-cols-2'>
              <div className='space-y-1.5'>
                <label htmlFor='agent-slug' className='text-xs font-medium'>
                  {t('Become Agent Slug', { defaultValue: '子域名(首次开通)' })}
                </label>
                <Input
                  id='agent-slug'
                  placeholder='myshop'
                  value={slug}
                  onChange={(e) => setSlug(e.target.value)}
                  className='h-9'
                />
                <p className='text-muted-foreground/70 text-[11px]'>
                  {t('Become Agent Slug Hint', {
                    defaultValue: '将生成 <子域名>.wedreamhub.com(仅 OEM/API 档启用站点)',
                  })}
                </p>
              </div>
              <div className='space-y-1.5'>
                <label htmlFor='agent-name' className='text-xs font-medium'>
                  {t('Become Agent Name', { defaultValue: '站点名(首次开通)' })}
                </label>
                <Input
                  id='agent-name'
                  placeholder={t('Become Agent Name Ph', { defaultValue: '我的 AI 站' })}
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  className='h-9'
                />
              </div>
            </div>
          )}

          {/* 支付方式 */}
          {officialProviders.length > 0 ? (
            <div className='flex flex-wrap items-center gap-2'>
              <span className='text-muted-foreground text-xs font-medium tracking-wider uppercase'>
                {t('Payment Method')}
              </span>
              {officialProviders.includes('wxpay') &&
                providerButton('wxpay', t('WeChat Pay'))}
              {officialProviders.includes('alipay') &&
                providerButton('alipay', t('Alipay'))}
            </div>
          ) : null}

          {/* 套餐卡 */}
          {isLoading ? (
            <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-3'>
              {[0, 1, 2].map((i) => (
                <Skeleton key={i} className='h-56 w-full' />
              ))}
            </div>
          ) : (
            <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-3'>
              {sorted.map((plan) => (
                <Card
                  key={plan.id}
                  className={plan.is_recommended ? 'border-primary/40 shadow-lg' : ''}
                >
                  <CardHeader>
                    <CardTitle className='flex items-center justify-between'>
                      <span>{plan.name}</span>
                      {plan.is_recommended ? (
                        <span className='bg-primary text-primary-foreground rounded-full px-2 py-0.5 text-[11px]'>
                          {t('Agent Join Plan Recommended', { defaultValue: '推荐' })}
                        </span>
                      ) : null}
                    </CardTitle>
                    {plan.description ? (
                      <CardDescription>{plan.description}</CardDescription>
                    ) : null}
                  </CardHeader>
                  <CardContent className='space-y-3'>
                    <div>
                      {plan.anchor_price_cny > plan.price_cny ? (
                        <p className='text-muted-foreground text-sm line-through'>
                          ¥{plan.anchor_price_cny.toLocaleString('zh-CN')}
                        </p>
                      ) : null}
                      <p>
                        <span className='text-3xl font-bold tracking-tight'>
                          ¥{plan.price_cny.toLocaleString('zh-CN')}
                        </span>
                        <span className='text-muted-foreground ml-1 text-xs'>
                          {t('Become Agent Validity', {
                            defaultValue: `有效期 ${plan.valid_days} 天`,
                          })}
                        </span>
                      </p>
                    </div>
                    <ul className='text-muted-foreground space-y-1 text-xs'>
                      <li className='flex items-center gap-1.5'>
                        <Check className='text-primary h-3.5 w-3.5' />
                        {t('Become Agent Cap Distribute', {
                          defaultValue: '分销获客 + 充值差价 + 消耗分润',
                        })}
                      </li>
                      {plan.grant_level >= 1 ? (
                        <li className='flex items-center gap-1.5'>
                          <Globe className='text-primary h-3.5 w-3.5' />
                          {/* API 档(can_api)明示「含 OEM 全部能力」——API=OEM 超集(2026-07-08 用户定档);OEM 档保持原文案 */}
                          {plan.grant_can_api
                            ? t('Become Agent Cap Oem Superset', {
                                defaultValue: '含 OEM 全部能力(独立域名 + 站点装修)',
                              })
                            : t('Become Agent Cap Oem', {
                                defaultValue: '独立域名 + 站点装修(OEM)',
                              })}
                        </li>
                      ) : null}
                      {plan.grant_can_api ? (
                        <li className='flex items-center gap-1.5'>
                          <KeyRound className='text-primary h-3.5 w-3.5' />
                          {t('Become Agent Cap Api', { defaultValue: '开放 API 能力' })}
                        </li>
                      ) : null}
                    </ul>
                    <Button
                      className='w-full'
                      disabled={noPaymentConfigured || purchase.isPending}
                      onClick={() => purchase.mutate(plan)}
                    >
                      {buyingId === plan.id
                        ? t('Processing', { defaultValue: '处理中…' })
                        : t('Agent Join Plan CTA', { defaultValue: '立即开通' })}
                    </Button>
                  </CardContent>
                </Card>
              ))}
            </div>
          )}

          {/* 微信支付二维码 */}
          {qrState ? (
            <Card className='border-primary/30 mx-auto max-w-sm'>
              <CardHeader>
                <CardTitle className='text-base'>
                  {t('Become Agent Scan', { defaultValue: '微信扫码支付' })}
                </CardTitle>
                <CardDescription>
                  {t('Become Agent Scan Hint', {
                    defaultValue: '支付完成后代理身份将自动开通,可刷新页面查看。',
                  })}
                </CardDescription>
              </CardHeader>
              <CardContent className='flex flex-col items-center gap-3'>
                <div className='rounded-lg bg-white p-3'>
                  <QRCodeSVG value={qrState.qr} size={200} />
                </div>
                {qrState.amountCny != null ? (
                  <p className='text-lg font-semibold'>
                    ¥{qrState.amountCny.toLocaleString('zh-CN')}
                  </p>
                ) : null}
                <div className='flex gap-2'>
                  <Button variant='outline' onClick={() => window.location.reload()}>
                    {t('Become Agent Paid Refresh', { defaultValue: '我已支付,刷新' })}
                  </Button>
                  <Button variant='ghost' onClick={() => setQrState(null)}>
                    {t('Close', { defaultValue: '关闭' })}
                  </Button>
                </div>
              </CardContent>
            </Card>
          ) : null}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
