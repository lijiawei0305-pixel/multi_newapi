import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { AlertTriangle, X } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { getSelfSubscriptions } from '../api'
import { computeUsageAlert } from '../lib/usage-alert'

export function SubscriptionUsageBanner() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [dismissedTick, setDismissedTick] = useState(0) // 触发重渲染
  const { data } = useQuery({
    queryKey: ['self-subscriptions'],
    queryFn: getSelfSubscriptions,
    staleTime: 60_000, // 每页挂载共享缓存，避免频繁重拉
  })

  const alert = computeUsageAlert(data?.data)
  if (alert.level === 'none') return null

  const dismissKey = `subUsageDismiss:${alert.subscriptionId}:${alert.level}`
  if (typeof localStorage !== 'undefined' && localStorage.getItem(dismissKey)) {
    return null
  }
  void dismissedTick // 关闭后 state 变化触发重渲染 → 上面 localStorage 命中 → 隐藏

  const pct = Math.round(alert.ratio * 100)
  const exhausted = alert.level === 'exhausted'

  const dismiss = () => {
    try {
      localStorage.setItem(dismissKey, '1')
    } catch {
      /* localStorage 不可用则仅本次隐藏 */
    }
    setDismissedTick((n) => n + 1)
  }

  return (
    <Alert
      variant={exhausted ? 'destructive' : 'default'}
      className={cn(
        'flex items-center gap-3 rounded-none border-x-0 border-t-0',
        !exhausted &&
          'border-yellow-500/50 text-yellow-800 dark:text-yellow-300 [&>svg]:text-yellow-600'
      )}
    >
      <AlertTriangle className='h-4 w-4 shrink-0' />
      <AlertDescription className='flex-1'>
        {exhausted
          ? t('subUsage.exhausted', {
              defaultValue: '你的套餐额度已用尽，续费或换套餐以继续使用。',
            })
          : t('subUsage.warn', {
              defaultValue: '你的套餐已用 {{pct}}%，快用完了，建议尽快续费。',
              pct,
            })}
      </AlertDescription>
      <Button
        size='sm'
        variant={exhausted ? 'secondary' : 'default'}
        onClick={() => navigate({ to: '/plans' })}
      >
        {t('subUsage.cta', { defaultValue: '去续费' })}
      </Button>
      <Button
        size='icon'
        variant='ghost'
        className='h-6 w-6 shrink-0'
        aria-label={t('subUsage.dismiss', { defaultValue: '关闭' })}
        onClick={dismiss}
      >
        <X className='h-4 w-4' />
      </Button>
    </Alert>
  )
}
