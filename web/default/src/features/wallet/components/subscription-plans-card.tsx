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
import { useState, useEffect, useCallback } from 'react'
import type { TFunction } from 'i18next'
import { Crown, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatQuota } from '@/lib/format'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { Progress } from '@/components/ui/progress'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'
import { TitledCard } from '@/components/ui/titled-card'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  StatusBadge,
  dotColorMap,
  textColorMap,
} from '@/components/status-badge'
import {
  getSelfSubscriptionFull,
  updateBillingPreference,
} from '@/features/subscriptions/api'
import type { UserSubscriptionRecord } from '@/features/subscriptions/types'

// ============================================================================
// "我的订阅" — read-only view of the buyer's already-purchased subscriptions.
// Purchasing lives on the dedicated tokenplan page (features/tenant-plans);
// this card must not duplicate that flow, so it only ever displays
// getSelfSubscriptionFull() data + lets the buyer tune billing preference
// for the subscription(s) they already own.
// ============================================================================

function getBillingPreferenceLabel(preference: string, t: TFunction): string {
  switch (preference) {
    case 'subscription_first':
      return t('Subscription First', { defaultValue: '订阅余额优先' })
    case 'wallet_first':
      return t('Wallet First', { defaultValue: '钱包余额优先' })
    case 'subscription_only':
      return t('Subscription Only', { defaultValue: '仅用订阅余额' })
    case 'wallet_only':
      return t('Wallet Only', { defaultValue: '仅用钱包余额' })
    default:
      return preference
  }
}

export function SubscriptionPlansCard() {
  const { t } = useTranslation()

  const [activeSubscriptions, setActiveSubscriptions] = useState<
    UserSubscriptionRecord[]
  >([])
  const [allSubscriptions, setAllSubscriptions] = useState<
    UserSubscriptionRecord[]
  >([])
  const [billingPreference, setBillingPreference] =
    useState('subscription_first')
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)

  const fetchSelfSubscription = useCallback(async () => {
    try {
      const res = await getSelfSubscriptionFull()
      if (res.success && res.data) {
        setBillingPreference(
          res.data.billing_preference || 'subscription_first'
        )
        setActiveSubscriptions(res.data.subscriptions || [])
        setAllSubscriptions(res.data.all_subscriptions || [])
      }
    } catch {
      // ignore
    }
  }, [])

  useEffect(() => {
    const init = async () => {
      setLoading(true)
      await fetchSelfSubscription()
      setLoading(false)
    }
    init()
  }, [fetchSelfSubscription])

  const handleRefresh = async () => {
    setRefreshing(true)
    try {
      await fetchSelfSubscription()
    } finally {
      setRefreshing(false)
    }
  }

  const handlePreferenceChange = async (pref: string) => {
    const previous = billingPreference
    setBillingPreference(pref)
    try {
      const res = await updateBillingPreference(pref)
      if (res.success) {
        toast.success(t('Updated successfully', { defaultValue: '更新成功' }))
        const normalized = res.data?.billing_preference || pref
        setBillingPreference(normalized)
      } else {
        toast.error(
          res.message || t('Update failed', { defaultValue: '更新失败' })
        )
        setBillingPreference(previous)
      }
    } catch {
      toast.error(t('Request failed', { defaultValue: '请求失败' }))
      setBillingPreference(previous)
    }
  }

  const hasActive = activeSubscriptions.length > 0
  const hasAny = allSubscriptions.length > 0
  const disablePref = !hasActive
  const isSubPref =
    billingPreference === 'subscription_first' ||
    billingPreference === 'subscription_only'
  const displayPref =
    disablePref && isSubPref ? 'wallet_first' : billingPreference

  const getRemainingDays = (sub: UserSubscriptionRecord) => {
    const endTime = sub?.subscription?.end_time || 0
    if (!endTime) return 0
    const now = Date.now() / 1000
    return Math.max(0, Math.ceil((endTime - now) / 86400))
  }

  const getUsagePercent = (sub: UserSubscriptionRecord) => {
    const total = Number(sub?.subscription?.amount_total || 0)
    const used = Number(sub?.subscription?.amount_used || 0)
    if (total <= 0) return 0
    return Math.round((used / total) * 100)
  }

  if (loading) {
    return (
      <Card data-card-hover='false' className='gap-0 overflow-hidden py-0'>
        <CardHeader className='border-b p-3 !pb-3 sm:p-5 sm:!pb-5'>
          <Skeleton className='h-6 w-32' />
        </CardHeader>
        <CardContent className='space-y-3 p-3 sm:p-5'>
          <Skeleton className='h-8 w-full' />
          <Skeleton className='h-20 w-full' />
          <Skeleton className='h-20 w-full' />
        </CardContent>
      </Card>
    )
  }

  return (
    <TitledCard
      title={t('My Subscriptions', { defaultValue: '我的订阅' })}
      description={t('Your purchased subscription plans and usage', {
        defaultValue: '查看已购买的订阅套餐及使用情况',
      })}
      icon={<Crown className='h-4 w-4' />}
      disableHoverEffect
      contentClassName='space-y-4 sm:space-y-5'
    >
      <div className='flex flex-wrap items-center justify-between gap-2.5 sm:gap-3'>
        <span className='flex items-center gap-1.5 text-xs font-medium'>
          <span
            className={cn(
              'size-1.5 shrink-0 rounded-full',
              hasActive ? dotColorMap.success : dotColorMap.neutral
            )}
            aria-hidden='true'
          />
          {hasActive ? (
            <span className={cn(textColorMap.success)}>
              {activeSubscriptions.length}{' '}
              {t('active', { defaultValue: '个生效中' })}
            </span>
          ) : (
            <span className='text-muted-foreground'>
              {t('No Active', { defaultValue: '暂无生效订阅' })}
            </span>
          )}
          {allSubscriptions.length > activeSubscriptions.length && (
            <>
              <span className='text-muted-foreground/30'>·</span>
              <span className='text-muted-foreground'>
                {allSubscriptions.length - activeSubscriptions.length}{' '}
                {t('expired', { defaultValue: '个已过期' })}
              </span>
            </>
          )}
        </span>
        <div className='flex w-full items-center gap-2 sm:w-auto'>
          <Select
            items={[
              {
                value: 'subscription_first',
                label: (
                  <>
                    {getBillingPreferenceLabel('subscription_first', t)}
                    {disablePref
                      ? ` (${t('No Active', { defaultValue: '暂无生效订阅' })})`
                      : ''}
                  </>
                ),
              },
              {
                value: 'wallet_first',
                label: getBillingPreferenceLabel('wallet_first', t),
              },
              {
                value: 'subscription_only',
                label: (
                  <>
                    {getBillingPreferenceLabel('subscription_only', t)}
                    {disablePref
                      ? ` (${t('No Active', { defaultValue: '暂无生效订阅' })})`
                      : ''}
                  </>
                ),
              },
              {
                value: 'wallet_only',
                label: getBillingPreferenceLabel('wallet_only', t),
              },
            ]}
            value={displayPref}
            onValueChange={(v) => v !== null && handlePreferenceChange(v)}
          >
            <SelectTrigger className='h-8 flex-1 text-xs sm:w-[140px] sm:flex-none'>
              <SelectValue>
                {getBillingPreferenceLabel(displayPref, t)}
              </SelectValue>
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                <SelectItem value='subscription_first' disabled={disablePref}>
                  {getBillingPreferenceLabel('subscription_first', t)}
                  {disablePref
                    ? ` (${t('No Active', { defaultValue: '暂无生效订阅' })})`
                    : ''}
                </SelectItem>
                <SelectItem value='wallet_first'>
                  {getBillingPreferenceLabel('wallet_first', t)}
                </SelectItem>
                <SelectItem value='subscription_only' disabled={disablePref}>
                  {getBillingPreferenceLabel('subscription_only', t)}
                  {disablePref
                    ? ` (${t('No Active', { defaultValue: '暂无生效订阅' })})`
                    : ''}
                </SelectItem>
                <SelectItem value='wallet_only'>
                  {getBillingPreferenceLabel('wallet_only', t)}
                </SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
          <Button
            variant='ghost'
            size='icon'
            className='h-8 w-8'
            onClick={handleRefresh}
            disabled={refreshing}
            aria-label={t('Refresh', { defaultValue: '刷新' })}
          >
            <RefreshCw
              className={`h-3.5 w-3.5 ${refreshing ? 'animate-spin' : ''}`}
            />
          </Button>
        </div>
      </div>

      {disablePref && isSubPref && (
        <p className='text-muted-foreground text-xs'>
          {t(
            'Preference saved as {{pref}}, but no active subscription. Wallet will be used automatically.',
            {
              pref:
                billingPreference === 'subscription_only'
                  ? t('Subscription Only', { defaultValue: '仅用订阅余额' })
                  : t('Subscription First', { defaultValue: '订阅余额优先' }),
              defaultValue:
                '偏好已设为 {{pref}}，但当前无生效订阅，系统将自动改用钱包余额扣费。',
            }
          )}
        </p>
      )}

      {hasAny ? (
        <>
          <Separator />
          <div className='max-h-64 space-y-3 overflow-y-auto pr-1'>
            {allSubscriptions.map((sub) => {
              const subscription = sub.subscription
              const totalAmount = Number(subscription?.amount_total || 0)
              const usedAmount = Number(subscription?.amount_used || 0)
              const remainAmount =
                totalAmount > 0 ? Math.max(0, totalAmount - usedAmount) : 0
              const remainDays = getRemainingDays(sub)
              const usagePercent = getUsagePercent(sub)
              const now = Date.now() / 1000
              const isExpired = (subscription?.end_time || 0) < now
              const isCancelled = subscription?.status === 'cancelled'
              const isActive = subscription?.status === 'active' && !isExpired

              return (
                <div
                  key={subscription?.id}
                  className='bg-background rounded-md border p-3 text-xs'
                >
                  <div className='flex items-center justify-between'>
                    <div className='flex items-center gap-2'>
                      <span className='font-medium'>
                        {t('Subscription', { defaultValue: '订阅' })} #
                        {subscription?.id}
                      </span>
                      {isActive ? (
                        <StatusBadge
                          label={t('Active', { defaultValue: '生效中' })}
                          variant='success'
                          copyable={false}
                        />
                      ) : isCancelled ? (
                        <StatusBadge
                          label={t('Cancelled', { defaultValue: '已取消' })}
                          variant='neutral'
                          copyable={false}
                        />
                      ) : (
                        <StatusBadge
                          label={t('Expired', { defaultValue: '已过期' })}
                          variant='neutral'
                          copyable={false}
                        />
                      )}
                    </div>
                    {isActive && (
                      <span className='text-muted-foreground'>
                        {t('{{count}} days remaining', {
                          count: remainDays,
                          defaultValue: '剩余 {{count}} 天',
                        })}
                      </span>
                    )}
                  </div>
                  <div className='text-muted-foreground mt-1.5'>
                    {isActive
                      ? t('Until', { defaultValue: '有效期至' })
                      : isCancelled
                        ? t('Cancelled at', { defaultValue: '取消于' })
                        : t('Expired at', { defaultValue: '过期于' })}{' '}
                    {new Date(
                      (subscription?.end_time || 0) * 1000
                    ).toLocaleString()}
                  </div>
                  {isActive && (subscription?.next_reset_time ?? 0) > 0 && (
                    <div className='text-muted-foreground mt-1'>
                      {t('Next reset', { defaultValue: '下次重置' })}:{' '}
                      {new Date(
                        subscription!.next_reset_time! * 1000
                      ).toLocaleString()}
                    </div>
                  )}
                  <div className='text-muted-foreground mt-1'>
                    {t('Total Quota', { defaultValue: '总额度' })}:{' '}
                    {totalAmount > 0 ? (
                      <Tooltip>
                        <TooltipTrigger
                          render={<span className='cursor-help' />}
                        >
                          {formatQuota(usedAmount)}/{formatQuota(totalAmount)}{' '}
                          · {t('Remaining', { defaultValue: '剩余' })}{' '}
                          {formatQuota(remainAmount)}
                        </TooltipTrigger>
                        <TooltipContent>
                          {t('Raw Quota', { defaultValue: '原始额度' })}:{' '}
                          {usedAmount}/{totalAmount} ·{' '}
                          {t('Remaining', { defaultValue: '剩余' })}{' '}
                          {remainAmount}
                        </TooltipContent>
                      </Tooltip>
                    ) : (
                      t('Unlimited', { defaultValue: '无限量' })
                    )}
                    {totalAmount > 0 && (
                      <span className='ml-2'>
                        {t('Used', { defaultValue: '已用' })} {usagePercent}%
                      </span>
                    )}
                  </div>
                  {totalAmount > 0 && isActive && (
                    <Progress value={usagePercent} className='mt-2 h-1.5' />
                  )}
                </div>
              )
            })}
          </div>
        </>
      ) : (
        <p className='text-muted-foreground py-6 text-center text-sm'>
          {t('You have not purchased any subscription plan yet', {
            defaultValue: '您还没有购买任何订阅套餐',
          })}
        </p>
      )}
    </TitledCard>
  )
}
