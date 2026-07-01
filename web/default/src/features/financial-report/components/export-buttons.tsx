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
import { Download, FileText } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { downloadFinanceDetail } from '../api'
import type { DetailParams, ExportFormat } from '../types'

// ============================================================================
// CSV / PDF export buttons. The `detail` endpoints stream a file when `format`
// is set (the ONE exception to the JSON envelope, contract §1). The stage-1
// `downloadFinanceDetail` helper self-triggers the browser save and bypasses
// the global business-error interceptor (blob body), so we toast failures here.
// SCOPE-AGNOSTIC: pass scope='admin' | 'tenant'.
// ============================================================================

export interface ExportButtonsProps {
  scope: 'admin' | 'tenant'
  params: DetailParams
  disabled?: boolean
}

export function ExportButtons({ scope, params, disabled }: ExportButtonsProps) {
  const { t } = useTranslation()
  const [busy, setBusy] = useState<ExportFormat | null>(null)

  const handleExport = async (format: ExportFormat) => {
    if (busy) return
    setBusy(format)
    try {
      await downloadFinanceDetail(scope, params, format)
    } catch {
      toast.error(t('Export failed. Please try again.'))
    } finally {
      setBusy(null)
    }
  }

  return (
    <div className='flex items-center gap-2'>
      <Button
        variant='outline'
        size='sm'
        disabled={disabled || busy !== null}
        onClick={() => handleExport('csv')}
        data-testid='export-csv'
      >
        <Download className={busy === 'csv' ? 'animate-pulse' : undefined} />
        {t('Export CSV')}
      </Button>
      <Button
        variant='outline'
        size='sm'
        disabled={disabled || busy !== null}
        onClick={() => handleExport('pdf')}
        data-testid='export-pdf'
      >
        <FileText className={busy === 'pdf' ? 'animate-pulse' : undefined} />
        {t('Export PDF')}
      </Button>
    </div>
  )
}
