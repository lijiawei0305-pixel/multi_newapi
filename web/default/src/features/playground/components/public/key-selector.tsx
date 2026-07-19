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
import { KeyRound, Loader2, Sparkles } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectSeparator,
  SelectTrigger,
} from '@/components/ui/select'
import { API_KEY_STATUS } from '@/features/keys/constants'
import type { ApiKey } from '@/features/keys/types'
import { cn } from '@/lib/utils'

// 「auto 分组·不指定密钥」哨兵值（Select 需要一个非空 value 才能高亮该项）。
// 选中 auto 时对外表现为「未选具体密钥」：目录展示全部模型，聊天走后端 auto 组自动路由。
const AUTO_VALUE = '__auto__'

// ============================================================================
// 状态标签映射（中文）
// ============================================================================

const STATUS_LABEL: Record<number, string> = {
  [API_KEY_STATUS.DISABLED]: 'Disabled',
  [API_KEY_STATUS.EXPIRED]: 'Expired',
  [API_KEY_STATUS.EXHAUSTED]: 'Quota exhausted',
}

// ============================================================================
// Props
// ============================================================================

export interface KeySelectorProps {
  keys: ApiKey[]
  selectedId: number | null
  /** id=选中某密钥；null=不指定密钥（浏览全部模型，发送仍需选密钥） */
  onSelect: (id: number | null) => void
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
  const { t } = useTranslation()
  const selectedKey =
    selectedId != null ? (keys.find((k) => k.id === selectedId) ?? null) : null

  // 整个下拉禁用：未登录 或 正在加载
  const rootDisabled = !isAuthed || loading

  const enabledKeys = keys.filter((k) => k.status === API_KEY_STATUS.ENABLED)
  const disabledKeys = keys.filter((k) => k.status !== API_KEY_STATUS.ENABLED)

  function handleValueChange(value: string | null) {
    if (value == null || value === AUTO_VALUE) {
      onSelect(null)
      return
    }
    const id = Number(value)
    if (!Number.isNaN(id)) {
      onSelect(id)
    }
  }

  return (
    <Select
      value={selectedId != null ? String(selectedId) : AUTO_VALUE}
      onValueChange={handleValueChange}
      disabled={rootDisabled}
    >
      <SelectTrigger
        className={cn(
          'min-w-[200px] max-w-[280px]',
          rootDisabled && 'cursor-not-allowed opacity-60'
        )}
        aria-label={t('Select an API key')}
      >
        {loading && (
          <Loader2 className='text-muted-foreground size-3.5 shrink-0 animate-spin' />
        )}
        {!loading && selectedKey != null && (
          <KeyRound className='text-muted-foreground size-3.5 shrink-0' />
        )}
        {!loading && selectedKey == null && (
          <Sparkles className='text-muted-foreground size-3.5 shrink-0' />
        )}
        {selectedKey != null ? (
          <span className='flex-1 truncate text-left text-sm'>
            {selectedKey.name}
          </span>
        ) : (
          <span className='text-muted-foreground flex-1 truncate text-left text-sm'>
            {!isAuthed ? t('Please sign in to select an API key') : 'auto'}
          </span>
        )}
      </SelectTrigger>

      <SelectContent align='start' className='min-w-[240px]'>
        {/* auto 分组：不指定密钥，默认展示全部模型；聊天走后端 auto 组自动路由 */}
        <SelectGroup>
          <SelectLabel>auto</SelectLabel>
          <SelectItem value={AUTO_VALUE}>
            <span className='flex flex-1 items-center gap-2 truncate'>
              <Sparkles className='size-3.5 shrink-0' />
              <span className='truncate'>
                {t('auto · Auto-routing (all models)')}
              </span>
            </span>
          </SelectItem>
        </SelectGroup>

        {(enabledKeys.length > 0 || disabledKeys.length > 0) && (
          <SelectSeparator />
        )}

        {/* 可用密钥分组 */}
        {enabledKeys.length > 0 && (
          <SelectGroup>
            <SelectLabel>{t('Available keys')}</SelectLabel>
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
              <SelectLabel>{t('Unavailable keys')}</SelectLabel>
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
                    <Badge
                      variant='outline'
                      className='ml-auto shrink-0 text-xs'
                    >
                      {t(STATUS_LABEL[key.status])}
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
            {t('No API keys yet')}
          </div>
        )}

        {/* 加载中 */}
        {loading && (
          <div className='text-muted-foreground px-2 py-3 text-center text-sm'>
            {t('Loading…')}
          </div>
        )}
      </SelectContent>
    </Select>
  )
}
