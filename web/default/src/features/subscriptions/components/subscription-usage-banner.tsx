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
import { useReducer } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { AlertTriangle, X } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { getTenantTokenPlans } from '@/features/tenant-plans/api'
import { getSelfSubscriptionFull } from '../api'
import { computeUsageAlert } from '../lib/usage-alert'

export function SubscriptionUsageBanner() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  // 关闭后强制重渲染，重读 localStorage 判据
  const [, forceUpdate] = useReducer((n: number) => n + 1, 0)
  const { data } = useQuery({
    queryKey: ['self-subscription-full'],
    queryFn: getSelfSubscriptionFull,
    // 数据为载入时快照（全局 refetchOnWindowFocus=false、无失效方），会话中不主动
    // 刷新——对促续费提示已足够；如需实时可后续接购买后失效。
    staleTime: 60_000,
  })
  // 买家套餐列表：把告警里的原生 plan_id 反查成"我们的"套餐 id，拼一键续费深链
  // /plans?renew=<id>（P3-RNW 降级版）。与购买页共用 queryKey → 共享缓存，零额外请求成本。
  const { data: plansData } = useQuery({
    queryKey: ['tenant-token-plans'],
    queryFn: async () => (await getTenantTokenPlans()).data || [],
    staleTime: 5 * 60 * 1000,
  })

  // /api/subscription/self 返回的是对象 { subscriptions, all_subscriptions, ... }，
  // 告警看当前生效中的订阅列表（subscriptions）：用量维（80%/100%）+ 时间维
  // （到期前 ≤7 天 expiring / 到期后 ≤7 天宽限 expired，见 computeUsageAlert）。
  const alert = computeUsageAlert(
    data?.data?.subscriptions,
    Math.floor(Date.now() / 1000)
  )
  if (alert.level === 'none') return null

  // key 带上 resetMarker（next_reset_time）：后端月度重置是原地清零 amount_used
  // （同一条订阅记录、同一个 id），不带周期标记的话关闭一次就永久不再弹。
  // 档位在 key 内 → warn 关过升 expiring/expired/exhausted 仍会重弹。
  const dismissKey = `subUsageDismiss:${alert.subscriptionId}:${alert.level}:${alert.resetMarker}`
  if (typeof localStorage !== 'undefined' && localStorage.getItem(dismissKey)) {
    return null
  }

  const pct = Math.round(alert.ratio * 100)
  const red = alert.level === 'exhausted' || alert.level === 'expired'
  // 一键续费：exhausted/expired/expiring 都指向"重购同套餐"；原生 plan_id → 我们的套餐 id。
  const renewPlanId =
    (plansData || []).find(
      (p) => alert.planId > 0 && p.native_plan_id === alert.planId
    )?.id ?? 0
  const oneClickRenew = alert.level !== 'warn' && renewPlanId > 0

  const message = (() => {
    switch (alert.level) {
      case 'exhausted':
        return t('Your plan quota is exhausted. Renew or switch plans to continue.', {
          defaultValue: '你的套餐额度已用尽，续费或换套餐以继续使用。',
        })
      case 'expired':
        return t('Your plan has expired. Renew now to continue using it.', {
          defaultValue: '你的套餐已到期，立即续费以继续使用。',
        })
      case 'expiring':
        return t('Your plan expires in {{days}} day(s) — renew soon to avoid interruption.', {
          defaultValue: '你的套餐将于 {{days}} 天后到期，及时续费以免服务中断。',
          days: alert.daysLeft,
        })
      default:
        return t('Your plan has used {{pct}}%, running low — renew soon.', {
          defaultValue: '你的套餐已用 {{pct}}%，快用完了，建议尽快续费。',
          pct,
        })
    }
  })()

  const dismiss = () => {
    try {
      localStorage.setItem(dismissKey, '1')
    } catch {
      /* localStorage 不可用则仅本次隐藏 */
    }
    forceUpdate()
  }

  return (
    <Alert
      variant={red ? 'destructive' : 'default'}
      className={cn(
        'flex items-center gap-3 rounded-none border-x-0 border-t-0',
        !red &&
          'border-yellow-500/50 text-yellow-800 dark:text-yellow-300 [&>svg]:text-yellow-600 *:data-[slot=alert-description]:text-yellow-800 dark:*:data-[slot=alert-description]:text-yellow-300'
      )}
    >
      <AlertTriangle className='h-4 w-4 shrink-0' />
      <AlertDescription className='flex-1'>{message}</AlertDescription>
      <Button
        size='sm'
        variant={red ? 'secondary' : 'default'}
        onClick={() =>
          navigate(
            oneClickRenew
              ? { to: '/plans', search: { renew: renewPlanId } }
              : { to: '/plans' }
          )
        }
      >
        {oneClickRenew
          ? t('Renew Now (one click)', { defaultValue: '立即续费' })
          : t('Renew now', { defaultValue: '去续费' })}
      </Button>
      <Button
        size='icon'
        variant='ghost'
        className='h-6 w-6 shrink-0'
        aria-label={t('Dismiss', { defaultValue: '关闭' })}
        onClick={dismiss}
      >
        <X className='h-4 w-4' />
      </Button>
    </Alert>
  )
}
