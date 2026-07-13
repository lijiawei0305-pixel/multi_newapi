# 实施计划 — Playground 公开创作页重建

> 权威契约。所有 Builder 严格对此实现，签名不得擅改。前端唯一目标 `web/default`。本机无构建链，typecheck/build 在服务器（W4）。

## 0. 关键事实（测绘核实）
- 现有 `features/playground/index.tsx` = 纯聊天页，**无任何 Tab**（智能体/灵感广场从不存在，无需删除）。
- `usePricingData()` 返回 `{ models, isLoading, error, refetch, ... }`；`PricingModel` 含 `model_name/description/icon/vendor_icon/quota_type/model_ratio/group_ratio/enable_groups/tags/supported_endpoint_types?`。
- `filterByEndpointType` 单值匹配 → 聊天桶(OR)需自建 `filterByCapability`。
- keys：`getApiKeys({p,size})→{success,data:{items,total,page,page_size}}`；`ApiKey{id,name,key(脱敏),status,group,remain_quota,unlimited_quota,...}`；`fetchTokenKey(id)→{success,data:{key}}`（裸串，拼 `sk-${key}`）；`API_KEY_STATUS{ENABLED:1,DISABLED:2,EXPIRED:3,EXHAUSTED:4}`。
- 路由：`PublicLayout`/`PublicHeader` from `@/components/layout`；`<PublicLayout showMainContainer={false}>` 做全屏自定义布局，PublicHeader（含 控制台/头像/登录）自动渲染。`useAuthStore()`→`const { auth }=useAuthStore(); const isAuthed=!!auth.user`。
- UI：`@base-ui/react` 非 Radix；Alert 无 warning variant；Card 无 data-card-hover；Badge `rounded-4xl`；feature 层图标用 `lucide-react`；`cn()` from `@/lib/utils`。

## 1. 决策（默认已定，除非用户改）
- D-cap：工作区由选中模型能力驱动（video>image>chat 优先），chips 仅筛目录。
- D-key：懒揭示——下拉切换只存 id+name，点「发送/生成」时才 `fetchTokenKey` 并缓存。
- D-create：「创建 API 密钥」= 深链跳密钥管理页（不在公开树挂 ApiKeysProvider）。
- D-cred：`PlaygroundCredentialContext` 提供当前 `sk-`，`use-stream-request` 消费注入 Authorization。
- D-chip：chips 用 Button 组（active=`variant="secondary"`，非 active=`variant="outline"`）。
- D-ratio：卡片「倍率」显示 `model_ratio`（A4 默认）。
- D-group：选中 key 后目录按 `key.group` 过滤（`enable_groups.includes(group)`）；未选按全部/公开组。
- D-i18n：复用聊天串 → 加 `zh.json` 条目；新组件直接写中文。
- D-badge：能力徽章严格中性（`variant="secondary"|"outline"`）。

## 2. 分层与文件归属（互不重叠；EDIT=改，NEW=新建，DEL=删）

### Layer 0 — 基础（无内部依赖，可全并行）
- **L0-const** EDIT `features/playground/constants.ts`：`API_ENDPOINTS.CHAT_COMPLETIONS`→`'/v1/chat/completions'`；新增 `IMAGES_GENERATIONS:'/v1/images/generations'`、`VIDEO_GENERATIONS:'/v1/video/generations'`、`VIDEO_TASK:(id)=>\`/v1/video/generations/\${id}\``；新增 `VIDEO_POLL={initialMs:2000,maxMs:5000,timeoutMs:180000}`；新增 `VIDEO_STATUS_SUCCESS=new Set(['SUCCESS','succeeded','completed'])`、`VIDEO_STATUS_FAILURE=new Set(['FAILURE','failed'])`；新增 `TASK_SUCCESS_CODE='success'`。**勿删** USER_MODELS/USER_GROUPS（chat 仍用）。
- **L0-types** EDIT `features/playground/types.ts`（末尾追加，不动现有）：见 §3。
- **L0-cap** NEW `features/playground/lib/capabilities.ts`：见 §3。
- **L0-i18n** EDIT `web/default/src/locales/zh.json`（若路径不同，Builder 先定位 zh 语言包）：为 §6 列出的复用聊天键补中文值。

### Layer 1 — hooks/context（依赖 L0）
- **L1-cred** NEW `features/playground/context/credential-context.tsx`：`PlaygroundCredentialProvider`、`usePlaygroundCredential()→{apiKey:string|null,setApiKey}`。
- **L1-key** NEW `features/playground/hooks/use-playground-keys.ts`：`usePlaygroundKeys(enabled)` + `useKeyReveal()`（见 §4）。
- **L1-catalog** NEW `features/playground/hooks/use-model-catalog.ts`：`useModelCatalog({group,filter,search})`（见 §4）。
- **L1-image** NEW `features/playground/hooks/use-image-generation.ts`（见 §4）。
- **L1-video** NEW `features/playground/hooks/use-video-generation.ts`（见 §4，含轮询+归一+blob 鉴权）。
- **L1-stream** EDIT `features/playground/hooks/use-stream-request.ts`：`import { usePlaygroundCredential }`，在 `new SSE(...)` 的 headers 里 `...(apiKey?{Authorization:\`Bearer \${apiKey}\`}:{})`。端点已由 L0 常量改为 /v1。

### Layer 2 — 组件（依赖 L1 + ui）
- **L2-keysel** NEW `components/public/key-selector.tsx`：顶栏下拉。props `{keys,selectedId,onSelect,isAuthed,loading}`。仅 `status===1` 可选，其余灰显 + 状态标签。未登录显示「请先登录后选择 API 密钥」。用 `DropdownMenu` 或 `Select`。
- **L2-createkey** NEW `components/public/create-key-button.tsx`：`Button variant="link"` → `Link`/`navigate` 到密钥管理路由（Builder 定位实际 keys 路由，如 `/console` 下 keys 页）。
- **L2-catalog** NEW `components/public/model-catalog.tsx`（含内部 `ModelCard`）：左栏，props `{filter,onFilter,search,onSearch,models,selectedModel,onSelect,loading}`。标题「AI 大模型聚合平台」、`Input h-8`搜索、Button chips(全部/聊天/图片/视频)、`ScrollArea` 包 `Card` 列表。卡片：图标(`model.icon??model.vendor_icon`)+名称+能力`Badge`(中性)+描述+「倍率 {model_ratio}」。
- **L2-intro** NEW `components/public/model-intro-card.tsx`：右侧介绍卡，props `{model}`；空态提示「从左侧选择模型」。
- **L2-gate** NEW `components/public/gating-alert.tsx`：`Alert` + `className="border-warning/30 bg-warning/10 text-warning-foreground"`，文案由 props。
- **L2-image** NEW `components/public/image-workspace.tsx`：props `WorkspaceProps`。prompt 输入(`PromptInput`)+n/size 控件+`useImageGeneration`；结果 `n>1` 网格；`b64_json`→`data:image/png;base64,`，`url`→直接 `<img>`；每图下载按钮。
- **L2-video** NEW `components/public/video-workspace.tsx`：props `WorkspaceProps`。prompt+可选参数+`useVideoGeneration`；轮询态显示「生成中…（{progress}）」；成功 `<video controls>`；失败「生成失败：{error}」。
- **L2-chat** NEW `components/public/chat-workspace.tsx`：props `WorkspaceProps`。**抽取现有 index.tsx 的聊天主体**（PlaygroundChat+PlaygroundInput 组合）迁入此文件为 `ChatWorkspace`；发送走已改的 use-stream-request（凭据来自 context）；无 key 时禁用发送。

### Layer 3 — 装配/路由（依赖 L2）
- **L3-shell** REWRITE `features/playground/index.tsx`：导出 `PlaygroundPublic`。`<PublicLayout showMainContainer={false}>` → `<PlaygroundCredentialProvider>` → 页面工具条(标题/`KeySelector`/`CreateKeyButton`) + 主体两栏(`ModelCatalog` | 右侧 `ModelIntroCard`+当前能力 `*Workspace`)。状态机：`selectedKeyId/revealedKey/filter/search/selectedModel`；门控见 §5。未登录/未选 key 时渲染 `GatingAlert` 并禁用发送。
- **L3-route** NEW `routes/playground/index.tsx`：`createFileRoute('/playground/')({ component: PlaygroundPublic })`，**无 beforeLoad**。`import { PlaygroundPublic } from '@/features/playground'`。
- **L3-del** DEL `routes/_authenticated/playground/index.tsx`（必须删，否则路由冲突）。
- **L3-nav** EDIT `hooks/use-top-nav-links.ts:75-82`：注释「认证路由，未登录点击由路由守卫跳转登录…」→「公开路由，未登录可直接访问」；`href:'/playground'` 不变。
- **勿手改** `routeTree.gen.ts`（服务器构建时 router-plugin 重生成）。

## 3. 共享类型契约（L0-types / L0-cap）
```ts
// types.ts 追加
export type PlaygroundCapability = 'chat' | 'image' | 'video'
export type CatalogFilter = 'all' | PlaygroundCapability
export interface WorkspaceProps { apiKey: string; model: string }        // shell 保证渲染时 apiKey 非空
export interface ImageGenParams { model: string; prompt: string; n?: number; size?: string; response_format?: 'url'|'b64_json' }
export interface ImageResultItem { url?: string; b64_json?: string; revised_prompt?: string }
export interface ImageGenResponse { created: number; data: ImageResultItem[] }
export interface VideoGenParams { model: string; prompt: string; image?: string; duration?: number; width?: number; height?: number; fps?: number; seed?: number; n?: number; response_format?: string }
export interface VideoTaskData { task_id: string; status: string; url?: string; result_url?: string; format?: string; progress?: string; error?: string; fail_reason?: string; metadata?: unknown }
export interface VideoTaskEnvelope { code: string; message: string; data?: VideoTaskData }

// lib/capabilities.ts
export function getModelCapabilities(m: PricingModel): PlaygroundCapability[]  // 基于 supported_endpoint_types
export function getPrimaryCapability(m: PricingModel): PlaygroundCapability     // video>image>chat；无 → 'chat'
export function filterByCapability(models: PricingModel[], f: CatalogFilter): PricingModel[]  // all=不筛；chat=OR(openai/openai-response/anthropic/gemini)
export const CATALOG_FILTERS: { value: CatalogFilter; label: string }[]  // [全部,聊天,图片,视频]
```

## 4. Hook 契约（L1）
```ts
usePlaygroundKeys(enabled: boolean): { keys: ApiKey[]; isLoading: boolean; error: Error|null }
  // getApiKeys({p:1,size:100})；enabled=isAuthed；返回全部（含非启用，UI 灰显）
useKeyReveal(): { reveal:(id:number)=>Promise<string>; revealing:boolean }
  // 内部 Map<number,string> 缓存 sk-；未命中→fetchTokenKey→`sk-${data.key}`→缓存；并发去重
useModelCatalog(opts:{ group:string|null; filter:CatalogFilter; search:string }):
  { models: PricingModel[]; isLoading:boolean; error:Error|null; counts:Record<CatalogFilter,number> }
  // usePricingData → group 过滤(enable_groups.includes(group) 或全部) → filterByCapability → filterBySearch；counts 各桶计数
useImageGeneration(): { generate:(apiKey:string,p:ImageGenParams)=>Promise<void>; status:'idle'|'loading'|'success'|'error'; images:ImageResultItem[]; error:string|null; reset:()=>void }
  // api.post('/v1/images/generations', body, {headers:{Authorization:`Bearer ${apiKey}`}})
useVideoGeneration(): { submit:(apiKey:string,p:VideoGenParams)=>Promise<void>; status:'idle'|'submitting'|'polling'|'success'|'error'; progress:string|null; videoUrl:string|null; error:string|null; reset:()=>void }
  // create→env.code==='success' 否则中止报错→取 data.task_id(PublicTaskID)→轮询 GET /v1/video/generations/:task_id
  // 每次先判 HTTP200 && code==='success'；归一 status(SUCCESS/succeeded/completed=成功, FAILURE/failed=失败, 余=进行中)
  // 成功地址=data.url ?? data.result_url；退避 2s→5s，超时 180s；401/非success信封→中止+中文 toast
  // 播放：data: URI→直接；否则 fetch(url,{headers:{Authorization}}) 取 blob→URL.createObjectURL；reset/unmount 撤销 objectURL
```

## 5. 门控状态机（L3-shell）
| 状态 | 判定 | 下拉 | 目录 | 发送 | 提示 |
|---|---|---|---|---|---|
| 未登录 | `!auth.user` | 「请先登录后选择 API 密钥」禁用 | 可浏览(pricing 公开时) | 禁用 | 请先登录并选择 API 密钥 |
| 已登录·未选 | `auth.user&&!selectedKey` | 可选(仅 status=1) | 可浏览/可选 | 禁用 | 请先在顶部选择 API 密钥后再生成 |
| 已登录·已选 | `auth.user&&revealedKey` | 显示 key 名 | 按 group 过滤 | 启用 | 无 |

## 6. zh.json 待补键（L0-i18n，值=中文）
ERROR：`Request error occurred`=请求发生错误｜`Network connection failed or server not responding`=网络连接失败或服务器无响应｜`Error parsing response data`=解析响应数据失败｜`Error establishing connection`=建立连接失败｜`Connection closed`=连接已关闭｜`Generation was interrupted`=生成已中断
ACTION：`Copy`=复制｜`Copied!`=已复制！｜`Regenerate`=重新生成｜`Show preview`=显示预览｜`Show source`=显示源码｜`Edit`=编辑｜`Delete`=删除｜`No content to copy`=没有可复制的内容｜`Please wait for the current generation to complete`=请等待当前生成完成
组件：`Open menu`=打开菜单｜`Model Price Not Configured`=模型价格未配置｜`Go to Settings`=前往设置｜`Error`=错误｜`Retry`=重试｜`Start a playground chat`=开始体验对话｜`Test a model with a starter prompt, or write your own request below.`=用示例提示词测试模型，或在下方输入你的请求。｜`Analyze data`=分析数据｜`Summarize text`=总结文本｜`Code`=写代码｜`Get advice`=获取建议｜`Conversation cleared`=对话已清空｜`Attach`=附件｜`Search`=搜索｜`Clear chat history`=清空聊天记录｜`Clear chat history?`=清空聊天记录？｜`All playground messages saved in this browser will be removed. This cannot be undone.`=本浏览器保存的所有对话消息将被删除，此操作不可撤销。｜`Clear`=清空｜`Upload file`=上传文件｜`Upload photo`=上传图片｜`Take screenshot`=截图｜`Take photo`=拍照｜`Feature in development`=功能开发中｜`Search feature in development`=搜索功能开发中｜`Response time: {{duration}}`=响应耗时：{{duration}}
（Builder 先确认 zh.json 里这些键是否已有值；有则跳过，无则补。）

## 7. 文案（中文，W5）
标题`AI 大模型聚合平台`｜搜索占位`搜索模型`｜chips`全部/聊天/图片/视频`｜徽章`聊天/图片/视频`｜顶栏`选择 API 密钥`/`创建 API 密钥`｜未登录`请先登录后选择 API 密钥`｜门控`请先在顶部选择 API 密钥后再生成`｜输入占位`输入提示词…`｜按钮`生成`(聊天`发送`)｜视频`生成中…（{progress}）`/`生成失败：{error}`。

## 8. 验收映射（DoD §12）
1 未登录 /playground 渲染不跳登录=L3-route(无守卫)+L3-shell(未登录框架) ｜ 2 无 Tab/无音频 chip=天然+L0-cap ｜ 3 密钥仅 status=1+揭示 sk-+门控=L2-keysel+L1-key+L3-shell ｜ 4 聊天/v1、图片同步网格、视频 code+归一+blob=L1-stream/image/video+L2 ｜ 5 全走 /v1 无旁路 ｜ 6 令牌+ui+中文=全体 ｜ 7 routeTree 未手改+注释更新=L3-nav ｜ 8 服务器 typecheck/build 通过。

## 9. 编排（Claude workflows）
Workflow ②：L0(4 并行)→barrier→L1(cred 先，余 5 并行)→barrier→L2(7 并行)→barrier→L3(shell 单 agent，含 route/del/nav)。每 Builder 严格文件范围，禁改范围外。
Workflow ③：并行审查（令牌纪律/中文完整/视频轮询归一/blob 鉴权/门控/契约一致）→ 我修 Critical。
验证：服务器 rsync→build（本机无链，遵 W4）——用户定时机。
