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
import { DownloadIcon, ImageIcon, Loader2Icon, RotateCcw } from 'lucide-react'

import {
  PromptInput,
  PromptInputFooter,
  PromptInputSubmit,
  PromptInputTextarea,
  PromptInputTools,
} from '@/components/ai-elements/prompt-input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Button } from '@/components/ui/button'
import { api } from '@/lib/api'
import { cn } from '@/lib/utils'

import { PROMPT_INPUT_SHELL_CLASS } from '../../constants'
import { useImageGeneration } from '../../hooks/use-image-generation'
import type { WorkspaceProps } from '../../types'

// 触发浏览器下载：临时 <a download> 点击后即移除
function triggerDownload(href: string, filename: string) {
  const a = document.createElement('a')
  a.href = href
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
}

// 可选张数
const N_OPTIONS = [
  { value: '1', label: '1 张' },
  { value: '2', label: '2 张' },
  { value: '3', label: '3 张' },
  { value: '4', label: '4 张' },
]

// 可选尺寸
const SIZE_OPTIONS = [
  { value: '1024x1024', label: '1024×1024（方形）' },
  { value: '1024x1792', label: '1024×1792（竖版）' },
  { value: '1792x1024', label: '1792×1024（横版）' },
]

export function ImageWorkspace({ apiKey, model }: WorkspaceProps) {
  const [prompt, setPrompt] = useState('')
  const [n, setN] = useState<string>('1')
  const [size, setSize] = useState<string>('1024x1024')

  const { generate, status, images, error, reset } = useImageGeneration()

  const isLoading = status === 'loading'

  function handleSubmit(submittedPrompt: string) {
    const resolvedPrompt = submittedPrompt.trim() || prompt.trim()
    if (!apiKey) return
    if (!resolvedPrompt) return

    reset()
    void generate(apiKey, {
      model,
      prompt: resolvedPrompt,
      n: Number(n),
      size,
      response_format: 'url',
    })
  }

  // 下载单张图片：data: 直接下载；其余（可能是需鉴权的代理 URL 或跨域 CDN）
  // 走鉴权 blob 下载，绕过跨域 download 属性被忽略的限制；失败兜底新标签打开。
  async function handleDownload(src: string, filename: string) {
    if (src.startsWith('data:')) {
      triggerDownload(src, filename)
      return
    }
    // 跨域外链 CDN：download 属性对跨域被忽略、带 Bearer 抓 blob 会 CORS 预检失败，
    // 直接新标签打开另存（图片 url 是免鉴权直链，无需鉴权取流）。
    let sameOrigin = false
    try {
      sameOrigin =
        new URL(src, window.location.origin).origin === window.location.origin
    } catch {
      sameOrigin = false
    }
    if (!sameOrigin) {
      window.open(src, '_blank', 'noopener')
      return
    }
    try {
      const resp = await api.get(src, {
        responseType: 'blob',
        headers: apiKey ? { Authorization: `Bearer ${apiKey}` } : {},
        skipErrorHandler: true,
      })
      const objectUrl = URL.createObjectURL(resp.data as Blob)
      triggerDownload(objectUrl, filename)
      // 延迟吊销：部分浏览器 a.click() 后异步启动下载，立即 revoke 可能截断
      window.setTimeout(() => URL.revokeObjectURL(objectUrl), 1000)
    } catch {
      window.open(src, '_blank', 'noopener')
    }
  }

  const numImages = Number(n)
  const isGrid = numImages > 1

  return (
    <div className='flex h-full flex-col gap-4'>
      {/* 结果区域 */}
      <div className='flex min-h-0 flex-1 flex-col'>
        {status === 'idle' && (
          <div className='flex flex-1 flex-col items-center justify-center gap-3 text-muted-foreground'>
            <ImageIcon className='size-12 opacity-30' />
            <p className='text-sm'>输入提示词，点击「生成」开始创作</p>
          </div>
        )}

        {isLoading && (
          <div className='flex flex-1 flex-col items-center justify-center gap-3 text-muted-foreground'>
            <Loader2Icon className='size-8 animate-spin' />
            <p className='text-sm'>生成中…</p>
          </div>
        )}

        {status === 'error' && error && (
          <div className='flex flex-1 flex-col items-center justify-center gap-3'>
            <p className='text-sm text-destructive'>{error}</p>
            <Button
              size='sm'
              variant='outline'
              onClick={() => handleSubmit(prompt)}
              disabled={!apiKey || !prompt.trim()}
            >
              <RotateCcw className='mr-1.5 size-3.5' />
              重试
            </Button>
          </div>
        )}

        {status === 'success' && images.length > 0 && (
          <div
            className={cn(
              'overflow-auto p-1',
              isGrid
                ? 'grid grid-cols-1 gap-3 sm:grid-cols-2'
                : 'flex items-center justify-center'
            )}
          >
            {images.map((item, index) => {
              const src = item.b64_json
                ? `data:image/png;base64,${item.b64_json}`
                : (item.url ?? '')

              const filename = `生成图片-${index + 1}.png`

              return (
                <div
                  key={index}
                  className='group relative overflow-hidden rounded-lg border border-border bg-muted'
                >
                  <img
                    alt={item.revised_prompt || prompt || `生成图片 ${index + 1}`}
                    className='h-auto max-h-full w-full object-contain'
                    src={src}
                  />
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
                    onClick={() => handleDownload(src, filename)}
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

      {/* 输入区域 */}
      <div className='shrink-0'>
        <PromptInput
          groupClassName={PROMPT_INPUT_SHELL_CLASS}
          onSubmit={({ text }) => {
            handleSubmit(text ?? '')
          }}
        >
          <PromptInputTextarea
            disabled={isLoading}
            placeholder='输入提示词…'
            value={prompt}
            onChange={(e) => setPrompt(e.currentTarget.value)}
          />
          <PromptInputFooter>
            <PromptInputTools>
              {/* 数量选择 */}
              <Select
                value={n}
                onValueChange={(val) => setN(val)}
              >
                <SelectTrigger size='sm' className='h-7 min-w-[72px] text-xs'>
                  <SelectValue placeholder='张数' />
                </SelectTrigger>
                <SelectContent>
                  {N_OPTIONS.map((opt) => (
                    <SelectItem key={opt.value} value={opt.value}>
                      {opt.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>

              {/* 尺寸选择 */}
              <Select
                value={size}
                onValueChange={(val) => setSize(val)}
              >
                <SelectTrigger size='sm' className='h-7 min-w-[120px] text-xs'>
                  <SelectValue placeholder='尺寸' />
                </SelectTrigger>
                <SelectContent>
                  {SIZE_OPTIONS.map((opt) => (
                    <SelectItem key={opt.value} value={opt.value}>
                      {opt.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </PromptInputTools>

            <PromptInputSubmit
              disabled={isLoading || !apiKey || !prompt.trim()}
            >
              {isLoading ? (
                <Loader2Icon className='size-4 animate-spin' />
              ) : (
                <span className='px-1 text-xs font-medium'>生成</span>
              )}
            </PromptInputSubmit>
          </PromptInputFooter>
        </PromptInput>
      </div>
    </div>
  )
}
