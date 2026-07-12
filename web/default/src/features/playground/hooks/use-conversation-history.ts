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
import { nanoid } from 'nanoid'

import type { Message } from '../types'

// ============================================================================
// 会话历史 —— 在现有「单会话工作缓冲」之上叠加多会话历史。
//
// 设计要点：
// - 工作缓冲仍由 usePlaygroundState 持有（messages / updateMessages）；本 hook 只
//   负责把「当前活动会话」镜像进历史列表，并支持新建 / 切换 / 删除。
// - 活动会话的消息变化经防抖（600ms）落库，避免流式逐 token 更新时高频写 localStorage。
// - 新建 / 切换 / 卸载前都会 flushActive() 同步落库，保证切走的会话不丢。
// - 空会话（无消息）不占历史条目；新建后未发言不会留下空条目。
// ============================================================================

const CONVERSATIONS_KEY = 'playground_conversations'
const ACTIVE_KEY = 'playground_active_conversation'
const MAX_CONVERSATIONS = 30
const TITLE_MAX = 28
const SYNC_DEBOUNCE_MS = 600

/** 对外暴露的会话摘要（不含完整消息体，供列表渲染） */
export interface ConversationSummary {
  id: string
  title: string
  messageCount: number
  updatedAt: number
}

interface StoredConversation {
  id: string
  title: string
  messages: Message[]
  updatedAt: number
}

// ── localStorage 读写（全部容错，隐私模式/配额异常静默降级）─────────────────────
function loadStored(): StoredConversation[] {
  if (typeof window === 'undefined') return []
  try {
    const raw = window.localStorage.getItem(CONVERSATIONS_KEY)
    if (!raw) return []
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    return parsed.filter(
      (c): c is StoredConversation =>
        !!c &&
        typeof (c as StoredConversation).id === 'string' &&
        Array.isArray((c as StoredConversation).messages)
    )
  } catch {
    return []
  }
}

function persistStored(list: StoredConversation[]): void {
  if (typeof window === 'undefined') return
  try {
    window.localStorage.setItem(CONVERSATIONS_KEY, JSON.stringify(list))
  } catch {
    // 忽略配额/隐私模式写入失败
  }
}

function loadActiveId(): string | null {
  if (typeof window === 'undefined') return null
  try {
    return window.localStorage.getItem(ACTIVE_KEY)
  } catch {
    return null
  }
}

function persistActiveId(id: string): void {
  if (typeof window === 'undefined') return
  try {
    window.localStorage.setItem(ACTIVE_KEY, id)
  } catch {
    // 忽略
  }
}

/** 用首条用户消息生成会话标题；无内容回退「新对话」 */
function deriveTitle(messages: Message[]): string {
  const firstUser = messages.find((m) => m.from === 'user')
  const versions = firstUser?.versions ?? []
  const text = versions[versions.length - 1]?.content ?? ''
  const trimmed = text.trim().replace(/\s+/g, ' ')
  if (!trimmed) return '新对话'
  return trimmed.length > TITLE_MAX ? `${trimmed.slice(0, TITLE_MAX)}…` : trimmed
}

interface UseConversationHistoryOptions {
  /** 当前工作缓冲的消息（来自 usePlaygroundState） */
  messages: Message[]
  /** 把某会话的消息载入工作缓冲（通常传 updateMessages） */
  onLoadMessages: (messages: Message[]) => void
  /** 工作缓冲是否已完成首次加载（false 时不同步，避免用空态覆盖历史） */
  ready: boolean
}

export function useConversationHistory({
  messages,
  onLoadMessages,
  ready,
}: UseConversationHistoryOptions) {
  const [conversations, setConversations] = useState<StoredConversation[]>(
    loadStored
  )
  const [activeId, setActiveId] = useState<string>(
    () => loadActiveId() ?? nanoid()
  )

  // 用 ref 追踪最新值，供防抖/事件回调在 fire 时读取（避免闭包过期）
  const messagesRef = useRef(messages)
  messagesRef.current = messages
  const activeIdRef = useRef(activeId)
  activeIdRef.current = activeId
  const conversationsRef = useRef(conversations)
  conversationsRef.current = conversations
  const syncTimerRef = useRef<number | null>(null)

  useEffect(() => {
    persistActiveId(activeId)
  }, [activeId])

  // 立即把「当前活动会话」同步进历史（非空 upsert；空则移除该条目）。
  // 直接读 ref + 直写 localStorage，卸载期调用也安全。
  const flushActive = useCallback(() => {
    if (syncTimerRef.current !== null) {
      window.clearTimeout(syncTimerRef.current)
      syncTimerRef.current = null
    }
    const id = activeIdRef.current
    const msgs = messagesRef.current
    const prev = conversationsRef.current

    let next: StoredConversation[]
    if (msgs.length === 0) {
      if (!prev.some((c) => c.id === id)) return
      next = prev.filter((c) => c.id !== id)
    } else {
      const entry: StoredConversation = {
        id,
        title: deriveTitle(msgs),
        messages: msgs,
        updatedAt: Date.now(),
      }
      next = [entry, ...prev.filter((c) => c.id !== id)].slice(
        0,
        MAX_CONVERSATIONS
      )
    }
    conversationsRef.current = next
    persistStored(next)
    setConversations(next)
  }, [])

  // 消息变化 → 防抖同步（流式期间不逐 token 写盘）
  useEffect(() => {
    if (!ready) return
    if (syncTimerRef.current !== null) {
      window.clearTimeout(syncTimerRef.current)
    }
    syncTimerRef.current = window.setTimeout(() => {
      syncTimerRef.current = null
      flushActive()
    }, SYNC_DEBOUNCE_MS)
    return () => {
      if (syncTimerRef.current !== null) {
        window.clearTimeout(syncTimerRef.current)
        syncTimerRef.current = null
      }
    }
  }, [messages, ready, flushActive])

  // 卸载前 flush，保证最后一次变更落库
  useEffect(
    () => () => {
      flushActive()
    },
    [flushActive]
  )

  const newConversation = useCallback(() => {
    flushActive() // 先把当前会话落库（非空），再清空缓冲、切到新 id
    setActiveId(nanoid())
    onLoadMessages([])
  }, [flushActive, onLoadMessages])

  const switchTo = useCallback(
    (id: string) => {
      const target = conversationsRef.current.find((c) => c.id === id)
      if (!target) return
      flushActive() // 先保存当前，再载入目标
      setActiveId(id)
      onLoadMessages(target.messages)
    },
    [flushActive, onLoadMessages]
  )

  const deleteConversation = useCallback(
    (id: string) => {
      const next = conversationsRef.current.filter((c) => c.id !== id)
      conversationsRef.current = next
      persistStored(next)
      setConversations(next)
      // 删除的是当前活动会话 → 清空缓冲并开新会话
      if (id === activeIdRef.current) {
        setActiveId(nanoid())
        onLoadMessages([])
      }
    },
    [onLoadMessages]
  )

  const summaries: ConversationSummary[] = conversations.map((c) => ({
    id: c.id,
    title: c.title,
    messageCount: c.messages.length,
    updatedAt: c.updatedAt,
  }))

  return {
    conversations: summaries,
    activeId,
    newConversation,
    switchTo,
    deleteConversation,
  }
}
