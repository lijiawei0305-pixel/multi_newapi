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

import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'
import type { PricingModel } from '@/features/pricing/types'
import { cn } from '@/lib/utils'

import {
  CAPABILITY_LABELS,
  formatModelRate,
  getModelCapabilities,
} from '../../lib/capabilities'

// ---------------------------------------------------------------------------
// Props
// ---------------------------------------------------------------------------
export interface ModelIntroCardProps {
  model: PricingModel | null
  className?: string
}

// 小节字段标签（overline 风格，安静地区分「字段名」与「内容」）
const SECTION_LABEL_CLASS =
  'text-[0.7rem] font-medium tracking-wide text-muted-foreground'

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------
export function ModelIntroCard({ model, className }: ModelIntroCardProps) {
  // ── 空态：主副两行引导 + 与实态一致的图标托底 ──────────────────────────────
  if (!model) {
    return (
      <Card
        className={cn(
          'flex h-full flex-col items-center justify-center gap-3 border-dashed border-border text-center',
          className
        )}
      >
        <div className="flex size-12 items-center justify-center rounded-2xl bg-muted ring-1 ring-foreground/10">
          <MousePointerClick className="size-6 text-muted-foreground" />
        </div>
        <div className="flex flex-col gap-0.5 px-4">
          <p className="text-sm font-medium text-foreground">选择一个模型</p>
          <p className="text-xs text-muted-foreground">
            从左侧目录挑选，查看能力、计费与介绍
          </p>
        </div>
      </Card>
    )
  }

  const iconSrc = model.icon ?? model.vendor_icon
  const capabilities = getModelCapabilities(model)

  return (
    <Card className={cn('h-full overflow-y-auto', className)}>
      <CardContent className="flex flex-col gap-4 pt-5">
        {/* 英雄区：居中图标托底 + 名称突出 + 能力徽章 */}
        <div className="flex flex-col items-center gap-3 text-center">
          <div className="flex size-14 items-center justify-center overflow-hidden rounded-2xl bg-muted ring-1 ring-foreground/10">
            {iconSrc ? (
              <img
                src={iconSrc}
                alt={model.model_name}
                className="size-9 object-contain"
                onError={(e) => {
                  ;(e.currentTarget as HTMLImageElement).style.display = 'none'
                }}
              />
            ) : (
              <span className="text-lg font-semibold text-muted-foreground">
                {model.model_name.slice(0, 1).toUpperCase()}
              </span>
            )}
          </div>
          <h2 className="text-lg font-semibold tracking-tight text-foreground">
            {model.model_name}
          </h2>
          {capabilities.length > 0 && (
            <div className="flex flex-wrap justify-center gap-1.5">
              {capabilities.map((cap) => (
                <Badge key={cap} variant="secondary">
                  {CAPABILITY_LABELS[cap]}
                </Badge>
              ))}
            </div>
          )}
        </div>

        {/* 介绍正文（整段，独立成块，保留换行） */}
        {model.description && (
          <>
            <div className="border-t border-border/60" />
            <p className="whitespace-pre-line text-sm leading-relaxed text-muted-foreground">
              {model.description}
            </p>
          </>
        )}

        {/* 计费方式 */}
        <div className="flex flex-col gap-1.5">
          <p className={SECTION_LABEL_CLASS}>计费方式</p>
          <div className="rounded-lg bg-muted px-3 py-2">
            <span className="text-sm font-semibold tabular-nums text-foreground">
              {formatModelRate(model)}
            </span>
          </div>
        </div>

        {/* 标签 */}
        {model.tags && (
          <div className="flex flex-col gap-1.5">
            <p className={SECTION_LABEL_CLASS}>标签</p>
            <div className="flex flex-wrap gap-1.5">
              {model.tags
                .split(',')
                .map((t) => t.trim())
                .filter(Boolean)
                .map((tag) => (
                  <Badge key={tag} variant="outline">
                    {tag}
                  </Badge>
                ))}
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
