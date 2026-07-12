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
import { Clapperboard, Loader2, RotateCcw, VideoOff } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  PromptInput,
  PromptInputFooter,
  PromptInputSubmit,
  PromptInputTextarea,
} from '@/components/ai-elements/prompt-input'
import { cn } from '@/lib/utils'

import { useVideoGeneration } from '../../hooks/use-video-generation'
import type { WorkspaceProps } from '../../types'

export function VideoWorkspace({ apiKey, model }: WorkspaceProps) {
  const [prompt, setPrompt] = useState('')
  const [duration, setDuration] = useState('')
  const [size, setSize] = useState('')
  const [seed, setSeed] = useState('')

  const { submit, status, progress, videoUrl, error, reset } =
    useVideoGeneration()

  const isSubmitting = status === 'submitting'
  const isPolling = status === 'polling'
  const isGenerating = isSubmitting || isPolling
  const isSuccess = status === 'success'
  const isError = status === 'error'

  const handleSubmit = () => {
    if (!apiKey) return
    if (!prompt.trim()) return

    const params = {
      model,
      prompt: prompt.trim(),
      ...(duration !== '' && !isNaN(Number(duration)) && Number(duration) > 0
        ? { duration: Number(duration) }
        : {}),
      ...(seed !== '' && !isNaN(Number(seed))
        ? { seed: Number(seed) }
        : {}),
      ...(size.trim() !== '' ? { response_format: size.trim() } : {}),
    }

    void submit(apiKey, params)
  }

  const handleReset = () => {
    reset()
    setPrompt('')
    setDuration('')
    setSize('')
    setSeed('')
  }

  return (
    <div className='flex h-full flex-col gap-4 p-4'>
      {/* 可选参数区域 */}
      <div className='grid grid-cols-3 gap-3'>
        <div className='flex flex-col gap-1.5'>
          <Label htmlFor='video-duration'>时长（秒）</Label>
          <Input
            id='video-duration'
            type='number'
            min={1}
            placeholder='例如：5'
            value={duration}
            onChange={(e) => setDuration(e.target.value)}
            disabled={isGenerating}
          />
        </div>
        <div className='flex flex-col gap-1.5'>
          <Label htmlFor='video-size'>格式</Label>
          <Input
            id='video-size'
            type='text'
            placeholder='例如：720p'
            value={size}
            onChange={(e) => setSize(e.target.value)}
            disabled={isGenerating}
          />
        </div>
        <div className='flex flex-col gap-1.5'>
          <Label htmlFor='video-seed'>随机种子</Label>
          <Input
            id='video-seed'
            type='number'
            placeholder='可选'
            value={seed}
            onChange={(e) => setSeed(e.target.value)}
            disabled={isGenerating}
          />
        </div>
      </div>

      {/* 提示词输入区域 */}
      <PromptInput
        groupClassName='bg-background/95 dark:bg-background/80 border-border/70 shadow-lg ring-1 ring-foreground/5 rounded-xl overflow-hidden transition-all duration-200 focus-within:border-primary/45 focus-within:ring-primary/15'
        onSubmit={() => handleSubmit()}
      >
        <PromptInputTextarea
          autoComplete='off'
          autoCorrect='off'
          autoCapitalize='off'
          spellCheck={false}
          className='min-h-20 px-5 pt-4 pb-3 leading-7 md:min-h-24 md:text-base'
          disabled={isGenerating}
          onChange={(e) => setPrompt(e.target.value)}
          placeholder='输入提示词…'
          value={prompt}
        />
        <PromptInputFooter className='border-border/60 bg-muted/20 dark:bg-muted/10 border-t px-3 py-2.5 backdrop-blur'>
          <div className='flex flex-1 items-center gap-2'>
            <Clapperboard className='text-muted-foreground size-4' />
            <span className='text-muted-foreground text-sm'>视频生成</span>
          </div>
          {/* 仅由 PromptInput 的 form onSubmit 驱动提交，勿再挂 onClick，
              否则单击会双发 create 请求（双建任务 / 潜在双计费）。 */}
          <PromptInputSubmit
            disabled={isGenerating || !prompt.trim() || !apiKey}
          >
            生成
          </PromptInputSubmit>
        </PromptInputFooter>
      </PromptInput>

      {/* 状态展示区域 */}
      {isGenerating && (
        <div className='bg-muted/50 flex items-center gap-3 rounded-lg border border-border p-4'>
          <Loader2 className='text-primary size-5 shrink-0 animate-spin' />
          <span className='text-foreground text-sm'>
            生成中…{progress ? `（${progress}）` : ''}
          </span>
        </div>
      )}

      {isSuccess && videoUrl && (
        <div className='flex flex-col gap-2'>
          <video
            controls
            src={videoUrl}
            className={cn(
              'w-full max-w-2xl rounded-xl border border-border bg-muted',
              'shadow-md'
            )}
          />
          <Button
            variant='outline'
            size='sm'
            className='w-fit'
            onClick={handleReset}
          >
            <RotateCcw className='mr-1.5 size-3.5' />
            重新生成
          </Button>
        </div>
      )}

      {isError && (
        <div className='flex items-start gap-3 rounded-lg border border-destructive/30 bg-destructive/10 p-4 text-destructive'>
          <VideoOff className='mt-0.5 size-4 shrink-0' />
          <div className='flex flex-col gap-1.5'>
            <span className='text-sm font-medium'>
              生成失败：{error}
            </span>
            <Button
              variant='outline'
              size='sm'
              className='w-fit border-destructive/30 text-destructive hover:bg-destructive/10'
              onClick={handleReset}
            >
              <RotateCcw className='mr-1.5 size-3.5' />
              重试
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}
