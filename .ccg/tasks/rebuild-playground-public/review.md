# 审查与修复记录 — Playground 公开创作页重建

Workflow ③ 4 审查员并行（契约一致 / 生成逻辑 / 令牌+base-ui+中文 / 路由门控），380k tokens。

## Critical（已全部处理）
1. **use-stream-request stale closure**（token-i18n + route-gating 双报）— `apiKey` 未进 `sendStreamRequest` 依赖 → 切 key 后 SSE 带旧/空凭据 → 401。**修**：`apiKeyRef` + `useEffect` 同步，header 读 `apiKeyRef.current`（不重建函数）。
2. **video-workspace 双提交** — `PromptInputSubmit` 同时 `type=submit`(form onSubmit)+`onClick` → 双发 create → 双计费。**修**：删 `onClick`，与 image-workspace 一致（纯 form 提交）。
3. **video-workspace `bg-black`** 硬编码 — **修** → `bg-muted`。
4. **video-workspace `rgba(0,0,0,.65)` 阴影** — **修** → `shadow-lg`。
5. **constants 英文错误串直显（token-i18n 报）— 误报**。核实 `use-chat-handler.ts:141-155 getDisplayError` 对已知串一律 `t(error)`，显示层全经翻译，`zh.json` 35 键已中文 → 渲染即中文。英文值是 i18n key，**不可改**（改则破坏 key 匹配）。**不动**。

## Warning（高价值已修，其余记录）
- **未登录 401**（route-gating）— 未选模型默认渲染 ChatWorkspace → `usePlaygroundOptions.getUserGroups` 无 enabled 守卫 → 未登录访客弹 401 toast。**修**：hook 加 `enabled?` 参数；chat-workspace 传 `enabled: isAuthed`。
- **video sleep 取消后 Promise 永挂**（genlogic + route-gating）— clearTimeout 后 resolve 永不触发，异步帧+闭包泄漏。**修**：`pollResolveRef`，stopPolling 主动 settle；退避改可中止 inline Promise，删除旧 `sleep`。
- **图片跨域 url 下载失效**（genlogic）— `<a download>` 对跨域 url 被忽略。**修**：改按钮 + 鉴权 blob 下载（data: 直下 / 代理带 Bearer / CDN 兜底新开）。
- **gating-alert 文字色被内部 text-muted-foreground 覆盖** — **修**：`AlertDescription` 加 `text-warning-foreground`。
- **顶栏 `Playground` 英文 defaultValue**（W5）— **修** → `创作平台`。
- **ChatWorkspace 内 PlaygroundInput 可绕过目录换模型**（不一致）— 记录，非 bug（聊天自带模型选择器，目录选择下压为默认）。MVP 可接受。
- **image PromptInput 无整体 disabled，Enter 可旁路**（被 `!apiKey` guard 拦截，不真发请求）— 现有 guard 已足够，未额外改。

## Info（记录/已顺手处理）
- 空目录 `_authenticated/playground/` — **已 rmdir**。
- 门控提示在 keysLoading 期间闪烁 — **修**：`gatingMessage && !keysLoading`。
- video `pollResp.status!==200` 死代码（axios 默认非 2xx 即 reject，走 catch）— 保留作防御。
- video `response_format` 塞 `720p` 语义错配 — 记录，取决上游宽容度。
- hooks barrel 未 re-export 新 hook（均相对路径直接 import）— 无碍。
- shell/CreateKeyButton 的 useAuthStore 用法风格不统一 — 无碍。

## 复查
- 新组件/hooks/context 全量色扫描：**零硬编码 hex/rgb/命名色阶/bg-black**。
- `sleep` 无悬挂引用。
- 落盘范围：改 6 + 删 1 + 新 15（不含 .ccg）。

## 未决（需用户）
- **服务器构建验证**（DoD#8）：本机无前端构建链（rsbuild/tsgo），typecheck/build/routeTree.gen.ts 重生成必须在服务器（W4）。待用户定部署时机：rsync→/root/newapi-test→`build:check`（`tsgo -b && rsbuild build`）。
- **A2 视频渠道**活体验证（Gemini 直链 vs Sora 代理 URL）——代码两路已覆盖，上线后确认。
