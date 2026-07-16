# 设计:把「写死 $」的界面接回中枢货币库,全站显示 ¥(config-driven)

- **日期**:2026-07-16
- **状态**:待用户复核
- **范围**:纯前端显示层。把绕过中枢、硬编码 `$`/本地 `usd()` 的界面接回 `lib/currency.ts`,让金额跟随全站货币开关(线上已是 CNY/7.3)统一显示 ¥。后端零改。
- **上游 spec**:本文件在货币主题上**取代** [`2026-07-15-cny-currency-and-subscription-menu-design.md`](2026-07-15-cny-currency-and-subscription-menu-design.md) 中「尚未落地」的部分(Part 2 模型定价 + 套餐类页面 + 全站扫 `$`);该 spec 的 Part 1 主开关已由线上运行时设置 `CNY` 满足、Part 3 充值页已随 PR #3 硬编码 ¥ 落地、Part 4 隐藏订阅菜单**本次不做**(与货币无关)。

---

## 1. 背景与前提(为什么范围比想象的小)

- **中枢已就位**:`web/default/src/lib/currency.ts` 是完整货币中枢,一切跟随设置 `quotaDisplayType`(USD/CNY/TOKENS/CUSTOM)+ `usdExchangeRate`。CNY 模式 `getDisplayMeta` 返回 `¥` + 汇率(`currency.ts:186-192`),USD 默认 `$` + rate 1(`currency.ts:204-212`)。导出:`formatCurrencyFromUSD`、`formatBillingCurrencyFromUSD`、`formatQuotaWithCurrency`、`formatLocalCurrencyAmount`、`getCurrencyLabel`。
- **线上开关已是 CNY**:实测 `GET /api/status` → `quota_display_type: 'CNY'`、`usd_exchange_rate: 7.3`。故凡「走中枢」的页面**现在本就显示 ¥**。代码默认值仍是 `USD`(`stores/system-config-store.ts:51`),由 `/api/status` 覆盖(`hooks/use-system-config.ts:44,70-71,100,106`;另 `hooks/use-status.ts:45-46`)。
- **本任务的本质**:不是「翻转全站」,而是**把还写死 `$` 的少数界面接回中枢**,让它们跟随 CNY 一起 ¥。汇率(7.3)与充值实收本就同源、天然一致,**不改汇率**。

---

## 2. 锁定的需求(用户逐条确认)

1. **模型定价范围 = 后台管理员配置 UI**(系统设置 → 模型)。前台价格表主链路已走中枢、本就 ¥,不在本次(除前台一处旁路见 §5.C 低优先)。
2. **模型定价 UI 显示 + 输入都 ¥**:显示 ¥;管理员录入也按 ¥ 敲,保存时反算回库里**无量纲倍率**,存库仍是美元制,**实际扣费零变化**。
3. **套餐月限额一致性范围 = 全链路**:后台套餐管理 + 用户「我的订阅」+ 代理上架 + 订阅监控,凡显示「月限额 / 已用额度 / 额度上限」处统一 ¥。
4. **全站扫净剩余 `$` = 是**:连兑换码面额、我的用户额度(及损耗监控若为 $)一并接回中枢,做到全站再无 `$`。
5. **USD 开关**:用户明确「不需要切美元、全部人民币最好」。→ **不为切换写任何额外代码**;走中枢本就顺带保留该能力(平台自带、线上已设 CNY),放着不用即可。这也让「临时切 USD 应自动恢复 $」成为免费的回归自检。

---

## 3. 核心原则与红线

- **一切跟随 `quotaDisplayType`,不硬编码货币符号。** 金额显示统一走 `lib/currency.ts`。
- **红线①「真实钱款」恒 ¥、绝不 ×汇率**:`base_price_cny`/`retail_price_cny`/`anchor_price_cny`/`min_price_cny` 等 `_cny` 字段是真金白银(微信/支付宝实收、套餐售价、代理成本收益、提现),**永远显示 ¥**,不经中枢 ×汇率(否则 ¥6.90→¥50,虚高 7.3 倍)。
  - **实现细节(重要)**:`_cny` **不得**改用 `formatLocalCurrencyAmount()`——该函数符号随模式变(USD 模式会显示 `$6.90`,对「本就是 ¥」的值是错的)。`_cny` 必须用「**恒 ¥、不 ×汇率、与模式无关**」的渲染(保留现有本地 `cny()`,或收敛到一个新的中枢 `formatCNYFixed()`,见 §5.C 可选项)。
- **红线②后端零改**:计费逻辑 / 桥接 / 额度桶 / `USDExchangeRate` 数值(7.3)/ `token_plans` 数据 / breakage 阈值,一律不动。存库的 `model_ratio`/`model_price` 与币种无关。
- **单一有效汇率源(结构性防 7.3 倍坑)**:在 `lib/currency.ts` 暴露**一个**「有效计费汇率」getter(如 `getEffectiveBillingRate()`,值取自 `getBillingDisplayMeta`:CNY→7.3、USD→1、CUSTOM→自定义率、TOKENS→1)。§5.A 的**显示(×rate)与输入反算(÷rate)共用它**,两侧不可能各写一套而漂移。

---

## 4. 货币中枢函数选用速查(本次改动统一按此)

| 数据类型 | 字段例 | 用哪个函数 | 模式行为 |
| --- | --- | --- | --- |
| 系统美元额度/价格 | `month_limit_usd`、`used_usd`、`limit_usd`、`amount_usd`、模型 `倍率×2`、`model_price` | `formatBillingCurrencyFromUSD(usd)` | CNY→¥(×7.3),USD→$。会随开关翻转。 |
| 原始 quota(token) | 用户额度 quota | `formatQuotaWithCurrency(quota)` 或 `formatCurrencyFromUSD(quota/perUnit)` | 同上,先 quota→USD 再折算。 |
| 真实人民币钱款 | `*_cny` | 恒 ¥ 渲染(本地 `cny()` / 新 `formatCNYFixed()`) | 永远 ¥,**不** ×汇率、不随开关变。红线①。 |

---

## 5. 改动清单(带 file:line 证据)

> 证据行号来自只读盘点,实现时以实际代码为准(行号可能已微移)。

### Part A · 后台模型定价 UI —— 显示 + 输入都 ¥
三文件,当前**全部硬编码 `$`、不走中枢**:

- `web/default/src/features/system-settings/models/model-pricing-core.ts`
  - `:160` `ratioToBasePrice`:`price = 倍率 × 2`(派生美元价)。
  - 预览价 `$X/1M` 硬编码 `` `$${…}` `` 于 `:245 :253 :260 :268 :276 :284 :292`(输入/补全/缓存/图像/音频)。
- `web/default/src/features/system-settings/models/model-pricing-sheet.tsx`
  - `:256` 录入反算:`倍率 = 输入价 ÷ 2`。
  - 按次固定价输入框符号硬编码 `$`:`:610 <InputGroupAddon>$</InputGroupAddon>`;USD 文案 `:567 :629`。
- `web/default/src/features/models/components/drawers/model-mutate-drawer.tsx`
  - `:256` 反算 `倍率 = parseFloat(value) ÷ 2`;`:1018` 正算 `倍率 × 2`。
  - `:1029` 硬编码「`Calculated price: $${…×2} per 1M tokens`」;USD 文案 `:951 :993`。

**改法(挂在既有「倍率↔价格」边界上)**:

- **显示**:所有派生价 `倍率 × 2`(及按次 `model_price`)→ `formatBillingCurrencyFromUSD(…)`,CNY 下自动 ¥×7.3。「Calculated price」行改中文并由中枢出符号(见 §6)。
- **输入(按显示货币录入)**:令 `rate = getEffectiveBillingRate()`。
  - 按 token 模型:`倍率 = 输入价 ÷ rate ÷ 2`;编辑回填输入框 `输入价 = 倍率 × 2 × rate`。CNY 下 `¥14.6 ⇄ 倍率 1.0`;USD 下 `$2 ⇄ 倍率 1.0`(rate=1,与旧行为一致)。
  - 按次模型 `model_price`(USD 绝对价):显示 `× rate`、录入 `÷ rate`,**存回仍 USD**。
  - 输入框 `$` 角标 → `getCurrencyLabel()`/中枢符号。
- **存库不变**:`model_ratio`/`model_price` 与币种无关 → 扣费零变化。

### Part B · 套餐月限额 / 订阅额度全链路 ¥
所有 `month_limit_usd`/`used_usd`/`limit_usd`(美元制)→ `formatBillingCurrencyFromUSD(…)`。`_cny` 字段**保持恒 ¥ 不动**(红线①)。

- `features/token-plans/token-plans-columns.tsx`:`:28-29` 本地 `cny`/`usd`;`:109` `usd(month_limit_usd)` → 改中枢;`:86 :98` `base_price_cny`/`anchor_price_cny` 用 `cny()` **保留**。(`token-plans-mutate-drawer.tsx:319` 月限额录入是纯数字表单,无符号,不必改;如加符号提示则用中枢。)
- `features/tenant-plans/`:`lib/format.ts:22,25` 本地 `cny`/`usd`;`plan-card.tsx:90` `usd(month_limit_usd)` → 中枢,`:78 :82` `retail_price_cny`/`anchor_price_cny` 保留;`my-subscriptions.tsx:66` `usd(used_usd)`/`usd(limit_usd)` → 中枢。
- `features/agent-listings/index.tsx`:`:35` 从 agent-format 引 `cny,usd`;`:77` `usd(month_limit_usd)` → 中枢;`:51 :75 :76 :93` `min_price_cny`/`base_price_cny` 用 `cny()` **保留**。
- `features/subscription-monitor/`:`lib/index.ts:24` 本地 `usd`;`subscription-monitor-columns.tsx:97` `usd(used_usd)`/`usd(limit_usd)` → 中枢。

### Part C · 全站扫净剩余 `$`
- **共享 `web/default/src/lib/agent-format.ts`**:`usd()`(`:35-36` 硬编码 `$`)、`quotaToUsd()`(`:39-40` 经 usd)改为**走中枢**:`usd(x)` → `formatBillingCurrencyFromUSD(x)`;`quotaToUsd(quota)` → `formatBillingCurrencyFromUSD(quota / quotaPerUnit)`(**固定选此**——保持「恒货币、不显 token」,忠实于旧的恒 `$` 行为;不用 `formatQuotaWithCurrency`,以免 TOKENS 模式下意外显示 token 数)。一次修好,连带解决:
  - `features/redemptions/index.tsx:52` `usd(amount_usd)`(兑换码面额)。
  - `features/my-users/index.tsx:52` `quotaToUsd(quota)`(下级用户额度)。
  - `features/agent-listings/index.tsx` 中经由 `usd` 的用量。
  - `cny()`(`:31-32`)**保持恒 ¥**(已正确,红线①)。
- **损耗监控** `features/breakage-monitor/`:实现时核对损耗额是否 `$`;是则接回中枢。
- **本地重复 `usd()`/`cny()` 收敛(可选清理)**:各 feature 的本地副本(tenant-plans/lib/format、subscription-monitor/lib、token-plans-columns)在改调用点后,可顺手删掉本地 `usd()`。`cny()` 若要收敛,统一到一个**恒 ¥、不 ×汇率**的中枢 `formatCNYFixed()`(新增),**切勿**用模式相关的 `formatLocalCurrencyAmount()`(红线①实现细节)。此项为低风险清理,非必需。
- **前台旁路(低优先)** `features/pricing/components/dynamic-pricing-breakdown.tsx:158-169`:自读 store 挑符号(`¥`/custom/`$`)、于 `:292 :368` 手拼 `${symbol}${(value*rate).toFixed(4)}`。**当前已显示 ¥、非 `$` 问题**,不影响观感;为贯彻「零硬编码」可收敛到中枢(顺带支持 TOKENS),列为最低优先,可放最后或另起。

---

## 6. 文案(W5 全中文)

改到的标签/表头一律中文,币种符号由中枢注入、不写死币种:

- `model-mutate-drawer.tsx:1029`「Calculated price: $X per 1M tokens」→「单价:{中枢格式化后的价} / 1M tokens」。
- 模型定价 UI 的「(USD)」文案(`:567 :629 :951 :993`)→ 中文、去币种硬编码。
- 各表头/标签「Monthly Limit (USD)」→「月限额」、「Amount ($)」→「面额」、「Used ($)」→「已用」等,币种交给数值本身的中枢符号。
- i18n 以 `zh.json` 为准;若并行工作区锁定 `zh.json`,用 `t('English Key', { defaultValue: '中文' })` 即时兜底(日后补 locale 透明覆盖)。

---

## 7. 明确不碰

后端计费 / 桥接 / 额度桶 / `USDExchangeRate` 值(7.3)/ `token_plans` 数据 / breakage 阈值 · `_cny` 字段的 ¥ 渲染(不二次放大)· 线上 `quota_display_type`(保持 CNY;代码默认 `USD` 也不改,留作回归入口)· 钱包充值 ¥ 整数档(已硬编码、所见即所付,本次不动)· 原生订阅管理菜单(与货币无关,本次不做)· Creem/EUR 国际支付。

---

## 8. 验证(服务器 Docker 构建后 —— W4,Mac 建不了前端)

1. **模型定价闭环(兜 7.3 倍坑)**:后台设某模型 `¥X/1M` → 保存 → 重开该模型,输入框仍显示 `¥X`(设→存→显回相等);同时前台/日志该模型单价一致。
2. **套餐/订阅额度全 ¥**:后台套餐管理月限额、用户「我的订阅」已用/上限、代理上架月限额、订阅监控,均显示 ¥。
3. **扫净 `$`**:兑换码面额、我的用户额度、损耗监控无 `$`。
4. **红线①未破**:抽查套餐售价 `¥6.90`、代理财务、提现等 `_cny`,数值**未**被放大 7.3 倍(仍是原 ¥ 值)。
5. **回归自检(零硬编码证明)**:临时把 `quota_display_type` 切回 `USD` → 抽象额度类应自动恢复 `$` 且数值 = 原 ¥ ÷7.3;`_cny` 真实钱款仍显 ¥(符合红线①)。验毕切回 `CNY`。

---

## 9. 部署纪律

- 前端为主,**服务器构建**(W4)。Mac 侧只做代码编辑;若跑 Go/前端门禁受限,按既有 `SKIP_PREFLIGHT` 与手动门流程。
- `deploy.sh` 打包整棵 Mac 工作树 → **并行 WIP 风险**([[deploy-scope-parallel-wip]])。部署前 `diff` 工作树 vs 服务器,**隔离 scope、只带本次改动文件**;当前工作树尚有未跟踪文件(audit html、bulb-orbit、docs specs 等),不影响前端构建但勿误打包进无关改动。
- 验收通过前不推 `origin/main`(定向部署测试栈即可)。
