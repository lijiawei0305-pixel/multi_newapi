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
import { useCallback, useEffect, useRef, useState } from 'react'

import { useAuthStore } from '@/stores/auth-store'

import { PlaygroundChat } from '../chat/playground-chat'
import { PlaygroundInput } from '../input/playground-input'
import {
  useChatHandler,
  usePlaygroundConversation,
  usePlaygroundOptions,
  usePlaygroundState,
} from '../../hooks'
import { useConversationHistory } from '../../hooks/use-conversation-history'
import type { WorkspaceProps } from '../../types'
import { ConversationHistoryBar } from './conversation-history-bar'

/**
 * 聊天工作区 —— 公开创作页的聊天能力主体。
 *
 * 由 index.tsx 的聊天主体（PlaygroundChat 消息滚动区 + PlaygroundInput 底部输入）
 * 抽取迁移而来。凭据已由 use-stream-request 经 credential context 注入，
 * 此处无需传 Authorization；apiKey 为空时禁用发送（外层门控亦会拦截）。
 */
export function ChatWorkspace({
  apiKey,
  model,
  group,
  introModel,
}: WorkspaceProps) {
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

  // 同步选中 key 所属分组到 config.group：/v1 的分组由 key 决定，这里对齐是为了让
  // usePlaygroundOptions 的 getUserModels 拉取正确分组的模型列表，令 catalog 选中的
  // 模型始终在列表内、不被 getModelFallback/shouldClearModelForGroup 回退覆盖。
  const lastSyncedGroupRef = useRef<string | null>(null)
  useEffect(() => {
    if (!group) return
    if (lastSyncedGroupRef.current === group) return
    lastSyncedGroupRef.current = group
    if (config.group !== group) {
      updateConfig('group', group)
    }
  }, [group, config.group, updateConfig])

  // 多会话历史：在单会话工作缓冲之上叠加「新建对话 / 历史」。
  const conversationHistory = useConversationHistory({
    messages,
    onLoadMessages: updateMessages,
    ready: !isLoadingMessages,
  })

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

  // 输入框受控文本 + 聚焦信号：starter 提示词点击后「填入输入框」而非立即发送，
  // 让用户可先编辑再发送（对齐目标交互）。
  const [inputText, setInputText] = useState('')
  const [promptFillSignal, setPromptFillSignal] = useState(0)
  const handleSelectPrompt = useCallback((prompt: string) => {
    setInputText(prompt)
    setPromptFillSignal((n) => n + 1)
  }, [])

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
      {/* 顶部：新建对话 / 历史 */}
      <div className='mx-auto w-full max-w-4xl shrink-0 px-3 pt-3'>
        <ConversationHistoryBar
          conversations={conversationHistory.conversations}
          activeId={conversationHistory.activeId}
          onNew={conversationHistory.newConversation}
          onSwitch={conversationHistory.switchTo}
          onDelete={conversationHistory.deleteConversation}
        />
      </div>

      {/* 全宽滚动容器：即便鼠标位于两侧留白区域也能滚动 */}
      <div className='flex min-h-0 flex-1 flex-col overflow-hidden'>
        <PlaygroundChat
          messages={messages}
          introModel={introModel}
          isLoadingMessages={isLoadingMessages}
          onRegenerateMessage={handleRegenerate}
          onEditMessage={handleEditMessage}
          onDeleteMessage={handleDeleteMessage}
          onSelectPrompt={handleSelectPrompt}
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
          text={inputText}
          onTextChange={setInputText}
          focusSignal={promptFillSignal}
        />
      </div>
    </div>
  )
}
