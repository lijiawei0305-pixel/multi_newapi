import { Info } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'

/**
 * SampleDataBadge —— 「示例数据」角标。
 *
 * 标注所在区块当前为演示占位数据(真实后端指标尚未接入),避免终端用户把 mock-stats 按模型名
 * 编造的数值当成真实指标据以选型(审计 #13)。当前仍需标注的区块:App 排行(buildAppRankings)、
 * 速率限制(buildRateLimits)。真实数据接入后,应连同调用点与本角标一并移除。
 */
export function SampleDataBadge({ className }: { className?: string }) {
  const { t } = useTranslation()
  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <Badge
            variant='outline'
            className={cn(
              'cursor-help gap-1 border-amber-400/60 font-normal text-amber-600 dark:text-amber-400',
              className,
            )}
          >
            <Info className='size-3' />
            {t('Sample data', { defaultValue: '示例数据' })}
          </Badge>
        </TooltipTrigger>
        <TooltipContent className='max-w-60 text-xs'>
          {t('sample-data-disclaimer', {
            defaultValue:
              '演示占位数据,真实监控指标接入后将自动替换,请勿据此做选型决策。',
          })}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
