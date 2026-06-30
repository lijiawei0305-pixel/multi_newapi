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
import { useEffect, useState, type ReactNode } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { SectionPageLayout } from '@/components/layout'
import { StatusBadge } from '@/components/status-badge'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { fmtDateTime } from '@/lib/agent-format'
import { getApiErrorCode } from '@/lib/api'
import type {
  BannedWord,
  MatchType,
  ModerationAction,
  WordsApi,
} from './types'

/** Create/edit dialog. `row=null` → create; otherwise edit. */
function WordDialog({
  api,
  row,
  open,
  onOpenChange,
  onSuccess,
}: {
  api: WordsApi
  row: BannedWord | null
  open: boolean
  onOpenChange: (open: boolean) => void
  onSuccess: () => void
}) {
  const { t } = useTranslation()
  const [word, setWord] = useState('')
  const [matchType, setMatchType] = useState<MatchType>('contains')
  const [action, setAction] = useState<ModerationAction>('block')
  const [enabled, setEnabled] = useState(true)
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (open) {
      setWord(row?.word ?? '')
      setMatchType((row?.match_type as MatchType) ?? 'contains')
      setAction((row?.action as ModerationAction) ?? 'block')
      setEnabled(row?.enabled ?? true)
    }
  }, [open, row])

  const handleSubmit = async () => {
    setSubmitting(true)
    try {
      const res = await api.upsertWord({
        id: row?.id ?? 0,
        word,
        match_type: matchType,
        action,
        enabled,
      })
      if (res.success) {
        toast.success(t('Saved'))
        onSuccess()
        onOpenChange(false)
      }
    } catch (err) {
      const code = getApiErrorCode(err)
      if (code === 'MODERATION_WORD_INVALID') {
        toast.error(t('Invalid banned word (empty / too long / bad regex)'))
      } else {
        toast.error(t('Request failed'))
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {row ? t('Edit Banned Word') : t('Add Banned Word')}
          </DialogTitle>
          <DialogDescription>
            {t('Scan user input; match → remind (log) or block (reject).')}
          </DialogDescription>
        </DialogHeader>
        <div className='flex flex-col gap-4'>
          <div className='flex flex-col gap-2'>
            <Label htmlFor='bw-word'>{t('Word')}</Label>
            <Input
              id='bw-word'
              value={word}
              onChange={(e) => setWord(e.target.value)}
              data-testid='bw-word-input'
            />
          </div>
          <div className='flex flex-col gap-2'>
            <Label htmlFor='bw-match'>{t('Match Type')}</Label>
            <Select
              value={matchType}
              onValueChange={(v) => setMatchType(v as MatchType)}
            >
              <SelectTrigger id='bw-match' className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='contains'>{t('Contains')}</SelectItem>
                <SelectItem value='exact'>{t('Exact')}</SelectItem>
                <SelectItem value='regex'>{t('Regex')}</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className='flex flex-col gap-2'>
            <Label htmlFor='bw-action'>{t('Action')}</Label>
            <Select
              value={action}
              onValueChange={(v) => setAction(v as ModerationAction)}
            >
              <SelectTrigger id='bw-action' className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='remind'>
                  {t('Remind (allow + log)')}
                </SelectItem>
                <SelectItem value='block'>{t('Block (reject)')}</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className='flex items-center justify-between'>
            <Label htmlFor='bw-enabled'>{t('Enabled')}</Label>
            <Switch id='bw-enabled' checked={enabled} onCheckedChange={setEnabled} />
          </div>
        </div>
        <DialogFooter>
          <DialogClose render={<Button variant='outline' />}>
            {t('Cancel')}
          </DialogClose>
          <Button
            type='button'
            disabled={submitting}
            onClick={handleSubmit}
            data-testid='bw-save-submit'
          >
            {submitting ? t('Saving...') : t('Save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** Shared banned-words CRUD page; admin & agent inject their own `api`. */
export function ModerationWordsManager({
  api,
  title,
  queryKey,
  footer,
}: {
  api: WordsApi
  title: string
  queryKey: string
  footer?: ReactNode
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [editRow, setEditRow] = useState<BannedWord | null>(null)
  const [editOpen, setEditOpen] = useState(false)
  const [deleteRow, setDeleteRow] = useState<BannedWord | null>(null)
  const [deleting, setDeleting] = useState(false)

  const { data, isLoading } = useQuery({
    queryKey: [queryKey],
    queryFn: async () => (await api.listWords()).data || [],
    placeholderData: (prev) => prev,
  })
  const rows = data || []
  const refresh = () => queryClient.invalidateQueries({ queryKey: [queryKey] })

  const handleDelete = async () => {
    if (!deleteRow) return
    setDeleting(true)
    try {
      const res = await api.deleteWord(deleteRow.id)
      if (res.success) {
        toast.success(t('Deleted'))
        refresh()
      }
    } catch {
      toast.error(t('Request failed'))
    } finally {
      setDeleting(false)
      setDeleteRow(null)
    }
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{title}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          onClick={() => {
            setEditRow(null)
            setEditOpen(true)
          }}
          data-testid='add-banned-word'
        >
          {t('Add Banned Word')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='overflow-hidden rounded-lg border' data-testid='banned-words-table'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('ID')}</TableHead>
                <TableHead>{t('Word')}</TableHead>
                <TableHead>{t('Match Type')}</TableHead>
                <TableHead>{t('Action')}</TableHead>
                <TableHead>{t('Enabled')}</TableHead>
                <TableHead>{t('Created At')}</TableHead>
                <TableHead className='text-right'>{t('Actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading ? (
                <TableRow>
                  <TableCell colSpan={7} className='text-muted-foreground text-center'>
                    {t('Loading...')}
                  </TableCell>
                </TableRow>
              ) : rows.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={7} className='text-muted-foreground text-center'>
                    {t('No banned words yet')}
                  </TableCell>
                </TableRow>
              ) : (
                rows.map((row: BannedWord) => (
                  <TableRow key={row.id} data-testid={`bw-row-${row.id}`}>
                    <TableCell className='tabular-nums'>{row.id}</TableCell>
                    <TableCell className='font-medium'>{row.word}</TableCell>
                    <TableCell>
                      <Badge variant='secondary'>{row.match_type}</Badge>
                    </TableCell>
                    <TableCell>
                      <Badge variant={row.action === 'block' ? 'destructive' : 'outline'}>
                        {row.action === 'block' ? t('Block') : t('Remind')}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <StatusBadge
                        variant={row.enabled ? 'success' : 'neutral'}
                        label={row.enabled ? t('Enabled') : t('Disabled')}
                        copyable={false}
                      />
                    </TableCell>
                    <TableCell className='text-muted-foreground text-sm'>
                      {fmtDateTime(row.created_at)}
                    </TableCell>
                    <TableCell className='space-x-2 text-right'>
                      <Button
                        variant='outline'
                        size='sm'
                        onClick={() => {
                          setEditRow(row)
                          setEditOpen(true)
                        }}
                        data-testid={`bw-edit-${row.id}`}
                      >
                        {t('Edit')}
                      </Button>
                      <Button
                        variant='outline'
                        size='sm'
                        onClick={() => setDeleteRow(row)}
                        data-testid={`bw-delete-${row.id}`}
                      >
                        {t('Delete')}
                      </Button>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>
        {footer}
      </SectionPageLayout.Content>

      <WordDialog
        api={api}
        row={editRow}
        open={editOpen}
        onOpenChange={setEditOpen}
        onSuccess={refresh}
      />
      {deleteRow && (
        <ConfirmDialog
          open
          onOpenChange={(v) => !v && setDeleteRow(null)}
          title={`${t('Confirm delete')}: ${deleteRow.word}`}
          desc={t('This banned word will be removed.')}
          handleConfirm={handleDelete}
          isLoading={deleting}
          confirmText={t('Delete')}
          destructive
        />
      )}
    </SectionPageLayout>
  )
}
