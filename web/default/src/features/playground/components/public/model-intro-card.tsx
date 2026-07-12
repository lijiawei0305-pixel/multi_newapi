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
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import type { PricingModel } from '@/features/pricing/types'
import { cn } from '@/lib/utils'

import { getModelCapabilities } from '../../lib/capabilities'
import type { PlaygroundCapability } from '../../types'

// ---------------------------------------------------------------------------
// Capability badge label map
// ---------------------------------------------------------------------------
const CAPABILITY_LABELS: Record<PlaygroundCapability, string> = {
  chat: '聊天',
  image: '图片',
  video: '视频',
}

// ---------------------------------------------------------------------------
// Props
// ---------------------------------------------------------------------------
export interface ModelIntroCardProps {
  model: PricingModel | null
  className?: string
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------
export function ModelIntroCard({ model, className }: ModelIntroCardProps) {
  // ── Empty state ──────────────────────────────────────────────────────────
  if (!model) {
    return (
      <Card
        className={cn(
          'flex h-full flex-col items-center justify-center gap-3 border-dashed',
          className
        )}
      >
        <div className="bg-muted flex size-12 items-center justify-center rounded-full">
          <MousePointerClick className="text-muted-foreground size-6" />
        </div>
        <p className="text-muted-foreground text-sm">
          从左侧选择一个模型开始创作
        </p>
      </Card>
    )
  }

  // ── Model icon (prefer model.icon, fallback vendor_icon) ─────────────────
  const iconSrc = model.icon ?? model.vendor_icon
  const capabilities = getModelCapabilities(model)

  return (
    <Card className={cn('h-full', className)}>
      {/* Header: icon + name + description */}
      <CardHeader className="gap-3">
        {iconSrc && (
          <img
            src={iconSrc}
            alt={model.model_name}
            className="size-10 rounded-lg object-contain"
          />
        )}
        <div className="min-w-0 flex-1">
          <CardTitle className="truncate text-base font-semibold">
            {model.model_name}
          </CardTitle>
          {model.description && (
            <CardDescription className="mt-0.5 line-clamp-2 text-sm">
              {model.description}
            </CardDescription>
          )}
        </div>
      </CardHeader>

      <CardContent className="flex flex-col gap-4">
        {/* Capability badges */}
        {capabilities.length > 0 && (
          <div className="flex flex-col gap-1.5">
            <p className="text-muted-foreground text-xs font-medium">支持能力</p>
            <div className="flex flex-wrap gap-1.5">
              {capabilities.map((cap) => (
                <Badge key={cap} variant="secondary">
                  {CAPABILITY_LABELS[cap]}
                </Badge>
              ))}
            </div>
          </div>
        )}

        {/* Ratio */}
        <div className="flex flex-col gap-1.5">
          <p className="text-muted-foreground text-xs font-medium">模型倍率</p>
          <div className="bg-muted rounded-lg px-3 py-2">
            <span className="text-foreground text-sm font-medium">
              {model.model_ratio}×
            </span>
          </div>
        </div>

        {/* Tags (if present) */}
        {model.tags && (
          <div className="flex flex-col gap-1.5">
            <p className="text-muted-foreground text-xs font-medium">标签</p>
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
