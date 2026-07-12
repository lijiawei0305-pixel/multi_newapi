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
import { useCallback, useState } from 'react'
import { toast } from 'sonner'

import { api } from '@/lib/api'

import { API_ENDPOINTS } from '../constants'
import type { ImageGenParams, ImageGenResponse, ImageResultItem } from '../types'

type GenerationStatus = 'idle' | 'loading' | 'success' | 'error'

export interface UseImageGenerationReturn {
  generate: (apiKey: string, params: ImageGenParams) => Promise<void>
  status: GenerationStatus
  images: ImageResultItem[]
  error: string | null
  reset: () => void
}

export function useImageGeneration(): UseImageGenerationReturn {
  const [status, setStatus] = useState<GenerationStatus>('idle')
  const [images, setImages] = useState<ImageResultItem[]>([])
  const [error, setError] = useState<string | null>(null)

  const generate = useCallback(
    async (apiKey: string, params: ImageGenParams): Promise<void> => {
      setStatus('loading')
      setError(null)
      setImages([])

      const body = {
        model: params.model,
        prompt: params.prompt,
        n: params.n,
        size: params.size,
        response_format: params.response_format ?? 'url',
      }

      try {
        const res = await api.post<ImageGenResponse>(
          API_ENDPOINTS.IMAGES_GENERATIONS,
          body,
          {
            headers: { Authorization: `Bearer ${apiKey}` },
            skipErrorHandler: true,
          }
        )

        const resultItems = res.data?.data
        // 逐项过滤：只保留真正带 url 或 b64_json 的图。n>1 部分失败时，某项可能只有
        // revised_prompt 而无图，直接渲染会得到 <img src=''> 破图并误请求当前页面。
        const validItems = Array.isArray(resultItems)
          ? resultItems.filter((it) => it.b64_json || (it.url && it.url.trim()))
          : []
        if (validItems.length === 0) {
          const msg = '图片生成未返回有效结果，请重试'
          setError(msg)
          setStatus('error')
          toast.error(msg)
          return
        }

        setImages(validItems)
        setStatus('success')
      } catch (err: unknown) {
        let msg = '图片生成失败，请稍后重试'

        if (err && typeof err === 'object') {
          const axiosErr = err as {
            response?: { data?: { message?: string; error?: { message?: string } }; status?: number }
            message?: string
          }
          const backendMsg =
            axiosErr.response?.data?.error?.message ??
            axiosErr.response?.data?.message
          if (backendMsg && typeof backendMsg === 'string') {
            msg = backendMsg
          } else if (axiosErr.response?.status === 401) {
            msg = 'API 密钥无效或已过期，请重新选择'
          } else if (axiosErr.message && typeof axiosErr.message === 'string') {
            msg = axiosErr.message
          }
        }

        setError(msg)
        setStatus('error')
        toast.error(msg)
      }
    },
    []
  )

  const reset = useCallback(() => {
    setStatus('idle')
    setImages([])
    setError(null)
  }, [])

  return { generate, status, images, error, reset }
}
