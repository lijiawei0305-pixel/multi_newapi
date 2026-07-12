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
