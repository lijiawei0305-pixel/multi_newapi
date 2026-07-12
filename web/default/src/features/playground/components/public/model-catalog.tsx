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
import { PackageOpen } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import { getLobeIcon } from '@/lib/lobe-icon'
import { cn } from '@/lib/utils'
import type { PricingModel } from '@/features/pricing/types'
import {
  CAPABILITY_LABELS,
  CATALOG_FILTERS,
  formatModelRate,
  getModelCapabilities,
} from '../../lib/capabilities'
import type { CatalogFilter } from '../../types'

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
  error?: Error | null
  onRetry?: () => void
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
  // icon / vendor_icon 是 lobe-icon 的「key」（如 "Gemini.Color"），不是图片 URL；
  // 必须经 getLobeIcon 解析为真实厂商 logo 组件（与「模型广场」一致）。
  const iconKey = model.icon || model.vendor_icon
  const initial = model.model_name?.charAt(0).toUpperCase() || '?'
  const vendorName = model.vendor_name?.trim()

  return (
    <button
      type='button'
      aria-pressed={selected}
      onClick={() => onSelect(model)}
      className={cn(
        'group w-full rounded-xl border p-2.5 text-left transition-colors',
        'focus-visible:ring-ring focus-visible:ring-2 focus-visible:outline-none',
        selected
          ? 'border-primary/50 bg-primary/5 ring-1 ring-primary/25'
          : 'border-border hover:border-border hover:bg-muted/40'
      )}
    >
      {/* 头部：logo + 名称/厂商 + 计费 */}
      <div className='flex items-center gap-2.5'>
        <div
          className={cn(
            'flex size-9 shrink-0 items-center justify-center overflow-hidden rounded-lg',
            selected ? 'bg-background' : 'bg-muted/60'
          )}
        >
          {iconKey ? (
            getLobeIcon(iconKey, 22)
          ) : (
            <span className='text-sm font-semibold text-muted-foreground'>
              {initial}
            </span>
          )}
        </div>

        <div className='min-w-0 flex-1'>
          <div className='truncate text-sm font-medium text-foreground'>
            {model.model_name}
          </div>
          {vendorName && (
            <div className='truncate text-xs text-muted-foreground'>
              {vendorName}
            </div>
          )}
        </div>

        <Badge
          variant='outline'
          className='shrink-0 self-start text-[0.7rem] tabular-nums'
        >
          {formatModelRate(model)}
        </Badge>
      </div>

      {/* 能力徽章 */}
      {capabilities.length > 0 && (
        <div className='mt-2 flex flex-wrap gap-1'>
          {capabilities.map((cap) => (
            <Badge key={cap} variant='secondary' className='text-[0.7rem]'>
              {CAPABILITY_LABELS[cap] ?? cap}
            </Badge>
          ))}
        </div>
      )}

      {/* 描述（仅在有值时显示；无简介不占位，保持清爽） */}
      {model.description && (
        <p className='mt-1.5 line-clamp-2 text-xs leading-relaxed text-muted-foreground'>
          {model.description}
        </p>
      )}
    </button>
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
  error,
  onRetry,
}: ModelCatalogProps) {
  return (
    <div className='flex h-full flex-col gap-3'>
      {/* Header */}
      <div className='flex shrink-0 items-baseline justify-between px-1'>
        <h2 className='text-sm font-semibold text-foreground'>模型目录</h2>
        <span className='text-xs tabular-nums text-muted-foreground'>
          {counts.all} 个模型
        </span>
      </div>

      {/* Search */}
      <div className='shrink-0 px-1'>
        <Input
          placeholder='搜索模型'
          aria-label='搜索模型'
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
              <span
                className={cn(
                  'ml-1 tabular-nums',
                  filter === value
                    ? 'text-secondary-foreground/60'
                    : 'text-muted-foreground'
                )}
              >
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
                className='flex items-start gap-2.5 rounded-xl bg-card p-3 ring-1 ring-foreground/10'
              >
                <div className='size-8 shrink-0 animate-pulse rounded-md bg-muted' />
                <div className='flex-1 space-y-2 py-0.5'>
                  <div className='h-3.5 w-1/2 animate-pulse rounded bg-muted' />
                  <div className='h-3 w-4/5 animate-pulse rounded bg-muted' />
                </div>
              </div>
            ))}
          </div>
        ) : error ? (
          <div className='flex h-40 flex-col items-center justify-center gap-3 px-4 text-center'>
            <p className='text-sm text-muted-foreground'>
              模型加载失败，请稍后重试
            </p>
            {onRetry && (
              <Button size='sm' variant='outline' onClick={onRetry}>
                重试
              </Button>
            )}
          </div>
        ) : models.length === 0 ? (
          <div className='flex h-40 flex-col items-center justify-center gap-3 px-4 text-center text-muted-foreground'>
            <div className='flex size-12 items-center justify-center rounded-full bg-muted'>
              <PackageOpen className='size-6' />
            </div>
            <p className='text-sm'>没有匹配的模型，试试其他关键词或筛选</p>
          </div>
        ) : (
          <div className='space-y-2 pb-2 pt-1'>
            {models.map((m) => (
              <ModelCard
                key={m.model_name}
                model={m}
                selected={selectedModel?.model_name === m.model_name}
                onSelect={onSelect}
              />
            ))}
          </div>
        )}
      </ScrollArea>
    </div>
  )
}
