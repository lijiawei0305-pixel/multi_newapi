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

import type { Withdrawal, WithdrawalAction } from '../types'

type WithdrawalsContextType = {
  action: WithdrawalAction | null
  currentRow: Withdrawal | null
  openAction: (row: Withdrawal, action: WithdrawalAction) => void
  closeAction: () => void
  refreshTrigger: number
  triggerRefresh: () => void
}

const WithdrawalsContext = React.createContext<WithdrawalsContextType | null>(
  null
)

export function WithdrawalsProvider({
  children,
}: {
  children: React.ReactNode
}) {
  const [action, setAction] = useState<WithdrawalAction | null>(null)
  const [currentRow, setCurrentRow] = useState<Withdrawal | null>(null)
  const [refreshTrigger, setRefreshTrigger] = useState(0)

  const openAction = (row: Withdrawal, next: WithdrawalAction) => {
    setCurrentRow(row)
    setAction(next)
  }
  const closeAction = () => setAction(null)
  const triggerRefresh = () => setRefreshTrigger((prev) => prev + 1)

  return (
    <WithdrawalsContext
      value={{
        action,
        currentRow,
        openAction,
        closeAction,
        refreshTrigger,
        triggerRefresh,
      }}
    >
      {children}
    </WithdrawalsContext>
  )
}

// eslint-disable-next-line react-refresh/only-export-components
export const useWithdrawals = () => {
  const ctx = React.useContext(WithdrawalsContext)
  if (!ctx) {
    throw new Error(
      'useWithdrawals has to be used within <WithdrawalsProvider>'
    )
  }
  return ctx
}
