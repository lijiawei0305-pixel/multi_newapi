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
import { api } from '@/lib/api'

import { API_ENDPOINTS } from './constants'
import type {
  ChatCompletionRequest,
  ChatCompletionResponse,
  ModelOption,
  GroupOption,
} from './types'

/**
 * Send chat completion request (non-streaming).
 *
 * 与流式路径对齐的鉴权/分组策略：
 * - session（auto 分组）→ POST /pg（登录态 New-Api-User + cookie，globals 已带
 *   withCredentials），不带 Bearer；分组由已覆写进 payload.group 的 'auto' 决定。
 * - token（选中密钥）→ POST /v1，带 `Authorization: Bearer sk-`。
 */
export async function sendChatCompletion(
  payload: ChatCompletionRequest,
  opts: { authMode: 'token' | 'session' | 'none'; apiKey: string | null },
  signal?: AbortSignal
): Promise<ChatCompletionResponse> {
  const isSession = opts.authMode === 'session'
  const endpoint = isSession
    ? API_ENDPOINTS.PG_CHAT_COMPLETIONS
    : API_ENDPOINTS.CHAT_COMPLETIONS
  const res = await api.post(endpoint, payload, {
    signal,
    skipErrorHandler: true,
    headers:
      !isSession && opts.apiKey
        ? { Authorization: `Bearer ${opts.apiKey}` }
        : undefined,
  } as Record<string, unknown>)
  return res.data
}

/**
 * Get user available models
 */
export async function getUserModels(group: string): Promise<ModelOption[]> {
  const res = await api.get(API_ENDPOINTS.USER_MODELS, {
    params: { group },
  })
  const { data } = res

  if (!data.success || !Array.isArray(data.data)) {
    return []
  }

  return data.data.map((model: string) => ({
    label: model,
    value: model,
  }))
}

/**
 * Get user groups
 */
export async function getUserGroups(): Promise<GroupOption[]> {
  const res = await api.get(API_ENDPOINTS.USER_GROUPS)
  const { data } = res

  if (!data.success || !data.data) {
    return []
  }

  const groupData = data.data as Record<string, { desc: string; ratio: number }>

  // label is for button display (name only); desc is for dropdown content
  return Object.entries(groupData).map(([group, info]) => ({
    label: group,
    value: group,
    ratio: info.ratio,
    desc: info.desc,
  }))
}
