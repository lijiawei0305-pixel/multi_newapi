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
import { useCallback, useEffect, useMemo, useState } from 'react'

import { PublicLayout } from '@/components/layout'
import { useAuthStore } from '@/stores/auth-store'
import { API_KEY_STATUS } from '@/features/keys/constants'
import type { PricingModel } from '@/features/pricing/types'

import {
  PlaygroundCredentialProvider,
  usePlaygroundCredential,
} from './context/credential-context'
import { usePlaygroundKeys, useKeyReveal } from './hooks/use-playground-keys'
import { useModelCatalog } from './hooks/use-model-catalog'
import { getPrimaryCapability } from './lib/capabilities'
import { KeySelector } from './components/public/key-selector'
import { CreateKeyButton } from './components/public/create-key-button'
import { ModelCatalog } from './components/public/model-catalog'
import { ModelIntroCard } from './components/public/model-intro-card'
import { GatingAlert } from './components/public/gating-alert'
import { ChatWorkspace } from './components/public/chat-workspace'
import { ImageWorkspace } from './components/public/image-workspace'
import { VideoWorkspace } from './components/public/video-workspace'
import type { CatalogFilter } from './types'

// ============================================================================
// localStorage：记住上次选中的 API 密钥
// ============================================================================
const LAST_KEY_STORAGE = 'playground:last-key-id'

function readLastKeyId(): number | null {
  if (typeof window === 'undefined') return null
  try {
    const raw = window.localStorage.getItem(LAST_KEY_STORAGE)
    if (raw == null) return null
    const id = Number(raw)
    return Number.isNaN(id) ? null : id
  } catch {
    return null
  }
}

function writeLastKeyId(id: number | null): void {
  if (typeof window === 'undefined') return
  try {
    if (id == null) {
      window.localStorage.removeItem(LAST_KEY_STORAGE)
    } else {
      window.localStorage.setItem(LAST_KEY_STORAGE, String(id))
    }
  } catch {
    // 忽略隐私模式等写入失败
  }
}

// ============================================================================
// 内层内容（位于 PlaygroundCredentialProvider 内部，可消费 credential context）
// ============================================================================
function PlaygroundPublicContent() {
  const user = useAuthStore((state) => state.auth.user)
  const isAuthed = !!user

  const { setApiKey } = usePlaygroundCredential()

  // ── 状态机 ────────────────────────────────────────────────────────────────
  const [selectedKeyId, setSelectedKeyId] = useState<number | null>(() =>
    readLastKeyId(),
  )
  const [selectedKeyName, setSelectedKeyName] = useState<string | null>(null)
  const [revealedKey, setRevealedKey] = useState<string | null>(null)
  const [filter, setFilter] = useState<CatalogFilter>('all')
  const [search, setSearch] = useState('')
  const [selectedModel, setSelectedModel] = useState<PricingModel | null>(null)

  // ── 数据源 ────────────────────────────────────────────────────────────────
  const { keys, isLoading: keysLoading } = usePlaygroundKeys(isAuthed)
  const { reveal } = useKeyReveal()

  // 选中 key 对象（仅当仍存在于当前 keys 列表且已启用时有效）
  const selectedKey = useMemo(
    () =>
      selectedKeyId != null
        ? keys.find((k) => k.id === selectedKeyId) ?? null
        : null,
    [keys, selectedKeyId],
  )

  // 选中 key 的分组 → 目录按此过滤；未选则传 null（全部/公开组）
  const group = selectedKey?.group ? selectedKey.group : null

  const {
    models,
    isLoading: modelsLoading,
    counts,
  } = useModelCatalog({ group, filter, search })

  // ── 选中 key 的懒揭示（仅对选中单个 id 揭示一次，不预揭示整表）───────────────
  useEffect(() => {
    // 未登录或未选 → 清空凭据
    if (!isAuthed || selectedKeyId == null) {
      setRevealedKey(null)
      setSelectedKeyName(null)
      setApiKey(null)
      return
    }

    // keys 尚未加载完成时等待
    if (keys.length === 0) return

    // 找到选中项；不存在（如已删除的旧记忆 id）→ 清理选择
    const key = keys.find((k) => k.id === selectedKeyId) ?? null
    if (key == null) {
      setSelectedKeyId(null)
      writeLastKeyId(null)
      setRevealedKey(null)
      setSelectedKeyName(null)
      setApiKey(null)
      return
    }

    // 仅可用（status=1）密钥才揭示；否则不注入凭据
    if (key.status !== API_KEY_STATUS.ENABLED) {
      setRevealedKey(null)
      setSelectedKeyName(key.name)
      setApiKey(null)
      return
    }

    setSelectedKeyName(key.name)

    let cancelled = false
    void reveal(selectedKeyId)
      .then((full) => {
        if (cancelled) return
        setRevealedKey(full)
        setApiKey(full)
      })
      .catch(() => {
        if (cancelled) return
        setRevealedKey(null)
        setApiKey(null)
      })

    return () => {
      cancelled = true
    }
  }, [isAuthed, selectedKeyId, keys, reveal, setApiKey])

  // ── 选择 key 回调 ───────────────────────────────────────────────────────────
  const handleSelectKey = useCallback((id: number) => {
    setSelectedKeyId(id)
    writeLastKeyId(id)
  }, [])

  // ── 门控判定（§5）─────────────────────────────────────────────────────────
  // 未登录 / 已登录未选（或未成功揭示）→ 禁用发送 + 显示提示
  const hasCredential = isAuthed && !!revealedKey
  const gatingMessage = !isAuthed
    ? '请先登录并选择 API 密钥'
    : !hasCredential
      ? '请先在顶部选择 API 密钥后再生成'
      : null

  // ── 当前能力工作区 ─────────────────────────────────────────────────────────
  const capability = selectedModel ? getPrimaryCapability(selectedModel) : 'chat'
  const workspaceProps = {
    apiKey: revealedKey ?? '',
    model: selectedModel?.model_name ?? '',
  }

  function renderWorkspace() {
    switch (capability) {
      case 'video':
        return <VideoWorkspace {...workspaceProps} />
      case 'image':
        return <ImageWorkspace {...workspaceProps} />
      case 'chat':
      default:
        return <ChatWorkspace {...workspaceProps} />
    }
  }

  return (
    <div className='flex h-[calc(100svh-3.5rem)] min-h-0 flex-col overflow-hidden pt-14'>
      {/* 页面工具条 */}
      <div className='flex shrink-0 flex-wrap items-center gap-3 border-b border-border px-4 py-3'>
        <div className='mr-auto flex min-w-0 flex-col'>
          <h1 className='truncate text-lg font-semibold text-foreground'>
            AI 大模型聚合平台
          </h1>
          <p className='truncate text-xs text-muted-foreground'>
            选择 API 密钥与模型，开始聊天、图片与视频创作
          </p>
        </div>

        <KeySelector
          keys={keys}
          selectedId={selectedKeyId}
          onSelect={handleSelectKey}
          isAuthed={isAuthed}
          loading={keysLoading}
        />
        <CreateKeyButton />
      </div>

      {/* 门控提示 */}
      {gatingMessage && !keysLoading && (
        <div className='shrink-0 px-4 pt-3'>
          <GatingAlert message={gatingMessage} />
        </div>
      )}

      {/* 主体两栏 */}
      <div className='grid min-h-0 flex-1 grid-cols-1 gap-4 overflow-hidden p-4 lg:grid-cols-[minmax(280px,340px)_1fr]'>
        {/* 左：模型目录 */}
        <div className='min-h-0 overflow-hidden'>
          <ModelCatalog
            filter={filter}
            onFilter={setFilter}
            search={search}
            onSearch={setSearch}
            models={models}
            selectedModel={selectedModel}
            onSelect={setSelectedModel}
            loading={modelsLoading}
            counts={counts}
          />
        </div>

        {/* 右：模型介绍 + 当前能力工作区 */}
        <div className='grid min-h-0 grid-rows-[auto_1fr] gap-4 overflow-hidden'>
          <div className='min-h-0'>
            <ModelIntroCard model={selectedModel} />
          </div>
          <div className='min-h-0 overflow-hidden rounded-xl border border-border bg-card'>
            {renderWorkspace()}
          </div>
        </div>
      </div>
    </div>
  )
}

// ============================================================================
// 导出：公开创作页外壳
// ============================================================================
export function PlaygroundPublic() {
  return (
    <PublicLayout showMainContainer={false}>
      <PlaygroundCredentialProvider>
        <PlaygroundPublicContent />
      </PlaygroundCredentialProvider>
    </PublicLayout>
  )
}
