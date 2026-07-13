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
import { Loader2Icon, Trash2Icon } from 'lucide-react'

import {
  Conversation,
  ConversationContent,
  ConversationScrollButton,
} from '@/components/ai-elements/conversation'
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
import { toast } from 'sonner'

import { api } from '@/lib/api'

import { PROMPT_INPUT_SHELL_CLASS } from '../../constants'
import { useImageConversation } from '../../hooks/use-image-conversation'
import type { ImageTurn } from '../../hooks/use-image-conversation'
import type { WorkspaceProps } from '../../types'
import { DEFAULT_IMAGE_SIZE, ImageSizeSelector } from './image-size-selector'
import { ImageLightbox } from './image-lightbox'
import { ImageTurnView } from './image-turn'
import { ModelIntroHero } from './model-intro-card'

// 触发浏览器下载：临时 <a download> 点击后即移除
function triggerDownload(href: string, filename: string) {
  const a = document.createElement('a')
  a.href = href
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
}

// 可选张数（上游 gpt-image 系列原生支持 n，一次请求返回对应数量的图）
const N_OPTIONS = [
  { value: '1', label: '1 张' },
  { value: '2', label: '2 张' },
  { value: '3', label: '3 张' },
  { value: '4', label: '4 张' },
]

interface ZoomState {
  src: string
  alt: string
}

export function ImageWorkspace({ apiKey, model, introModel }: WorkspaceProps) {
  const [prompt, setPrompt] = useState('')
  const [n, setN] = useState<string>('1')
  const [size, setSize] = useState<string>(DEFAULT_IMAGE_SIZE)
  // 点击放大预览的当前图片；null 表示灯箱关闭
  const [zoom, setZoom] = useState<ZoomState | null>(null)

  const { turns, isGenerating, generate, clear } = useImageConversation()

  function runGenerate(params: {
    prompt: string
    n: number
    size: string
  }) {
    if (!apiKey) return
    if (!params.prompt) return
    void generate(apiKey, {
      model,
      prompt: params.prompt,
      n: params.n,
      size: params.size,
      response_format: 'url',
    })
  }

  function handleSubmit(submittedPrompt: string) {
    const resolvedPrompt = submittedPrompt.trim() || prompt.trim()
    if (!resolvedPrompt) return
    runGenerate({ prompt: resolvedPrompt, n: Number(n), size })
    // 会话式：发送后清空输入框，历史回合已留住提示词
    setPrompt('')
  }

  // 重试：用该回合原始参数重新生成（追加为新回合，符合对话语义）
  function handleRetry(turn: ImageTurn) {
    runGenerate({ prompt: turn.prompt, n: turn.n, size: turn.size })
  }

  // 下载单张图片：data: 直接下载；其余（可能是需鉴权的代理 URL 或跨域 CDN）
  // 走鉴权 blob 下载，绕过跨域 download 属性被忽略的限制；失败兜底新标签打开。
  async function handleDownload(src: string, filename: string) {
    if (!src) return
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
      // blob 抓取失败：此处已脱离原始点击手势，window.open 常被弹窗拦截而静默失败，
      // 故显式 toast 反馈，再尽力打开新标签（成功则用户可另存）。
      toast.error('下载失败，请右键图片另存或稍后重试')
      window.open(src, '_blank', 'noopener')
    }
  }

  const hasTurns = turns.length > 0

  return (
    <div className='flex h-full flex-col'>
      {/* 结果区域：对话式滚动，历史回合累积，向上滚动仍见每次提示词 */}
      <Conversation>
        <ConversationContent className='p-0'>
          <div className='mx-auto w-full max-w-3xl px-4 py-4'>
            {hasTurns ? (
              turns.map((turn) => (
                <ImageTurnView
                  key={turn.id}
                  turn={turn}
                  onZoom={(src, alt) => setZoom({ src, alt })}
                  onDownload={handleDownload}
                  onRetry={() => handleRetry(turn)}
                  retryDisabled={isGenerating || !apiKey}
                />
              ))
            ) : (
              <div className='flex min-h-[52vh] flex-col items-center justify-center gap-4 text-muted-foreground'>
                <ModelIntroHero model={introModel ?? null} />
                <p className='text-xs'>输入提示词，点击「生成」开始创作</p>
              </div>
            )}
          </div>
        </ConversationContent>
        <ConversationScrollButton />
      </Conversation>

      {/* 输入区域 */}
      <div className='mx-auto w-full max-w-3xl shrink-0 px-4 pb-4'>
        <PromptInput
          groupClassName={PROMPT_INPUT_SHELL_CLASS}
          onSubmit={({ text }) => {
            handleSubmit(text ?? '')
          }}
        >
          <PromptInputTextarea
            disabled={isGenerating}
            placeholder='输入提示词…'
            value={prompt}
            onChange={(e) => setPrompt(e.currentTarget.value)}
          />
          <PromptInputFooter>
            <PromptInputTools>
              {/* 数量选择 */}
              <Select value={n} onValueChange={(val) => val && setN(val)}>
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

              {/* 尺寸选择：按真实宽高比绘制的 SVG 形状，直观区分方形/横版/竖版 */}
              <ImageSizeSelector
                value={size}
                onChange={setSize}
                disabled={isGenerating}
              />

              {/* 清空当前会话（有历史回合时才出现） */}
              {hasTurns && (
                <Button
                  type='button'
                  size='sm'
                  variant='ghost'
                  className='h-7 px-2 text-xs text-muted-foreground'
                  onClick={clear}
                  disabled={isGenerating}
                >
                  <Trash2Icon className='mr-1 size-3.5' />
                  清空
                </Button>
              )}
            </PromptInputTools>

            <PromptInputSubmit disabled={isGenerating || !apiKey || !prompt.trim()}>
              {isGenerating ? (
                <Loader2Icon className='size-4 animate-spin' />
              ) : (
                <span className='px-1 text-xs font-medium'>生成</span>
              )}
            </PromptInputSubmit>
          </PromptInputFooter>
        </PromptInput>
      </div>

      {/* 点击放大预览 */}
      <ImageLightbox
        src={zoom?.src ?? null}
        alt={zoom?.alt}
        open={zoom !== null}
        onOpenChange={(open) => {
          if (!open) setZoom(null)
        }}
        onDownload={
          zoom ? () => handleDownload(zoom.src, '生成图片.png') : undefined
        }
      />
    </div>
  )
}
