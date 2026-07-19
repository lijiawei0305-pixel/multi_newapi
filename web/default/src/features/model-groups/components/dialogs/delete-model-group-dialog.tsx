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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'

import { deleteModelGroup } from '../../api'
import { useModelGroups } from '../model-groups-provider'

export function DeleteModelGroupDialog() {
  const { t } = useTranslation()
  const { open, setOpen, currentRow, triggerRefresh } = useModelGroups()
  const [loading, setLoading] = useState(false)

  if (open !== 'delete' || !currentRow) return null

  const handleConfirm = async () => {
    setLoading(true)
    try {
      const res = await deleteModelGroup(currentRow.name)
      if (res.success) {
        toast.success(t('Model group deleted'))
        triggerRefresh()
        setOpen(null)
      }
    } catch {
      toast.error(t('Operation failed'))
    } finally {
      setLoading(false)
    }
  }

  return (
    <ConfirmDialog
      open
      onOpenChange={(v) => !v && setOpen(null)}
      title={`${t('Confirm delete')}: ${currentRow.name}`}
      desc={t(
        'After deleting, users can no longer select this model group. This cannot be undone. Continue?'
      )}
      handleConfirm={handleConfirm}
      isLoading={loading}
      confirmText={t('Delete')}
      destructive
    />
  )
}
