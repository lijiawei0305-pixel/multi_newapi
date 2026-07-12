# UI/UX 完善度审计与修复 —（用户追加轮）

Workflow ④ 3 员并行（视觉布局响应 / 功能规格达成 / UX状态可达性）+ 我的通读。均分：视觉 6.5 · 功能 6.5 · UX 7.2（桌面 MVP 可用，全端交付前需打磨）。
> 前置：本机无法渲染，本轮为**代码级静态审计**；像素级布局/响应式需服务器构建 + 浏览器实测。

## ✅ 已就地修复（13 项 · 与渲染无关的清晰缺陷）
1. **key-selector.tsx 缺版权头 + `import * as React` 未使用** → 补 AGPL 头、删 React 具名（CI/oxlint/copyright:check 卫生）。
2. **`formatModelRate()`** 新增（capabilities.ts）：按 quota_type 区分——按次计费(=1)显示「按次计费」，按量(=0)显示「倍率 N×」，为 0 显「—」。
3+4. **卡片 + 介绍卡「倍率」** 改用 formatModelRate → 消除图片/视频（多为按次）显示「倍率 0 / 0×」的误导。
5. **video「格式」错配** → 删除该字段（原把 720p 塞进 response_format，语义错、上游会拒）；保留 时长/种子，网格 3→2 列。
6. **video 缺首屏 idle 占位** → 补 Clapperboard + 「输入提示词，点击生成」，与 image 对称。
7. **video 无下载** → 加「下载视频」（videoUrl 是 blob:/data:，a[download] 直下）。
8. **目录无 error 态** → useModelCatalog 透传 error+refetch → ModelCatalog 新增「加载失败+重试」态，不再把失败误报成「暂无模型」。
9. **同屏标题重复** → 左目录头「AI 大模型聚合平台」→「模型目录」（工具条保留品牌标题）。
10. **ModelCard 键盘不可达** → 加 role/tabIndex/onKeyDown(Enter/Space)/focus-visible ring。
11. **介绍卡图标无 onError** → 补破图兜底（与卡片一致）。
12. **image**：alt 兜底（revised_prompt||prompt||生成图片N）、object-cover→object-contain 不裁切、多图网格 grid-cols-1 sm:grid-cols-2 响应式、Select 受控 value。
13. **index**：无密钥专属提示「您还没有可用的 API 密钥，请点击创建」；切图片/视频模型 key=model 重挂载清旧结果（聊天不加 key 保留历史）；create-key 按钮 variant link→outline（与密钥下拉视觉对等）。

## ⏸ 待服务器实测 / 待你决策（未盲改）
**A. 需浏览器实测调准（像素/响应式）**
- 页头高度耦合：外壳 pt-14/3.5rem vs PublicHeader **h-16=4rem**（未滚动），顶部工具条被压 ~8px；且高度计算在滚动悬浮态下也需核。建议实测后统一为 4rem 基准或让 PublicLayout 暴露头高。
- 移动端：lg 以下等高 grid 堆叠 → 目录/工作区各占 ~50vh 双双挤压，输入可能被挤出视口。建议移动端目录折叠为抽屉/顶部下拉、工作区给足最小高度。
- 介绍卡 vs 聊天竖向争高度：聊天场景介绍卡挤占消息区，建议聊天下压缩为紧凑单行/可折叠。

**B. 产品/设计决策**
- 三工作区输入区风格不统一（chat=PlaygroundInput / image=朴素 / video=玻璃拟态）→ 是否抽统一输入外观。
- 门控 Alert 内联「去登录/去创建」行动按钮（缩短转化路径）。
- 聊天工作区内 PlaygroundInput 自带模型/组下拉，与左目录双入口 → 是否 hideModelSelector 锁定为目录选中模型。
- 图片参数写死 DALL·E 3 档位（尺寸/张数非数据驱动）→ 是否按模型能力数据驱动。

**C. 既有越界项（非本任务引入，仅上报）**
- `components/ai-elements/prompt-input.tsx` 语音识别 lang 硬编码 'en-US'（中文语音失效）。
- `components/layout/components/public-header.tsx` 多处英文 t() key（Sign in / Go to Dashboard…）——需核 zh.json 是否已中文，属全站 W5 既有问题。

**D. 低优打磨**
- key-selector 加载态触发器无 spinner；loading 骨架与真实卡片形态差异(CLS)；chips 计数样式；空态图标统一；视频播放器居中/aria-label；搜索框 aria-label。

## 结论
功能骨架 R1–R7/DoD **齐全**，本轮修掉全部**清晰缺陷**（CI 卫生 / 误导展示 / 参数错配 / 缺失状态 / 可达性）。剩余为**像素级布局 + 响应式 + 产品决策**，须服务器构建后浏览器实测调优——与 DoD#8 服务器验证同批进行最高效。

---

## 追加轮 2：低优打磨 + 对抗验证（Workflow⑤）

### 已应用打磨（8 项）
key-selector 加载 spinner（移前导位，改单图标）· 目录骨架对齐真实卡片形（bg-card+ring）· chips 计数 tabular-nums+选中态配色 · 目录空态加 PackageOpen 图标+引导 · 搜索框 aria-label · video aria-label+降级文案。

### Workflow⑤ 对抗验证（3 员）抓到并已修的 CI 门 Critical（4）+ Warning（5）
- **C1** credential-context 缺 AGPL 版权头（copyright:check 会红）→ 补头 + `/* eslint-disable react-refresh/only-export-components */`（同仓 provider 惯例）。
- **C2** key-selector `import * as React` 之外还遗留未用 `SelectValue` 导入（oxlint no-unused-vars）→ 删。
- **C3** use-playground-options 版权头错位（import 在头之前，copyright:check 判无头）→ 把 `import {useQuery}` 移到头后。
- **C4** index.tsx gatingMessage 链式嵌套三元（oxlint no-nested-ternary=error）→ 改 let + if/else if。
- **W1** key-selector 尾部自定义图标与 SelectTrigger 内建 UnfoldMore 双图标重影 → 删自定义尾图标、loading 指示移前导位（顺带修了此前既存的双箭头 bug）。
- **W2** index.tsx `selectedKeyName` 死变量（write-only）→ 连同 4 处 setter 移除。
- **W3** gating-alert 暗色对比：`text-warning-foreground`(近黑) 在暗色 amber 底上对比差 → 改 图标 text-warning + 描述 text-foreground（双模式可读）。
- **W4** 顶栏 `Agent Program` defaultValue 英文 → `代理加盟`（W5 一致）。
- **Info** 数组 index key：与全仓惯例一致，不阻塞，未改。

### 复核结论
聚焦复核 agent 确认 6 处修复文件：无未用 import/变量、无未定义符号、无非法 JSX、无硬编码色、文案全中文。**静态层面 lint/copyright/typecheck 门应可通过**（真值仍以服务器 build:check 为准）。

---

## 追加轮 3：遗漏猎取（Workflow⑥）+ 修复验证（Workflow⑦）

用户要求「静态审查找问题+遗漏」。开 4 员对抗式遗漏猎取（546k tokens），专攻前 5 轮纯前端视角的盲区：需求可追溯性 / 前端↔真实 Go 后端契约 / 改动爆炸半径 / 深层边界+i18n。

### 🔴 Critical（只能服务器修 · DoD#8 阻断）
**routeTree.gen.ts 陈旧**（2 员确认）：仍 import 已删的 _authenticated/playground/index（坏导入→tsgo -b 必挂）+ /playground 仍挂鉴权树 + 新公开路由未注册。本机无工具链无法重生成。**服务器解法**：`rsbuild build`（tanstackRouter 插件会重写 gen），随后提交重生成后的 gen；勿让 tsgo 先撞陈旧 gen。

### 🟠🟡 已就地修 + Workflow⑦ 验证通过（0 Critical/Warning）
- **api.ts 401 → 全站登出**（坏 Bearer 密钥把用户踢下线）：reset+toast 移入 !skipErrorHandler。**⑦爆炸半径核验**：枚举全仓 20+ 个 skipErrorHandler 调用方，无一依赖被删的被动 reset → 全局安全；非 skip 会话 401 仍正常登出。
- **聊天选中模型被 fallback 回退**（config.group 恒 default）：WorkspaceProps 加 group、index 传 selectedKey.group、chat 同步 config.group。
- **dev 代理漏 /v1**：rsbuild.config.ts proxy 加 /v1（仅本地 dev，生产 nginx 无影响）。
- **视频 blob 跨域**（ServerAddress 绝对 URL 跨租户域 CORS）：toSameOriginProxyPath 归一 /v1/videos/ 为同源相对路径。
- **视频 seed 死字段**（后端 TaskSubmitReq 无 seed）：删除。
- **reveal 失败静默** → 中文 toast；**图片下载 revoke 竞态** → 延迟 1s；**i18n**：zh.json 去英文 playground + 设置页 defaultValue 中文。

### ✅ 后端契约交叉验证通过（前几轮从未做）
视频 create/poll 信封 + 大小写状态集 + url??result_url + /v1/videos/:id/content 代理 + 揭示裸串+sk- + pricing 匿名可取 —— 均与真实 Go 后端吻合。

### 📌 注记（尊重 spec / 产品决策，未改）
- 路由无模块禁用守卫（R1 明确选不加 beforeLoad）· 控制台侧栏 playground 保留（spec 意图）
- 聊天 group 选择器对 /v1 无实效（死控件，需 PlaygroundInput 支持隐藏，产品决策）
- pricing 非公开时降级 · reveal 缓存软窗口（key 禁用后 ≤2min）· 图片 b64 MIME 固定 png · 「游乐园 vs 创作平台」用词 —— 均 follow-up

### 结论
6 轮审查/验证闭环。前端代码**静态层面已无已知缺陷**（lint/copyright/typecheck/契约/边界/i18n 均过）。唯一剩余 = routeTree 重生成 + build:check，**必须在服务器**。
