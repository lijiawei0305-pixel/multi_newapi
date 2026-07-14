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
import type { PricingModel } from '@/features/pricing/types'

// Message types
export type MessageRole = 'user' | 'assistant' | 'system'

export type MessageStatus = 'loading' | 'streaming' | 'complete' | 'error'

export type PlaygroundMessageLayoutMode = 'alternating' | 'left'

export interface MessageVersion {
  id: string
  content: string
}

export interface Message {
  key: string
  from: MessageRole
  versions: MessageVersion[]
  createdAt?: number
  startedAt?: number
  completedAt?: number
  durationMs?: number
  sources?: { href: string; title: string }[]
  reasoning?: {
    content: string
    duration: number
    startedAt?: number
    completedAt?: number
    durationMs?: number
  }
  isReasoningStreaming?: boolean
  isReasoningComplete?: boolean
  isContentComplete?: boolean
  status?: MessageStatus
  errorCode?: string | null
}

// API payload types
export interface ChatCompletionMessage {
  role: MessageRole
  content: string | ContentPart[]
}

export interface ContentPart {
  type: 'text' | 'image_url'
  text?: string
  image_url?: {
    url: string
  }
}

export interface ChatCompletionRequest {
  model: string
  group?: string
  messages: ChatCompletionMessage[]
  stream: boolean
  temperature?: number
  top_p?: number
  max_tokens?: number
  frequency_penalty?: number
  presence_penalty?: number
  seed?: number
}

export interface ChatCompletionChunk {
  id: string
  object: string
  created: number
  model: string
  choices: Array<{
    index: number
    delta: {
      role?: MessageRole
      content?: string
      reasoning_content?: string
    }
    finish_reason: string | null
  }>
}

export interface ChatCompletionResponse {
  id: string
  object: string
  created: number
  model: string
  choices: Array<{
    index: number
    message: {
      role: MessageRole
      content: string
      reasoning_content?: string
    }
    finish_reason: string
  }>
  usage?: {
    prompt_tokens: number
    completion_tokens: number
    total_tokens: number
  }
}

// Configuration types
export interface PlaygroundConfig {
  model: string
  group: string
  temperature: number
  top_p: number
  max_tokens: number
  frequency_penalty: number
  presence_penalty: number
  seed: number | null
  stream: boolean
}

export interface ParameterEnabled {
  temperature: boolean
  top_p: boolean
  max_tokens: boolean
  frequency_penalty: boolean
  presence_penalty: boolean
  seed: boolean
}

// Model and group options
export interface ModelOption {
  label: string
  value: string
}

export interface GroupOption {
  label: string
  value: string
  ratio: number
  desc?: string
}

// Playground public page types
export type PlaygroundCapability = 'chat' | 'image' | 'video'

export type CatalogFilter = 'all' | PlaygroundCapability

export interface WorkspaceProps {
  apiKey: string
  model: string
  /** 选中 key 所属分组；聊天用于同步 config.group，使模型列表拉取正确的组 */
  group?: string
  /**
   * auto 分组（不指定密钥）模式：聊天走登录态 /pg 端点、后端 auto 组自动路由；
   * 此时不按单一分组同步 config.group（避免模型列表被清空），发送分组由凭据上下文决定。
   */
  autoMode?: boolean
  /** 当前选中模型的完整对象；用于在工作区空态渲染「模型介绍」英雄区 */
  introModel?: PricingModel | null
}

export interface ImageGenParams {
  model: string
  prompt: string
  n?: number
  size?: string
  response_format?: 'url' | 'b64_json'
}

export interface ImageResultItem {
  url?: string
  b64_json?: string
  revised_prompt?: string
}

export interface ImageGenResponse {
  created: number
  data: ImageResultItem[]
}

export interface VideoGenParams {
  model: string
  prompt: string
  image?: string
  duration?: number
  width?: number
  height?: number
  fps?: number
  seed?: number
  n?: number
  response_format?: string
}

export interface VideoTaskData {
  task_id: string
  status: string
  url?: string
  result_url?: string
  format?: string
  progress?: string
  error?: string
  fail_reason?: string
  metadata?: unknown
}

export interface VideoTaskEnvelope {
  code: string
  message: string
  data?: VideoTaskData
}
