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
import { useCallback, useRef, useState } from 'react'
import { toast } from 'sonner'

import i18n from '@/i18n/config'
import { api } from '@/lib/api'

import { API_ENDPOINTS } from '../constants'
import type { ImageGenParams, ImageGenResponse, ImageResultItem } from '../types'

export type ImageTurnStatus = 'loading' | 'success' | 'error'

// 一次图片生成 = 一个对话回合：既留住提示词与请求参数（供“查看请求参数”回看），
// 也承载各自的结果/错误，历史累积后向上滚动仍能看到每次的提示词。
export interface ImageTurn {
  id: string
  prompt: string
  model: string
  n: number
  size: string
  responseFormat: string
  status: ImageTurnStatus
  images: ImageResultItem[]
  error: string | null
  createdAt: number
}

export interface UseImageConversationReturn {
  turns: ImageTurn[]
  isGenerating: boolean
  generate: (apiKey: string, params: ImageGenParams) => Promise<void>
  clear: () => void
}

// 从 axios 风格错误里尽力提取后端可读信息，回退到通用中文提示。
function extractImageError(err: unknown): string {
  let msg = i18n.t('Image generation failed, please try again later')
  if (err && typeof err === 'object') {
    const axiosErr = err as {
      response?: {
        data?: { message?: string; error?: { message?: string } }
        status?: number
      }
      message?: string
    }
    const backendMsg =
      axiosErr.response?.data?.error?.message ?? axiosErr.response?.data?.message
    if (backendMsg && typeof backendMsg === 'string') {
      msg = backendMsg
    } else if (axiosErr.response?.status === 401) {
      msg = i18n.t('The API key is invalid or expired, please reselect')
    } else if (axiosErr.message && typeof axiosErr.message === 'string') {
      msg = axiosErr.message
    }
  }
  return msg
}

export function useImageConversation(): UseImageConversationReturn {
  const [turns, setTurns] = useState<ImageTurn[]>([])
  const [isGenerating, setIsGenerating] = useState(false)
  // 单调自增序号：与时间戳拼成稳定的回合 id，避免同毫秒多次提交 key 撞车。
  const seqRef = useRef(0)

  const generate = useCallback(
    async (apiKey: string, params: ImageGenParams): Promise<void> => {
      const n = params.n ?? 1
      const size = params.size ?? ''
      const responseFormat = params.response_format ?? 'url'
      const id = `img-${Date.now()}-${(seqRef.current += 1)}`

      const turn: ImageTurn = {
        id,
        prompt: params.prompt,
        model: params.model,
        n,
        size,
        responseFormat,
        status: 'loading',
        images: [],
        error: null,
        createdAt: Date.now(),
      }
      setTurns((prev) => [...prev, turn])
      setIsGenerating(true)

      const body = {
        model: params.model,
        prompt: params.prompt,
        n,
        size,
        response_format: responseFormat,
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
        // 逐项过滤：只留真正带 url 或 b64_json 的图。n>1 部分失败时某项可能只有
        // revised_prompt 而无图，渲染会得到 <img src=''> 破图并误请求当前页面。
        const validItems = Array.isArray(resultItems)
          ? resultItems.filter((it) => it.b64_json || (it.url && it.url.trim()))
          : []

        if (validItems.length === 0) {
          const msg = i18n.t('Image generation returned no valid result, please try again')
          setTurns((prev) =>
            prev.map((t) =>
              t.id === id ? { ...t, status: 'error', error: msg } : t
            )
          )
          toast.error(msg)
          return
        }

        setTurns((prev) =>
          prev.map((t) =>
            t.id === id ? { ...t, status: 'success', images: validItems } : t
          )
        )
      } catch (err: unknown) {
        const msg = extractImageError(err)
        setTurns((prev) =>
          prev.map((t) =>
            t.id === id ? { ...t, status: 'error', error: msg } : t
          )
        )
        toast.error(msg)
      } finally {
        setIsGenerating(false)
      }
    },
    []
  )

  const clear = useCallback(() => setTurns([]), [])

  return { turns, isGenerating, generate, clear }
}
