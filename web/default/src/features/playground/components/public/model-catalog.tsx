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
import { Sparkles } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import { cn } from '@/lib/utils'
import type { PricingModel } from '@/features/pricing/types'
import {
  CATALOG_FILTERS,
  getModelCapabilities,
} from '../../lib/capabilities'
import type { CatalogFilter } from '../../types'

// ---------------------------------------------------------------------------
// Capability label map (中文)
// ---------------------------------------------------------------------------
const CAPABILITY_LABELS: Record<string, string> = {
  chat: '聊天',
  image: '图片',
  video: '视频',
}

// ---------------------------------------------------------------------------
// Props
// ---------------------------------------------------------------------------
export interface ModelCatalogProps {
  filter: CatalogFilter
  onFilter: (f: CatalogFilter) => void
  search: string
  onSearch: (s: string) => void
  models: PricingModel[]
  selectedModel: PricingModel | null
  onSelect: (m: PricingModel) => void
  loading: boolean
  counts: Record<CatalogFilter, number>
}

// ---------------------------------------------------------------------------
// Internal ModelCard
// ---------------------------------------------------------------------------
interface ModelCardProps {
  model: PricingModel
  selected: boolean
  onSelect: (m: PricingModel) => void
}

function ModelCard({ model, selected, onSelect }: ModelCardProps) {
  const capabilities = getModelCapabilities(model)
  const iconSrc = model.icon ?? model.vendor_icon

  return (
    <Card
      size='sm'
      onClick={() => onSelect(model)}
      className={cn(
        'cursor-pointer transition-shadow',
        'hover:ring-2 hover:ring-foreground/15',
        selected && 'ring-2 ring-foreground/30 bg-muted/40'
      )}
    >
      <CardHeader>
        <div className='flex items-start gap-2.5'>
          {/* Model icon */}
          <div className='mt-0.5 flex size-8 shrink-0 items-center justify-center overflow-hidden rounded-md bg-muted'>
            {iconSrc ? (
              <img
                src={iconSrc}
                alt={model.model_name}
                className='size-full object-contain'
                onError={(e) => {
                  // fallback to placeholder on broken image
                  ;(e.currentTarget as HTMLImageElement).style.display = 'none'
                  const next = e.currentTarget.nextElementSibling as HTMLElement | null
                  if (next) next.style.display = 'flex'
                }}
              />
            ) : null}
            <span
              className={cn(
                'flex size-full items-center justify-center text-muted-foreground',
                iconSrc ? 'hidden' : 'flex'
              )}
            >
              <Sparkles className='size-4' />
            </span>
          </div>

          {/* Name + badges */}
          <div className='min-w-0 flex-1'>
            <CardTitle className='truncate text-sm leading-5'>
              {model.model_name}
            </CardTitle>

            {/* Capability badges */}
            {capabilities.length > 0 && (
              <div className='mt-1 flex flex-wrap gap-1'>
                {capabilities.map((cap) => (
                  <Badge key={cap} variant='secondary' className='text-xs'>
                    {CAPABILITY_LABELS[cap] ?? cap}
                  </Badge>
                ))}
              </div>
            )}
          </div>

          {/* Ratio badge */}
          <Badge variant='outline' className='ml-auto shrink-0 self-start text-xs'>
            倍率&nbsp;{model.model_ratio}
          </Badge>
        </div>
      </CardHeader>

      {model.description && (
        <CardContent>
          <CardDescription className='line-clamp-2 text-xs'>
            {model.description}
          </CardDescription>
        </CardContent>
      )}
    </Card>
  )
}

// ---------------------------------------------------------------------------
// ModelCatalog (exported)
// ---------------------------------------------------------------------------
export function ModelCatalog({
  filter,
  onFilter,
  search,
  onSearch,
  models,
  selectedModel,
  onSelect,
  loading,
  counts,
}: ModelCatalogProps) {
  return (
    <div className='flex h-full flex-col gap-3'>
      {/* Header */}
      <div className='shrink-0 px-1'>
        <p className='text-base font-medium text-foreground'>AI 大模型聚合平台</p>
      </div>

      {/* Search */}
      <div className='shrink-0 px-1'>
        <Input
          placeholder='搜索模型'
          value={search}
          onChange={(e) => onSearch(e.target.value)}
          className='h-8 rounded-lg'
        />
      </div>

      {/* Capability filter chips */}
      <div className='flex shrink-0 flex-wrap gap-1.5 px-1'>
        {CATALOG_FILTERS.map(({ value, label }) => (
          <Button
            key={value}
            size='sm'
            variant={filter === value ? 'secondary' : 'outline'}
            onClick={() => onFilter(value)}
            className='h-7 rounded-full px-3 text-xs'
          >
            {label}
            {counts[value] !== undefined && (
              <span className='ml-1 text-muted-foreground'>
                {counts[value]}
              </span>
            )}
          </Button>
        ))}
      </div>

      {/* Model list */}
      <ScrollArea className='min-h-0 flex-1 px-1'>
        {loading ? (
          <div className='space-y-2 pt-1'>
            {Array.from({ length: 5 }).map((_, i) => (
              <div
                key={i}
                className='h-16 animate-pulse rounded-xl bg-muted'
              />
            ))}
          </div>
        ) : models.length === 0 ? (
          <div className='flex h-32 items-center justify-center'>
            <p className='text-sm text-muted-foreground'>暂无模型</p>
          </div>
        ) : (
          <div className='space-y-2 pb-2 pt-1'>
            {models.map((m) => (
              <ModelCard
                key={m.id}
                model={m}
                selected={selectedModel?.id === m.id}
                onSelect={onSelect}
              />
            ))}
          </div>
        )}
      </ScrollArea>
    </div>
  )
}
