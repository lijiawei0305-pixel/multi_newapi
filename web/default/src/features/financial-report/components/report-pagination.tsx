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
import { useTranslation } from 'react-i18next'

import {
  Pagination,
  PaginationContent,
  PaginationItem,
  PaginationNext,
  PaginationPrevious,
} from '@/components/ui/pagination'

// ============================================================================
// Shared prev/next pagination footer for the report tables (detail + ranking).
// Pagination is nested INSIDE `data` on the wire (contract §7.2): the parent
// passes the raw `total`/`page`/`page_size` echoed by the backend. Hidden when
// everything fits on one page.
// ============================================================================

export interface ReportPaginationProps {
  page: number
  pageSize: number
  total: number
  onPageChange: (page: number) => void
}

export function ReportPagination({
  page,
  pageSize,
  total,
  onPageChange,
}: ReportPaginationProps) {
  const { t } = useTranslation()
  const totalPages = Math.max(1, Math.ceil(total / Math.max(1, pageSize)))
  const canPrev = page > 1
  const canNext = page < totalPages

  if (total <= pageSize) return null

  return (
    <div className='flex items-center justify-between gap-2 border-t px-3 py-2'>
      <span
        className='text-muted-foreground text-xs'
        data-testid='report-pagination-info'
      >
        {t('Page {{page}} of {{total}}', { page, total: totalPages })}
      </span>
      <Pagination className='mx-0 w-auto justify-end'>
        <PaginationContent>
          <PaginationItem>
            <PaginationPrevious
              text={t('Previous')}
              aria-disabled={!canPrev}
              className={
                !canPrev ? 'pointer-events-none opacity-50' : undefined
              }
              onClick={(e) => {
                e.preventDefault()
                if (canPrev) onPageChange(page - 1)
              }}
            />
          </PaginationItem>
          <PaginationItem>
            <PaginationNext
              text={t('Next')}
              aria-disabled={!canNext}
              className={
                !canNext ? 'pointer-events-none opacity-50' : undefined
              }
              onClick={(e) => {
                e.preventDefault()
                if (canNext) onPageChange(page + 1)
              }}
            />
          </PaginationItem>
        </PaginationContent>
      </Pagination>
    </div>
  )
}
