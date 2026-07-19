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
import { useCallback, useEffect, useRef, useState } from 'react'
import { SSE } from 'sse.js'

import { getCommonHeaders } from '@/lib/api'

import { API_ENDPOINTS, ERROR_MESSAGES } from '../constants'
import { usePlaygroundCredential } from '../context/credential-context'
import {
  getStreamReadyStateError,
  isStreamClosedReadyState,
  isStreamDoneMessage,
  parseStreamErrorDetails,
  parseStreamMessageUpdates,
} from '../lib'
import type { ChatCompletionRequest } from '../types'

/**
 * Hook for handling streaming chat completion requests
 */
export function useStreamRequest() {
  const sseSourceRef = useRef<SSE | null>(null)
  const isStreamCompleteRef = useRef(false)
  const [isStreaming, setIsStreaming] = useState(false)
  const { apiKey, authMode, sendGroup } = usePlaygroundCredential()
  // 用 ref 持有最新凭据：sendStreamRequest 不把凭据列入依赖（避免重建函数
  // 导致下游引用失效），却始终读到切换密钥/模式后的最新值，杜绝 stale-closure。
  const apiKeyRef = useRef(apiKey)
  const authModeRef = useRef(authMode)
  const sendGroupRef = useRef(sendGroup)
  useEffect(() => {
    apiKeyRef.current = apiKey
    authModeRef.current = authMode
    sendGroupRef.current = sendGroup
  }, [apiKey, authMode, sendGroup])

  const closeActiveStream = useCallback((source?: SSE) => {
    const streamSource = source ?? sseSourceRef.current
    streamSource?.close()

    if (!source || sseSourceRef.current === source) {
      sseSourceRef.current = null
      setIsStreaming(false)
    }
  }, [])

  const sendStreamRequest = useCallback(
    (
      payload: ChatCompletionRequest,
      onUpdate: (type: 'reasoning' | 'content', chunk: string) => void,
      onComplete: () => void,
      onError: (error: string, errorCode?: string) => void
    ) => {
      sseSourceRef.current?.close()

      // session 模式（auto 分组）：走 /pg 登录态端点，不带 Bearer，靠 New-Api-User +
      // cookie 鉴权；请求体 group 覆写为 sendGroup（'auto'）交后端校验后自动路由。
      // token 模式：走 /v1，带 Bearer；后端忽略请求体 group（分组由密钥决定）。
      const isSession = authModeRef.current === 'session'
      const endpoint = isSession
        ? API_ENDPOINTS.PG_CHAT_COMPLETIONS
        : API_ENDPOINTS.CHAT_COMPLETIONS
      const finalPayload = sendGroupRef.current
        ? { ...payload, group: sendGroupRef.current }
        : payload

      const source = new SSE(endpoint, {
        headers: {
          ...getCommonHeaders(),
          ...(!isSession && apiKeyRef.current
            ? { Authorization: `Bearer ${apiKeyRef.current}` }
            : {}),
        },
        // 登录态跨域（多租户代理域）需携带 cookie；同源亦无害。
        withCredentials: isSession,
        method: 'POST',
        payload: JSON.stringify(finalPayload),
      })

      sseSourceRef.current = source
      isStreamCompleteRef.current = false
      setIsStreaming(true)

      const handleError = (errorMessage: string, errorCode?: string) => {
        if (!isStreamCompleteRef.current) {
          onError(errorMessage, errorCode)
          closeActiveStream(source)
        }
      }

      source.addEventListener('message', (e: MessageEvent) => {
        if (isStreamDoneMessage(e.data)) {
          isStreamCompleteRef.current = true
          closeActiveStream(source)
          onComplete()
          return
        }

        try {
          const updates = parseStreamMessageUpdates(e.data)

          for (const update of updates) {
            onUpdate(update.type, update.chunk)
          }
        } catch (error) {
          // eslint-disable-next-line no-console
          console.error('Failed to parse SSE message:', error)
          handleError(ERROR_MESSAGES.PARSE_ERROR)
        }
      })

      source.addEventListener('error', (e: Event & { data?: string }) => {
        // Only handle errors if stream didn't complete normally
        if (!isStreamClosedReadyState(source.readyState)) {
          // eslint-disable-next-line no-console
          console.error('SSE Error:', e)
          const { errorCode, errorMessage } = parseStreamErrorDetails(e.data)
          handleError(errorMessage, errorCode)
        }
      })

      source.addEventListener(
        'readystatechange',
        (e: Event & { readyState?: number }) => {
          const errorMessage = getStreamReadyStateError(e.readyState, source)

          if (errorMessage) {
            handleError(errorMessage)
          }
        }
      )

      try {
        source.stream()
      } catch (error: unknown) {
        // eslint-disable-next-line no-console
        console.error('Failed to start SSE stream:', error)
        onError(ERROR_MESSAGES.STREAM_START_ERROR)
        closeActiveStream(source)
      }
    },
    [closeActiveStream]
  )

  const stopStream = useCallback(() => {
    closeActiveStream()
  }, [closeActiveStream])

  return {
    sendStreamRequest,
    stopStream,
    isStreaming,
  }
}
