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
import { getSelfSubscriptionFull } from '../api'
import { computeUsageAlert } from '../lib/usage-alert'

export function SubscriptionUsageBanner() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [, forceUpdate] = useReducer((n: number) => n + 1, 0) // 关闭后强制重渲染，重读 localStorage 判据
  const { data } = useQuery({
    queryKey: ['self-subscription-full'],
    queryFn: getSelfSubscriptionFull,
    // 数据为载入时快照（全局 refetchOnWindowFocus=false、无失效方），会话中不主动
    // 刷新——对促续费提示已足够；如需实时可后续接购买后失效。
    staleTime: 60_000,
  })

  // /api/subscription/self 返回的是对象 { subscriptions, all_subscriptions, ... }，
  // 满额提醒只看当前生效中的订阅列表（subscriptions），过期/已取消的不计入。
  const alert = computeUsageAlert(data?.data?.subscriptions)
  if (alert.level === 'none') return null

  // key 带上 resetMarker（next_reset_time）：后端月度重置是原地清零 amount_used
  // （同一条订阅记录、同一个 id），不带周期标记的话关闭一次就永久不再弹。
  const dismissKey = `subUsageDismiss:${alert.subscriptionId}:${alert.level}:${alert.resetMarker}`
  if (typeof localStorage !== 'undefined' && localStorage.getItem(dismissKey)) {
    return null
  }

  const pct = Math.round(alert.ratio * 100)
  const exhausted = alert.level === 'exhausted'

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
      variant={exhausted ? 'destructive' : 'default'}
      className={cn(
        'flex items-center gap-3 rounded-none border-x-0 border-t-0',
        !exhausted &&
          'border-yellow-500/50 text-yellow-800 dark:text-yellow-300 [&>svg]:text-yellow-600 *:data-[slot=alert-description]:text-yellow-800 dark:*:data-[slot=alert-description]:text-yellow-300'
      )}
    >
      <AlertTriangle className='h-4 w-4 shrink-0' />
      <AlertDescription className='flex-1'>
        {exhausted
          ? t('Your plan quota is exhausted. Renew or switch plans to continue.', {
              defaultValue: '你的套餐额度已用尽，续费或换套餐以继续使用。',
            })
          : t('Your plan has used {{pct}}%, running low — renew soon.', {
              defaultValue: '你的套餐已用 {{pct}}%，快用完了，建议尽快续费。',
              pct,
            })}
      </AlertDescription>
      <Button
        size='sm'
        variant={exhausted ? 'secondary' : 'default'}
        onClick={() => navigate({ to: '/plans' })}
      >
        {t('Renew now', { defaultValue: '去续费' })}
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
