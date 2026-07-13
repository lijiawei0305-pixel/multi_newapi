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
import React, { createContext, useContext, useState } from 'react'

interface PlaygroundCredentialContextValue {
  apiKey: string | null
  setApiKey: (k: string | null) => void
}

const PlaygroundCredentialContext = createContext<PlaygroundCredentialContextValue | null>(null)

export function PlaygroundCredentialProvider({ children }: { children: React.ReactNode }) {
  const [apiKey, setApiKey] = useState<string | null>(null)

  return (
    <PlaygroundCredentialContext.Provider value={{ apiKey, setApiKey }}>
      {children}
    </PlaygroundCredentialContext.Provider>
  )
}

export function usePlaygroundCredential(): PlaygroundCredentialContextValue {
  const ctx = useContext(PlaygroundCredentialContext)
  if (ctx === null) {
    throw new Error(
      'usePlaygroundCredential 必须在 <PlaygroundCredentialProvider> 内部使用',
    )
  }
  return ctx
}
