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
import { useQuery } from '@tanstack/react-query'
import { useAuthStore } from '@/stores/auth-store'
import { getApiKeys, fetchTokenKey } from '@/features/keys/api'
import type { ApiKey } from '@/features/keys/types'

// ============================================================================
// usePlaygroundKeys
// ============================================================================

/**
 * 获取当前用户全部 API 密钥（含未启用项，UI 自行灰显）。
 * enabled 由调用方传入（通常为 isAuthed）。
 */
export function usePlaygroundKeys(enabled: boolean): {
  keys: ApiKey[]
  isLoading: boolean
  error: Error | null
} {
  const userId = useAuthStore((state) => state.auth.user?.id)

  const { data, isLoading, error } = useQuery({
    queryKey: ['playground-keys', userId],
    queryFn: async () => {
      const res = await getApiKeys({ p: 1, size: 100 })
      if (!res.success) {
        throw new Error(res.message || '获取 API 密钥失败')
      }
      return res.data?.items ?? []
    },
    enabled: enabled && Boolean(userId),
    staleTime: 2 * 60 * 1000,
    gcTime: 5 * 60 * 1000,
  })

  return {
    keys: data ?? [],
    isLoading,
    error: error as Error | null,
  }
}

// ============================================================================
// useKeyReveal
// ============================================================================

/**
 * 懒揭示 API 密钥完整凭据。
 *
 * - 内部用 Map<number, string> 缓存已揭示结果，同一 id 不重复请求。
 * - 并发去重：对同一 id 的并发调用共享同一个 Promise（in-flight Map）。
 * - 返回值为拼接后的完整凭据：`sk-${data.key}`。
 */
export function useKeyReveal(): {
  reveal: (id: number) => Promise<string>
  revealing: boolean
} {
  // 已揭示结果缓存
  const cacheRef = useRef<Map<number, string>>(new Map())
  // 正在飞行中的请求（并发去重）
  const inFlightRef = useRef<Map<number, Promise<string>>>(new Map())
  const [revealing, setRevealing] = useState(false)

  const reveal = useCallback(async (id: number): Promise<string> => {
    // 命中缓存直接返回
    const cached = cacheRef.current.get(id)
    if (cached !== undefined) {
      return cached
    }

    // 并发去重：复用已在飞行中的 Promise
    const inFlight = inFlightRef.current.get(id)
    if (inFlight !== undefined) {
      return inFlight
    }

    // 发起新请求
    const promise = (async (): Promise<string> => {
      setRevealing(true)
      try {
        const res = await fetchTokenKey(id)
        if (!res.success || !res.data?.key) {
          throw new Error(res.message || '获取密钥失败')
        }
        const fullKey = `sk-${res.data.key}`
        cacheRef.current.set(id, fullKey)
        return fullKey
      } finally {
        inFlightRef.current.delete(id)
        // 只有当没有其他飞行中请求时才清除 revealing 状态
        if (inFlightRef.current.size === 0) {
          setRevealing(false)
        }
      }
    })()

    inFlightRef.current.set(id, promise)
    return promise
  }, [])

  return { reveal, revealing }
}
