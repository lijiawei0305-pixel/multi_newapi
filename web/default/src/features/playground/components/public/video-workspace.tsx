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
import { Clapperboard, Download, Loader2, RotateCcw, VideoOff } from 'lucide-react'

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

import { PROMPT_INPUT_SHELL_CLASS } from '../../constants'
import { useVideoGeneration } from '../../hooks/use-video-generation'
import type { WorkspaceProps } from '../../types'

export function VideoWorkspace({ apiKey, model }: WorkspaceProps) {
  const [prompt, setPrompt] = useState('')
  const [duration, setDuration] = useState('')
  // <video> 加载/解码失败（直链过期/403/CORS、blob 损坏、MIME 误判）。hook 置
  // status='success' 后不再关心元素能否解码，无此兜底则用户只见黑框、无任何提示。
  const [playbackError, setPlaybackError] = useState(false)

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

    setPlaybackError(false)
    const params = {
      model,
      prompt: prompt.trim(),
      ...(duration !== '' && !isNaN(Number(duration)) && Number(duration) > 0
        ? { duration: Number(duration) }
        : {}),
    }

    void submit(apiKey, params)
  }

  const handleReset = () => {
    reset()
    setPrompt('')
    setDuration('')
    setPlaybackError(false)
  }

  const handleDownload = () => {
    if (!videoUrl) return
    // videoUrl 是 blob: 或 data:（同源/内联），原生 a[download] 即可下载
    const a = document.createElement('a')
    a.href = videoUrl
    a.download = '生成视频.mp4'
    document.body.appendChild(a)
    a.click()
    a.remove()
  }

  return (
    <div className='flex h-full flex-col gap-4 p-4'>
      {/* 可选参数区域 */}
      <div className='grid grid-cols-2 gap-3'>
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
      </div>

      {/* 提示词输入区域 */}
      <PromptInput
        groupClassName={cn(PROMPT_INPUT_SHELL_CLASS, 'overflow-hidden')}
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
      {status === 'idle' && (
        <div className='flex flex-1 flex-col items-center justify-center gap-3 text-muted-foreground'>
          <Clapperboard className='size-12 opacity-30' />
          <p className='text-sm'>输入提示词，点击「生成」开始创作视频</p>
        </div>
      )}

      {isGenerating && (
        <div className='bg-muted/50 flex items-center gap-3 rounded-lg border border-border p-4'>
          <Loader2 className='text-primary size-5 shrink-0 animate-spin' />
          <span className='text-foreground text-sm'>
            生成中…{progress ? `（${progress}）` : ''}
          </span>
        </div>
      )}

      {isSuccess && videoUrl && !playbackError && (
        <div className='flex flex-col gap-2'>
          <video
            controls
            src={videoUrl}
            aria-label='生成的视频'
            className={cn(
              'w-full max-w-2xl rounded-xl border border-border bg-muted',
              'shadow-md'
            )}
            // 加载/解码失败切错误态；成功加载则清除（防上一次的失败态残留）。
            onError={() => setPlaybackError(true)}
            onLoadedData={() => setPlaybackError(false)}
          >
            您的浏览器不支持视频播放
          </video>
          <div className='flex gap-2'>
            <Button
              variant='outline'
              size='sm'
              className='w-fit'
              onClick={handleDownload}
            >
              <Download className='mr-1.5 size-3.5' />
              下载视频
            </Button>
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
        </div>
      )}

      {isSuccess && videoUrl && playbackError && (
        <div className='flex items-start gap-3 rounded-lg border border-destructive/30 bg-destructive/10 p-4 text-destructive'>
          <VideoOff className='mt-0.5 size-4 shrink-0' />
          <div className='flex flex-col gap-1.5'>
            <span className='text-sm font-medium'>
              视频加载失败，可能是链接已失效或格式不受支持
            </span>
            <Button
              variant='outline'
              size='sm'
              className='w-fit border-destructive/30 text-destructive hover:bg-destructive/10'
              onClick={handleReset}
            >
              <RotateCcw className='mr-1.5 size-3.5' />
              重新生成
            </Button>
          </div>
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
