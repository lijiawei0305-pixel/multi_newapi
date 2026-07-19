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
/* eslint-disable react-refresh/only-export-components */
import React, { createContext, useContext, useMemo, useState } from 'react'

/**
 * 发送鉴权模式：
 * - token：选中了某个密钥 → 走 /v1 + `Authorization: Bearer sk-`，分组由密钥决定。
 * - session：auto 分组 → 走 /pg（登录态 New-Api-User + cookie），请求体 group='auto'
 *   由后端校验后自动路由；无需 sk- 密钥。
 * - none：未登录 / 密钥未揭示 → 不可发送（仅浏览）。
 */
export type PlaygroundAuthMode = 'token' | 'session' | 'none'

export interface PlaygroundCredential {
  /** token 模式下的 `sk-` 明文；session/none 模式为 null */
  apiKey: string | null
  authMode: PlaygroundAuthMode
  /** 发送时写入请求体的分组：session→'auto'，token→密钥分组，none→null */
  sendGroup: string | null
}

interface PlaygroundCredentialContextValue extends PlaygroundCredential {
  setCredential: (c: PlaygroundCredential) => void
}

const NONE_CREDENTIAL: PlaygroundCredential = {
  apiKey: null,
  authMode: 'none',
  sendGroup: null,
}

const PlaygroundCredentialContext =
  createContext<PlaygroundCredentialContextValue | null>(null)

export function PlaygroundCredentialProvider({
  children,
}: {
  children: React.ReactNode
}) {
  const [credential, setCredential] =
    useState<PlaygroundCredential>(NONE_CREDENTIAL)

  const value = useMemo<PlaygroundCredentialContextValue>(
    () => ({ ...credential, setCredential }),
    [credential]
  )

  return (
    <PlaygroundCredentialContext.Provider value={value}>
      {children}
    </PlaygroundCredentialContext.Provider>
  )
}

export function usePlaygroundCredential(): PlaygroundCredentialContextValue {
  const ctx = useContext(PlaygroundCredentialContext)
  if (ctx === null) {
    throw new Error(
      'usePlaygroundCredential 必须在 <PlaygroundCredentialProvider> 内部使用'
    )
  }
  return ctx
}
