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
import { useState } from 'react'
import {
  ChevronDownIcon,
  DownloadIcon,
  ImageIcon,
  Loader2Icon,
  RotateCcw,
  SlidersHorizontalIcon,
  ZoomInIcon,
} from 'lucide-react'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

import type { ImageResultItem } from '../../types'
import type { ImageTurn } from '../../hooks/use-image-conversation'
import { IMAGE_SIZE_OPTIONS } from './image-size-selector'

// 把 1024x1024 还原成「1024×1024（1:1 方形）」这类可读描述。
function describeSize(size: string): string {
  const opt = IMAGE_SIZE_OPTIONS.find((o) => o.value === size)
  const dims = size.replace('x', '×')
  return opt ? `${dims}（${opt.ratio} ${opt.orientation}）` : dims
}

// 单张图片可解析出的展示 src：data:/b64 优先，其次 url。
function resolveSrc(item: ImageResultItem): string {
  if (item.b64_json) return `data:image/png;base64,${item.b64_json}`
  return item.url ?? ''
}

export interface ImageTurnViewProps {
  turn: ImageTurn
  onZoom: (src: string, alt: string) => void
  onDownload: (src: string, filename: string) => void
  onRetry: () => void
  retryDisabled?: boolean
}

// 一个图片生成回合：右侧提示词气泡（含请求参数折叠）+ 下方结果（加载/错误/图片网格）。
export function ImageTurnView({
  turn,
  onZoom,
  onDownload,
  onRetry,
  retryDisabled,
}: ImageTurnViewProps) {
  const [showParams, setShowParams] = useState(false)
  // 每回合各自记录加载失败的图片下标，互不影响。
  const [failedIndices, setFailedIndices] = useState<Set<number>>(new Set())

  const isGrid = turn.images.length > 1

  const requestBody = {
    model: turn.model,
    prompt: turn.prompt,
    n: turn.n,
    size: turn.size,
    response_format: turn.responseFormat,
  }

  return (
    <div className='flex flex-col gap-3 py-3'>
      {/* 用户提示词（右对齐气泡） */}
      <div className='flex justify-end'>
        <div className='max-w-[85%] rounded-2xl rounded-br-md bg-primary/10 px-3.5 py-2 text-sm text-foreground'>
          <p className='break-words whitespace-pre-wrap'>{turn.prompt}</p>

          {/* 请求参数：折叠展开，回看每次生成用了什么参数 */}
          <button
            type='button'
            aria-expanded={showParams}
            onClick={() => setShowParams((v) => !v)}
            className='mt-1.5 flex items-center gap-1 text-xs text-muted-foreground transition-colors hover:text-foreground'
          >
            <SlidersHorizontalIcon className='size-3' />
            <span>请求参数</span>
            <ChevronDownIcon
              className={cn(
                'size-3 transition-transform',
                showParams && 'rotate-180'
              )}
            />
          </button>

          {showParams && (
            <div className='mt-1.5 space-y-1.5 border-t border-primary/15 pt-1.5 text-xs text-muted-foreground'>
              <dl className='grid grid-cols-[auto_1fr] gap-x-2 gap-y-0.5'>
                <dt className='text-muted-foreground/70'>接口</dt>
                <dd className='font-mono break-all'>
                  POST /v1/images/generations
                </dd>
                <dt className='text-muted-foreground/70'>模型</dt>
                <dd className='break-all'>{turn.model}</dd>
                <dt className='text-muted-foreground/70'>尺寸</dt>
                <dd>{describeSize(turn.size)}</dd>
                <dt className='text-muted-foreground/70'>数量</dt>
                <dd>{turn.n} 张</dd>
              </dl>
              <pre className='overflow-x-auto rounded-md bg-background/70 p-2 font-mono text-[11px] leading-relaxed text-foreground/80'>
                {JSON.stringify(requestBody, null, 2)}
              </pre>
            </div>
          )}
        </div>
      </div>

      {/* 结果区 */}
      {turn.status === 'loading' && (
        <div className='flex items-center gap-2 text-sm text-muted-foreground'>
          <Loader2Icon className='size-4 animate-spin' />
          <span>生成中…</span>
        </div>
      )}

      {turn.status === 'error' && (
        <div className='flex flex-col items-start gap-2'>
          <p className='text-sm text-destructive'>
            {turn.error ?? '图片生成失败，请稍后重试'}
          </p>
          <Button
            size='sm'
            variant='outline'
            onClick={onRetry}
            disabled={retryDisabled}
          >
            <RotateCcw className='mr-1.5 size-3.5' />
            重试
          </Button>
        </div>
      )}

      {turn.status === 'success' && turn.images.length > 0 && (
        <div
          className={cn(
            'max-w-md',
            isGrid ? 'grid grid-cols-2 gap-2' : 'flex'
          )}
        >
          {turn.images.map((item, index) => {
            const src = resolveSrc(item)
            const alt = item.revised_prompt || turn.prompt || `生成图片 ${index + 1}`
            const filename = `生成图片-${index + 1}.png`

            if (failedIndices.has(index)) {
              return (
                <div
                  key={index}
                  className='flex aspect-square flex-col items-center justify-center gap-2 rounded-lg border border-border bg-muted p-4 text-center text-muted-foreground'
                >
                  <ImageIcon className='size-8 opacity-40' />
                  <p className='text-xs'>图片加载失败</p>
                  {src && (
                    <Button
                      size='sm'
                      variant='outline'
                      onClick={() => window.open(src, '_blank', 'noopener')}
                    >
                      在新标签打开
                    </Button>
                  )}
                </div>
              )
            }

            return (
              <div
                key={index}
                className='group relative overflow-hidden rounded-lg border border-border bg-muted'
              >
                {/* 点击图片放大预览 */}
                <button
                  type='button'
                  aria-label='放大查看'
                  className='block w-full cursor-zoom-in'
                  onClick={() => onZoom(src, alt)}
                >
                  <img
                    alt={alt}
                    className='h-auto max-h-full w-full object-contain'
                    src={src}
                    onError={() =>
                      setFailedIndices((prev) => new Set(prev).add(index))
                    }
                  />
                  {/* 悬停放大提示 */}
                  <span className='pointer-events-none absolute inset-0 flex items-center justify-center bg-black/0 opacity-0 transition-opacity group-hover:bg-black/20 group-hover:opacity-100'>
                    <ZoomInIcon className='size-7 text-white drop-shadow' />
                  </span>
                </button>

                {/* 下载按钮，悬停显示 */}
                <button
                  type='button'
                  aria-label='下载图片'
                  className={cn(
                    'absolute right-2 top-2 flex size-8 items-center justify-center rounded-md',
                    'bg-card/80 text-foreground opacity-0 backdrop-blur-sm transition-opacity',
                    'hover:bg-card group-hover:opacity-100',
                    'focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none'
                  )}
                  onClick={() => onDownload(src, filename)}
                  title='下载图片'
                >
                  <DownloadIcon className='size-4' />
                  <span className='sr-only'>下载图片</span>
                </button>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
