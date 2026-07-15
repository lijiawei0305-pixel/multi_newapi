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
import { cn } from '@/lib/utils'
import { useTranslation } from 'react-i18next'

// gpt-image 系列支持的三种尺寸。ratio/orientation 用于形状与标签展示，
// w/h 用于按真实宽高比绘制缩略矩形（避免各处硬编码，改上游支持时只动这里）。
export interface ImageSizeOption {
  value: string
  ratio: string
  orientation: string
  w: number
  h: number
}

export const IMAGE_SIZE_OPTIONS: ImageSizeOption[] = [
  { value: '1024x1024', ratio: '1:1', orientation: 'Square', w: 1024, h: 1024 },
  { value: '1536x1024', ratio: '3:2', orientation: 'Landscape', w: 1536, h: 1024 },
  { value: '1024x1536', ratio: '2:3', orientation: 'Portrait', w: 1024, h: 1536 },
]

export const DEFAULT_IMAGE_SIZE = '1024x1024'

// 按真实宽高比绘制的缩略矩形：长边固定占满，短边等比缩放，直观表达尺寸形状。
// 用 currentColor + fillOpacity，随选中态（primary）/未选中态（muted）自然变色。
function AspectRatioGlyph({
  width,
  height,
  className,
}: {
  width: number
  height: number
  className?: string
}) {
  const BOX = 24
  const MAX = 18
  const longer = Math.max(width, height)
  const w = (width / longer) * MAX
  const h = (height / longer) * MAX
  const x = (BOX - w) / 2
  const y = (BOX - h) / 2
  return (
    <svg
      viewBox={`0 0 ${BOX} ${BOX}`}
      className={className}
      aria-hidden='true'
      focusable='false'
    >
      <rect
        x={x}
        y={y}
        width={w}
        height={h}
        rx={2.4}
        fill='currentColor'
        fillOpacity={0.15}
        stroke='currentColor'
        strokeWidth={1.6}
      />
    </svg>
  )
}

export interface ImageSizeSelectorProps {
  value: string
  onChange: (value: string) => void
  disabled?: boolean
  className?: string
}

// 尺寸选择：以按比例缩放的 SVG 形状替代纯文字下拉，一眼看清方形/横版/竖版。
// 单选语义用 radiogroup/radio + aria-checked，键盘与读屏可用。
export function ImageSizeSelector({
  value,
  onChange,
  disabled,
  className,
}: ImageSizeSelectorProps) {
  const { t } = useTranslation()
  return (
    <div
      role='radiogroup'
      aria-label={t('Image size')}
      className={cn(
        'flex items-center gap-0.5 rounded-lg border border-border/70 bg-background/60 p-0.5',
        className
      )}
    >
      {IMAGE_SIZE_OPTIONS.map((opt) => {
        const active = value === opt.value
        return (
          <button
            key={opt.value}
            type='button'
            role='radio'
            aria-checked={active}
            aria-label={`${t(opt.orientation)} ${opt.ratio}`}
            title={`${opt.w}×${opt.h} · ${t(opt.orientation)}`}
            disabled={disabled}
            onClick={() => onChange(opt.value)}
            className={cn(
              'flex h-6 items-center gap-1.5 rounded-md px-2 text-xs transition-colors',
              'focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none',
              'disabled:pointer-events-none disabled:opacity-50',
              active
                ? 'bg-primary/10 text-primary'
                : 'text-muted-foreground hover:bg-muted hover:text-foreground'
            )}
          >
            <AspectRatioGlyph width={opt.w} height={opt.h} className='size-4' />
            <span className='font-medium tabular-nums'>{opt.ratio}</span>
          </button>
        )
      })}
    </div>
  )
}
