# 后端 API 契约（前后端对齐） — api-contract.md

> **用途**：前端（另一 CC 用 superpowers 出 UIUX → 按项对接）与后端的**唯一对齐事实源**。前端按本契约对接，后端 handler 按本契约实现。
> **来源**：[proposal.md](proposal.md) §10/§13 ＋ [detailed-design.md](detailed-design.md) §12.7 ＋ 14 个后端模块（`internal/*`）已落地的接口与错误码。
> **状态**：**契约先行**。后端 14 模块**逻辑层已实现并单测全绿**；HTTP handler/`cmd/main` 装配在集成阶段补齐（届时本契约即实现规范）。基线仓库 `github.com/QuantumNous/new-api`。
> **变更纪律**：契约改动必须双方同步；遇冲突/不清记 `RETRO.md` 并在此更新。

---

## 1. 通用约定

| 项 | 约定 |
| --- | --- |
| Base URL | 同源；控制台/前台 `/api/**`，模型调用 `/v1/**`，支付回调 `/pay/** /auth/**` |
| 租户识别 | **按 Host**：`<slug>.wedreamhub.com` 或自定义域名 → 后端 `TenantResolver` 注入租户上下文。前端无需传 tenant_id；后端按 Host + 会话/Token 推断 |
| 鉴权（控制台） | 复用 new-api 会话（Cookie/Session）或 Bearer access token；角色 `admin` / `agent_owner` / `user` |
| 鉴权（模型调用） | `Authorization: Bearer <api-token>`，Token 绑租户 |
| 错误信封 | 统一 `{ "code": "<ERROR_CODE>", "message": "<人类可读>" }`，HTTP 状态由 code 决定（见 §1.1）。成功 `{ "data": ... }` 或资源对象 |
| 金额/币种 | **额度/调用计量 = USD**（`*_usd`、quota）；**充值实付/代理收益/套餐售价 = CNY ¥**（`*_cny`、price、spread）。前端按字段名区分，勿混算 |
| 时间 | ISO-8601 UTC（`2026-06-28T12:00:00Z`）；展示侧本地化 |
| 分页 | `?page=1&page_size=20` → `{ "data": [...], "total": N, "page": 1, "page_size": 20 }` |
| 幂等 | 支付回调按 `order_no` 幂等；购买/充值下单返回 `order_no` |

### 1.1 错误码注册表（前端按 code 做文案/分支，**唯一事实源**）

> 约定：**错误码必须带模块前缀**；跨域映射须保留 cause（见 `RETRO.md` 工具链与协作）。

| 模块 | code | HTTP | 含义/前端提示方向 |
| --- | --- | --- | --- |
| 通用 | `INTERNAL` | 500 | 服务器内部错误 |
| Tenant | `TENANT_NOT_FOUND` | 404 | 站点未开通/未绑定 |
| Tenant | `SLUG_RESERVED` / `SLUG_INVALID` / `SLUG_DUPLICATE` | 400/400/409 | 子域名保留/格式错/已占用 |
| Tenant | `TENANT_STATUS_INVALID` / `TENANT_SUSPENDED` | 400/403 | 状态非法/站点已停用 |
| Identity | `UNAUTHORIZED` / `TOKEN_INVALID` | 401 | 未登录/Token 无效，跳登录 |
| Identity | `FORBIDDEN_ADMIN` / `FORBIDDEN_TENANT` / `TENANT_INACTIVE` | 403 | 无管理员/租户权限/租户未激活 |
| Pricing | `RATIO_BELOW_FLOOR` / `PRICE_BELOW_PROTECTION` | 400 | 倍率/价格低于主站保护线 |
| Agent | `AGENT_TYPE_INVALID` | 400 | 代理类型/参数非法 |
| Agent | `WITHDRAW_INSUFFICIENT` / `WITHDRAW_NOT_PENDING` / `WITHDRAW_NOT_FOUND` | 400/409/404 | 提现超额/状态非待审/不存在 |
| Billing/Quota | `QUOTA_INSUFFICIENT` | 402 | 钱包余额不足，提示充值 |
| Billing/Quota | `SUBSCRIPTION_EXHAUSTED` / `SUBSCRIPTION_EXPIRED` | 402 | 套餐用尽/到期，提示**重购**（不回退钱包） |
| Billing/Relay | `MODEL_NOT_ALLOWED` | 403 | 该 Token 不允许此模型 |
| Wallet | `REDEEM_CODE_INVALID` / `REDEEM_CODE_USED` | 400/409 | 兑换码无效/已用 |
| Wallet | `RECHARGE_ORDER_INVALID` / `WALLET_AMOUNT_INVALID` | 400 | 充值单/金额非法 |
| TokenPlan | `PLAN_NOT_FOUND` / `PLAN_DISABLED` / `PLAN_NOT_LISTED` | 404/409/409 | 套餐不存在/停用/代理未上架 |
| TokenPlan | `RETAIL_BELOW_MIN` | 400 | 代理零售价低于保护线（cause=`PRICE_BELOW_PROTECTION`） |
| TokenPlan | `PURCHASE_LIMIT_EXCEEDED` | 409 | 限购（Trial 等）已达上限 |
| TokenPlan | `SUBSCRIPTION_NOT_FOUND` / `REFUND_NOT_SUPPORTED` | 404/400 | 订阅不存在/一期不支持退款 |
| Payment | `PAY_SIGN_INVALID` / `PAY_CALLBACK_INVALID` | 400 | 验签失败/回调报文非法（回调侧） |
| Payment | `PAY_ORDER_INVALID` / `PAY_ORDER_DUPLICATE` / `ORDER_NOT_FOUND` | 400/409/404 | 下单参数/重复单/单不存在 |
| Promotion | `CHANNEL_PREFIX_DUP` / `CHANNEL_PREFIX_INVALID` / `CHANNEL_NOT_FOUND` | 409/400/404 | 渠道前缀重复/非法/不存在 |
| SiteConfig | `THEME_NOT_IN_PALETTE` / `HOME_MODE_LOCKED` | 400/403 | 主题色不在色板/首页模式锁定 |
| SiteConfig | `ASSET_TYPE_FORBIDDEN` / `ASSET_TOO_LARGE` / `ASSET_NOT_FOUND` | 400/400/404 | 图片类型/大小/不存在 |
| Stats | `STATS_CROSS_TENANT` / `STATS_RANGE_INVALID` | 403/400 | 跨租户/范围非法 |
| Risk | `RATE_LIMITED` / `IP_NOT_ALLOWED` / `STATUS_FORBIDDEN` | 429/403/403 | 限流/IP 不允许/状态禁止 |
| Relay | `UPSTREAM_ERROR` | 502 | 上游渠道错误 |

---

## 2. 端点总览

> 角色：🅐=admin ｜ 🅖=agent_owner ｜ 🅤=user ｜ 🔓=公开/Token。复用 new-api 的标 *(reuse)*。

### 2.1 认证与会话 *(reuse new-api)*
`POST /api/user/register` · `POST /api/user/login` · `POST /api/user/logout` · `GET /api/user/self`
> 注册支持 `?channel=<prefix>_<rand>`（推广归属）与 Host（代理域名归属）。

### 2.2 站点与租户
| 方法 | 路径 | 角色 | 说明 |
| --- | --- | --- | --- |
| GET | `/api/tenant/current` | 🔓 | 按 Host 返回当前站点品牌配置（渲染前台用） |
| GET | `/api/tenant/site-config` | 🅖 | 取本站装修配置（未配置回退主站默认） |
| PATCH | `/api/tenant/site-config` | 🅖 | 改装修（主题色限色板、`home_mode∈{default,config}`、`custom_html` 锁定） |
| GET | `/api/tenant/site-config/theme-options` | 🅖 | 预设色板（8–12 色，前端选色用） |
| POST | `/api/tenant/assets` | 🅖 | 上传图片（jpg/png/webp，≤2MB；返回 url） |

### 2.3 用户钱包与充值
| 方法 | 路径 | 角色 | 说明 |
| --- | --- | --- | --- |
| GET | `/api/tenant/wallet` | 🅤 | 当前余额(USD额度)/订单入口/（代理另见可提现） |
| POST | `/api/tenant/wallet/recharge` | 🅤 | 下单充值（`{amount_usd, provider:wxpay\|alipay}`）→ 返回 `{order_no, pay:{wxpay_qr/alipay_url}}`，进程内官方 SDK |
| GET | `/api/tenant/wallet/recharge/methods` | 🅤 | 官方微信/支付宝可用渠道（enabled && configured 交集）→ 买家页据此呈现官方卡片 |
| POST | `/api/tenant/wallet/redeem` | 🅤 | `{code}` 兑换码入账 |
| GET | `/api/tenant/wallet/orders` | 🅤 | 充值/订单历史（分页） |

> **官方支付标识（前端派发）**：管理员在「系统设置 → 支付 → 新增支付方式」选 `wxpay_official` / `alipay_official`
> 标识（区别于 Epay 的 `wxpay`/`alipay`、Stripe `stripe`、Waffo `waffo_pancake`）。买家页将官方标识从 Epay
> 按钮网格中排除，改走上方 `/api/tenant/wallet/recharge`（进程内真实 SDK，USD/扫码/跳转）。后端 PayMethods 透传，
> 按 `type` 由前端决定支付流程；官方标识永不进入 Epay (`RequestEpay`) 路径。凭据在「微信/支付宝」选项卡（存 DB）。

### 2.4 tokenplan 套餐 ★
| 方法 | 路径 | 角色 | 说明 |
| --- | --- | --- | --- |
| GET | `/api/tenant/token-plans` | 🅤 | 可购套餐列表（含代理零售价、营销字段，见 §3 Plan） |
| POST | `/api/tenant/token-plans/:id/purchase` | 🅤 | 购买 → 经限购校验 → 返回 `{order_no, pay:{...}}`；支付成功后激活 30 天订阅 |
| GET | `/api/tenant/subscriptions` | 🅤 | 我的套餐：`[{plan_code, month_limit_usd, used_usd, remaining_usd, status, expire_at}]` |
| GET | `/api/tenant/token-plans/manage` | 🅖 | 代理可上架套餐与当前定价 |
| PATCH | `/api/tenant/token-plans/manage` | 🅖 | `{plan_id, enabled, retail_price}` 上架/退出+改价（后端校验 `retail≥min_price`→`RETAIL_BELOW_MIN`） |
| GET/POST/PATCH | `/api/admin/token-plans[/:id]` | 🅐 | 套餐 CRUD（售价/原价/月限额/成本价/保护线/排序/状态） |
| GET | `/api/admin/subscriptions` | 🅐 | 全站订阅监控 + **满额逼近预警**（防巨亏） |

**购买响应示例**
```json
{ "order_no":"sub_20260628_abc", "plan_code":"mini",
  "month_limit_usd":220, "valid_days":30,
  "pay":{ "method":"wxpay", "qr":"weixin://wxpay/..." } }
```

### 2.5 Token 管理
`GET/POST/PATCH/DELETE /api/tenant/tokens[/:id]` 🅤 — Token 绑租户，支持 `model_allowlist`、`ip_allowlist`。

### 2.6 代理 / OEM 管理 🅖
- 设置：`GET/PATCH /api/tenant/settings`
- 用户组倍率：`GET/POST/PATCH/DELETE /api/tenant/groups[/:id]`（倍率 `≥ floor`，否则 `RATIO_BELOW_FLOOR`）；`GET/PATCH /api/tenant/group-pricing`
- 我的用户：`GET /api/tenant/users` · `PATCH /api/tenant/users/:id/status|group`
- 推广渠道：`GET/POST/DELETE /api/tenant/promotion-channels[/:id]`（生成 `/sign-up?channel=<prefix>_<rand>`）
- 兑换码：`GET/POST /api/tenant/redemption-codes` · `PATCH /api/tenant/redemption-codes/:id/status`
- 收益与提现：`GET /api/tenant/earning-logs` · `GET /api/tenant/wallet`（含可提现）· `POST/GET /api/tenant/withdrawals`
- 统计：`GET /api/tenant/stats`（仅本站）

### 2.7 管理员 🅐
- 租户/代理：`GET /api/admin/tenants[/:id]` · `POST/PATCH /api/admin/agents[/:id]` · `POST /api/admin/agents/:id/enable|disable`
- 代理等级：`GET/POST/PATCH /api/admin/agent-levels[/:id]`
- 租户管控：`POST /api/admin/tenants/:id/suspend|activate` · `GET/PATCH /api/admin/tenants/:id/site-config`
- 域名/SSL（二期）：`GET /api/admin/tenants/:id/domains` · `POST /api/admin/domains/:id/ssl-configured|suspend`
- 资源/风控：`GET /api/admin/assets` · `POST /api/admin/assets/:id/takedown`
- 提现审核：`GET /api/admin/withdrawals` · `POST /api/admin/withdrawals/:id/approve|reject`
- 流水/统计：`GET /api/admin/billing-logs|recharge-orders|stats`

### 2.8 开放 API（仅 API 代理）🔓(开放密钥)
`POST /api/open/token/create` · `GET /api/open/token/:id` · `POST /api/open/token/recharge` · `DELETE /api/open/token/:id` · `GET /api/open/tokens`

### 2.9 模型调用入口 /v1（OpenAI 兼容）🔓(Bearer)
`POST /v1/chat/completions` · `POST /v1/completions` · `POST /v1/embeddings` · `GET /v1/models`
> 调用链（后端编排，前端仅需带 Token）：**鉴权 → 租户 → 风控 → 模型权限 → 桶路由 → 转发 → 扣费 → 日志**。桶路由：有 active 套餐→套餐桶（独立计量，超额/过期→`SUBSCRIPTION_EXHAUSTED/EXPIRED`，**不回退钱包**）；否则钱包桶（不足→`QUOTA_INSUFFICIENT`）。

### 2.10 支付回调（server-to-server，**非前端**）
`POST /pay/wxpay/notify` · `POST /auth/alipay/notify` — 由 auth-service 处理：验签→`order_no` 幂等→入账分发（钱包充值 or 激活订阅）。运维/Nginx 转发，详见 [deployment.md](deployment.md)。

---

## 3. 关键对象字段（前端渲染依据）

**Plan（套餐，§2.4 列表项）** — 含营销字段，供购买页卡片：
```json
{ "id":1, "code":"mini", "name":"Mini",
  "base_price_cny":119, "anchor_price_cny":1360, "discount_label":"-91%",
  "retail_price_cny":119, "month_limit_usd":220, "multiplier":1.0, "valid_days":30,
  "is_recommended":false, "badge":"", "sort":2 }
```
> `anchor_price_cny` 仅划线营销不计费；`retail_price_cny` 为当前站点（代理可改）实售价；`base_price_cny` 主站基准。

**Subscription（我的套餐）**：`{ id, plan_code, month_limit_usd, used_usd, remaining_usd, status: active|exhausted|expired|refunded, start_at, expire_at }`

**SiteConfig**：`site_name, logo_url, favicon_url, theme_color, template_key, hero_title, hero_subtitle, hero_image_url, primary_button_text, announcement, customer_service_*, footer_text, home_mode, enabled_modules[]`（二期预留 `banner_json, custom_html, custom_html_status`）

**Wallet**：`{ balance_usd, withdrawable_cny?, total_earnings_cny?, frozen_withdraw_cny? }`（后两者仅代理）

**Earning**：`{ source_type: recharge_spread|consume_commission|tokenplan_spread|tokenplan_commission|manual_adjustment, amount_cny, created_at }`

**Withdrawal**：`{ amount_cny, fee_cny, actual_cny, status: pending|approved|rejected, payment_method, created_at }`

**Channel**：`{ name, prefix, channel_code, signup_url, registered_count }`
**RedemptionCode**：`{ name, code, amount_usd, status: enabled|disabled|used|expired, expires_at }`

---

## 4. 装配映射（组装层 — 影响契约的字段对齐）

> 后端各模块用消费者定义接口，`cmd/main` 适配器对齐（见 `RETRO.md`）。对前端透明，但契约字段以此为准：

- 充值/购买的 `order_no` 即支付回调激活的幂等键（`Payment.PaidOrder.OrderNo` → `TokenPlan.ActivateFromPayment(orderID)` / `Wallet.Credit`）。
- 收益币种统一 ¥（`amount_cny`）；额度/计量统一 USD（`*_usd`）。
- `RETAIL_BELOW_MIN` 由 `PRICE_BELOW_PROTECTION` 映射而来（保留 cause）。

---

## 5. 前端对接注意（UIUX 对齐点）

1. **套餐购买页**：用 `anchor_price_cny` 划线 + `discount_label` 角标 + `is_recommended/badge` 推荐位；金额 ¥，月限额标 USD。**Trial 限购**：购买失败 `PURCHASE_LIMIT_EXCEEDED` 需友好提示"每人限购一次"。
2. **套餐 vs 钱包**：UI 明确显示"当前计量来源"；套餐用尽/到期 → 引导**重购**（不自动用钱包）。
3. **主题色**：只能从 `theme-options` 色板选，禁止任意取色（后端会拒 `THEME_NOT_IN_PALETTE`）。
4. **图片上传**：前端先校验类型/大小再传，后端二次校验（`ASSET_TYPE_FORBIDDEN/ASSET_TOO_LARGE`）。
5. **错误展示**：统一读 `code` 映射文案（§1.1），勿仅依赖 `message`。
6. **风格**：一期沿用 new-api（React + Semi-UI）风格；二期再按 superpowers UIUX 模型品牌化。前端按本契约 mock 数据先行，后端 handler 就绪后切真实接口。
7. **验证**：联调阶段用 **playwright-cli** 跑浏览器 E2E（注册→开站→配置→购买套餐→调用→看板）对照本契约。
