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
// Prev/Next pagination footer for the ticket list. Pagination is nested inside
// `data` on the wire (contract §2.12); the parent passes the raw total / page /
// page_size echoed by the backend. Hidden when everything fits on one page.
// ============================================================================

export function TicketsPagination(props: {
  page: number
  pageSize: number
  total: number
  onPageChange: (page: number) => void
}) {
  const { t } = useTranslation()
  const totalPages = Math.max(1, Math.ceil(props.total / Math.max(1, props.pageSize)))
  const canPrev = props.page > 1
  const canNext = props.page < totalPages

  if (props.total <= props.pageSize) return null

  return (
    <div className='flex items-center justify-between gap-2 px-1'>
      <span
        className='text-muted-foreground text-xs'
        data-testid='tickets-pagination-info'
      >
        {t('Page {{page}} of {{total}}', { page: props.page, total: totalPages })}
      </span>
      <Pagination className='mx-0 w-auto justify-end'>
        <PaginationContent>
          <PaginationItem>
            <PaginationPrevious
              text={t('Previous')}
              aria-disabled={!canPrev}
              className={!canPrev ? 'pointer-events-none opacity-50' : undefined}
              onClick={(e) => {
                e.preventDefault()
                if (canPrev) props.onPageChange(props.page - 1)
              }}
            />
          </PaginationItem>
          <PaginationItem>
            <PaginationNext
              text={t('Next')}
              aria-disabled={!canNext}
              className={!canNext ? 'pointer-events-none opacity-50' : undefined}
              onClick={(e) => {
                e.preventDefault()
                if (canNext) props.onPageChange(props.page + 1)
              }}
            />
          </PaginationItem>
        </PaginationContent>
      </Pagination>
    </div>
  )
}
