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
import { useEffect, useState } from 'react'
import type { Row } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Switch } from '@/components/ui/switch'
import { updateModelGroup } from '../api'
import type { ModelGroup } from '../types'
import { useModelGroups } from './model-groups-provider'

/** Inline "启用开关" — toggles `enabled` via PUT, optimistic with revert. */
export function EnabledSwitchCell({ row }: { row: Row<ModelGroup> }) {
  const { t } = useTranslation()
  const { triggerRefresh } = useModelGroups()
  const [checked, setChecked] = useState(row.original.enabled)
  const [loading, setLoading] = useState(false)

  useEffect(() => setChecked(row.original.enabled), [row.original.enabled])

  const handleToggle = async (next: boolean) => {
    setChecked(next)
    setLoading(true)
    try {
      const res = await updateModelGroup(row.original.name, { enabled: next })
      if (res.success) {
        toast.success(next ? t('Has been enabled') : t('Has been disabled'))
        triggerRefresh()
      } else {
        setChecked(!next)
      }
    } catch {
      setChecked(!next)
      toast.error(t('Operation failed'))
    } finally {
      setLoading(false)
    }
  }

  return (
    <Switch
      checked={checked}
      onCheckedChange={handleToggle}
      disabled={loading}
      aria-label={t('Enabled')}
      data-testid={`model-group-toggle-${row.original.name}`}
    />
  )
}
