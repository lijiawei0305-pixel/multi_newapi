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
import type { Row } from '@tanstack/react-table'
import { Pencil, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { deleteAgent } from '../api'
import type { Agent } from '../types'
import { useAgents } from './agents-provider'

interface DataTableRowActionsProps {
  row: Row<Agent>
}

export function DataTableRowActions({ row }: DataTableRowActionsProps) {
  const { t } = useTranslation()
  const { setOpen, setCurrentRow, triggerRefresh } = useAgents()
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [isDeleting, setIsDeleting] = useState(false)
  const agent = row.original

  const handleEdit = () => {
    setCurrentRow(agent)
    setOpen('update')
  }

  const handleDelete = async () => {
    setIsDeleting(true)
    try {
      const res = await deleteAgent(agent.id)
      if (res.success) {
        toast.success(t('Agent deleted', { defaultValue: '代理已删除' }))
        setDeleteOpen(false)
        triggerRefresh()
      }
      // 业务失败（如有未提现收益）由共享 axios 拦截器读 message/code 提示。
    } catch {
      toast.error(t('Request failed', { defaultValue: '请求失败' }))
    } finally {
      setIsDeleting(false)
    }
  }

  return (
    <div className='-ml-1.5 flex items-center gap-1'>
      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              variant='ghost'
              size='icon-sm'
              onClick={handleEdit}
              aria-label={t('Edit', { defaultValue: '编辑' })}
              data-testid={`agent-edit-${agent.id}`}
            />
          }
        >
          <Pencil />
        </TooltipTrigger>
        <TooltipContent>{t('Edit', { defaultValue: '编辑' })}</TooltipContent>
      </Tooltip>

      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              variant='ghost'
              size='icon-sm'
              onClick={() => setDeleteOpen(true)}
              aria-label={t('Delete', { defaultValue: '删除' })}
              data-testid={`agent-delete-${agent.id}`}
            />
          }
        >
          <Trash2 className='text-destructive' />
        </TooltipTrigger>
        <TooltipContent>{t('Delete', { defaultValue: '删除' })}</TooltipContent>
      </Tooltip>

      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t('Confirm delete agent', { defaultValue: '确认删除代理' })}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t('Delete agent warning', {
                defaultValue:
                  '将归档该代理并下线其站点：回收子域名、把其名下用户迁回主站（账号/余额保留、继续可用）、数据留存归档。若代理钱包有未提现/冻结中收益需先结清才能删除。此操作会关停该代理站。',
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={isDeleting}>
              {t('Cancel', { defaultValue: '取消' })}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={(e) => {
                e.preventDefault()
                handleDelete()
              }}
              disabled={isDeleting}
              className='bg-destructive text-white hover:bg-destructive/90'
            >
              {isDeleting
                ? t('Deleting...', { defaultValue: '删除中…' })
                : t('Delete', { defaultValue: '删除' })}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
