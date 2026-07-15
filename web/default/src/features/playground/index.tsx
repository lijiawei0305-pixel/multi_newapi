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
import { useTranslation } from 'react-i18next'
import { useNavigate } from '@tanstack/react-router'
import { toast } from 'sonner'

import { PublicLayout } from '@/components/layout'
import { useAuthStore } from '@/stores/auth-store'
import { API_KEY_STATUS } from '@/features/keys/constants'
import type { PricingModel } from '@/features/pricing/types'

import './playground-i18n' // AI 工坊多语言补丁：启动即注入 ja/ru/fr/vi 缺失译文(自安装,幂等)
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
import { ModelIntroHero } from './components/public/model-intro-card'
import { GatingAlert } from './components/public/gating-alert'
import { ChatWorkspace } from './components/public/chat-workspace'
import { ImageWorkspace } from './components/public/image-workspace'
import { VideoWorkspace } from './components/public/video-workspace'
import { AUTO_GROUP } from './constants'
import type { CatalogFilter } from './types'

// ============================================================================
// 内层内容（位于 PlaygroundCredentialProvider 内部，可消费 credential context）
// ============================================================================
function PlaygroundPublicContent() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const isAuthed = !!user

  const { setCredential } = usePlaygroundCredential()
  const navigate = useNavigate()

  // ── 状态机 ────────────────────────────────────────────────────────────────
  // 默认 auto（不指定密钥）：目录展示全部模型，聊天走后端 auto 组自动路由。
  // 刻意不从 localStorage 恢复上次密钥——每次进入都以 auto 为默认（用户诉求）。
  const [selectedKeyId, setSelectedKeyId] = useState<number | null>(null)
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
    error: modelsError,
    refetch: refetchModels,
  } = useModelCatalog({ group, filter, search })

  // ── 凭据决策：根据登录态 + 选择计算发送凭据（token / session / none）────────────
  // - 未登录 → none（仅浏览）
  // - auto（未选具体密钥）→ session：走 /pg 登录态端点，后端 auto 组自动路由，无需 sk-
  // - 选中可用密钥 → token：懒揭示 sk-，走 /v1 + Bearer，分组由密钥决定
  // - 选中不可用密钥 / 揭示失败 → none
  useEffect(() => {
    if (!isAuthed) {
      setRevealedKey(null)
      setCredential({ apiKey: null, authMode: 'none', sendGroup: null })
      return
    }

    // auto 分组：登录态即可发送聊天（session），不需要 sk- 密钥
    if (selectedKeyId == null) {
      setRevealedKey(null)
      setCredential({ apiKey: null, authMode: 'session', sendGroup: AUTO_GROUP })
      return
    }

    // keys 尚未加载完成时等待
    if (keys.length === 0) return

    // 找到选中项；不存在（如已删除的旧 id）→ 回退 auto
    const key = keys.find((k) => k.id === selectedKeyId) ?? null
    if (key == null) {
      setSelectedKeyId(null)
      setRevealedKey(null)
      setCredential({ apiKey: null, authMode: 'session', sendGroup: AUTO_GROUP })
      return
    }

    // 仅可用（status=1）密钥才揭示；否则不注入凭据（禁用发送）
    if (key.status !== API_KEY_STATUS.ENABLED) {
      setRevealedKey(null)
      setCredential({ apiKey: null, authMode: 'none', sendGroup: null })
      return
    }

    let cancelled = false
    void reveal(selectedKeyId)
      .then((full) => {
        if (cancelled) return
        setRevealedKey(full)
        setCredential({
          apiKey: full,
          authMode: 'token',
          sendGroup: key.group ? key.group : null,
        })
      })
      .catch(() => {
        if (cancelled) return
        setRevealedKey(null)
        setCredential({ apiKey: null, authMode: 'none', sendGroup: null })
        toast.error(t('Failed to fetch the API key; please retry or reselect'))
      })

    return () => {
      cancelled = true
    }
  }, [isAuthed, selectedKeyId, keys, reveal, setCredential])

  // ── 选择 key 回调（id=选中密钥；null=切回 auto 分组，浏览全部模型）──────────────
  const handleSelectKey = useCallback((id: number | null) => {
    setSelectedKeyId(id)
  }, [])

  // ── 当前能力（video>image>chat；未选模型按 chat）─────────────────────────────
  const capability = selectedModel ? getPrimaryCapability(selectedModel) : 'chat'

  // ── 门控判定（§5，按能力区分）───────────────────────────────────────────────
  // - auto（session）：聊天可发送；图片/视频后端无对应端点，仍需选具体密钥
  // - token：选中可用密钥且已揭示 → 全能力可发送
  const autoActive = isAuthed && selectedKeyId == null // auto/session 模式
  const tokenReady = isAuthed && selectedKeyId != null && !!revealedKey
  const hasNoKeys = isAuthed && !keysLoading && keys.length === 0
  let gatingMessage: string | null = null
  let gatingAction: { label: string; onClick: () => void } | undefined
  if (!isAuthed) {
    gatingMessage = t('Please sign in to start creating')
    gatingAction = {
      label: t('Go to sign in'),
      onClick: () => void navigate({ to: '/sign-in' }),
    }
  } else if (selectedKeyId != null && !tokenReady) {
    // 选中了具体密钥但不可用 / 尚未就绪
    gatingMessage = t('The selected key is unavailable; please reselect, or switch to auto')
  } else if (capability !== 'chat' && !tokenReady) {
    // 图片 / 视频：auto 分组无对应后端端点，必须选一个可用密钥
    if (hasNoKeys) {
      gatingMessage = t('Image / video generation requires an API key; please create one first')
      gatingAction = {
        label: t('Create API key'),
        onClick: () => void navigate({ to: '/keys' }),
      }
    } else {
      gatingMessage = t('The auto group does not support image / video generation yet; please select an API key at the top')
    }
  }
  // 其余：聊天 + auto(session) 或 token 就绪 → 无门控，可发送

  // ── 当前能力工作区 ─────────────────────────────────────────────────────────
  const workspaceProps = {
    apiKey: revealedKey ?? '',
    model: selectedModel?.model_name ?? '',
    group: selectedKey?.group ?? '',
    autoMode: autoActive,
    introModel: selectedModel,
  }

  function renderWorkspace() {
    switch (capability) {
      case 'video':
        // key=模型名：切换到不同视频模型时重挂载，清空上一次的轮询/结果
        return <VideoWorkspace key={workspaceProps.model} {...workspaceProps} />
      case 'image':
        // key=模型名：切换到不同图片模型时重挂载，清空上一次生成的图片
        return <ImageWorkspace key={workspaceProps.model} {...workspaceProps} />
      case 'chat':
        // 聊天不加 key：切换模型应保留对话历史
        return <ChatWorkspace {...workspaceProps} />
      default:
        // capability===null：选中的模型既非聊天/图片/视频（embedding/rerank 等），
        // 不给可发送的工作区，改渲染禁用占位，避免对错误能力的模型发请求。
        return (
          <div className='flex h-full flex-col items-center justify-center gap-2 p-6 text-center text-muted-foreground'>
            <p className='text-sm'>{t('This model is not yet supported in the studio')}</p>
            <p className='text-xs'>
              {t('Only non-creative endpoints like embedding / rerank are supported; please choose a chat / image / video model on the left')}
            </p>
          </div>
        )
    }
  }

  return (
    <div className='flex h-[calc(100svh-3.5rem)] min-h-0 flex-col overflow-hidden pt-14'>
      {/* 页面工具条 */}
      <div className='flex shrink-0 flex-wrap items-center gap-3 border-b border-border px-4 py-3'>
        <div className='mr-auto flex min-w-0 flex-col gap-0.5'>
          <h1 className='truncate text-lg font-semibold text-foreground'>
            {t('AI Model Aggregation Platform')}
          </h1>
          <p className='truncate text-[0.8rem] leading-tight text-muted-foreground'>
            {t('Select a key and model to start chat / image / video creation')}
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
          <GatingAlert message={gatingMessage} action={gatingAction} />
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
            error={modelsError}
            onRetry={refetchModels}
          />
        </div>

        {/* 右：工作区（未选模型时以「模型介绍」英雄区占据主区居中引导；
            选中后由各工作区在空态内渲染介绍卡作为中央主角） */}
        <div className='min-h-0 overflow-hidden rounded-xl border border-border bg-card'>
          {selectedModel ? (
            renderWorkspace()
          ) : (
            <div className='flex h-full items-center justify-center overflow-y-auto p-6'>
              <ModelIntroHero model={null} />
            </div>
          )}
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
