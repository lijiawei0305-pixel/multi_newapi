# 设计规格 · 文档页 `/docs`（忠实克隆 tokenhub /docs）

- 日期：2026-07-05
- 分支：`feat/docs-page`
- 状态：已通过设计评审，待写实现计划
- 参考页：`https://tokenhub.todoucloud.com/docs`（品牌显示为「灵渠 AI」）

## 1. 目标

在本平台新增一个**公开、免登录**的 `/docs` 文档页，**忠实克隆**参考页的结构与观感，内容替换为本项目信息。页眉的「文档」导航链接已存在（`use-top-nav-links.ts`），建好页面即点通。

**非目标（Out of scope）：** 不做完整操作手册 / 左侧栏文档中心（参考仓库 `TOKEN HUB 文档.md` 的十章手册）；不改后端；不新增路由守卫；不动锁定文件（`zh.json`/`en.json`、支付相关文件等）。

## 2. 参考页结构（克隆对象）

单列居中、卡片式、深色（本项目做深浅色自适应）。**头部 + 四段内容**：

- **头部**：眉标 `DOCUMENTATION` → 标题「使用文档」→ 副标题「TOKEN HUB API 接入指南」
1. **快速开始**：一句话 + 代码框 `https://your-domain.com/v1`
2. **接入步骤**：4 张编号卡（01 注册账号 / 02 获取令牌 / 03 替换地址 / 04 选择模型）
3. **示例代码**：Python OpenAI SDK 调用示例
4. **联系支持**：邮箱 + 微信（参考页微信为空占位）

## 3. 已确认决策（评审拍板）

| 项 | 决策 |
| --- | --- |
| 方案范围 | **A 忠实克隆**：只做上述 4 段，内容换成本项目，单页轻量 |
| 示例 base_url | **动态当前域名**：`window.location.origin + '/v1'`，多租户下代理站自动显示自己的域名 |
| 联系支持 | **只放微信** `chen13477359255`，无邮箱、无二维码 |

## 4. 文件计划（2 新文件，0 改 header）

- `web/default/src/routes/docs/index.tsx` — 路由外壳：`createFileRoute('/docs/')({ component: Docs })`，加项目版权头；因在 `routes/` 顶层而**公开免登录**。`routeTree.gen.ts` 由插件自动重生，勿手改。
- `web/default/src/features/docs/index.tsx` — 页面本体，导出具名组件 `Docs`。用 `<PublicLayout>` 包裹 + 复用 `<Footer/>`（与 `features/agent-join`、`features/about` 同构）。

**接线前提**：后台系统设置 `docs_link` 需留空——留空时导航「文档」指向内部 `/docs`；若被设为外链，导航会走外链而非本页。部署时确认留空即可（不改代码）。

## 5. 页面结构与内容（逐段，最终中文文案）

外层：`<PublicLayout>`（自带 `container` 居中、`pt-20` 避让浮动页眉）。内容单列**不额外收窄**——实测参考页内容宽度 ≈ `container`（约 1024–1032px，比 `max-w-4xl` 宽），沿用 PublicLayout 默认容器即可对齐；各段之间大间距（`space-y-16`）。

### 头部
- 眉标：「开发文档」（**中文**，合 W5；不用英文 `DOCUMENTATION`）
- 标题（h1）：「使用文档」
- 副标题：「{站点名称} API 接入指南」——读动态站点名（status/system 名），取不到则回退「API 接入指南」
- 下方分隔线

### ① 快速开始
- 卡片文案：「将您的 OpenAI SDK `base_url` 替换为以下地址即可接入：」
- 代码框（等宽、`bg-muted` 圆角）：`{origin}/v1`，右上角**复制按钮**

### ② 接入步骤（4 张编号卡：大号淡色序号 + 粗标题 + 灰描述）

| 序号 | 标题 | 描述 |
| --- | --- | --- |
| 01 | 注册账号 | 在本站注册账号，或通过代理邀请链接 / 邀请码加入 |
| 02 | 获取令牌 | 进入 控制台 → 令牌，新建令牌并复制 `sk-xxx` |
| 03 | 替换地址 | 将 `base_url` 换成本站地址，请求头 `Authorization: Bearer 你的令牌` |
| 04 | 选择模型 | 在「模型广场」查看可用模型，把名称填入 `model` 参数 |

### ③ 示例代码（深色代码块 + 复制按钮，base_url 注入当前域名）
```python
from openai import OpenAI

client = OpenAI(
    api_key="sk-your-token",
    base_url="{origin}/v1",  # origin 已含 https://，运行时注入 window.location.origin
)

response = client.chat.completions.create(
    model="gpt-4o",  # 以模型广场实际在售模型为准
    messages=[{"role": "user", "content": "你好"}],
)
print(response.choices[0].message.content)
```

### ④ 联系支持（一张卡）
- 文案：「如有问题，请通过以下方式联系我们：」
- 「微信：`chen13477359255`」+ 复制按钮
- **仅微信**，无邮箱 / 二维码

## 6. 动态域名行为
- `const origin = typeof window !== 'undefined' ? window.location.origin : ''`
- 「快速开始」代码框与「示例代码」块的 base_url 都用 `${origin}/v1`
- SSR/首帧无 `window` 时回退空串或占位，客户端水合后填充真实域名

## 7. i18n / W5
- 所有面向用户文案走 `t('Docs Xxx', { defaultValue: '中文文案' })`，**不改锁定的 `zh.json`/`en.json`**，即时渲染中文
- 导航键 `Docs` 已有（`zh.json`→「文档」），无需新增
- 交付前肉眼核对页面**显示**全中文、无英文残留（含眉标）

## 8. 排版规格（从参考页实测）与样式

**布局与节奏**
- 内容列 = PublicLayout `container` 居中（≈1024px 宽，**不要** `max-w-4xl` 收窄，否则比参考页窄）
- 纵向节奏大：每个二级区块间距 ≈ `mt-16`（64px）；区块标题↔卡片 ≈ `mt-4~6`；步骤卡之间 ≈ `gap-3~4`
- 顶部标题区到首个区块留白充足

**字体层级**
| 元素 | 规格 |
|---|---|
| 眉标 | `text-xs tracking-widest text-muted-foreground`（W5：用中文「开发文档」；见 §9 开放项，若要与参考页完全一致改回 `DOCUMENTATION`） |
| 主标题「使用文档」 | `text-4xl md:text-5xl font-bold text-foreground` |
| 副标题 | `text-base text-muted-foreground` |
| 分隔线 | 标题区下方一条 `border-border` 细线 |
| 区块标题 | `text-xl md:text-2xl font-semibold` |
| 步骤序号 01–04 | `text-2xl font-bold text-muted-foreground/40`（大号淡灰） |
| 步骤标题 / 描述 | 标题 `font-semibold text-foreground`；描述 `text-sm text-muted-foreground` |
| 正文 / 联系行 | `text-sm~base text-muted-foreground`（标签「邮箱/微信」同色） |

**卡片与代码**
- 卡片：shadcn `<Card>`（`border-border` + `rounded-xl` + `p-6`），深色下几乎透明、靠边框勾勒（即参考页观感）
- 快速开始卡：卡内嵌一个更暗的**代码 chip**（`bg-muted rounded-md px-4 py-3 font-mono`）显示 `{origin}/v1`
- 示例代码：整块 `<Card>` 内 `<pre><code>` 纯等宽（**不做语法高亮**，参考页就是纯灰等宽）、`overflow-x-auto`
- 复制按钮：卡右上角 `lucide-react` Copy 图标 + 成功 toast

**共享框架（已与本站一致，无需另做）**
- 页眉：复用浮动、随滚动收缩的 `PublicHeader`（与参考页同款交互）
- 页脚：复用本站 `<Footer/>`（我们自己的产品/加盟/支持列——即“与本站一致”；参考页页脚是他们的，不照搬）

**适配**
- 深浅色都要正常（参考页仅深色，本项目两色都要过）
- 移动端：单列、卡片纵向堆叠、代码块横向滚动不撑破
- 可选：进入视口渐显 `AnimateInView`（与 agent-join 一致），非必需

## 9. 交付前需核对 / 开放项（不影响结构）
1. 控制台里令牌菜单的**确切名称**（令牌 / API 令牌 / API 密钥），据实写进步骤 02
2. 示例代码 `model=` 换成本平台**实际在售**的模型名（如模型广场首个可用模型）
3. 眉标语言：默认中文「开发文档」（合 W5）；若要与参考页**完全一致**改回英文 `DOCUMENTATION`——待你定夺

## 10. 实现约束
- 两个新 `.tsx` 需加项目**版权头**（有 `copyright:check` 脚本）
- 不手改 `routeTree.gen.ts`（自动重生）
- W4：Mac 只编辑 / 调试，构建部署在服务器

## 11. 验收（DoD）
- `bun run typecheck`（tsgo）通过、`build` 通过、`copyright:check` 通过
- 访问 `/docs`：页眉「文档」可点通、当前域名正确注入两处代码、微信号 `chen13477359255` 正确且可复制
- 中文无英文残留；深 / 浅色、桌面 / 移动端均正常；代码块不撑破布局
