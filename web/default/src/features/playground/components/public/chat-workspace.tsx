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
import { useCallback, useEffect, useRef } from 'react'

import { useAuthStore } from '@/stores/auth-store'

import { PlaygroundChat } from '../chat/playground-chat'
import { PlaygroundInput } from '../input/playground-input'
import {
  useChatHandler,
  usePlaygroundConversation,
  usePlaygroundOptions,
  usePlaygroundState,
} from '../../hooks'
import type { WorkspaceProps } from '../../types'

/**
 * 聊天工作区 —— 公开创作页的聊天能力主体。
 *
 * 由 index.tsx 的聊天主体（PlaygroundChat 消息滚动区 + PlaygroundInput 底部输入）
 * 抽取迁移而来。凭据已由 use-stream-request 经 credential context 注入，
 * 此处无需传 Authorization；apiKey 为空时禁用发送（外层门控亦会拦截）。
 */
export function ChatWorkspace({ apiKey, model }: WorkspaceProps) {
  const {
    config,
    parameterEnabled,
    messages,
    isLoadingMessages,
    models,
    groups,
    updateMessages,
    setModels,
    setGroups,
    updateConfig,
    clearMessages,
  } = usePlaygroundState()

  // 将传入的 model 同步为聊天默认选中模型（写入 playground 的 model 配置）。
  // 仅在 model prop 真正变化时下压，避免与 usePlaygroundOptions 的兜底逻辑相互抖动。
  const lastSyncedModelRef = useRef<string | null>(null)
  useEffect(() => {
    if (!model) return
    if (lastSyncedModelRef.current === model) return
    lastSyncedModelRef.current = model
    if (config.model !== model) {
      updateConfig('model', model)
    }
  }, [model, config.model, updateConfig])

  const { sendChat, stopGeneration, isGenerating } = useChatHandler({
    config,
    parameterEnabled,
    onMessageUpdate: updateMessages,
  })

  const {
    editingMessageKey,
    handleSendMessage,
    handleRegenerateMessage,
    handleEditMessage,
    handleEditOpenChange,
    applyEdit,
    handleDeleteMessage,
  } = usePlaygroundConversation({
    messages,
    updateMessages,
    sendChat,
  })

  // apiKey 为空时禁用发送（外层也有门控）。
  const canSend = Boolean(apiKey)

  const handleSend = useCallback(
    (text: string) => {
      if (!canSend) return
      handleSendMessage(text)
    },
    [canSend, handleSendMessage]
  )

  const handleRegenerate = useCallback(
    (message: Parameters<typeof handleRegenerateMessage>[0]) => {
      if (!canSend) return
      handleRegenerateMessage(message)
    },
    [canSend, handleRegenerateMessage]
  )

  const handleApplyEdit = useCallback(
    (newContent: string, shouldSubmit: boolean) => {
      // 编辑并提交需要发送；无凭据时降级为仅保存不提交。
      applyEdit(newContent, shouldSubmit && canSend)
    },
    [applyEdit, canSend]
  )

  const handleClearMessages = () => {
    handleEditOpenChange(false)
    clearMessages()
  }

  // 未登录访客也会默认渲染聊天工作区；此时禁止拉取需鉴权的 groups/models，
  // 否则会对私有端点发 401 并弹错误 toast（违反公开页「可浏览」承诺）。
  const isAuthed = !!useAuthStore((state) => state.auth.user)
  const { isLoadingModels } = usePlaygroundOptions({
    currentGroup: config.group,
    currentModel: config.model,
    setGroups,
    setModels,
    updateConfig,
    enabled: isAuthed,
  })

  return (
    <div className='relative flex size-full min-h-0 flex-col overflow-hidden'>
      {/* 全宽滚动容器：即便鼠标位于两侧留白区域也能滚动 */}
      <div className='flex min-h-0 flex-1 flex-col overflow-hidden'>
        <PlaygroundChat
          messages={messages}
          isLoadingMessages={isLoadingMessages}
          onRegenerateMessage={handleRegenerate}
          onEditMessage={handleEditMessage}
          onDeleteMessage={handleDeleteMessage}
          onSelectPrompt={handleSend}
          isGenerating={isGenerating}
          editingKey={editingMessageKey}
          onCancelEdit={handleEditOpenChange}
          onSaveEdit={(newContent) => handleApplyEdit(newContent, false)}
          onSaveEditAndSubmit={(newContent) => handleApplyEdit(newContent, true)}
        />
      </div>

      {/* 输入区：内容居中并与消息区同宽 */}
      <div className='mx-auto w-full max-w-4xl'>
        <PlaygroundInput
          disabled={isGenerating || !canSend}
          groups={groups}
          groupValue={config.group}
          isGenerating={isGenerating}
          isModelLoading={isLoadingModels}
          modelValue={config.model}
          models={models}
          onGroupChange={(value) => updateConfig('group', value)}
          onClearMessages={handleClearMessages}
          onModelChange={(value) => updateConfig('model', value)}
          onStop={stopGeneration}
          onSubmit={handleSend}
          hasMessages={messages.length > 0}
        />
      </div>
    </div>
  )
}
