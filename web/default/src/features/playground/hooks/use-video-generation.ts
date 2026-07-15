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
import { toast } from 'sonner'

import i18n from '@/i18n/config'
import { api } from '@/lib/api'
import {
  API_ENDPOINTS,
  TASK_SUCCESS_CODE,
  VIDEO_POLL,
  VIDEO_STATUS_FAILURE,
  VIDEO_STATUS_SUCCESS,
} from '../constants'
import type {
  VideoGenParams,
  VideoTaskData,
  VideoTaskEnvelope,
} from '../types'

export type VideoGenerationStatus =
  | 'idle'
  | 'submitting'
  | 'polling'
  | 'success'
  | 'error'

export interface UseVideoGenerationResult {
  submit: (apiKey: string, params: VideoGenParams) => Promise<void>
  status: VideoGenerationStatus
  progress: string | null
  videoUrl: string | null
  error: string | null
  reset: () => void
}

/**
 * Extract the PublicTaskID (`task_xxxx`) from the rewritten create response.
 * The upstream body is proxied/rewritten, so the platform task id may sit on
 * `task_id`, `id`, or nested under `data.task_id`. We must poll with this id,
 * never the raw upstream id.
 */
function extractTaskId(payload: unknown): string | null {
  if (!payload || typeof payload !== 'object') return null
  const record = payload as Record<string, unknown>
  const nested =
    record.data && typeof record.data === 'object'
      ? (record.data as Record<string, unknown>)
      : undefined
  const candidate =
    record.task_id ?? record.id ?? (nested ? nested.task_id : undefined)
  return typeof candidate === 'string' && candidate.length > 0
    ? candidate
    : null
}

/**
 * newapi 视频代理地址由后端用 ServerAddress 拼成绝对 URL（如
 * https://api.example.com/v1/videos/<id>/content）。多租户下页面可能从代理域名访问，
 * 而 ServerAddress 常为主 API 域，二者不同源 → 带 Authorization 的 blob 抓取会触发
 * CORS 预检并失败（视频虽生成成功却播放不出）。对该代理端点归一为【同源相对路径】，
 * 使 blob 抓取始终同源、无预检；非代理端点（外链 CDN 等）保持原样。
 */
// 代理端点形状：/v1/videos/<task_id>/content（后端 BuildProxyURL 固定拼接）。
// 用精确形状而非仅 includes('/v1/videos/')，避免外链 CDN 路径恰好含该子串被误判为代理。
const VIDEO_PROXY_PATH_RE = /\/v1\/videos\/[^/]+\/content$/

function toSameOriginProxyPath(u: string): string {
  try {
    const parsed = new URL(u, window.location.origin)
    // 归一多余前导斜杠：当 ServerAddress 配了尾斜杠时后端 BuildProxyURL 拼出
    // `https://host//v1/videos/x/content`，pathname 会是 `//v1/videos/...`，
    // 既让下游 startsWith('/v1/videos/') 判否（误当 CDN 直链），又会被 <video>
    // 解析成协议相对 URL（host 变字面量 v1）永久播不出。先压成单斜杠再判定。
    const pathname = parsed.pathname.replace(/^\/{2,}/, '/')
    if (VIDEO_PROXY_PATH_RE.test(pathname)) {
      return pathname + parsed.search
    }
    return u
  } catch {
    return u
  }
}

/**
 * Asynchronous video generation hook: submit a create request, poll the task
 * envelope until a terminal state, then resolve a playable `videoUrl`.
 *
 * Because the proxied media URL (`/v1/videos/<id>/content`) requires a Bearer
 * token that a native `<video src>` cannot carry, a non-`data:` result URL is
 * fetched as an authenticated blob and exposed via `createObjectURL`.
 */
export function useVideoGeneration(): UseVideoGenerationResult {
  const [status, setStatus] = useState<VideoGenerationStatus>('idle')
  const [progress, setProgress] = useState<string | null>(null)
  const [videoUrl, setVideoUrl] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  // Currently created object URL (revoked on reset / unmount / re-submit).
  const objectUrlRef = useRef<string | null>(null)
  // Monotonic run id: any run whose id no longer matches must stop writing.
  const runIdRef = useRef(0)
  // Pending backoff timer, so an in-flight sleep can be cleared on teardown.
  const pollTimerRef = useRef<number | null>(null)
  // Resolver of the in-flight backoff wait, so teardown settles it early
  // instead of leaving a hanging Promise/frame when the timer is cleared.
  const pollResolveRef = useRef<(() => void) | null>(null)

  const revokeObjectUrl = useCallback(() => {
    const url = objectUrlRef.current
    objectUrlRef.current = null
    if (url) {
      // 延后到宏任务再吊销：先让 React 卸载 / 换掉 <video> 的 src，避免「元素还在、
      // src 已 revoke」的瞬时窗口导致播放中断 / 黑屏（用户播放中点「重新生成」场景）。
      window.setTimeout(() => URL.revokeObjectURL(url), 0)
    }
  }, [])

  const stopPolling = useCallback(() => {
    // Invalidate any in-flight run.
    runIdRef.current += 1
    if (pollTimerRef.current !== null) {
      window.clearTimeout(pollTimerRef.current)
      pollTimerRef.current = null
    }
    // Settle any awaiting backoff sleep so its async frame unwinds promptly
    // (the post-await runId guard then bails out); avoids a hanging Promise.
    if (pollResolveRef.current) {
      const resolvePending = pollResolveRef.current
      pollResolveRef.current = null
      resolvePending()
    }
  }, [])

  const reset = useCallback(() => {
    stopPolling()
    revokeObjectUrl()
    setStatus('idle')
    setProgress(null)
    setVideoUrl(null)
    setError(null)
  }, [stopPolling, revokeObjectUrl])

  // Stop polling and revoke the object URL when the component unmounts.
  useEffect(
    () => () => {
      stopPolling()
      revokeObjectUrl()
    },
    [stopPolling, revokeObjectUrl]
  )

  const fail = useCallback((message: string, runId: number) => {
    if (runId !== runIdRef.current) return
    setStatus('error')
    setError(message)
    setProgress(null)
    toast.error(message)
  }, [])

  const submit = useCallback(
    async (apiKey: string, params: VideoGenParams) => {
      // 1) Client-side validation before touching the network.
      const model = params.model?.trim()
      const prompt = params.prompt?.trim()
      if (!model) {
        toast.error(i18n.t('Please select a model to use first'))
        return
      }
      if (!prompt) {
        toast.error(i18n.t('Please enter a prompt before generating'))
        return
      }

      // Start a fresh run: invalidate any previous run and clear old state.
      stopPolling()
      revokeObjectUrl()
      const runId = ++runIdRef.current

      setStatus('submitting')
      setError(null)
      setVideoUrl(null)
      setProgress(null)

      const authHeaders = { Authorization: `Bearer ${apiKey}` }

      // 2) Create the generation task.
      let taskId: string | null = null
      try {
        const createResp = await api.post(
          API_ENDPOINTS.VIDEO_GENERATIONS,
          params,
          { headers: authHeaders, skipErrorHandler: true }
        )
        if (runId !== runIdRef.current) return
        taskId = extractTaskId(createResp.data)
      } catch {
        fail(i18n.t('Failed to submit the video generation task, please try again later'), runId)
        return
      }

      if (!taskId) {
        fail(i18n.t('No video task ID was returned; cannot query generation progress'), runId)
        return
      }

      // 3) Poll the task envelope with exponential backoff.
      setStatus('polling')
      const startedAt = Date.now()
      let delay = VIDEO_POLL.initialMs

      while (runId === runIdRef.current) {
        if (Date.now() - startedAt > VIDEO_POLL.timeoutMs) {
          fail(i18n.t('Video generation timed out, please try again later'), runId)
          return
        }

        let envelope: VideoTaskEnvelope
        try {
          const pollResp = await api.get(API_ENDPOINTS.VIDEO_TASK(taskId), {
            headers: authHeaders,
            skipErrorHandler: true,
            disableDuplicate: true,
          })
          if (runId !== runIdRef.current) return

          // Guard: must be HTTP 200 with a `success` envelope code.
          if (pollResp.status !== 200) {
            fail(i18n.t('Failed to query video generation progress, please try again later'), runId)
            return
          }
          envelope = pollResp.data as VideoTaskEnvelope
          if (!envelope || envelope.code !== TASK_SUCCESS_CODE) {
            fail(
              envelope?.message
                ? i18n.t('Failed to query video generation progress: {{message}}', { message: envelope.message })
                : i18n.t('Failed to query video generation progress, please try again later'),
              runId
            )
            return
          }
        } catch {
          // 401 / 4xx / network error mid-poll: abort immediately, never spin.
          if (runId !== runIdRef.current) return
          fail(i18n.t('Failed to query video generation progress, please try again later'), runId)
          return
        }

        const data: VideoTaskData | undefined = envelope.data
        const taskStatus = data?.status ?? ''

        // Progress is display-only.
        if (data?.progress != null) {
          setProgress(data.progress)
        }

        // Terminal: failure.
        if (VIDEO_STATUS_FAILURE.has(taskStatus)) {
          const reason = data?.error || data?.fail_reason
          fail(reason ? i18n.t('Video generation failed: {{reason}}', { reason }) : i18n.t('Video generation failed'), runId)
          return
        }

        // Terminal: success.
        if (VIDEO_STATUS_SUCCESS.has(taskStatus)) {
          const resultUrl = data?.url ?? data?.result_url
          if (!resultUrl) {
            fail(i18n.t('Video generated, but no playable URL was returned'), runId)
            return
          }

          // A `data:` URI can be used directly as the source.
          if (resultUrl.startsWith('data:')) {
            if (runId !== runIdRef.current) return
            setVideoUrl(resultUrl)
            setStatus('success')
            setProgress(null)
            return
          }

          // 区分「需鉴权代理端点」与「免鉴权外链直链」。
          const proxyPath = toSameOriginProxyPath(resultUrl)
          if (!proxyPath.startsWith('/v1/videos/')) {
            // 免鉴权 CDN 直链（Kling/Ali/Doubao/Vidu/Jimeng 等上游直链）——原生 <video>
            // 跨域播放不需要 CORS，直接作为 src；若像代理那样带 Bearer 抓 blob，反而会
            // 触发跨域预检失败，把本可播的视频弄成「加载失败」。
            if (runId !== runIdRef.current) return
            setVideoUrl(resultUrl)
            setStatus('success')
            setProgress(null)
            return
          }
          // newapi 视频代理端点 /v1/videos/<id>/content 需 Bearer，而原生 <video src>
          // 带不了 Authorization —— 归一同源后鉴权抓 blob 再 createObjectURL 播放。
          try {
            const blobResp = await api.get(proxyPath, {
              responseType: 'blob',
              headers: authHeaders,
              skipErrorHandler: true,
              disableDuplicate: true,
            })
            if (runId !== runIdRef.current) return
            // Content-Type 兜底：代理透传若为 octet-stream / 空，<video> 依 Blob.type
            // 判定可播性，非 video/* 会拒绝解码。兜底 MIME 优先取轮询信封的 format
            // （Gemini/Vertex 通道返回完整 mimeType 如 'video/webm'；亦兼容短码），
            // 避免把真实 WebM 硬贴 video/mp4 致严格浏览器(WebKit)拒解码 → 黑屏。
            const rawBlob = blobResp.data as Blob
            const fmt = (data?.format ?? '').toLowerCase()
            const fallbackType = fmt.startsWith('video/')
              ? fmt
              : fmt === 'webm'
                ? 'video/webm'
                : fmt === 'mov'
                  ? 'video/quicktime'
                  : 'video/mp4'
            const playableBlob =
              rawBlob.type && rawBlob.type.startsWith('video/')
                ? rawBlob
                : new Blob([rawBlob], { type: fallbackType })
            const objectUrl = URL.createObjectURL(playableBlob)
            objectUrlRef.current = objectUrl
            setVideoUrl(objectUrl)
            setStatus('success')
            setProgress(null)
          } catch {
            fail(i18n.t('Failed to load the video, please try again later'), runId)
          }
          return
        }

        // In-progress: back off and poll again. The wait can be settled early
        // by stopPolling (reset / unmount / re-submit) via pollResolveRef, so a
        // cancelled backoff never leaves a hanging Promise.
        await new Promise<void>((resolve) => {
          const timerId = window.setTimeout(() => {
            pollTimerRef.current = null
            pollResolveRef.current = null
            resolve()
          }, delay)
          pollTimerRef.current = timerId
          pollResolveRef.current = resolve
        })
        if (runId !== runIdRef.current) return
        delay = Math.min(delay * 2, VIDEO_POLL.maxMs)
      }
    },
    [stopPolling, revokeObjectUrl, fail]
  )

  return { submit, status, progress, videoUrl, error, reset }
}
