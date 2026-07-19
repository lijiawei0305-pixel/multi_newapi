# New API 多租户代理分销平台 — 开发方案（proposal v2.0）

> **文档定位**：本文件是项目的**权威需求文档（完整替代版）**，整合并取代 `../newapi-multitenant-development-plan.md`，对标 `../TOKEN HUB 文档.md`，并新增 **tokenplan 套餐**、**可行性分析**、**改进建议**、**技术选型/实现细节** 与 **第二阶段前端接口预留**。
> **范围**：本期只交付**第一阶段**，但所有第二阶段前端能力均在第一阶段**预留接口/字段/插槽**。
> **基座**：基于 New API（One API 衍生）二次开发；前期 UI 沿用 New API 风格，第二阶段再做品牌化。
> 版本 v2.0 ｜ 替代 plan v1.0 ｜ 栈：Go 1.25.1+ / Bun / MySQL 8.0+ / Nginx 1.18+ / Docker Compose（工具链版本以 `go.mod` 与 `web/bun.lock` 为准）
> **读法**：本文定义业务需求；其中“一期/二期/待开发/工期”是方案形成时的规划语境，不能据此判断当前完成度。已建成功能与真实剩余项只看 [`tasks/STATUS.md`](tasks/STATUS.md)，验收决策看 [`acceptance.md`](acceptance.md)。

---

## 0. 决策记录（本次互动确认）

| 决策点 | 结论 |
| --- | --- |
| tokenplan 计费本质 | 固定价买 30 天 → **月度上游额度封顶（月限额 USD）**，x1 计价，**独立于钱包**，用满/到期即停、手动重购 |
| 日/周限额 | **取消**，仅保留月限额（小米式月度额度） |
| 套餐到期 | 到期清零，手动重购（不自动续订） |
| 套餐与钱包关系 | **独立计量**：买套餐的用户调用走套餐桶，不动钱包余额，二者并行互不干扰 |
| 代理定价权 | 主站定 6 个基准套餐（Trial~Max）；代理**可在保护线内改零售价**，月限额/结构由主站锁定 |
| 代理收益 | **充值差价 + 消耗分润**双模式（沿用现有，超管可配） |
| 成本保护 | tokenplan **纳入**最低成本价/最低利润率保护线 |
| 一期范围 | tokenplan **后端完整 + New API 风格购买/展示页 + 代理上架开关** |
| 二期前端预留 | OEM 品牌换肤、代理自定义首页/装修、tokenplan 营销化展示、多语言/多币种/移动端 — **全部预留** |

> 仍待你最终确认的小项（已在文中标 `[待确认]`）：x1 是否=按模型上游成本价 1:1 计量；Trial 限购口径。套餐耗尽/过期**绝对不回退钱包**已由验收决策 D5 锁定；Redis 已作为缓存/风控依赖引入（数据库仍是财务权威），支付已确定为主站进程内 SDK，不再使用独立 auth-service。

---

## 目录

1. 项目定位与目标
2. **可行性分析** ★
3. **改进建议** ★
4. 总体架构
5. 角色与权限
6. 核心数据模型（含 tokenplan 新增表）
7. 计费、收益与成本保护
8. **tokenplan 套餐设计** ★
9. 域名与租户识别
10. 第一阶段落地范围（含 tokenplan + 工期重估）
11. **第二阶段前端接口预留** ★
12. **技术选型 / 实现细节** ★
13. API 模块规划（含 tokenplan 接口）
14. 权限与隔离要求
15. 部署方案
16. 风险与对策（含 tokenplan 新风险）
17. 里程碑
18. 第一阶段验收清单（含 tokenplan 验收项）

---

## 1. 项目定位与目标

基于 New API 二次开发的**多租户代理分销平台**，对标 **TOKEN HUB v1.0**。**单套后端 + 多租户隔离 + 统一 API 网关**，不是给每个代理部署独立 New API。

- **主站**：统一管控上游渠道、模型、支付、计费、风控、系统配置与管理员能力。
- **代理商**（普通 / OEM / API 三类）：在主站体系内获得分销、品牌定制、用户组倍率、兑换码、开放 API、**tokenplan 套餐转售**等能力，管理下级用户与收益。
- **终端用户**：经主站、代理域名或推广链接注册，归属对应代理商；可按量充值（钱包）或购买 tokenplan 套餐。

**核心目标**（在 plan v1.0 基础上 + tokenplan）：

- 主站统一管理上游渠道、模型、用户、充值、扣费、风控、统计、流水与代理参数。
- 支持普通代理 / OEM 代理 / API 代理三种类型。
- 代理可管理下级用户、用户组倍率、兑换码、推广渠道、收益数据，并**上架/退出 tokenplan 套餐**。
- OEM 代理可配置品牌信息、Logo、首页、联系信息与自定义域名（二期）。
- API 代理可通过开放 API 创建/查询/充值 Token。
- 终端用户可注册、登录、按量充值或买套餐、创建 API Key、调用模型、查看日志。

---

## 2. 可行性分析 ★

### 2.1 New API 基座能力盘点 vs 需求 Gap

| 需求能力 | New API 原生 | 结论 |
| --- | --- | --- |
| 用户 / Token / 渠道 / 模型 / 日志 / 额度(quota) | ✅ 有 | 直接复用 |
| 分组(group) 与分组倍率 | ✅ 有（**全局**，非租户隔离） | 需加 `tenant_id` 隔离改造 |
| 模型倍率 / 补全倍率 / 令牌额度 / RPM-TPM 限流 | ✅ 有 | 复用 |
| 充值 / 兑换码 / 订单 | ✅ 有 | 复用并加租户维度 |
| OpenAI 兼容中继 `/v1/*` | ✅ 有 | 复用，入口加租户识别 |
| **多租户隔离层**（tenant 贯穿全表/全查询） | ❌ 无 | **核心自研** |
| **按 Host 识别租户**的路由中间件 | ❌ 无 | 自研 |
| **代理体系**：类型/等级/钱包(可提现)/收益/提现 | ❌ 无 | 自研 |
| **充值差价 + 消耗分润** 收益计算 | ❌ 无 | 自研 |
| **代理站装修配置 + 自定义域名** | ❌ 无 | 自研（二期前端，一期建数据层） |
| **tokenplan 订阅制**（月度封顶/到期清零/独立桶） | ❌ 无 | **全新自研** |
| **成本保护线**（floor/min-margin 强校验） | ❌ 无 | 自研 |

**结论**：基座覆盖了"调用/计费/中继"底盘，**约 50–60% 可复用**；多租户隔离、代理收益、tokenplan、成本保护是**主要自研工作量**，可行性高但不是"配置即得"。

### 2.2 TOKEN HUB 对标差距

TOKEN HUB 已实现的能力本方案**全部对标复刻**：品牌配置 / 首页定制 / 自定义域名 / 用户组倍率 / 双收益模式 / 兑换码 / 我的用户 / 推广渠道 / 开放 API / 管理员渠道·模型·用户·子代理·提现管理 / 系统设置。**tokenplan 套餐是本方案在 TOKEN HUB 之上的增量**，TOKEN HUB 文档中无对应能力，需独立设计。

### 2.3 tokenplan 计费模型可行性

- New API **无原生订阅**；需新增「订阅实例 + 周期用量计量 + 独立额度桶」。
- 简化后（仅月限额、到期清零、独立计量）**可行性高**：本质 = "带 30 天有效期 + 月度消耗封顶 + 独立 USD 额度桶"。
- 计量链路：每次调用结束 → 计算本次 `upstream_cost_usd` → 原子累加到订阅 `used_usd` → 达到 `month_limit` 或过期 → 该订阅失效、拦截后续调用。
- 主要难点：与现有"钱包 quota 扣费"并存（**双桶路由**），以及**并发扣减超额**的一致性。

### 2.4 ⚠️ 定价模型可行性（最关键发现）

把你的 6 档套餐反推（按 ¥7/$ 上游成本汇率）：

| 套餐 | 月限额(USD) | 满额上游成本(≈¥7/$) | 成本估算(¥) | 估算/满额 | 售价(¥) |
| --- | --- | --- | --- | --- | --- |
| Trial | $80 | ≈¥560 | 6.00 | **1.07%** | 6.9 |
| Mini | $220 | ≈¥1,540 | 16.50 | **1.07%** | 119 |
| Solo | $560 | ≈¥3,920 | 42.00 | **1.07%** | 279 |
| Lite | $2,200 | ≈¥15,400 | 165.00 | **1.07%** | 899 |
| Pro | $7,200 | ≈¥50,400 | 540.00 | **1.07%** | 2,699 |
| Max | $25,000 | ≈¥175,000 | 1,875.00 | **1.07%** | 8,999 |

**发现**：6 档「成本估算/满额成本」恒为 ~1.07% → 定价假设**用户只消耗月限额的约 1%（月限额≈预期消耗的 93 倍）**。整个商业模型依赖 **breakage（用户用不满）**。

**风险与对策**（详见 §16）：

- 若 tokenplan 被**刷满 / Key 共享 / 二次倒卖**，单个 Max 用户满额上游成本 ≈¥17.5 万 vs 售价 ¥8999 → **巨亏**。
- **必须配套**：① 套餐**限购**（尤其 Trial 每用户/每实名/每设备 1 次）；② **防滥用风控**（异常调用、并发、IP/UA 指纹）；③ **满额逼近监控/告警**；④ 月限额按真实成本**重新标定**或保留主站随时下调能力。
- Trial（售价≈成本）定位为**引流亏本款**，需最强限购。

> 这是 tokenplan 第一期**必须做**的边界，否则不建议上线大额套餐（Pro/Max 可先灰度）。

---

## 3. 改进建议 ★（对 plan v1.0 的优化）

1. **数据库统一 MySQL 8.0**：plan §12 写 "PostgreSQL / MySQL" 混用，技术栈已确认 MySQL，建议全文统一为 MySQL，去掉 PG 表述，避免方言分歧。
2. **隔离下沉到 ORM 层**：不要每个查询手写 `WHERE tenant_id=?`（易漏 → 越权）。建议 GORM **global scope / 统一 `scopeByTenant`** + 中间件注入 `ctx.tenant`，从框架层强制租户边界。
3. **统一「额度桶」抽象**：把"钱包 quota"和"tokenplan 桶"抽象为 `QuotaSource` 接口（`Charge()/Balance()`），扣费按路由规则选桶，避免两套扣费逻辑分叉。
4. **统一定价/保护服务**：分组倍率、tokenplan 零售价共用一个 `PricingGuard.validate(price, cost, minMargin)`，保护线只实现一处。
5. **Redis 定位明确**：Redis 用于 **Host→tenant 解析缓存 / 限流 / 会话**；财务、额度和订单写入不得以 Redis 成功代替数据库持久化。
6. **支付边界明确**：一期采用**主站单体内置回调**，微信/支付宝 SDK 与回调路由都由主站提供，无独立支付容器或 Nginx upstream。
7. **tokenplan 纳入主数据模型**：新增 4 张表 + `tenants.tokenplan_enabled` 开关（见 §6.17–6.20），与现有计费同源。
8. **工期重估**：3 天 MVP 已偏满，叠加 tokenplan 需现实加时（见 §10.5），避免承诺落空。

---

## 4. 总体架构

```text
用户访问 aaa.yourbrand.com / ai.example.com（二期自定义域名）
        ↓
Nginx / 网关（443，wildcard 证书；所有 `/api/` 请求统一转发主站）
        ↓
Tenant Router：按 Host 识别租户（tenant_domains / slug，Redis 缓存）
        ↓
代理站前台 / 控制台（同一套前端，按 tenant_site_configs 渲染）
        ↓
统一 API 网关层：鉴权 → 租户识别 → 模型权限 → 额度桶路由（钱包/套餐）→ 扣费 → 限流/风控 → 日志
        ↓
主站上游渠道池：OpenAI / Azure / Claude / Gemini / DeepSeek / Qwen 等
```

域名规划：主站 `www|api|admin.yourbrand.com`；代理 `*.yourbrand.com`（一期）；OEM 自定义域名（二期）。

---

## 5. 角色与权限

### 5.1 超级管理员
全局权限：管理全部用户/代理；增删子代理；设代理类型（普通/OEM/API）、成本价、套餐折扣、消耗分润比例、代理等级；配置上游渠道、模型倍率、分组保护线、最低成本价/利润率；**管理 tokenplan 套餐（售价/原价/月限额/成本价/保护线）**；管理模型广场、渠道、用户、兑换码、推广、系统设置；查看全站消耗/充值/扣费/代理收益/提现；禁用代理或其下级用户。

### 5.2 代理商（租户 owner，三类）

| 类型 | 功能 |
| --- | --- |
| 普通代理 | 基础推广：用户分组、兑换码、推广渠道、下级用户、**tokenplan 上架/退出+改零售价** |
| OEM 代理 | 完整品牌定制：自定义域名、Logo、首页、联系信息（二期前端） + 普通代理全部能力 |
| API 代理 | 开放 API 接入 + **OEM 代理全部能力**（2026-07-08 定档：三档递进超集 普通⊂OEM⊂API，套餐授予 `grant_level=1 + can_api`；原「+普通代理全部能力」口径作废） |

代理可：看控制台数据；管下级用户；建用户组倍率；建兑换码（从自己额度预扣）；建推广渠道；看可提现余额/累计收益/收益来源；提交提现；**上架/退出 tokenplan 并在保护线内改零售价**。
代理**不能**：改主站上游渠道；改模型官方成本价；设低于保护线的倍率/套餐价；看其他代理数据；绕过主站统一扣费与风控；接自己的支付通道。

### 5.3 终端用户
经主站/代理域名/推广链接注册并归属代理：注册登录、按量充值或**购买 tokenplan 套餐**、创建 Token、Playground 对话、调用模型、查额度/订单/日志/扣费。

---

## 6. 核心数据模型

> 字段沿用 plan v1.0（§6.1–6.16），并新增 tokenplan 4 表（§6.17–6.20）。所有租户维度表必须带 `tenant_id` 并建索引。统一 MySQL 8.0 / utf8mb4 / UTC。

### 6.1–6.16 既有表（保留）
`tenants`（+ 新增 `tokenplan_enabled`）、`tenant_site_configs`、`tenant_domains`、`tenant_users`、`tenant_groups`、`tenant_tokens`、`tenant_pricing`、`tenant_group_pricing`、`tenant_billing_logs`、`agent_levels`、`agent_wallets`、`agent_earning_logs`、`agent_withdrawals`、`agent_promotion_channels`、`agent_redemption_codes`、`agent_open_api_keys`。

> 字段清单见 plan v1.0 §4，本文不重复抄录；改动点：
> - `tenants` 增 `tokenplan_enabled BOOLEAN`（代理级 tokenplan 总开关）。
> - `agent_earning_logs.source_type` 增 `tokenplan_spread`、`tokenplan_commission`。

### 6.17 token_plans（主站套餐定义）
```sql
token_plans
- id
- code            -- trial / mini / solo / lite / pro / max
- name
- base_price      -- 主站官方售价（¥/30天）
- anchor_price    -- 原价（营销锚点划线价，仅展示，不参与计费）
- discount_label  -- 如 "-94%"
- multiplier      -- 计量倍率，默认 1.0（x1）
- month_limit_usd -- 月度上游额度封顶（USD）
- valid_days      -- 有效期，默认 30
- upstream_cost_est -- 上游成本估算（¥，P&L 参考）
- agent_cost_price  -- 主站给代理的进货成本价（¥，差价收益基准）
- min_price       -- 代理零售价保护线（¥，= 成本价 ×(1+min_margin) 或 floor）
- is_recommended  -- 营销：推荐/热门（二期展示用，一期预留）
- badge           -- 营销角标文案（二期预留）
- sort
- status          -- enabled / disabled
- created_at / updated_at
```

### 6.18 tenant_token_plans（代理上架与定价）
```sql
tenant_token_plans
- id
- tenant_id
- plan_id
- enabled         -- 代理是否上架该套餐（退出=false）
- retail_price    -- 代理零售价（¥），后端强校验 >= token_plans.min_price
- created_at / updated_at
- UNIQUE(tenant_id, plan_id)
```

### 6.19 user_subscriptions（用户购买的套餐实例）
```sql
user_subscriptions
- id
- tenant_id
- user_id
- plan_id
- purchased_price -- 实付（¥）
- month_limit_usd -- 购买时快照
- used_usd        -- 已消耗（USD，累加）
- status          -- active / exhausted / expired / refunded
- start_at
- expire_at       -- start_at + valid_days
- source_order_id -- 关联充值/支付订单
- created_at / updated_at
- INDEX(tenant_id, user_id, status)
```

### 6.20 subscription_usage_logs（套餐内调用计量）
```sql
subscription_usage_logs
- id
- subscription_id
- tenant_id
- user_id
- model
- prompt_tokens / completion_tokens
- upstream_cost_usd  -- 本次按 multiplier 计的上游成本
- request_id
- created_at
- INDEX(subscription_id)
```

---

## 7. 计费、收益与成本保护

两类余额（沿用 TOKEN HUB）：**当前余额/API 额度**（调用扣减）与**可提现余额**（代理人民币收益）。

- **用户组倍率**：1.0 正常 / 0.9 优惠 / 1.5 溢价；`用户组倍率 ≥ 主站 group_floor_ratio` 且 `实付 ≥ 代理成本 + 最低利润保护`，后端强校验。
- **充值差价**（代理盈利一）：`差价 = 用户实付 − 代理成本`，写 `agent_earning_logs(recharge_spread)` 并增 `withdrawable_balance`。
- **消耗分润**（代理盈利二）：`分润 = 用户本次消耗 × 分润比例`，写 `agent_earning_logs(consume_commission)`。
- **模型扣费**：`扣费 = 输入Token×输入单价 + 输出Token×输出单价`，可乘分组模型倍率（保护线内）。
- **提现**：申请 → 冻结 → 管理员审核 → 通过线下打款/拒绝解冻；`实际到账 = 提现金额 − 手续费`。
- **tokenplan 计费**：见 §8，走独立桶，收益同样支持差价+分润（`tokenplan_spread` / `tokenplan_commission`）。

---

## 8. tokenplan 套餐设计 ★

### 8.1 计费模型（确认语义）

> **买套餐（固定价）→ 获得 30 天有效期 + 月度上游额度封顶（month_limit USD）→ 调用按 multiplier(x1) 从套餐桶扣减 USD → 用满 month_limit 或到期 → 套餐失效、拦截后续调用、需手动重购。无日/周限额。套餐桶独立于钱包余额。**

- **x1 含义**：套餐内调用按"模型上游成本价 × 1.0"计入 `used_usd`，**不叠加分组倍率**（套餐是"成本价直供"型）。`[待确认]`
- **桶路由规则**（独立计量）：用户存在 `active` 套餐 → 调用走套餐桶；**套餐耗尽/过期 → 调用被拦截并提示重购，不自动回退钱包**（验收决策 D5 已确认）；无套餐的用户 → 走钱包 quota（原逻辑）。
- **到期**：`expire_at < now()` → 状态置 `expired`（定时任务 + 调用时惰性校验双保险）。

### 8.2 套餐表（主站基准，Trial~Max）

| 套餐 | 售价(¥/30天) | 原价(¥) | 折扣 | 倍率 | 月限额(USD) | 成本估算(¥) | 毛利估算(¥)* |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Trial 引流体验包 | **6.9** | 1,020 | -99% | x1 | $80 | 6.00 | ≈0.9（贴本引流） |
| Mini | **119** | 1,360 | -91% | x1 | $220 | 16.50 | ≈102.5 |
| Solo | **279** | 3,400 | -92% | x1 | $560 | 42.00 | ≈237 |
| Lite | **899** | 13,892 | -94% | x1 | $2,200 | 165.00 | ≈734 |
| Pro | **2,699** | 45,975 | -94% | x1 | $7,200 | 540.00 | ≈2,159 |
| Max | **8,999** | 160,000 | -94% | x1 | $25,000 | 1,875.00 | ≈7,124 |

> *毛利估算 = 售价 − 成本估算，**基于"预期消耗"（≈月限额 1%）**，非满额。满额上游成本远高于售价（见 §2.4 / §16），故 month_limit 是封顶闸门 + 依赖 breakage。
> 「原价」仅营销锚点划线，不参与计费。日/周限额列已按确认删除。

### 8.3 代理与 tokenplan

- **上架/退出开关**：`tenants.tokenplan_enabled` + `tenant_token_plans.enabled`（按套餐粒度）。**主站与代理都可开放**该服务；代理可**自行退出**（关闭上架）。
- **代理改零售价**：代理可设 `tenant_token_plans.retail_price`，月限额/有效期/结构由主站锁定不可改。
- **成本保护线**：后端强校验 `retail_price ≥ token_plans.min_price`（= `agent_cost_price ×(1+min_margin)` 或 floor），防代理亏本甩卖冲击主站。
- **代理收益**（双模式，超管可配）：
  - 差价：`tokenplan_spread = retail_price − agent_cost_price` → `agent_earning_logs(tokenplan_spread)`。
  - 分润：用户套餐内消耗按比例 → `agent_earning_logs(tokenplan_commission)`。
- **限购**（防刷，尤其 Trial）：每用户/每实名/每设备对 Trial 限购 1 次，其余档可配上限。`[待确认]` 口径。

### 8.4 购买与计量流程

```text
用户在购买页选套餐
        ↓
下单支付（复用钱包/支付通道，订单 type=subscription）
        ↓
支付成功回调 → 幂等创建 user_subscriptions(active, start_at, expire_at=+30d, used_usd=0)
        ↓
计算代理 tokenplan 差价收益（如启用）
        ↓
用户调用 /v1/* → 命中 active 套餐 → 计 upstream_cost_usd
        ↓
原子累加 used_usd（行锁/CAS）；超 month_limit → 拒绝并置 exhausted
        ↓
到期/耗尽 → 拦截，提示重购
```

### 8.5 与现有计费整合与边界
- 套餐调用写 `subscription_usage_logs` + `tenant_billing_logs`（标记来源=subscription）。
- 钱包 quota 与套餐桶**互不混用**（独立计量）；UI 明确显示"当前计量来源"。
- 保护线、收益日志与现有体系同源（共用 `PricingGuard` 与 `agent_earning_logs`）。

---

## 9. 域名与租户识别

- **一期**：仅 wildcard 二级域名 `*.yourbrand.com`；按 Host → `slug` → tenant。保留 `www/api/admin/root/dashboard/static/cdn/status/support` 不可被代理申请；slug 唯一性 + 保留校验。
- **二期**：OEM 自定义域名（代理填域名 → DNS A 记录指向公网 IP `64.90.4.114` → 管理员配 SSL → `tenant_domains.ssl_status=configured`）。未配 SSL 不对外展示。
- **路由**：读 `Host` → 查 `tenant_domains`（Redis 缓存）→ 命中加载租户配置渲染；未命中显示"站点未开通"。
- **HTTPS**：一期 `*.yourbrand.com` 通配符证书；二期自定义域名由管理员手动配证书。

---

## 10. 第一阶段落地范围（含 tokenplan + 工期重估）

### 10.1 一期闭环（沿用 plan §7 全部）
用户注册/登录 → 管理员设为代理（普通/OEM/API）→ 生成 tenant + 二级域名 → 终端用户经代理链接/域名注册 → 充值得额度 → 代理按倍率得差价 → 用户建 Key 调模型 → 扣费/日志/分润 → 代理查用户/收益/推广/兑换码/提现。

### 10.2 一期页面范围（New API 风格，能完整使用）
- **主站/管理员**：登录注册、控制台、API Key、钱包充值、使用日志、全局统计、用户管理、子代理管理、代理等级管理、提现管理、**tokenplan 套餐管理**。
- **代理商**：品牌配置（基础）、我的用户组、我的用户、推广渠道、兑换码、钱包/提现、开放 API 密钥、**tokenplan 上架/改价管理页**。
- **终端用户**：控制台、Token 创建/列表、余额、钱包充值、**tokenplan 购买/我的套餐页**、Playground、调用日志。

### 10.3 一期 tokenplan 范围（后端完整 + 购买页）
套餐 CRUD（管理员）、上架/改价（代理）、购买/支付/创建订阅、月度计量与超额/到期拦截、收益写入、限购防刷、New API 风格购买页与"我的套餐"页。

### 10.4 一期暂不做（顺延二/三期）
OEM 自定义域名 + SSL、高完成度品牌化前端、自定义 HTML 首页、多语言、精细风控引擎、复杂财务报表。

### 10.5 ⚠️ 工期重估
plan 原定 **3 天** MVP 已偏满（plan §13.1 自承时间风险）。叠加 tokenplan（后端完整 + 购买页 + 限购/计量一致性）建议：

| 方案 | 说明 | 现实工期 |
| --- | --- | --- |
| A（推荐） | 核心多租户 MVP（原 3 天范围）**先交付**，tokenplan 作为一期第二批 +1.5~2 天 | **≈4.5–5 天** |
| B | tokenplan 与核心并行（需 2 人分工） | ≈3.5–4 天 |
| C | 严格 3 天：tokenplan 只做后端+数据层，购买页降级为最简列表 | 3 天（功能打折） |

> 建议方案 A：先稳核心闭环，再上 tokenplan，避免计量一致性/防刷仓促埋雷。`[待你选择 A/B/C]`

---

## 11. 第二阶段前端接口预留 ★

> 原则：**一期就埋好字段、配置开关与组件插槽**，二期只接样式与逻辑，不改数据层。

| 二期方向 | 一期预留点（数据/开关/插槽） |
| --- | --- |
| **OEM 品牌换肤** | `tenant_site_configs` 全字段建好（logo/favicon/theme_color/template_key/hero_*/footer 等）；前端包 `ThemeProvider` 插槽 + 主站预设色板接口 `GET /api/tenant/site-config/theme-options`；一期渲染 New API 默认皮肤，但**全部读 config**（未配置回退主站默认）。 |
| **代理自定义首页/装修** | 预留 `home_mode(default/config/custom_html)`、`banner_json`、`enabled_modules`、`custom_html` + `custom_html_status`(审核位)；首页用**模块化组件 + 配置驱动渲染**，一期只渲染 `default`，模块开关字段已通。 |
| **tokenplan 营销化展示** | `token_plans` 预留 `anchor_price`(划线)、`discount_label`(角标)、`is_recommended`(热门)、`badge`、`sort`；购买页用**卡片组件**，一期朴素渲染，二期接划线/角标/对比/推荐样式，无需改表。 |
| **多语言/多币种/移动端** | 文案走 **i18n key**（不硬编码）；价格存储与展示币种分离（金额 + currency 字段）；后端读 `Accept-Language` 预留；前端**响应式栅格**布局骨架，一期 PC 为主、移动可用。 |

---

## 12. 技术选型 / 实现细节 ★

### 12.1 选型
- **后端**：Go 1.25.1+（以 `go.mod` 为准；New API fork，Gin + GORM）。**单体优先**；微信/支付宝 SDK 与回调均在主站进程内。
- **前端**：`web/default` 使用 React 19 + TypeScript + Rsbuild + Base UI/shadcn + Tailwind；`web/classic` 为兼容主题并由独立门禁维护。包管理与脚本统一优先使用 Bun。
- **DB**：MySQL 8.0 / utf8mb4 / UTC 存储；迁移用版本化脚本（含回滚）。
- **缓存/限流**：Redis（Host→tenant 解析缓存、限流、会话）；账务、额度与订单仍以数据库为权威。
- **部署**：单一 Docker Compose 应用栈（主站 app + MySQL + Redis），Nginx 统一反代到主站。

### 12.2 多租户实现
```text
中间件 TenantResolver: Host → tenant_domains/slug（Redis 缓存）→ ctx.tenant
GORM 统一 scope: scopeByTenant(query, ctx.tenant.id) 强制注入，禁止裸查租户表
鉴权: requireAdmin() / requireTenantOwner(tid) / requireTenantActive(tid)
```

### 12.3 额度桶抽象
```go
type QuotaSource interface {
    // 返回是否成功扣减；并发安全（行锁/CAS）
    Charge(ctx, costUSD float64) error
    Balance(ctx) (float64, error)
}
// WalletQuota（钱包 quota）/ SubscriptionQuota（tokenplan 桶）
// 网关按"有无 active 套餐"选择 QuotaSource（独立计量）
```

### 12.4 tokenplan 计量一致性
- 调用结束算 `upstream_cost_usd = 模型上游成本 × multiplier`。
- `UPDATE user_subscriptions SET used_usd = used_usd + ? WHERE id=? AND used_usd + ? <= month_limit_usd AND status='active' AND expire_at > now()`（条件更新，受影响行=0 → 超额/失效 → 拒绝并置 `exhausted`）。
- 高并发下用行锁或乐观重试，杜绝超额穿透。

### 12.5 保护线
```text
PricingGuard.validate(price, cost, minMarginRatio):
  require price >= cost * (1 + minMarginRatio)   // 分组倍率 & tokenplan 共用
```

### 12.6 支付回调（主站进程内）
- `POST /api/pay/wechat/notify`、`POST /api/pay/alipay/notify`；Nginx 与其他 `/api/` 请求一样统一反代到主站，不设独立支付 upstream。
- 回调必须：验签 → 订单号唯一约束**幂等** → 绑定 `tenant_id/user_id/order` → 入账（钱包充值 or 创建订阅）→ 触发代理收益。

### 12.7 关键接口签名（示例）
```http
POST /api/tenant/token-plans/:id/purchase
  → 200 { subscription_id, plan_code, month_limit_usd, expire_at, pay: {...} }

GET  /api/tenant/subscriptions
  → 200 [{ id, plan_code, month_limit_usd, used_usd, remaining_usd, status, expire_at }]

PATCH /api/tenant/token-plans/manage   // 代理上架/改价
  body { plan_id, enabled, retail_price }   // 后端校验 retail_price>=min_price

POST /api/admin/token-plans            // 管理员套餐 CRUD
  body { code,name,base_price,anchor_price,month_limit_usd,agent_cost_price,min_price,... }
```

---

## 13. API 模块规划（含 tokenplan）

> 既有模块沿用 plan §10：租户模块、管理员/子代理模块、Token 模块、计费日志模块、支付回调模块、开放接口、`/v1/*` 调用入口。**新增 tokenplan 接口**：

```text
# 用户侧
GET    /api/tenant/token-plans              # 可购套餐列表（含代理零售价）
POST   /api/tenant/token-plans/:id/purchase # 购买
GET    /api/tenant/subscriptions            # 我的套餐（剩余/到期）

# 代理侧
GET    /api/tenant/token-plans/manage       # 代理可上架的套餐与当前定价
PATCH  /api/tenant/token-plans/manage       # 上架/退出 + 改零售价（保护线校验）

# 管理员侧
GET    /api/admin/token-plans
POST   /api/admin/token-plans
PATCH  /api/admin/token-plans/:id
GET    /api/admin/subscriptions             # 全站订阅/计量监控（满额预警）
```

---

## 14. 权限与隔离要求

所有代理站相关查询**必须带 `tenant_id`**：代理只能查本租户用户/日志/订阅；Token、扣费、充值、**订阅与计量**必须绑定租户；管理员接口单独鉴权。统一方法 `requireAdmin / requireTenantOwner / requireTenantActive / scopeByTenant`。**新增跨租户访问测试用例**（含 tokenplan 订阅）。

---

## 15. 部署方案

```text
DNS（A: yourbrand.com / www / api / admin / *.yourbrand.com → 64.90.4.114）
  ↓ Nginx(443, wildcard 证书; `/api/` 统一转发主站)
  ↓ 后端服务(Go，内置微信/支付宝 SDK 与回调)
  ↓ MySQL 8.0 + Redis
```
- 一期 `*.yourbrand.com` 通配符证书；二期自定义域名管理员手动配证书；国内服务器注意备案。
- 支付回调配置见 §12.6；凭据由管理后台写入 options 数据库配置，回调必须验签 + 幂等 + 绑定 tenant。
- DNS 调试注意：macOS 若返回 `198.18.x.x` 为 Clash/TUN fake-ip，用 `dig @1.1.1.1` / `dig +trace` / 服务器侧 dig 对比。

---

## 16. 风险与对策（含 tokenplan 新风险）

| 风险 | 对策 |
| --- | --- |
| **三天工期**（plan §13.1） | 严格控范围；按 §10.5 重估为 ≈4.5–5 天（方案 A） |
| **数据隔离/跨租户泄露**（§13.2） | 全表 tenant_id；ORM 强制 scope；跨租户测试用例 |
| **倍率/成本价卖穿**（§13.3） | 主站 floor + 后端强校验；负收益拦截 |
| **充值入账一致性**（§13.4） | 统一商户；回调幂等；绑定 tenant_id/user_id；流水可按租户筛 |
| 代理前端 DIY/上传（§13.6） | 一期不开放自定义 HTML；图片限格式/大小；管理员可下架 |
| 自定义域名/SSL（§13.7） | 一期只做二级域名；二期 A 记录 + 管理员配证书 |
| **★ tokenplan 满额巨亏 / 被刷**（§2.4） | month_limit 按真实成本标定；**Trial/各档限购**；满额逼近**监控告警**；保留主站随时下调月限额能力；Pro/Max 可先灰度 |
| **★ tokenplan 计量超额穿透** | 条件 UPDATE / 行锁 / 乐观重试；定时 + 惰性双校验到期；兜底对账 |
| **★ 双桶混淆** | 明确路由规则（有 active 套餐走套餐桶）；UI 显示计量来源；耗尽/过期按 D5 拦截重购，绝不回退钱包 |
| **★ Key 共享/倒卖** | 调用风控（并发/IP/UA 指纹）、异常告警、单 Token RPM 限制 |

---

## 17. 里程碑

- **第一阶段**（全栈可演示 MVP，**≈4.5–5 天**，方案 A）：多租户识别 + 三类代理 + 用户组倍率 + 推广/兑换码 + 充值差价/消耗分润 + Key/调用/扣费/日志 + 提现 + 基础统计 + **tokenplan（后端完整 + 购买页 + 限购）**，三类角色最小可用页面。
- **第二阶段**（统一前端 + 受控装修，5–10 天）：按主办方参考图改造主站/代理站前端；品牌配置生效；模板 A/B/C；OEM 自定义域名 + SSL；**tokenplan 营销化展示**；多语言/移动端。
- **第三阶段**（运营增强，2–4 周）：自定义域名管理 + SSL 到期提醒、完整风控、财务报表、运营后台、tokenplan 续订/自动化。

---

## 18. 第一阶段验收清单

> 沿用 plan §15 全部 33 项（用户注册、代理设置、wildcard 路由、装修配置、用户组倍率保护线、推广/兑换码、充值入账、支付回调、Key/调用/扣费/分润、提现、开放 API、统计、跨租户拦截、接口文档、部署说明…），**新增 tokenplan 验收项**：

- [ ] 管理员可创建/编辑 tokenplan 套餐（售价/原价/月限额/成本价/保护线/排序/状态）。
- [ ] 代理可上架/退出 tokenplan，可在**保护线内**改零售价；低于保护线被拦截。
- [ ] 用户可在 New API 风格购买页购买套餐，生成 30 天有效订阅。
- [ ] 套餐内调用按 **month_limit 月度封顶**计量；超额/到期被拦截并提示重购。
- [ ] 套餐桶计量**独立于钱包余额**（互不混用）。
- [ ] tokenplan 收益（差价/分润）写入 `agent_earning_logs` 并计入可提现余额。
- [ ] **Trial 限购防刷**生效（每用户/实名/设备 1 次）。
- [ ] 并发调用不会击穿 month_limit（计量一致性）。
- [ ] 管理员可在订阅监控页看到满额逼近预警。

---

> **后续维护**：本文继续作为需求权威；根级 `CLAUDE.md` 已路由到本文件，实际实现状态维护在 `doc/tasks/STATUS.md`。仍标 `[待确认]` 的业务口径以 `doc/acceptance.md` 决策表为准，不得用早期工期方案推断发布状态。
