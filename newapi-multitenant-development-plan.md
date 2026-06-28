# New API 多租户分销平台开发方案

版本：v1.0  
日期：2026-06-27  
阶段：第一期全栈可演示 MVP + 第二期前端品牌化增强

> 2026-06-28 补充：已基于 TOKEN HUB 操作文档和截图整理出两份需求文档派生版：
>
> - `newapi-multitenant-requirements-with-images.md`：带截图版，适合业务方、主办方和产品评审。
> - `newapi-multitenant-requirements-ui-details.md`：无图 UI 详述版，适合前端、后端和测试按页面落地。

## 1. 项目定位

本项目不是单纯部署一个 New API / One API 实例，而是参照 TOKEN HUB v1.0 标准，在 New API 基础上扩展为“代理商 / OEM / API 代理平台”。

技术上仍然是单套系统、多租户识别和统一 API 网关；业务上不应理解成“每个代理都是独立 New API 站长”。更准确的定位是：

- 主站统一控制上游渠道、模型、支付、计费、风控、系统配置和管理员能力。
- 代理商在主站体系内获得代理权限，根据类型使用推广、OEM 品牌、用户组倍率、兑换码、开放 API 等能力。
- 终端用户通过主站、代理域名或代理推广链接注册，归属到对应代理商名下。

核心目标：

- 主站统一管理上游渠道、模型、用户、充值、扣费、风控、统计、流水和代理商参数。
- 支持普通代理、OEM 代理、API 代理三种代理类型。
- 代理商可以管理自己的下级用户、用户组倍率、兑换码、推广渠道和收益数据。
- OEM 代理可以配置品牌信息、Logo、首页内容、联系信息和自定义域名。
- API 代理可以通过开放 API 创建 Token、查询 Token、给 Token 充值额度。
- 终端用户可以注册、登录、充值、创建 API Key、调用模型、查看使用日志。

第一期重点是把 `multi_newapi` 项目完整跑通，交付一个可以实际演示的 TOKEN HUB 标准全栈 MVP：用户注册登录、控制台、API Key、钱包充值、使用日志、代理商用户组、代理商用户管理、兑换码、推广渠道、管理员子代理管理都要能用。第一期前端不追求精美品牌化，但必须能支撑完整业务流程。第二期再做 OEM 品牌、自定义域名、首页配置和主办方参考图级别的前端优化。

## 2. 总体架构

推荐采用单套后端、多租户隔离架构，而不是为每个代理部署一套独立 New API。

```text
用户访问 aaa.yourbrand.com
        ↓
Nginx / 网关
        ↓
Tenant Router：根据 Host 识别租户
        ↓
代理站前台 / 控制台
        ↓
统一 API 网关层：鉴权、扣费、限流、风控
        ↓
主站上游渠道池：OpenAI / Azure / Claude / Gemini / DeepSeek / Qwen 等
```

主站建议域名：

- `www.yourbrand.com`：主站官网 / 登录入口
- `admin.yourbrand.com`：主站管理员后台
- `api.yourbrand.com`：统一 API 服务入口

代理站 / OEM 站建议域名：

- `aaa.yourbrand.com`
- `bbb.yourbrand.com`
- `customer1.yourbrand.com`

第一期先支持 wildcard 二级域名，即 `*.yourbrand.com`。OEM 自定义域名按 TOKEN HUB 标准后续开放：代理在品牌配置里填写域名，DNS 添加 A 记录指向服务器公网 IP，然后联系管理员配置 SSL 证书。

## 3. 角色与权限

### 3.1 超级管理员

超级管理员拥有全局权限：

- 管理全部用户和代理。
- 添加和管理子代理。
- 设置代理类型：普通代理 / OEM 代理 / API 代理。
- 设置代理成本价、套餐折扣、消耗分润比例、代理等级。
- 配置上游渠道。
- 配置模型倍率、分组倍率保护线、最低成本价、最低利润率。
- 管理模型广场、渠道、用户、兑换码、推广渠道和系统设置。
- 查看全站消耗、充值、扣费、代理收益和提现申请。
- 禁用代理商或代理商下级用户。

### 3.2 代理商

代理商是某个租户的 owner。TOKEN HUB 标准中代理商分三类：

| 类型 | 功能 |
| --- | --- |
| 普通代理 | 基础推广功能，管理用户分组、兑换码、推广渠道和下级用户 |
| OEM 代理 | 完整品牌定制，自定义域名、Logo、首页内容、联系信息 |
| API 代理 | 面向合作方，提供开放 API 接入能力 |

代理商可用能力：

- 查看控制台数据：余额、总用量、请求次数、用量图表。
- 管理自己的下级用户。
- 创建和管理用户组倍率。
- 创建兑换码，兑换码额度从代理商自己的 API 额度中预扣。
- 创建推广渠道，生成专属注册链接。
- 查看可提现余额、累计收益和收益来源。
- 提交提现申请，由超级管理员审核后人工打款。
- OEM 代理可配置站点名称、Logo、页脚版权、联系信息、公告、关于我们、协议和自定义域名。
- API 代理可使用开放 API 创建 Token、查询 Token、充值 Token、删除 Token。

代理商不能：

- 修改主站上游渠道。
- 修改主站模型官方成本价。
- 设置低于主站保护线的倍率。
- 查看其他代理商的数据。
- 绕过主站统一扣费和风控。
- 接入自己的独立支付商户通道。

### 3.3 终端用户

终端用户可以属于主站，也可以通过代理邀请链接、推广渠道、代理域名注册后归属到某个代理商：

- 在代理站注册 / 登录。
- 充值余额。
- 创建 API Token。
- 使用 Playground AI 对话。
- 调用模型接口。
- 查看自己的额度、订单历史、调用日志和扣费记录。

## 4. 核心数据模型

第一期建议最少落地以下表结构。字段可按实际 New API 现有表结构做适配。

### 4.1 tenants

代理站 / 租户主表。

```sql
tenants
- id
- owner_user_id
- agent_type
- agent_level_id
- slug
- custom_domain
- status
- brand_name
- logo
- theme
- allowed_models
- default_price_ratio
- cost_price
- package_discount
- consume_commission_ratio
- min_margin_ratio
- settlement_mode
- created_at
- updated_at
```

说明：

- `slug` 对应二级域名前缀，例如 `aaa`。
- `agent_type` 建议包含 `normal`、`oem`、`api`，分别对应普通代理、OEM 代理、API 代理。
- `status` 建议包含 `active`、`suspended`、`deleted`；如保留申请流程，可额外使用 `pending`、`rejected`。
- `default_price_ratio` 是代理站默认倍率，只作为兜底值，不能替代 New API 原本的用户分组倍率能力。
- `cost_price` 是代理商进货成本价，影响充值差价收益。
- `package_discount` 是代理商购买套餐时的折扣。
- `consume_commission_ratio` 是用户 API 消耗时给代理商的分润比例。
- `min_margin_ratio` 是主站设置的最低利润保护。

### 4.2 tenant_site_configs

代理站前端装修配置表。第二阶段的核心不是让代理完全 DIY 前端，而是主站提供统一前端，代理只上传图片、Logo、文案和选择主题色。

```sql
tenant_site_configs
- id
- tenant_id
- site_name
- logo_url
- favicon_url
- theme_color
- template_key
- hero_title
- hero_subtitle
- hero_image_url
- primary_button_text
- announcement
- customer_service_type
- customer_service_link
- contact_email
- phone
- wechat_id
- wechat_qr_url
- qq
- docs_url
- recharge_url
- recharge_notice
- about_content
- seo_title
- seo_description
- user_agreement
- privacy_policy
- pricing_display_mode
- pricing_description
- home_mode
- banner_json
- custom_html
- custom_html_status
- footer_text
- enabled_modules
- created_at
- updated_at
```

示例配置：

```json
{
  "site_name": "AAA AI API",
  "logo_url": "https://cdn.xxx.com/aaa-logo.webp",
  "favicon_url": "https://cdn.xxx.com/aaa-favicon.ico",
  "theme_color": "#4f46e5",
  "template_key": "template_a",
  "hero_title": "稳定高速的 AI API 服务",
  "hero_subtitle": "支持 GPT、Claude、Gemini、DeepSeek 等模型",
  "hero_image_url": "https://cdn.xxx.com/aaa-home.webp",
  "primary_button_text": "立即开始",
  "announcement": "新用户充值满 100 送 10%",
  "customer_service_type": "telegram",
  "customer_service_link": "https://t.me/xxx",
  "docs_url": "https://docs.xxx.com",
  "recharge_notice": "充值后额度实时到账",
  "pricing_display_mode": "card",
  "home_mode": "default",
  "footer_text": "© AAA API",
  "enabled_modules": ["models", "pricing", "advantages", "steps", "faq"]
}
```

说明：

- 主站统一控制页面结构、支付流程、充值流程、API Key 页面、风控提示、登录注册和价格展示逻辑。
- 代理只能在允许范围内修改 Logo、首页图、主题色、标题文案、公告、客服入口、套餐展示文案、底部信息和部分模块开关。
- 首页模式按 TOKEN HUB 标准保留 `default`、`config`、`custom_html` 三种设计；第一阶段只开放 `default` 和 `config`。
- `custom_html` 属于高风险能力，后续如开放必须由管理员审核后生效。
- 主题色建议只允许从主站预设色板选择，不允许随意填写任意颜色。

### 4.3 tenant_domains

代理站域名绑定表。所有代理域名仍然指向同一套系统，系统根据请求 Host 判断域名属于哪个租户。

```sql
tenant_domains
- id
- tenant_id
- domain
- domain_type
- purpose
- ssl_status
- server_ip
- admin_remark
- is_primary
- created_at
- ssl_configured_at
- deleted_at
```

说明：

- `domain_type` 建议包含 `platform_subdomain`、`custom_domain`。
- `purpose` 建议包含 `portal`、`api`、`both`。
- `ssl_status` 建议包含 `pending_admin_config`、`configured`、`failed`、`expired`。
- 第一阶段写入主站分配的二级域名，例如 `aaa.yourbrand.com`。
- OEM 代理可在品牌配置中填写自定义域名，例如 `ai.example.com`。
- 自定义域名由代理添加 A 记录指向服务器公网 IP，再联系管理员配置 SSL 证书。
- 第一阶段建议只允许一个主域名。

示例：

```text
tenant_id: 1001
domain: ai.example.com
domain_type: custom_domain
purpose: both
server_ip: 64.90.4.114
ssl_status: configured
is_primary: true
```

### 4.4 tenant_users

代理站用户关系表。

```sql
tenant_users
- id
- tenant_id
- user_id
- balance
- group_id
- group_key
- status
- created_at
- updated_at
```

说明：

- 同一个主站用户可以成为某个代理站的站长，也可以是另一个代理站的普通用户。
- 第一阶段可以先限制一个用户只管理一个代理站，降低复杂度。
- `group_id` / `group_key` 用于绑定代理站内的用户分组，例如 `default`、`vip`、`svip`。

### 4.5 tenant_groups

代理站用户分组表。该表用于保留 New API 原本“不同分组不同倍率”的能力，并且把分组配置隔离到每个租户内。

```sql
tenant_groups
- id
- tenant_id
- group_key
- group_name
- group_ratio
- min_group_ratio
- max_group_ratio
- status
- created_at
- updated_at
```

说明：

- 每个代理站至少有一个默认分组，例如 `default`。
- 代理可以创建或修改自己租户内的分组倍率。
- `group_ratio` 是该分组默认倍率，会参与最终扣费。
- `min_group_ratio` 和 `max_group_ratio` 由主站限制，防止代理把分组价格调到成本以下。
- 如果现有 New API 已有分组表，应优先复用现有分组逻辑，并增加 `tenant_id` 做租户隔离。

### 4.6 tenant_tokens

代理站 API Token 表。

```sql
tenant_tokens
- id
- tenant_id
- user_id
- token_hash
- quota_limit
- model_allowlist
- ip_allowlist
- status
- created_at
- updated_at
```

说明：

- Token 必须绑定 `tenant_id`。
- 日志、扣费、限流都必须从 Token 反查租户，不能只依赖用户 ID。

### 4.7 tenant_pricing

代理站模型价格表。该表表示租户层面的模型默认倍率，不区分用户分组。

```sql
tenant_pricing
- id
- tenant_id
- model
- model_ratio
- completion_ratio
- fixed_price
- min_floor_price
- created_at
- updated_at
```

说明：

- `min_floor_price` 由主站控制。
- 代理只能设置高于 `min_floor_price` 的价格。
- 第一阶段可以只做 `model_ratio`，暂不做复杂 fixed price。
- 如果没有配置分组专属模型倍率，则使用该模型默认倍率。

### 4.8 tenant_group_pricing

代理站分组模型价格表。该表用于支持“不同用户分组对不同模型设置不同倍率”的高级能力。

```sql
tenant_group_pricing
- id
- tenant_id
- group_id
- group_key
- model
- model_ratio
- completion_ratio
- fixed_price
- min_floor_price
- created_at
- updated_at
```

说明：

- 这是对 `tenant_pricing` 的细分覆盖。
- 如果代理只需要简单分组倍率，可以只使用 `tenant_groups.group_ratio`。
- 如果代理需要 `VIP` 用户使用某个模型更便宜或更贵，则使用 `tenant_group_pricing`。
- 第一阶段建议至少支持 `tenant_groups.group_ratio`；如果现有 New API 已经具备分组模型倍率，则直接按 `tenant_id` 隔离复用。

### 4.9 tenant_billing_logs

代理站扣费和利润日志。

```sql
tenant_billing_logs
- id
- tenant_id
- user_id
- group_id
- group_key
- token_id
- model
- prompt_tokens
- completion_tokens
- upstream_cost
- charged_quota
- gross_profit
- request_id
- created_at
```

说明：

- `upstream_cost` 是主站侧成本。
- `charged_quota` 是对终端用户实际扣费。
- `gross_profit = charged_quota - upstream_cost`。
- 记录 `group_id` / `group_key` 是为了后续按分组统计收入、成本和代理收益。

### 4.10 agent_levels

代理等级表。超级管理员可创建金牌、银牌等等级，并给代理商套用默认参数。

```sql
agent_levels
- id
- name
- cost_price
- package_discount
- consume_commission_ratio
- withdraw_fee_ratio
- status
- created_at
- updated_at
```

说明：

- `cost_price` 是代理商进货价，影响充值差价收益。
- `package_discount` 是代理商购买套餐折扣。
- `consume_commission_ratio` 是用户 API 消耗分润比例。

### 4.11 agent_wallets

代理商钱包表。TOKEN HUB 标准中要区分 API 额度和可提现余额。

```sql
agent_wallets
- id
- tenant_id
- owner_user_id
- api_balance
- withdrawable_balance
- total_earnings
- frozen_withdraw_amount
- created_at
- updated_at
```

说明：

- `api_balance` 是代理商自己的 API 调用额度，可用于创建兑换码时预扣。
- `withdrawable_balance` 是代理商从充值差价和消耗分润中获得的人民币收益。
- `frozen_withdraw_amount` 是提现申请提交后已预扣、等待管理员审核的金额。

### 4.12 agent_earning_logs

代理商收益日志表。

```sql
agent_earning_logs
- id
- tenant_id
- user_id
- source_type
- source_id
- amount
- currency
- description
- created_at
```

说明：

- `source_type` 建议包含 `recharge_spread`、`consume_commission`、`manual_adjustment`。
- 充值差价和用户消耗分润都要写入该表，便于代理商查看收益来源。

### 4.13 agent_withdrawals

代理商提现申请表。

```sql
agent_withdrawals
- id
- tenant_id
- owner_user_id
- amount
- fee_amount
- actual_amount
- payment_method
- account_name
- account_no
- status
- admin_remark
- created_at
- reviewed_at
```

说明：

- `status` 建议包含 `pending`、`approved`、`rejected`。
- 提交提现时先冻结可提现余额。
- 管理员审核通过后手动打款；审核拒绝后解冻余额。

### 4.14 agent_promotion_channels

代理推广渠道表。

```sql
agent_promotion_channels
- id
- tenant_id
- name
- prefix
- channel_code
- signup_url
- registered_count
- created_at
- updated_at
```

说明：

- 代理商可创建渠道，例如 `wechat`、`xiaohongshu`、`douyin`。
- 系统生成推广链接，例如 `/sign-up?channel=wechat_xxxxxx`。
- 用户通过推广链接注册后归属到对应代理商和渠道。

### 4.15 agent_redemption_codes

代理兑换码表。

```sql
agent_redemption_codes
- id
- tenant_id
- name
- code
- amount
- status
- expires_at
- used_by_user_id
- used_at
- created_at
```

说明：

- 代理商创建兑换码时，从代理商自己的 API 额度中预扣。
- 状态建议包含 `enabled`、`disabled`、`used`、`expired`。

### 4.16 agent_open_api_keys

API 代理开放接口密钥表。

```sql
agent_open_api_keys
- id
- tenant_id
- key_hash
- status
- last_used_at
- created_at
```

说明：

- 仅 API 代理可见。
- 用于开放 API：创建用户 Token、查询 Token、给 Token 充值额度、删除 Token、列出 Token。

## 5. TOKEN HUB 计费、收益与成本保护

TOKEN HUB 标准中必须区分两类余额：

| 余额类型 | 说明 | 用途 |
| --- | --- | --- |
| 当前余额 / API 额度 | 用户充值后获得的调用额度 | 调用 AI 模型时扣减 |
| 可提现余额 | 代理商从充值差价和用户消耗分润中获得的人民币收益 | 代理商申请提现 |

### 5.1 用户组倍率

用户组倍率影响用户充值时实际支付金额和代理商充值差价收益。

示例：

```text
代理商成本价 = ¥8 / 刀
用户组倍率 = 1.5
用户充值额度 = $10
用户实际支付 = ¥150
代理商成本 = ¥80
代理商充值差价收益 = ¥70
```

倍率含义：

| 倍率 | 含义 | 举例 |
| --- | --- | --- |
| 1.0 | 正常价格 | 充 $10 按平台默认价格支付 |
| 0.9 | 用户享受九折优惠 | 用户支付更少 |
| 1.5 | 用户溢价 50% | 代理商获得更多差价 |

主站必须设置最低倍率保护，避免代理商卖穿成本。

```text
用户组倍率 >= 主站 group_floor_ratio
用户实际支付金额 >= 代理商成本金额 + 主站最低利润保护
```

### 5.2 充值差价收益

充值差价是代理商第一种盈利方式：

```text
充值差价 = 用户实际支付金额 - 代理商成本金额
```

充值成功后：

- 终端用户获得 API 额度。
- 充值订单写入用户订单历史。
- 若用户属于代理商，则计算充值差价。
- 充值差价写入 `agent_earning_logs`。
- 充值差价增加代理商 `withdrawable_balance`。

如果不启用充值差价，可将代理商成本价设为 0 或按主办方要求关闭该收益项。

### 5.3 消耗分润收益

消耗分润是代理商第二种盈利方式：

```text
消耗分润 = 用户本次 API 消耗金额 × 代理消耗分润比例
```

用户每次调用 API 后：

- 从用户 API 额度中扣费。
- 写入使用日志。
- 若用户属于代理商，按系统配置计算消耗分润。
- 分润写入 `agent_earning_logs`。
- 分润增加代理商 `withdrawable_balance`。

如果不启用消耗分润，可将分润比例设为 0。

### 5.4 模型定价与扣费

模型输入 / 输出 Token 单价由超级管理员统一配置。代理商不能修改主站上游渠道和模型官方成本价。

推荐计算方式：

```text
API 扣费 = 输入 Token × 输入单价 + 输出 Token × 输出单价
```

如需保留 New API 分组模型倍率能力，可以在主站保护线内扩展：

```text
最终扣费 = 模型基础扣费 × 分组模型倍率
```

### 5.5 提现流程

仅代理商账号可见：

```text
代理商查看可提现余额
        ↓
提交提现申请
        ↓
系统冻结申请金额
        ↓
超级管理员审核
        ↓
通过后管理员线下打款
        ↓
拒绝则解冻余额
```

提现可配置手续费：

```text
实际到账 = 提现金额 - 手续费
```

第一期必须完成：

- 超级管理员可配置代理成本价、分组最低倍率、消耗分润比例。
- 代理商可配置用户组倍率。
- 用户充值时按分组倍率计算订单金额。
- 用户 API 调用时扣减 API 额度并写入使用日志。
- 代理商收益写入 `agent_earning_logs`。
- 代理商可查看可提现余额、累计收益。
- 代理商可提交提现申请，管理员可审核。

## 6. 域名与租户识别

### 6.1 第一阶段域名方案

第一期只做 wildcard 二级域名：

```text
*.yourbrand.com -> 同一套后端 / 网关
```

后端根据请求 Host 识别租户：

```text
aaa.yourbrand.com -> slug = aaa -> tenant = aaa
```

主站保留域名不能被代理申请：

- `www`
- `api`
- `admin`
- `root`
- `dashboard`
- `static`
- `cdn`
- `status`
- `support`

### 6.2 TOKEN HUB 自定义域名方案

自定义域名按 TOKEN HUB 文档标准执行：代理填写域名，DNS 添加 A 记录指向服务器公网 IP，然后联系管理员配置 SSL 证书。

OEM 代理在后台：

```text
OEM 管理 -> 品牌配置 -> 品牌 Tab
```

填写自定义域名，注意不包含 `https://`：

```text
ai.example.com
```

然后在域名 DNS 服务商添加 A 记录，指向服务器公网 IP：

```text
记录类型：A
主机记录：ai
记录值：64.90.4.114
```

说明：

- 本项目服务器公网 IP 为 `64.90.4.114`。
- 代理填写域名并完成 DNS 解析后，需要联系管理员配置 SSL 证书。
- 管理员配置证书完成后，代理访问该域名即可看到品牌化站点。
- 自定义域名仍然访问同一套前端、后端、API 网关和数据库，不是单独部署。

### 6.3 自定义域名绑定流程

TOKEN HUB 标准流程：

```text
1. 代理进入 品牌配置 -> 品牌 Tab
2. 填写自定义域名，例如 ai.example.com
3. 点击保存
4. 在域名 DNS 服务商添加 A 记录，指向服务器公网 IP
5. 联系管理员配置 SSL 证书
6. 管理员配置完成后，访问该域名即可看到品牌化站点
```

状态建议：

```text
未配置 -> 等待管理员配置 SSL -> 已配置 -> 已启用
```

最终用户访问：

```text
https://ai.example.com
```

系统读取请求头：

```text
Host: ai.example.com
```

然后查询 `tenant_domains`：

```text
ai.example.com -> tenant_123
```

再加载该代理站的配置：

- Logo。
- 首页图。
- 站点名称。
- 主题色。
- 倍率。
- 价格。
- 公告。
- 客服入口。
- 注册入口。
- 用户余额。
- API Key 管理。

### 6.4 请求路由逻辑

前端和后端入口可以共用一套路由。伪代码：

```js
const host = request.headers.host

const domain = await db.tenant_domains.findOne({
  domain: host,
  ssl_status: 'configured'
})

if (!domain) {
  return showUnboundDomainPage()
}

const tenant = await db.tenants.findById(domain.tenant_id)

request.tenant = tenant

return renderTenantSite(tenant)
```

用户访问：

```text
https://ai.example.com
```

系统返回对应 OEM 代理站点。

用户访问：

```text
https://agent1.yourdomain.com
```

系统返回对应 OEM 代理站点。

### 6.5 HTTPS 证书方案

HTTPS 按 TOKEN HUB 文档标准处理：

- 代理先把自定义域名 A 记录解析到服务器公网 IP。
- 管理员在服务器 / Nginx / 宝塔面板中为该域名配置 SSL 证书。
- 证书配置完成后，将 `tenant_domains.ssl_status` 更新为 `configured`。
- 未配置 SSL 的自定义域名不作为正式可用域名对外展示。

## 7. 第一阶段目标：3 天全栈 MVP

第一阶段目标不是只完成后端接口，而是在 3 天内把 `multi_newapi` 跑成一个符合 TOKEN HUB 标准的完整可演示 MVP。这个 MVP 要允许用户注册登录、充值、创建 API Key、调用模型、查看使用日志；允许代理商管理用户组、下级用户、兑换码、推广渠道、收益和提现；允许超级管理员配置子代理、代理等级、渠道、模型和系统计费参数。

第一阶段的前端要求是“能完整使用”，不是“最终视觉效果”。页面可以简洁，但不能只靠接口调试工具演示。

第一阶段必须跑通以下闭环：

```text
用户注册 / 登录
        ↓
超级管理员设置用户为代理商
        ↓
配置代理类型：普通代理 / OEM 代理 / API 代理
        ↓
系统生成 tenant、代理推广关系和平台二级域名
        ↓
终端用户通过代理链接或代理域名注册
        ↓
用户充值并获得 API 额度
        ↓
代理按用户组倍率获得充值差价
        ↓
用户创建 API Key 并调用模型
        ↓
系统扣费、记录使用日志、计算代理消耗分润
        ↓
代理查看我的用户、收益、推广渠道、兑换码和提现
```

### 7.1 第一阶段 MVP 页面范围

3 天内至少要完成以下页面。页面可以直接复用现有 New API 风格，不做深度视觉设计。

主站页面：

- 主站登录 / 注册页。
- 控制台首页。
- API Key 管理页。
- 钱包 / 充值页。
- 使用日志页。
- 管理员全局统计页。
- 管理员用户管理页。
- 管理员子代理管理页。
- 管理员代理等级管理页。
- 管理员提现管理页。

代理商页面：

- OEM 管理 / 品牌配置页。
- OEM 管理 / 我的用户组页。
- OEM 管理 / 我的用户页。
- OEM 管理 / 推广渠道页。
- OEM 管理 / 兑换码页。
- 代理商钱包 / 提现申请页。
- API 代理 / 开放 API 密钥页。

终端用户页面：

- 用户控制台首页。
- API Token 创建和查看页。
- 余额展示页。
- 钱包充值页。
- Playground AI 对话页。
- 调用日志页。

### 7.2 第一天：项目跑通、数据结构、租户识别、基础页面

目标：先让 `multi_newapi` 项目能本地启动，并完成多租户 MVP 骨架。

任务：

- 拉通当前项目运行环境：
  - 确认后端、前端、数据库、Redis、环境变量启动方式。
  - 补齐 `.env.example` 或部署说明中缺失的关键配置。
  - 本地跑通登录、基础 API 调用和数据库连接。
- 梳理现有 New API 用户、Token、渠道、日志、充值相关表。
- 新增或改造 `tenants`、`tenant_site_configs`、`tenant_domains`、`tenant_users`、`tenant_groups`、`tenant_tokens`、`tenant_billing_logs`、`agent_levels`、`agent_wallets`、`agent_earning_logs`、`agent_withdrawals`、`agent_promotion_channels`、`agent_redemption_codes`、`agent_open_api_keys`。
- 增加租户 / 代理状态枚举：`active`、`suspended`、`deleted`，如保留申请流程再使用 `pending`、`rejected`。
- 增加保留 slug 校验。
- 增加 slug 唯一性校验。
- 第一阶段创建租户时自动写入主站二级域名记录，例如 `aaa.yourbrand.com`。
- 实现 Host 解析逻辑：
  - 主站域名进入主站逻辑。
  - 代理二级域名进入租户逻辑。
  - 第一阶段从 `tenant_domains` 或 `tenants.slug` 解析平台二级域名。
  - 本地开发支持通过 Header 或 query 参数模拟 Host。
  - 未找到租户返回“站点不存在 / 站点未开通”的前端状态。
- 实现管理员子代理管理接口和页面：
  - 搜索用户。
  - 设置为普通代理 / OEM 代理 / API 代理。
  - 设置代理成本价、套餐折扣、消耗分润比例。
  - 设置代理等级。
  - 禁用 / 启用代理。
- 实现代理等级管理：
  - 新增等级。
  - 设置成本价、套餐折扣、消耗分润比例。
  - 给代理商套用等级参数。
- 实现基础页面路由：
  - 主站用户页面。
  - 管理员页面。
  - 代理站页面。
  - 未开通租户提示页。

交付物：

- 项目本地启动说明。
- 数据库迁移文件。
- 租户解析中间件 / 服务。
- 子代理管理 API 和页面。
- 代理等级管理 API 和页面。
- 可通过浏览器完成“用户注册 -> 管理员设为代理 -> 代理进入 OEM 管理”的演示。

验收标准：

- `multi_newapi` 可以在本地正常启动。
- 主站登录、控制台、API Key、钱包、使用日志页面可以访问。
- 管理员可以把普通用户设置为普通代理 / OEM 代理 / API 代理。
- `aaa.yourbrand.com` 或本地模拟 Host 请求能解析到 `aaa` 租户。
- 未激活或已禁用代理不能正常对外服务。
- 重复 slug、保留 slug 不能申请成功。
- 管理员可配置代理成本价、套餐折扣和消耗分润比例。

### 7.3 第二天：用户组、兑换码、推广、钱包、API Key、调用扣费

目标：让代理商和终端用户可以通过页面完成 TOKEN HUB 核心业务操作。

任务：

- 实现用户控制台首页：
  - 当前余额。
  - 总用量。
  - 请求次数。
  - 用量图表。
- 实现代理站基础设置接口和页面：
  - 修改站点名称。
  - 修改 Logo。
  - 修改 Favicon。
  - 修改首页 Banner / Hero 图片。
  - 修改首页主标题。
  - 修改首页副标题。
  - 修改主题色，第一阶段使用主站预设色板。
  - 修改公告。
  - 修改客服入口。
  - 修改页脚文案。
  - 套餐说明文案可部分修改，但价格底线由主站控制。
- 实现基础图片配置能力：
  - 第一阶段可以先支持图片 URL。
  - 如实现上传，则必须限制图片格式和大小。
  - 上传图片必须绑定 `tenant_id`。
  - 不允许上传 SVG、HTML、JS、EXE、ZIP。
- 实现代理用户分组倍率配置接口和页面：
  - 查询当前租户用户分组。
  - 创建代理站内用户分组，例如 `default`、`vip`、`svip`。
  - 修改每个分组的倍率。
  - 给终端用户分配分组。
  - 后端校验分组倍率不能低于主站 floor。
- 实现代理我的用户页面：
  - 查看下级用户余额、已消耗、邀请渠道、状态。
  - 禁用 / 启用用户。
  - 修改用户分组。
- 实现推广渠道：
  - 创建渠道名称和渠道前缀。
  - 生成 `/sign-up?channel=xxx` 推广链接。
  - 用户通过链接注册后自动归属代理和渠道。
- 实现兑换码管理：
  - 创建兑换码名称、额度、数量、过期时间。
  - 创建时从代理商 API 额度中预扣。
  - 支持启用、禁用、已使用状态。
- 实现租户 Token 创建逻辑和页面：
  - Token 绑定 `tenant_id`。
  - Token 绑定 `user_id`。
  - Token hash 存储。
  - 支持模型 allowlist。
- 实现终端用户基础页面：
  - 用户余额展示。
  - 钱包充值。
  - 订单历史。
  - 兑换码兑换。
  - Token 创建。
  - Token 列表。
  - 调用日志列表。
  - Playground AI 对话。
- 改造 API 调用鉴权：
  - 根据 Token 找到 tenant。
  - 校验 tenant 状态必须为 `active`。
  - 校验用户状态和余额。
  - 校验模型是否允许。
- 改造充值和收益逻辑：
  - 获取用户所属租户分组。
  - 按用户组倍率计算充值金额。
  - 计算代理商成本金额和充值差价。
  - 充值差价写入代理商收益日志。
  - 增加代理商可提现余额。
- 改造 API 调用扣费逻辑：
  - 按模型输入 / 输出 Token 单价计算扣费。
  - 扣除用户余额。
  - 写入 `tenant_billing_logs`。
  - 根据消耗分润比例计算代理商分润。
  - 写入 `agent_earning_logs`。
- 增加 API 调用演示能力：
  - 可以使用页面生成的 Token 调用 `/v1/chat/completions`。
  - 可以在页面看到本次调用产生的扣费日志。

交付物：

- 用户控制台页面。
- 代理站设置 API 和页面。
- 代理站最小装修配置 API 和页面。
- 代理用户分组倍率配置 API 和页面。
- 我的用户 API 和页面。
- 推广渠道 API 和页面。
- 兑换码 API 和页面。
- 租户 Token API 和页面。
- 钱包充值 API 和页面。
- 终端用户控制台页面。
- 扣费和日志写入逻辑。
- API 调用演示记录。

验收标准：

- 代理商可以通过页面修改站点基础信息。
- OEM 代理可以通过页面配置 Logo、Favicon、Hero 图片、标题、副标题、公告和客服入口。
- 代理站主题色只能从主站预设色板选择。
- 代理商可以通过页面配置每个用户分组的倍率。
- 代理可以把终端用户分配到不同分组。
- 代理不能设置低于最低保护线的分组倍率。
- 代理可以创建推广渠道并生成注册链接。
- 代理可以创建兑换码，且会预扣代理 API 额度。
- 终端用户可以通过页面创建 Token。
- 终端用户可以充值并获得 API 额度。
- 终端用户 API 调用后会产生租户扣费日志。
- 代理商能看到充值差价和消耗分润收益。
- 页面日志中可以看到模型、消耗、扣费和时间。
- 后台日志中可以看到扣费、充值差价、消耗分润和代理收益。
- 冻结租户的 Token 不能继续调用。

### 7.4 第三天：收益提现、开放 API、统计看板、部署和端到端验收

目标：完成第一期完整可演示 MVP，并部署到测试环境或本地稳定运行环境。

任务：

- 复用或适配 New API 原有充值能力。
- 第一阶段统一使用主站收款和充值入账能力。
- 给租户用户余额入账时必须带 `tenant_id`。
- 钱包页面按 TOKEN HUB 标准至少展示：
  - 当前余额 / API 额度。
  - 充值入口。
  - 订单历史。
  - 兑换码兑换。
  - 代理商钱包，仅代理商可见。
- 支付方式按现有项目可用能力接入：
  - 微信支付。
  - 支付宝。
  - 如果线上支付暂时未打通，必须提供管理员人工入账兜底。
- 补齐支付回调配置：
  - 微信支付回调地址：`https://你的域名/pay/wxpay/notify`。
  - 支付宝异步通知地址：`https://你的域名/auth/alipay/notify`。
  - Nginx 需要把 `/pay/` 和 `/auth/` 转发到支付 / 认证服务。
  - 回调入账必须关联订单、用户、`tenant_id` 和充值额度。
- 如果支付接入来不及，第一阶段至少提供管理员人工入账能力：
  - 管理员选择租户。
  - 管理员选择用户。
  - 管理员输入入账额度。
  - 系统记录入账日志。
- 增加管理员统计接口和页面：
  - 全站租户数量。
  - 活跃租户数量。
  - 全站调用量。
  - 全站扣费。
  - 全站代理收益。
- 增加代理站统计接口和页面：
  - 本站用户数。
  - 本站 Token 数。
  - 本站调用量。
  - 本站收入。
  - 本站代理收益。
- 增加代理商钱包和提现：
  - 展示 API 额度。
  - 展示可提现余额。
  - 展示累计收益。
  - 提交提现申请。
  - 系统冻结申请金额。
  - 管理员审核通过 / 拒绝。
- 增加 API 代理开放 API：
  - 生成开放 API 密钥。
  - `POST /api/open/token/create` 创建用户 Token。
  - `GET /api/open/token/:id` 查询 Token 信息。
  - `POST /api/open/token/recharge` 给 Token 充值额度。
  - `DELETE /api/open/token/:id` 删除 Token。
  - `GET /api/open/tokens` 列出 Token。
- 增加管理员侧 TOKEN HUB 标准页面：
  - 渠道管理。
  - 模型管理。
  - 用户管理。
  - 兑换码管理。
  - 子代理管理。
  - 提现管理。
  - 系统设置中的计费与支付参数。
- 增加基础风控：
  - 租户状态校验。
  - 用户状态校验。
  - Token 状态校验。
  - 余额不足拦截。
  - 简单 IP allowlist 校验。
- 完善前端状态：
  - 加载状态。
  - 空数据状态。
  - 权限不足状态。
  - API 调用失败提示。
- 补充错误码和错误文案。
- 完成端到端联调。
- 输出第一阶段演示流程。
- 部署到测试环境，或提供本地一键启动方式。

交付物：

- 充值入账适配。
- 管理员人工入账页面，或已接通的在线充值页面。
- 主站统计接口和页面。
- 代理统计接口和页面。
- 代理钱包和提现申请页面。
- 管理员提现审核页面。
- API 代理开放 API。
- 风控校验。
- 第一阶段部署说明。
- 第一阶段接口文档。
- 第一阶段页面清单。
- 第一阶段演示账号和演示步骤。

验收标准：

- 用户注册、管理员设为代理、配置用户组倍率、创建推广渠道、下级用户注册、充值、创建 Token、调用 API、扣费、计算代理收益、提交提现全流程可在浏览器中演示。
- 主站管理员能看到全局数据。
- 代理只能看到自己站点数据。
- 代理商能看到自己的用户、Token、调用量、收益、可提现余额和推广渠道。
- 终端用户能看到自己的余额、Token 和调用日志。
- API 代理可以通过开放 API 创建和充值 Token。
- 终端用户余额不足时无法继续调用。
- 关键接口具备权限校验，不能跨租户读取数据。
- 测试环境或本地环境可以按文档重新启动。

## 8. 第一阶段暂不做事项

为了保证 3 天内能交付，以下内容不进入第一阶段：

- OEM 自定义域名绑定，按第 6.2 节后续阶段开放，并由管理员配置 SSL 证书。
- 高完成度品牌化前端。
- 自定义 HTML 首页模式。
- 多语言系统。
- 精细化风控规则引擎。
- 复杂图表分析和财务报表。

这些能力可以作为第二阶段或第三阶段迭代。

## 9. 第二阶段目标：按主办方需求改造前端与品牌形象

第二阶段重点是根据主办方后续给出的需求、参考图片、品牌素材和页面风格，对第一阶段已经跑通的 MVP 进行前端改造。第一阶段先保证功能闭环，第二阶段再按主办方确认的视觉方向做页面还原、品牌统一和交互优化。

第二阶段不建议在没有参考图和明确需求前提前定死视觉方案。正确流程是：

```text
主办方提供参考图片 / 品牌素材 / 页面需求
        ↓
整理页面清单和改版范围
        ↓
确认视觉风格、颜色、Logo、布局和交互
        ↓
按页面逐个改造前端
        ↓
主办方验收截图和线上效果
        ↓
根据反馈进行二次修改
```

### 9.1 主办方需提供的资料

第二阶段开始前，建议向主办方收集以下资料：

- 主站 Logo、代理站默认 Logo、favicon。
- 主色、辅助色、按钮颜色、背景颜色。
- 登录页、控制台、价格页、Token 页、充值页等参考图片。
- 希望模仿或对标的网站链接。
- 不希望出现的设计风格。
- 主站名称、平台介绍、页脚文案、客服链接。
- 代理站是否允许自定义 Logo、主题色、公告和客服信息。
- 移动端是否需要重点适配。
- 是否需要暗色模式。
- 是否需要中英文或其他语言。

### 9.2 第二阶段核心原则

不建议让代理完全自由 DIY 前端。正确方案是“统一代理站前端 + 受控装修配置”。TOKEN HUB 标准里存在自定义 HTML 首页模式，但该能力应作为高风险能力后续开放，第一阶段不开放：

```text
用户访问 aaa.newapi.com
        ↓
系统根据 Host 识别 tenant = aaa
        ↓
读取 tenant_site_configs
        ↓
同一套代理站前端按配置渲染不同 Logo、图片、主题色和文案
```

主站统一控制：

- 页面结构。
- 登录注册流程。
- 支付流程。
- 充值流程。
- API Key 页面。
- 价格展示逻辑。
- 风控提示。
- 用户余额和扣费逻辑。
- 模型列表和可售套餐底线。
- 表单校验、错误提示、权限控制。

代理可配置：

- Logo。
- Favicon。
- 首页大图 / Hero 图片。
- 主题色。
- 站点名称。
- 标题文案。
- 首页副标题。
- 公告。
- 客服入口。
- 套餐展示文案。
- 底部备案 / 版权信息。
- 部分首页模块开关。

高级代理可选：

- 模板 A。
- 模板 B。
- 模板 C。
- 首页模式：默认。
- 首页模式：配置模式，使用 Banner JSON。
- 首页模式：自定义 HTML，后续按主办方要求和管理员审核机制开放。

高级模板只改变布局风格和展示节奏。自定义 HTML 不进入第一阶段；后续如开放，必须经过管理员审核、内容过滤和资源隔离。

### 9.3 代理站配置示例

统一前端根据租户配置渲染页面。例如：

```json
{
  "site_name": "AAA AI API",
  "logo": "https://cdn.xxx.com/aaa-logo.webp",
  "favicon": "https://cdn.xxx.com/aaa-favicon.ico",
  "theme_color": "#4f46e5",
  "hero_title": "稳定高速的 AI API 服务",
  "hero_subtitle": "支持 GPT、Claude、Gemini、DeepSeek 等模型",
  "hero_image": "https://cdn.xxx.com/aaa-home.webp",
  "announcement": "新用户充值满 100 送 10%",
  "customer_service_link": "https://t.me/xxx",
  "pricing_display_mode": "card",
  "footer_text": "© AAA API"
}
```

同一套代码可以渲染成不同代理站：

- `aaa.newapi.com`：蓝色主题 + AAA Logo + AAA 首页图。
- `bbb.newapi.com`：黑金主题 + BBB Logo + BBB 首页图。
- `ccc.newapi.com`：极简主题 + CCC Logo + CCC 文案。

### 9.4 第一阶段装修配置开放范围

第一阶段不要做复杂装修器，只开放最必要的品牌配置项。

| 配置项 | 是否开放 | 难度 | 说明 |
| --- | --- | --- | --- |
| 站点名称 | 开放 | 低 | 显示在首页、浏览器标题、后台名称 |
| Logo | 开放 | 低 | 代理最需要的品牌差异化 |
| Favicon | 开放 | 低 | 浏览器小图标 |
| 首页 Banner / Hero 图片 | 开放 | 低 | 差异化最明显 |
| 首页主标题 | 开放 | 低 | 例如“高速稳定 AI API 服务” |
| 首页副标题 | 开放 | 低 | 介绍支持模型、稳定性、价格优势 |
| 主题色 | 开放，但用色板 | 中 | 给 8-12 个主题色，不允许随便填颜色 |
| 公告栏 | 开放 | 低 | 活动、维护通知、充值优惠 |
| 客服链接 | 开放 | 低 | Telegram、微信、QQ、Discord、邮箱 |
| 套餐说明 | 部分开放 | 中 | 可以改文案，但价格底线由主站控制 |
| 页脚备案 / 版权 | 部分开放 | 中 | 主站可以强制保留技术支持信息 |
| 首页模式 | 部分开放 | 中 | 第一阶段只开放默认和配置模式，自定义 HTML 后续审核开放 |

### 9.5 固定首页模块

代理站首页使用固定模块，代理只能在允许范围内调整内容和开关。

固定模块：

- 顶部导航。
- Hero 首屏区域。
- 支持模型区域。
- 价格 / 套餐区域。
- 平台优势区域。
- 使用步骤区域。
- 常见问题区域。
- 页脚。

代理可配置：

- Hero 图片。
- Hero 标题。
- Hero 副标题。
- 主按钮文案。
- 公告内容。
- 优势卡片文案。
- 客服入口。
- 部分模块是否显示。

### 9.6 主站前端

主站需要完成：

- 首页 / 登录页品牌化。
- 主站用户中心。
- 代理加盟 / 代理说明入口。
- 子代理管理入口。
- 管理员后台首页。
- 子代理管理列表。
- 租户详情页。
- 全局渠道和模型配置页。
- 全局价格保护配置页。
- 全站统计看板。
- 全站充值和扣费流水页。
- 代理站装修配置审核 / 管理页。
- 图片资源下架管理。

设计重点：

- 主站应体现平台可信度。
- 管理后台应偏工具化和数据化，不做花哨营销风格。
- 关键指标要清晰，例如调用量、收入、成本、代理收益、活跃代理、异常代理。
- 具体视觉样式以后续主办方提供的参考图为准。

### 9.7 代理站前端

代理站需要完成：

- 代理站独立登录 / 注册页。
- 代理站首页。
- 控制台首页。
- API 密钥管理页。
- 钱包 / 充值 / 订单历史页。
- Playground AI 对话页。
- 使用日志页。
- 模型价格展示页。
- OEM 管理 / 品牌配置页。
- OEM 管理 / 我的用户组页。
- OEM 管理 / 我的用户页。
- OEM 管理 / 推广渠道页。
- OEM 管理 / 兑换码页。
- OEM 管理 / 开放 API 页。
- 代理商钱包 / 提现申请页。

品牌配置页按 TOKEN HUB 标准分为：

- 品牌 Tab：站点名称、Logo 地址、页脚版权、自定义域名。
- 联系 Tab：联系邮箱、手机号、微信号、微信二维码、QQ 号、文档链接。
- 充值 Tab：充值链接、充值说明。
- 内容 Tab：首页公告、关于我们、SEO 标题、SEO 描述。
- 协议 Tab：用户协议、隐私政策。
- 首页 Tab：默认模式、配置模式、自定义 HTML 模式。

第一阶段站点设置页至少包含：

- 站点名称。
- Logo。
- Favicon。
- 首页 Banner / Hero 图片。
- 主题色。
- 首页主标题。
- 首页副标题。
- 公告。
- 客服入口。
- 页脚信息。

代理商后台包含：

- 用户列表。
- Token 列表。
- 消耗统计。
- 收入统计。
- 收益统计。
- 用户组倍率配置。
- 兑换码管理。
- 推广渠道管理。
- 提现申请。
- API 代理开放 API 密钥。

设计重点：

- 同一套前端根据 `tenant` 加载不同品牌配置。
- 页面标题、Logo、主题色、公告根据租户配置动态变化。
- 代理商只能看到自己站点数据。
- 普通终端用户不能进入代理商管理页面。
- 代理站默认风格跟随主站品牌，代理自定义内容只开放在主站允许范围内。

### 9.8 图片上传和资源安全限制

不要简单地让代理随便上传任何文件。图片上传必须做格式、大小、内容类型和存储限制。

建议限制：

- 允许格式：`jpg`、`jpeg`、`png`、`webp`。
- 禁止格式：`svg`、`html`、`js`、`exe`、`zip`。
- 图片大小：单张不超过 2MB 或 5MB。
- 首页 Banner 比例：建议 16:9 或 3:1。
- Logo 比例：建议 1:1 或横版 4:1。
- 上传后自动压缩并转为 webp。
- 自动重命名，不保留原始文件名。
- 图片存储使用对象存储 + CDN。
- 图片记录必须绑定 `tenant_id`。
- 管理员至少可以下架违规图片。

### 9.9 参考图还原范围

主办方提供图片后，前端修改应按页面拆解，不建议只做局部截图替换。

需要根据图片还原或调整：

- 页面整体布局。
- 导航栏结构。
- 侧边栏样式。
- 登录页视觉。
- 表格样式。
- 卡片样式。
- 按钮、输入框、弹窗、标签等基础组件。
- 数据看板图表样式。
- 颜色、字体、间距、圆角和阴影。
- 移动端布局。

每个页面修改完成后，应输出对应页面截图给主办方确认。

### 9.10 品牌系统

第二阶段建议建立基础品牌配置：

```text
site_name
logo
favicon
theme_color
template_key
hero_title
hero_subtitle
hero_image
primary_button_text
announcement
customer_service_link
pricing_display_mode
footer_text
enabled_modules
```

前端应支持：

- 主站默认品牌。
- 代理站覆盖品牌。
- 未配置时回退主站默认值。
- 主办方统一修改默认品牌后，所有未自定义的代理站自动继承。
- 代理只能选择主站允许的主题色、模板和模块开关。

### 9.11 前端验收标准

第二阶段完成后应满足：

- 主站和代理站视觉上可以区分，但底层页面结构和业务流程由主站统一控制。
- 代理站可展示自己的 Logo、名称、主题色、首页图、公告和客服入口。
- 第一阶段不开放自定义 HTML 首页；后续开放时必须有管理员审核机制。
- 管理员、代理商、终端用户三类角色入口清晰。
- 主要页面移动端可用。
- 表格、筛选、搜索、分页可正常使用。
- 错误状态、空状态、加载状态完整。
- 前端不会展示用户无权限访问的数据。
- 图片上传格式、大小、存储和下架机制可用。
- 主办方提供的核心参考图已完成对应页面还原或合理适配。
- 关键页面已提供截图或测试地址给主办方确认。
- 主办方反馈的问题已按优先级完成修改。

## 10. API 模块规划

第一期建议至少提供以下接口模块。

### 10.1 租户模块

```text
GET    /api/tenant/current
GET    /api/tenant/settings
PATCH  /api/tenant/settings
GET    /api/tenant/site-config
PATCH  /api/tenant/site-config
GET    /api/tenant/site-config/theme-options
POST   /api/tenant/assets
GET    /api/tenant/domains
POST   /api/tenant/domains
DELETE /api/tenant/domains/:id
GET    /api/tenant/pricing
PATCH  /api/tenant/pricing
GET    /api/tenant/groups
POST   /api/tenant/groups
PATCH  /api/tenant/groups/:id
DELETE /api/tenant/groups/:id
GET    /api/tenant/group-pricing
PATCH  /api/tenant/group-pricing
GET    /api/tenant/stats
GET    /api/tenant/users
PATCH  /api/tenant/users/:id/status
PATCH  /api/tenant/users/:id/group
GET    /api/tenant/promotion-channels
POST   /api/tenant/promotion-channels
DELETE /api/tenant/promotion-channels/:id
GET    /api/tenant/redemption-codes
POST   /api/tenant/redemption-codes
PATCH  /api/tenant/redemption-codes/:id/status
GET    /api/tenant/wallet
POST   /api/tenant/wallet/recharge
POST   /api/tenant/wallet/redeem
GET    /api/tenant/wallet/orders
GET    /api/tenant/earning-logs
POST   /api/tenant/withdrawals
GET    /api/tenant/withdrawals
```

### 10.2 管理员 / 子代理模块

```text
GET    /api/admin/tenants
GET    /api/admin/tenants/:id
POST   /api/admin/agents
PATCH  /api/admin/agents/:id
POST   /api/admin/agents/:id/enable
POST   /api/admin/agents/:id/disable
GET    /api/admin/agent-levels
POST   /api/admin/agent-levels
PATCH  /api/admin/agent-levels/:id
POST   /api/admin/tenants/:id/suspend
POST   /api/admin/tenants/:id/activate
GET    /api/admin/tenants/:id/site-config
PATCH  /api/admin/tenants/:id/site-config
GET    /api/admin/tenants/:id/domains
POST   /api/admin/domains/:id/ssl-configured
POST   /api/admin/domains/:id/suspend
GET    /api/admin/assets
POST   /api/admin/assets/:id/takedown
GET    /api/admin/withdrawals
POST   /api/admin/withdrawals/:id/approve
POST   /api/admin/withdrawals/:id/reject
GET    /api/admin/promotion-channels
GET    /api/admin/redemption-codes
GET    /api/admin/stats
```

### 10.3 Token 模块

```text
GET    /api/tenant/tokens
POST   /api/tenant/tokens
PATCH  /api/tenant/tokens/:id
DELETE /api/tenant/tokens/:id
```

### 10.4 计费日志模块

```text
GET    /api/tenant/billing-logs
GET    /api/tenant/recharge-orders
GET    /api/admin/billing-logs
GET    /api/admin/recharge-orders
```

### 10.5 支付回调模块

按 TOKEN HUB 文档标准保留以下支付回调路径：

```text
POST   /pay/wxpay/notify
POST   /auth/alipay/notify
```

说明：

- `/pay/wxpay/notify` 是微信支付商户平台配置的回调地址。
- `/auth/alipay/notify` 是支付宝开放平台配置的异步通知地址。
- 两个回调都应由支付 / 认证服务处理，Nginx 转发到 `auth-service`。
- 回调处理必须完成订单校验、签名校验、幂等入账、用户余额增加和充值记录写入。

### 10.6 API 代理开放接口

仅 API 代理可用：

```text
POST   /api/open/token/create
GET    /api/open/token/:id
POST   /api/open/token/recharge
DELETE /api/open/token/:id
GET    /api/open/tokens
```

### 10.7 API 调用入口

保持兼容 OpenAI 风格：

```text
POST /v1/chat/completions
POST /v1/completions
POST /v1/embeddings
GET  /v1/models
```

调用入口必须完成：

- Token 鉴权。
- 租户识别。
- 模型权限校验。
- 余额校验。
- 扣费。
- 日志记录。

## 11. 权限与隔离要求

多租户系统最重要的是数据隔离。所有与代理站相关的数据查询都必须带上 `tenant_id`。

必须遵守：

- 代理商查询用户时，只能查询本租户用户。
- 代理商查询日志时，只能查询本租户日志。
- Token 必须绑定租户。
- 扣费日志必须绑定租户。
- 充值入账必须绑定租户。
- 管理员接口必须单独鉴权，不能和代理商权限混用。

建议后端增加统一方法：

```text
requireAdmin()
requireTenantOwner(tenant_id)
requireTenantActive(tenant_id)
scopeByTenant(query, tenant_id)
```

## 12. 部署建议

第一期推荐最小部署：

```text
域名 DNS
        ↓
Nginx
        ↓
后端服务
        ↓
auth-service / 支付回调服务
        ↓
PostgreSQL / MySQL
        ↓
Redis
```

DNS：

```text
A yourbrand.com       -> 64.90.4.114
A www.yourbrand.com   -> 64.90.4.114
A api.yourbrand.com   -> 64.90.4.114
A admin.yourbrand.com -> 64.90.4.114
A *.yourbrand.com     -> 64.90.4.114
```

HTTPS：

- 第一阶段使用 `*.yourbrand.com` 通配符证书。
- OEM 代理自定义域名按 TOKEN HUB 标准添加 A 记录到服务器公网 IP。
- 管理员为代理自定义域名手动配置 SSL 证书。
- 国内服务器需要注意域名备案和实名要求。

### 12.1 支付回调配置

TOKEN HUB 文档中支付回调由 `auth-service` 处理。部署时必须保证以下公网回调地址可以被微信支付和支付宝访问：

```text
微信支付回调：https://你的域名/pay/wxpay/notify
支付宝回调：https://你的域名/auth/alipay/notify
```

如果主站域名为 `yourbrand.com`，则配置为：

```text
微信支付回调：https://yourbrand.com/pay/wxpay/notify
支付宝回调：https://yourbrand.com/auth/alipay/notify
```

微信支付配置到服务器配置文件：

```text
/www/wwwroot/auth-service/config/config.yaml
```

参考：

```yaml
wxpay:
  mch_id: "你的商户号"
  app_id: "你的AppID"
  mch_cert_serial_num: "证书序列号"
  private_key_path: "/path/to/apiclient_key.pem"
  pub_key_path: "/path/to/pub_key.pem"
  pub_key_id: "公钥ID"
  v3_key: "APIv3密钥"
```

支付宝证书上传到服务器后，同样配置到：

```text
/www/wwwroot/auth-service/config/config.yaml
```

参考：

```yaml
alipay:
  app_id: "你的AppID"
  private_key_path: "/path/to/app_private_key.pem"
  app_cert_path: "/path/to/appCertPublicKey.crt"
  root_cert_path: "/path/to/appCertPublicKey.crt"
  public_cert_path: "/path/to/alipayCertPublicKey_RSA2.crt"
  notify_url: "https://yourbrand.com/auth/alipay/notify"
  is_prod: true
```

### 12.2 Nginx 回调转发

Nginx 需要把主应用、微信支付回调和支付宝回调分别转发到对应服务。按 TOKEN HUB 文档写法：

```nginx
server {
    listen 443 ssl;
    server_name yourbrand.com;

    ssl_certificate /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;

    location / {
        proxy_pass http://127.0.0.1:3000;
        proxy_set_header Host $http_host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }

    location ^~ /pay/ {
        proxy_pass http://127.0.0.1:8080/pay/;
        proxy_set_header Host $http_host;
        proxy_set_header X-Real-IP $remote_addr;
    }

    location ^~ /auth/ {
        proxy_pass http://127.0.0.1:8080/auth/;
        proxy_set_header Host $http_host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

回调处理要求：

- 回调必须校验支付平台签名。
- 回调必须按订单号做幂等处理，防止重复入账。
- 回调入账必须绑定 `tenant_id`、`user_id`、订单号和充值额度。
- 微信、支付宝回调成功后必须写入充值记录，并更新用户 API 额度。
- 如果用户属于代理商，还需要触发充值差价收益计算。

DNS / HTTPS 调试注意：

- 如果本地 macOS DNS 返回 `198.18.x.x`，应视为 Clash / sing-box / TUN fake-ip，不要直接判断为真实公网 DNS。
- 排查域名时应对比：

```bash
dig domain +short
dig domain @1.1.1.1 +short
ssh server "dig domain +short"
dig +trace domain
```

## 13. 主要风险

### 13.1 三天时间风险

3 天只能完成全栈可演示 MVP，不适合承诺完整商业闭环和高完成度品牌视觉。需要严格控制范围，优先保证代理功能主流程可以真实跑通。

控制方式：

- 第一阶段只做 wildcard 二级域名。
- 第一阶段只做统一商户收款。
- 第一阶段必须保留用户组倍率、充值差价、消耗分润和扣费日志。
- 第一阶段只做简洁可用页面，不做主办方参考图级别的精细还原。

### 13.2 数据隔离风险

多租户系统最容易出现跨租户数据泄露。

控制方式：

- 所有代理站数据表必须带 `tenant_id`。
- 所有代理站查询必须强制 tenant scope。
- 增加跨租户访问测试用例。

### 13.3 用户组倍率和成本价风险

如果代理商可以随便设置用户组倍率，可能低于代理成本价或主站保护线，导致充值差价为负。

控制方式：

- 主站设置用户组最低倍率和代理成本价。
- 后端强校验用户组倍率，不依赖前端限制。
- 充值时计算用户实际支付、代理成本和充值差价。
- 发现负收益或低于保护线时直接拦截。

### 13.4 充值入账一致性风险

代理站模式下，终端用户充值必须准确入账到对应租户和用户，不能只记录主站用户余额。

控制方式：

- 第一阶段统一使用主站收款。
- 微信支付回调地址必须配置为 `/pay/wxpay/notify`。
- 支付宝异步通知地址必须配置为 `/auth/alipay/notify`。
- Nginx 必须把 `/pay/` 和 `/auth/` 转发到 `auth-service`。
- 充值入账必须绑定 `tenant_id` 和 `user_id`。
- 充值、人工入账、扣费、退款修正都必须写入日志。
- 管理员后台需要能按租户筛选充值和扣费流水。

### 13.5 主办方需求变更风险

第二阶段需要根据主办方后续提供的图片和需求修改前端，视觉范围可能发生变化。

控制方式：

- 第二阶段开始前先收集主办方参考图片和品牌素材。
- 先确认页面清单，再开始逐页改造。
- 每个页面改完提供截图或测试地址确认。
- 新增页面、重做交互、替换整体风格应作为变更项单独评估工期。

### 13.6 代理前端 DIY 和上传资源风险

如果允许代理自由编写前端代码或随意上传文件，会带来安全、合规、支付流程失控和品牌失控风险。TOKEN HUB 标准保留自定义 HTML 首页能力，但它不应进入第一阶段。

控制方式：

- 第一阶段不开放自定义 HTML。
- 后续如开放自定义 HTML，必须管理员审核后发布。
- 不开放代理自行修改支付、充值、登录、API Key 和价格计算页面逻辑。
- 前端只允许通过 `tenant_site_configs` 做受控配置。
- 主题色只能从主站预设色板选择。
- 上传文件只允许图片格式，并限制大小、类型和存储路径。
- 管理员可以下架违规 Logo、首页图、公告和客服链接。

### 13.7 自定义域名与 SSL 配置风险

TOKEN HUB 标准中，自定义域名由代理在品牌配置中填写，DNS 添加 A 记录到服务器公网 IP，再由管理员手动配置 SSL 证书。风险主要在于代理 DNS 配置错误、SSL 未配置或证书过期。

控制方式：

- 第一阶段只做 `aaa.yourbrand.com` 这种平台二级域名。
- OEM 自定义域名按 TOKEN HUB 标准使用 A 记录指向服务器公网 IP。
- 代理保存自定义域名后，状态为等待管理员配置 SSL。
- 管理员配置 SSL 后，将域名状态改为已配置。
- SSL 未配置的域名不能作为正式可用域名对外展示。
- 管理员可以暂停异常域名绑定。

## 14. 推荐里程碑

### 第一阶段：全栈可演示 MVP，3 天

交付结果：

- 用户可注册账号，管理员可将用户设置为子代理。
- 管理员可把普通用户设置为普通代理 / OEM 代理 / API 代理。
- 可通过二级域名识别租户。
- 可自动生成并绑定平台二级域名，例如 `aaa.yourbrand.com`。
- 可设置代理用户组倍率。
- 可创建推广渠道和兑换码。
- 可计算充值差价和消耗分润。
- 可提交和审核代理提现。
- 可创建 API Key。
- 可调用 API 并按租户扣费。
- 可在浏览器页面查看使用日志和基础统计。
- 超级管理员、代理商、终端用户三类角色都有最小可用页面。

### 第二阶段：统一前端 + 受控装修配置，预计 5-10 天

交付结果：

- 根据主办方参考图片调整主站界面。
- 根据主办方参考图片调整统一代理站前端。
- 代理站配置可生效，包括 Logo、Favicon、首页图、主题色、标题、公告、客服入口和底部信息。
- 页面结构、支付流程、充值流程、登录注册、API Key 页面和价格展示逻辑由主站统一控制。
- 高级代理可选择模板 A / B / C。
- 支持 OEM 自定义域名配置，例如 `ai.example.com`。
- 自定义域名按 TOKEN HUB 标准添加 A 记录到服务器 IP，并由管理员配置 SSL 证书。
- 管理员、代理商、终端用户角色体验完整。
- 主办方验收反馈完成修改。

### 第三阶段：运营增强，预计 2-4 周

交付结果：

- 更完善的自定义域名管理和 SSL 到期提醒。
- 更完整风控。
- 更完整财务报表。
- 运营后台。

## 15. 第一阶段最终验收清单

- [ ] 用户可以注册和登录。
- [ ] 管理员可以把用户设置为普通代理 / OEM 代理 / API 代理。
- [ ] 管理员可以配置代理成本价、套餐折扣、消耗分润比例和代理等级。
- [ ] wildcard 二级域名可以正确路由到租户。
- [ ] 代理开站后会生成 `tenant_domains` 记录，例如 `aaa.yourbrand.com`。
- [ ] 未激活或已禁用代理无法服务。
- [ ] 代理可以配置站点基础信息。
- [ ] 代理可以配置 Logo、Favicon、首页 Hero 图片、标题、副标题、公告和客服入口。
- [ ] 代理主题色只能从主站预设色板选择。
- [ ] 第一阶段不开放自定义 HTML 首页；后续开放时必须具备管理员审核机制。
- [ ] 代理可以配置用户分组倍率。
- [ ] 代理可以把终端用户分配到不同分组。
- [ ] 用户组倍率不能低于主站最低价格保护线。
- [ ] 代理可以创建推广渠道并生成注册链接。
- [ ] 用户通过代理推广链接注册后归属到对应代理。
- [ ] 代理可以创建兑换码，且从代理 API 额度中预扣。
- [ ] 终端用户可以充值并获得 API 额度。
- [ ] 微信支付回调 `/pay/wxpay/notify` 可访问并能正确入账。
- [ ] 支付宝回调 `/auth/alipay/notify` 可访问并能正确入账。
- [ ] 钱包页面可以查看当前余额、订单历史和兑换码入口。
- [ ] 充值成功后可以计算代理充值差价收益。
- [ ] 终端用户可以创建 API Key。
- [ ] 终端用户可以使用 Playground 发起模型调用。
- [ ] API 调用可以识别租户和用户。
- [ ] API 调用可以完成余额校验和扣费。
- [ ] API 调用后可以计算代理消耗分润。
- [ ] 使用日志记录模型、输入 Token、输出 Token、消耗额度、API Key 和渠道。
- [ ] 代理可以查看自己的用户、推广渠道、兑换码、收益和可提现余额。
- [ ] 代理可以提交提现申请，管理员可以审核。
- [ ] API 代理可以通过开放 API 创建、查询、充值和删除 Token。
- [ ] 超级管理员可以查看全局统计。
- [ ] 代理商只能查看自己租户统计。
- [ ] 跨租户访问被拦截。
- [ ] 第一阶段接口文档完成。
- [ ] 第一阶段部署说明完成。
