import * as React from 'react'
import { KeyRound, ChevronsUpDown } from 'lucide-react'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectSeparator,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import { API_KEY_STATUS } from '@/features/keys/constants'
import type { ApiKey } from '@/features/keys/types'

// ============================================================================
// 状态标签映射（中文）
// ============================================================================

const STATUS_LABEL: Record<number, string> = {
  [API_KEY_STATUS.DISABLED]: '已禁用',
  [API_KEY_STATUS.EXPIRED]: '已过期',
  [API_KEY_STATUS.EXHAUSTED]: '额度耗尽',
}

// ============================================================================
// Props
// ============================================================================

export interface KeySelectorProps {
  keys: ApiKey[]
  selectedId: number | null
  onSelect: (id: number) => void
  isAuthed: boolean
  loading?: boolean
}

// ============================================================================
// 组件
// ============================================================================

export function KeySelector({
  keys,
  selectedId,
  onSelect,
  isAuthed,
  loading = false,
}: KeySelectorProps) {
  const selectedKey = selectedId != null
    ? keys.find((k) => k.id === selectedId) ?? null
    : null

  // 整个下拉禁用：未登录 或 正在加载
  const rootDisabled = !isAuthed || loading

  // 触发器显示文案
  const triggerLabel = !isAuthed
    ? '请先登录后选择 API 密钥'
    : selectedKey != null
      ? selectedKey.name
      : '选择 API 密钥'

  const enabledKeys = keys.filter((k) => k.status === API_KEY_STATUS.ENABLED)
  const disabledKeys = keys.filter((k) => k.status !== API_KEY_STATUS.ENABLED)

  function handleValueChange(value: string) {
    const id = Number(value)
    if (!Number.isNaN(id)) {
      onSelect(id)
    }
  }

  return (
    <Select
      value={selectedId != null ? String(selectedId) : ''}
      onValueChange={handleValueChange}
      disabled={rootDisabled}
    >
      <SelectTrigger
        className={cn(
          'min-w-[200px] max-w-[280px]',
          rootDisabled && 'cursor-not-allowed opacity-60',
        )}
        aria-label='选择 API 密钥'
      >
        <KeyRound className='text-muted-foreground size-3.5 shrink-0' />
        {selectedKey != null ? (
          <span className='flex-1 truncate text-left text-sm'>
            {selectedKey.name}
          </span>
        ) : (
          <span
            className={cn(
              'flex-1 truncate text-left text-sm',
              !isAuthed || selectedId == null
                ? 'text-muted-foreground'
                : 'text-foreground',
            )}
          >
            {triggerLabel}
          </span>
        )}
        <ChevronsUpDown className='text-muted-foreground size-3.5 shrink-0' />
      </SelectTrigger>

      <SelectContent align='start' className='min-w-[240px]'>
        {/* 可用密钥分组 */}
        {enabledKeys.length > 0 && (
          <SelectGroup>
            <SelectLabel>可用密钥</SelectLabel>
            {enabledKeys.map((key) => (
              <SelectItem key={key.id} value={String(key.id)}>
                <span className='flex flex-1 items-center gap-2 truncate'>
                  <KeyRound className='size-3.5 shrink-0' />
                  <span className='truncate'>{key.name}</span>
                </span>
              </SelectItem>
            ))}
          </SelectGroup>
        )}

        {/* 不可用密钥分组 */}
        {disabledKeys.length > 0 && (
          <>
            {enabledKeys.length > 0 && <SelectSeparator />}
            <SelectGroup>
              <SelectLabel>不可用密钥</SelectLabel>
              {disabledKeys.map((key) => (
                <SelectItem
                  key={key.id}
                  value={String(key.id)}
                  disabled
                  className='opacity-50'
                >
                  <span className='flex flex-1 items-center gap-2 truncate'>
                    <KeyRound className='size-3.5 shrink-0' />
                    <span className='truncate'>{key.name}</span>
                  </span>
                  {STATUS_LABEL[key.status] != null && (
                    <Badge variant='outline' className='ml-auto shrink-0 text-xs'>
                      {STATUS_LABEL[key.status]}
                    </Badge>
                  )}
                </SelectItem>
              ))}
            </SelectGroup>
          </>
        )}

        {/* 空态：已登录但无密钥 */}
        {keys.length === 0 && !loading && (
          <div className='text-muted-foreground px-2 py-3 text-center text-sm'>
            暂无 API 密钥
          </div>
        )}

        {/* 加载中 */}
        {loading && (
          <div className='text-muted-foreground px-2 py-3 text-center text-sm'>
            加载中…
          </div>
        )}
      </SelectContent>
    </Select>
  )
}
