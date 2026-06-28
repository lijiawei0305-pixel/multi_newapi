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
import React, { useState } from 'react'
import useDialogState from '@/hooks/use-dialog'
import type { ModelGroup, ModelGroupsDialogType } from '../types'

type ModelGroupsContextType = {
  open: ModelGroupsDialogType | null
  setOpen: (str: ModelGroupsDialogType | null) => void
  currentRow: ModelGroup | null
  setCurrentRow: React.Dispatch<React.SetStateAction<ModelGroup | null>>
  refreshTrigger: number
  triggerRefresh: () => void
}

const ModelGroupsContext = React.createContext<ModelGroupsContextType | null>(
  null
)

export function ModelGroupsProvider({
  children,
}: {
  children: React.ReactNode
}) {
  const [open, setOpen] = useDialogState<ModelGroupsDialogType>(null)
  const [currentRow, setCurrentRow] = useState<ModelGroup | null>(null)
  const [refreshTrigger, setRefreshTrigger] = useState(0)

  const triggerRefresh = () => setRefreshTrigger((prev) => prev + 1)

  return (
    <ModelGroupsContext
      value={{
        open,
        setOpen,
        currentRow,
        setCurrentRow,
        refreshTrigger,
        triggerRefresh,
      }}
    >
      {children}
    </ModelGroupsContext>
  )
}

// eslint-disable-next-line react-refresh/only-export-components
export const useModelGroups = () => {
  const ctx = React.useContext(ModelGroupsContext)
  if (!ctx) {
    throw new Error(
      'useModelGroups has to be used within <ModelGroupsProvider>'
    )
  }
  return ctx
}
