# 设计:全站人民币计价切换(config-driven)+ 隐藏原生订阅菜单

- **日期**:2026-07-15
- **状态**:已确认并实施
- **范围**:两个独立诉求合并一次实现与部署;货币展示以 frontend 为主,套餐治理同时覆盖 frontend 导航/交互与 backend 归属保护/迁移

---

## 背景

### A. 货币显示:美元 → 人民币(国内站)

前端已内建完整货币显示层 `web/default/src/lib/currency.ts`,由设置 **`quota_display_type`**(`USD`/`CNY`/`TOKENS`/`CUSTOM`)+ `usd_exchange_rate` 驱动;设为 `CNY` 时符号自动 `¥`、USD 数值自动 ×汇率。现状之所以全站显示 `$`:

1. `quota_display_type` 默认停在 `USD`;
2. 部分页面**绕过中枢、硬编码 `$`**(见 Part 2 清单),即使切设置也不变。

**关键一致性(好消息)**:后端 `USDExchangeRate = 7.3`(`setting/operation_setting/payment_setting_old.go:18`)**同时**用于:

- ① 充值实收:`internal/mtwire/recharge.go:249` → `actualPaidCNY = amountUSD × 7.3`;
- ② 前端展示:经 `model/option.go:84` 以 `/api/status` 的 `usd_exchange_rate` 暴露。

二者是**同一个值**,故"展示的 ¥"与"实收的 ¥"天然一致,**汇率无需改动**。

### B. 隐藏原生「订阅管理」菜单

原生 `subscription_plans`(New API 自带 Stripe/国际化订阅后台,在本多租户部署里由桥接层硬编码 `PriceAmount:0`)与自建 `token_plans`(套餐管理,真定价/收益源)在管理员侧栏并列,造成"重复 + 价格打架(¥ vs $0)"困惑。计费/报表/告警链路经查证均健康(用量走原生桶、报表 P2-BRK-02 已读原生桶),**无数据 bug**;实现收起重复菜单,保留第三方商品 ID 的隐藏高级入口,并把 Token Plans 映射的原生套餐设为后端强制只读。

---

## 核心原则(用户明确要求)

**一切跟随 `quota_display_type`,不硬编码任何货币符号。** 覆盖模型定价、充值输入框、相关 i18n 文案。日后切回 `USD` 或换币种,全站自动跟随——这也是天然的回归验证。

---

## 决定

1. 显示货币 = **CNY**,汇率 **7.3**(已就位,不改汇率)。
2. **全面 ¥ 化**,含管理员模型定价 UI(用户 2026-07-15 追加)。
3. 所有金额显示改走中枢 `lib/currency.ts`,**消除硬编码 `$`/`¥`**。
4. 充值输入框改为"**按显示货币输入**"(CNY 时输 ¥),提交前换算回后端要的 USD。
5. **红线**:`_cny` 字段(已是 ¥、走 `cny()`)**绝不**再经中枢 ×汇率(否则虚高 7 倍)。
6. **计费语义零改动**:后端计费、额度桶、`USDExchangeRate` 值、`token_plans` 数据和 breakage 监控保持不变;仅增加套餐归属治理与写入保护。

---

## 改动清单

### Part 1 · 主开关(覆盖 ~80%,零改业务代码)

- 令 `quota_display_type` 生效为 `CNY`:烤入默认(国内站长期)+ 确保线上设置为 `CNY`(实现时确认默认 seed 位置:`web/default/src/features/system-settings/billing/index.tsx:37` 前端默认 + 后端 `general_setting` 默认)。
- 自动变 ¥(无需改代码):仪表盘余额、使用日志/消耗、前台模型定价、API Key 额度、用户订阅额度、钱包余额 + 账单 Amount 列。

### Part 2 · 消除硬编码 `$`(改走中枢,跟随设置)

把下列本地 `usd()` / `quotaToUsd()` / 写死 `$` 改为调用 `lib/currency.ts`:USD 值用 `formatBillingCurrencyFromUSD`,原生 quota 用 `formatQuotaWithCurrency`,标签用 `getCurrencyLabel`:

- `web/default/src/lib/agent-format.ts` — `usd()`、`quotaToUsd()`(代理自助页中枢)
- `features/tenant-plans/`(my-subscriptions、plan-card;`used_usd`/`limit_usd`/`month_limit_usd`)
- `features/agent-listings/`(`month_limit_usd`)
- `features/redemptions/`(`amount_usd`、预扣提示)
- `features/subscription-monitor/`(`used_usd`/`limit_usd`)
- `features/breakage-monitor/`(损耗 USD 值)
- `features/my-users/`(`quotaToUsd(quota)` → `formatQuota`)
- `features/token-plans/`(后台套餐 `month_limit_usd`)
- **模型定价管理员 UI**(用户追加 · **可行性已核实、对扣费零影响**):`features/system-settings/models/model-pricing-core.ts`、`model-pricing-sheet.tsx`、`features/models/components/drawers/model-mutate-drawer.tsx`
  - **存储货币中性**:按 token 模型存 `model_ratio`(无量纲倍率,`ratio 1 ⟷ $2/1M`);按次模型存 `model_price`(USD 绝对价)。后端计费直接用这些(`setting/ratio_setting/model_ratio.go`)。UI 的 `$X/1M` 是 `ratio × 2` 的**派生显示**(`model-pricing-core.ts:160`),录入时反向 `ratio = 价格 / 2`(`model-mutate-drawer.tsx:256`、`model-pricing-sheet.tsx:256`)。
  - **改法(在既有 price↔ratio 边界挂有效汇率 `rate`)**:显示 `ratio × 2 × rate`(经中枢出 ¥);录入把 `/2` 改为 `/rate/2`;按次 `model_price` 显示 ×rate、录入 ÷rate 保持 USD 存储。
  - **安全性**:存回的 `model_ratio`/`model_price` 与币种无关 → **实际扣费 quota 零变化**;切回 `USD` 自动显 $。故与其余项一致,纯 config-driven、无需两套数据。

### Part 3 · 充值页 config-driven ¥ 化(纯前端,汇率已对齐)

- `features/wallet/components/tenant-recharge-card.tsx` + `hooks/use-tenant-recharge.ts`:
  - 输入框按显示货币(CNY 时 ¥,USD 时 $);标签/占位符符号取自中枢,不写死。
  - 提交前换算:`amountUSD = 输入额 / 有效汇率`(有效汇率取自 currency 配置:USD=1、CNY=7.3、CUSTOM=自定义率;复用 `wallet/index.tsx:78-80` 既有"有效汇率"模式)。
  - 由于后端 `usd × 7.3 = 输入 ¥`,**实收 = 展示,分毫一致**;入账 = 等值额度。
  - 最低充值按显示货币:CNY 时 **¥10**(≈$1.37),可调。
  - 覆盖口径:本站只用 `USD`/`CNY` 两模式,二者已验证一致;`TOKENS`/`CUSTOM` 模式的充值输入按 USD 基准兜底(微信/支付宝实收恒为 ¥,非本站用例,不做深度适配)。
- `features/wallet/components/dialogs/recharge-qr-dialog.tsx`:CNY 模式仅显"支付 ¥X";符号来自中枢。
- **i18n 去 `$` 硬编码**,改为注入"已带符号的金额"或用 `getCurrencyLabel()`,使文案币种中性:
  - `"Amount (USD), minimum ${{amount}}"`、`"Minimum recharge amount is ${{amount}}"`、`"Recharge ${{amount}}"`、`"Recharge ${{usd}} (pay ¥{{cny}})"`、`"Calculated price: ${{price}} per 1M tokens"`,以及表头/标签键 `"Balance ($)"`/`"Used ($)"`/`"Monthly Limit (USD)"`/`"Amount ($)"`/`"Fixed price (USD)"` 等(`zh.json`/`en.json`)。

### Part 4 · 隐藏原生「订阅管理」菜单

- `web/default/src/hooks/use-sidebar-data.ts`(约 247–250 行):移除 admin 侧栏「订阅」(`/subscriptions`)条目;`use-sidebar-config.ts` 保留旧 URL 与 `subscription` 模块的配置映射说明。
- `/subscriptions` 保留为兼容路由,管理员访问时重定向至统一的「套餐管理」(`/token-plans`)。
- 原生 Stripe / Creem / Waffo Pancake 第三方商品 ID 的应急配置保留在管理员限定的隐藏入口 `/advanced-subscription-plans`;该入口不加入侧栏,由相关支付设置提供明确链接。
- 管理端原生套餐列表附加 `managed_by` / `read_only`;Default 与 Classic 对 Token Plans 映射项显示只读归属并禁用编辑、启停。
- 原生套餐 PUT/PATCH 在同一事务内校验归属;映射项返回稳定错误码 `TOKEN_PLAN_MANAGED`,归属存储不可用时 fail-closed 并返回 `PLAN_OWNERSHIP_UNAVAILABLE`,展示消息走后端 en/zh i18n。
- `mt_native_subscription_plans` 的 GORM 契约集中在 `model.TokenPlanNativeSubscriptionPlan`,桥接迁移、查询和测试共同复用;`native_plan_id` 索引为兼容性加法迁移,不删除或改写既有套餐/订阅数据。

---

## 明确不碰

- 后端计费语义 / 额度桶 / `USDExchangeRate` 数值 / `token_plans` 数据 / breakage 监控;桥接只集中映射表契约,不改变购买、激活或扣费流程。
- `_cny` 字段与 `cny()` 渲染(代理财务、财报、套餐售价、提现)——本就是 ¥,不得再 ×汇率。
- Creem / EUR 国际支付(国内站不用,保留原样;如需隐藏另议)。
- tokenplan 经济模型(维持大额度/breakage,已确认)。

---

## 验证

服务器 Docker 构建后:

1. 主站 + 代理站金额全显 ¥;
2. 充值输入 ¥、微信/支付宝实收同额 ¥、入账等值额度;
3. 模型定价管理员 UI 显示 ¥ 且可按 ¥ 正确录入回存;
4. 侧栏无「订阅管理」,`/subscriptions` 重定向至 `/token-plans`,管理员可从支付设置进入 `/advanced-subscription-plans` 配置原生第三方商品 ID;
5. 抽查代理财务/财报 ¥ 值**未**被二次放大(红线未破);
6. **回归证明零硬编码**:临时把 `quota_display_type` 切回 `USD`,全站应自动恢复 `$`。

---

## 部署

本次同时包含前端导航/只读交互与后端套餐归属治理变更。`deploy/ops/deploy.sh` 只接受干净 Git 提交,并通过 `git archive HEAD` 生成发布归档;本地未跟踪的审计 HTML 必须留在原处,部署时改用临时 clean worktree 隔离,不得纳入提交或发布包。

数据库迁移仅集中复用既有 `mt_native_subscription_plans` 表契约并为 `native_plan_id` 增加索引,属于向后兼容的加法变更,不删除、改写套餐或订阅数据。部署脚本会先生成配对备份再构建和验收新版本;旧版应用可容忍该表及新增索引,因此应用回滚无需执行逆向 schema 迁移。
