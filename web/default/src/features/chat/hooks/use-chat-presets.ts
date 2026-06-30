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
import { useMemo } from 'react'
import { useStatus } from '@/hooks/use-status'
import type { SystemStatus } from '@/features/auth/types'
import {
  type ChatPreset,
  parseChatConfig,
  type RawChatConfig,
} from '../lib/chat-links'

function getStoredStatusChats(): RawChatConfig {
  if (typeof window === 'undefined') return undefined
  try {
    const raw = window.localStorage.getItem('status')
    if (!raw) return undefined
    const parsed = JSON.parse(raw)
    return parsed?.chats ?? parsed?.Chats
  } catch {
    return undefined
  }
}

function extractServerAddress(status: SystemStatus | null) {
  const fromStatus =
    (status?.server_address as string | undefined) ??
    (status?.serverAddress as string | undefined) ??
    status?.data?.server_address ??
    (status?.data as Record<string, unknown> | undefined)?.serverAddress

  const origin = typeof window !== 'undefined' ? window.location.origin : ''

  // 多租户：导入到聊天客户端（Cherry Studio 等）的 base_url 必须是用户「当前访问的域名」
  // （各代理站/主站各自域名），而非全局 ServerAddress。后端未配置 ServerAddress 时默认返回
  // http://localhost:3000，会污染导入地址——忽略该默认值、优先同源 origin；仅显式配了真实地址才用它。
  if (
    fromStatus &&
    typeof fromStatus === 'string' &&
    !/(localhost|127\.0\.0\.1):3000/.test(fromStatus)
  ) {
    return fromStatus
  }

  return origin
}

function extractChats(status: SystemStatus | null): RawChatConfig {
  const raw =
    status?.Chats ?? status?.chats ?? status?.data?.Chats ?? status?.data?.chats

  return (raw as RawChatConfig) ?? getStoredStatusChats()
}

export function useChatPresets(): {
  chatPresets: ChatPreset[]
  serverAddress: string
} {
  const { status } = useStatus()

  const serverAddress = useMemo(() => extractServerAddress(status), [status])

  const chatPresets = useMemo(() => {
    const raw = extractChats(status)
    return parseChatConfig(raw)
  }, [status])

  return {
    chatPresets,
    serverAddress,
  }
}
