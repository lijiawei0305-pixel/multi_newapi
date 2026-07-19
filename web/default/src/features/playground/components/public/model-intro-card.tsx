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
import { MousePointerClick } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import type { PricingModel } from '@/features/pricing/types'
import { getLobeIcon } from '@/lib/lobe-icon'
import { cn } from '@/lib/utils'

import {
  CAPABILITY_LABELS,
  formatModelRate,
  getModelCapabilities,
} from '../../lib/capabilities'

// ---------------------------------------------------------------------------
// Props
// ---------------------------------------------------------------------------
export interface ModelIntroHeroProps {
  model: PricingModel | null
  className?: string
}

// ---------------------------------------------------------------------------
// ModelIntroHero —— 工作区空态的「模型介绍」主角
//
// 无边框、自然高度、水平居中，供各工作区（聊天/图片/视频）与外壳在「尚未生成」
// 时作为中央英雄区渲染；宿主容器负责边框与垂直居中。
// 厂商 logo 复用全站统一的 getLobeIcon（与「模型广场」一致），彻底修复此前把
// lobe-icon key 当图片 URL 直接塞进 <img> 导致永远回退占位图标的问题。
// ---------------------------------------------------------------------------
export function ModelIntroHero({ model, className }: ModelIntroHeroProps) {
  const { t } = useTranslation()
  // ── 空态：未选择模型 ────────────────────────────────────────────────────────
  if (!model) {
    return (
      <div
        className={cn(
          'flex flex-col items-center gap-3 px-6 text-center',
          className
        )}
      >
        <div className='bg-muted ring-foreground/10 flex size-14 items-center justify-center rounded-2xl ring-1'>
          <MousePointerClick className='text-muted-foreground size-6' />
        </div>
        <div className='flex flex-col gap-1'>
          <p className='text-foreground text-sm font-medium'>
            {t('Select a model')}
          </p>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Pick from the catalog on the left to view capabilities, pricing and details, then start creating'
            )}
          </p>
        </div>
      </div>
    )
  }

  const iconKey = model.icon || model.vendor_icon
  const capabilities = getModelCapabilities(model)
  const initial = model.model_name?.charAt(0).toUpperCase() || '?'
  const tags = model.tags
    ? model.tags
        .split(',')
        .map((t) => t.trim())
        .filter(Boolean)
    : []

  return (
    <div
      className={cn(
        'flex w-full max-w-xl flex-col items-center gap-4 text-center',
        className
      )}
    >
      {/* 厂商 logo */}
      <div className='bg-muted ring-foreground/10 flex size-16 shrink-0 items-center justify-center overflow-hidden rounded-2xl ring-1'>
        {iconKey ? (
          getLobeIcon(iconKey, 40)
        ) : (
          <span className='text-muted-foreground text-2xl font-semibold'>
            {initial}
          </span>
        )}
      </div>

      {/* 名称 + 能力 / 计费徽章 */}
      <div className='flex flex-col items-center gap-2'>
        <h2 className='text-foreground text-xl font-semibold tracking-tight'>
          {model.model_name}
        </h2>
        <div className='flex flex-wrap items-center justify-center gap-1.5'>
          {capabilities.map((cap) => (
            <Badge key={cap} variant='secondary'>
              {t(CAPABILITY_LABELS[cap])}
            </Badge>
          ))}
          <Badge variant='outline' className='tabular-nums'>
            {formatModelRate(model)}
          </Badge>
        </div>
      </div>

      {/* 介绍正文（独立成块，保留换行） */}
      {model.description && (
        <div className='bg-muted/40 w-full rounded-xl px-5 py-4'>
          <p className='text-muted-foreground text-left text-sm leading-relaxed whitespace-pre-line'>
            {model.description}
          </p>
        </div>
      )}

      {/* 标签 */}
      {tags.length > 0 && (
        <div className='flex flex-wrap justify-center gap-1.5'>
          {tags.map((tag) => (
            <Badge key={tag} variant='outline'>
              {tag}
            </Badge>
          ))}
        </div>
      )}
    </div>
  )
}
