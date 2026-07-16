# 全站 ¥ 化(接回中枢货币库)实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把绕过中枢、硬编码 `$`/本地 `usd()` 的前端界面(后台模型定价 UI、套餐/订阅月限额、兑换码、我的用户等)接回中枢货币库 `lib/currency.ts`,使其跟随全站货币开关(线上已是 CNY/7.3)统一显示 ¥,后端零改。

**Architecture:** 新增**单一有效汇率源** `getEffectiveBillingRate()` 与**纯换算模块** `lib/model-pricing-currency.ts`(价↔倍率、显示价↔存储价),drawer/sheet/core 全部调用它,使「显示 ×rate、录入 ÷rate」两侧共享一个值、结构上杜绝 7.3 倍错配。其余页面把本地 `usd(x)` 替换为中枢 `formatBillingCurrencyFromUSD(x)`;真实人民币 `*_cny` 字段保持恒 ¥ 渲染不动。

**Tech Stack:** React + TypeScript(rsbuild)、Zustand(`useSystemConfigStore`)、react-hook-form + zod、vitest(纯逻辑单测)、i18next(W5 全中文)。

## Global Constraints

- **汇率不改**:`USDExchangeRate = 7.3`;`quota_per_unit = 500000`;模型定价换算常量 `倍率 1 ⟷ $2 / 1M tokens`(即 `RATIO_USD_FACTOR = 2`)。
- **红线①(真实钱款恒 ¥)**:`base_price_cny`/`retail_price_cny`/`anchor_price_cny`/`min_price_cny` 等 `*_cny` 字段**永远显示 ¥、绝不 ×rate**;**不得**改用会随模式变符号的 `formatLocalCurrencyAmount()`;保留本地 `cny()`(`¥${v.toFixed(2)}`)。
- **红线②(后端零改)**:计费/桥接/额度桶/汇率值/`token_plans` 数据/breakage 阈值不动;存库 `model_ratio`(无量纲)、`model_price`(USD 绝对价)**与币种无关**,实际扣费 quota 零变化。
- **单一汇率源**:所有「显示 ×rate / 录入 ÷rate」只经 `getEffectiveBillingRate()`,禁止各处自算汇率。
- **W5 全中文**:改到的面向用户文案一律中文;`zh.json` 被并行工作区锁定时用 `t('English Key', { defaultValue: '中文' })` 即时兜底。
- **W4 服务器构建**:Mac 只编辑;纯逻辑 vitest 可本地 `npx vitest run <file>`(纯 `lib/*` 不依赖路由树),若 Mac 跑不了则在服务器跑;`tsgo -b` 全量 typecheck 与前端 build 一律以**服务器 Docker 构建**为准。
- **部署 scope 隔离**:`deploy.sh` 打包整棵工作树,部署前 `git diff`/`git status` 核对只带本次改动文件,勿裹入无关 WIP。

---

## 文件结构总览

| 文件 | 责任 | 动作 |
| --- | --- | --- |
| `web/default/src/lib/currency.ts` | 中枢货币库 | 修改:导出 `getEffectiveBillingRate()` |
| `web/default/src/lib/currency.test.ts` | 汇率源单测 | 新建 |
| `web/default/src/lib/model-pricing-currency.ts` | 纯换算:价↔倍率、显示价↔存储价 | 新建 |
| `web/default/src/lib/model-pricing-currency.test.ts` | 换算往返单测(防 7.3 倍) | 新建 |
| `web/default/src/features/models/components/drawers/model-mutate-drawer.tsx` | 单模型定价抽屉 | 修改:接换算模块 + ¥ 符号 + i18n |
| `web/default/src/features/system-settings/models/model-pricing-core.ts` | 定价核心(初始态/预览) | 修改:`ratioToBasePrice`/`createInitialLaneState` 加 `rate`;`buildPreviewRows` 加 `currencySymbol` |
| `web/default/src/features/system-settings/models/model-pricing-sheet.tsx` | 批量定价表 | 修改:接换算模块 + ¥ 符号 + i18n |
| `web/default/src/lib/agent-format.ts` | 代理页共享格式化 | 修改:`usd()`/`quotaToUsd()` 走中枢;`cny()` 不动 |
| `web/default/src/features/token-plans/token-plans-columns.tsx` | 后台套餐管理列 | 修改:`month_limit_usd` 走中枢 |
| `web/default/src/features/tenant-plans/lib/format.ts` + `plan-card.tsx` + `my-subscriptions.tsx` | 用户订阅/套餐卡 | 修改:`*_usd` 走中枢 |
| `web/default/src/features/agent-listings/index.tsx` | 代理上架 | 修改:`month_limit_usd` 走中枢 |
| `web/default/src/features/subscription-monitor/lib/index.ts` + `subscription-monitor-columns.tsx` | 订阅监控 | 修改:`used_usd`/`limit_usd` 走中枢 |
| `web/default/src/features/breakage-monitor/*` | 损耗监控 | 核对,若 `$` 则接回中枢 |

---

## Task 1: 单一有效汇率源 `getEffectiveBillingRate()`

**Files:**
- Modify: `web/default/src/lib/currency.ts`(在文件内已有的私有 `getConfig()`、`getBillingDisplayMeta()` 之后新增导出)
- Test: `web/default/src/lib/currency.test.ts`(新建)

**Interfaces:**
- Produces: `getEffectiveBillingRate(): number` —— 返回 `formatBillingCurrencyFromUSD` 实际使用的汇率:CNY→`usdExchangeRate`(线上 7.3)、USD→1、CUSTOM→`customCurrencyExchangeRate`、TOKENS→1。后续 Task 2/3/4 全部以它为唯一汇率来源。

- [ ] **Step 1: 写失败测试**

`web/default/src/lib/currency.test.ts`:
```ts
import { describe, it, expect, beforeEach } from 'vitest'
import { useSystemConfigStore } from '@/stores/system-config-store'
import { getEffectiveBillingRate } from './currency'

function setCurrency(partial: Record<string, unknown>) {
  useSystemConfigStore.setState({
    config: {
      currency: {
        quotaDisplayType: 'USD',
        usdExchangeRate: 7.3,
        quotaPerUnit: 500000,
        customCurrencyExchangeRate: 1,
        customCurrencySymbol: '¤',
        ...partial,
      },
    },
  } as never)
}

describe('getEffectiveBillingRate', () => {
  beforeEach(() => setCurrency({}))

  it('returns 1 in USD mode', () => {
    setCurrency({ quotaDisplayType: 'USD' })
    expect(getEffectiveBillingRate()).toBe(1)
  })

  it('returns usdExchangeRate in CNY mode', () => {
    setCurrency({ quotaDisplayType: 'CNY', usdExchangeRate: 7.3 })
    expect(getEffectiveBillingRate()).toBe(7.3)
  })

  it('returns customCurrencyExchangeRate in CUSTOM mode', () => {
    setCurrency({ quotaDisplayType: 'CUSTOM', customCurrencyExchangeRate: 0.9 })
    expect(getEffectiveBillingRate()).toBe(0.9)
  })

  it('returns 1 in TOKENS mode (billing never tokenized)', () => {
    setCurrency({ quotaDisplayType: 'TOKENS' })
    expect(getEffectiveBillingRate()).toBe(1)
  })
})
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd web/default && npx vitest run src/lib/currency.test.ts`
Expected: FAIL —— `getEffectiveBillingRate is not a function`(尚未导出)。
（若 Mac 无法运行 vitest,记录并转服务器执行此步。）

- [ ] **Step 3: 实现**

在 `web/default/src/lib/currency.ts` 内,`getBillingDisplayMeta` 函数定义之后、`getCurrencyDisplay` 附近新增导出(可直接调用同文件私有的 `getConfig()` 与 `getBillingDisplayMeta()`):
```ts
/**
 * The exchange rate actually applied by formatBillingCurrencyFromUSD:
 * CNY→usdExchangeRate, USD→1, CUSTOM→customCurrencyExchangeRate, TOKENS→1.
 * Single source of truth for "display ×rate / input ÷rate" so the two sides
 * can never drift (prevents the 7.3× mispricing bug in model-pricing UI).
 */
export function getEffectiveBillingRate(): number {
  const config = getConfig()
  const meta = getBillingDisplayMeta(config)
  return meta.kind === 'currency' || meta.kind === 'custom'
    ? meta.exchangeRate
    : 1
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd web/default && npx vitest run src/lib/currency.test.ts`
Expected: PASS(4 passed)。

- [ ] **Step 5: 提交**

```bash
git add web/default/src/lib/currency.ts web/default/src/lib/currency.test.ts
git commit -m "feat(currency): 导出 getEffectiveBillingRate 单一有效汇率源"
```

---

## Task 2: 纯换算模块 `model-pricing-currency.ts`(防 7.3 倍的往返保证)

**Files:**
- Create: `web/default/src/lib/model-pricing-currency.ts`
- Test: `web/default/src/lib/model-pricing-currency.test.ts`

**Interfaces:**
- Consumes: 无(纯函数,`rate` 由调用方传入,通常来自 Task 1 的 `getEffectiveBillingRate()`)。
- Produces:
  - `RATIO_USD_FACTOR = 2`(倍率 1 ⟷ $2/1M)
  - `ratioToDisplayPrice(ratio: number, rate: number): number` —— 存储倍率 → 显示价(¥)。`= ratio * 2 * rate`
  - `displayPriceToRatio(displayPrice: number, rate: number): number` —— 显示价(¥)→ 存储倍率。`= displayPrice / rate / 2`
  - `usdPriceToDisplay(usdPrice: number, rate: number): number` —— 按次存储价(USD)→ 显示价(¥)。`= usdPrice * rate`
  - `displayToUsdPrice(displayPrice: number, rate: number): number` —— 按次显示价(¥)→ 存储价(USD)。`= displayPrice / rate`

- [ ] **Step 1: 写失败测试**

`web/default/src/lib/model-pricing-currency.test.ts`:
```ts
import { describe, it, expect } from 'vitest'
import {
  RATIO_USD_FACTOR,
  ratioToDisplayPrice,
  displayPriceToRatio,
  usdPriceToDisplay,
  displayToUsdPrice,
} from './model-pricing-currency'

describe('model-pricing-currency', () => {
  it('ratio 1 ⟷ $2/1M at rate 1 (USD, backward compatible)', () => {
    expect(RATIO_USD_FACTOR).toBe(2)
    expect(ratioToDisplayPrice(1, 1)).toBe(2)
    expect(displayPriceToRatio(2, 1)).toBe(1)
  })

  it('ratio 1 ⟷ ¥14.6/1M at rate 7.3 (CNY)', () => {
    expect(ratioToDisplayPrice(1, 7.3)).toBeCloseTo(14.6, 10)
    expect(displayPriceToRatio(14.6, 7.3)).toBeCloseTo(1, 10)
  })

  it('ratio round-trips through display at rate 7.3 (no 7.3× drift)', () => {
    for (const ratio of [0.25, 1, 2.5, 40]) {
      const shown = ratioToDisplayPrice(ratio, 7.3)
      expect(displayPriceToRatio(shown, 7.3)).toBeCloseTo(ratio, 10)
    }
  })

  it('per-request price round-trips (¥ display ⟷ USD store)', () => {
    for (const usd of [0.01, 0.5, 3]) {
      const shown = usdPriceToDisplay(usd, 7.3)
      expect(displayToUsdPrice(shown, 7.3)).toBeCloseTo(usd, 10)
    }
    expect(usdPriceToDisplay(0.01, 7.3)).toBeCloseTo(0.073, 10)
  })
})
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd web/default && npx vitest run src/lib/model-pricing-currency.test.ts`
Expected: FAIL —— 模块不存在。

- [ ] **Step 3: 实现**

`web/default/src/lib/model-pricing-currency.ts`(顶部需带项目版权头,复制任一同目录 `lib/*.ts` 的头注释):
```ts
// (复制 lib/currency.ts 的版权头到此)

/**
 * Pure conversions between stored, currency-neutral model pricing
 * (model_ratio dimensionless; model_price in USD) and the display/entry
 * currency. `rate` is the effective billing rate (getEffectiveBillingRate()).
 * Keeping these pure + one rate source makes the 7.3× mispricing structurally
 * impossible: display uses ×rate, entry uses ÷rate, same `rate`.
 */

/** model_ratio 1 corresponds to $2 per 1M tokens. */
export const RATIO_USD_FACTOR = 2

/** stored ratio → display price (e.g. ¥). */
export function ratioToDisplayPrice(ratio: number, rate: number): number {
  return ratio * RATIO_USD_FACTOR * rate
}

/** display price (e.g. ¥) → stored ratio. */
export function displayPriceToRatio(displayPrice: number, rate: number): number {
  return displayPrice / rate / RATIO_USD_FACTOR
}

/** per-request stored price (USD) → display price (e.g. ¥). */
export function usdPriceToDisplay(usdPrice: number, rate: number): number {
  return usdPrice * rate
}

/** per-request display price (e.g. ¥) → stored price (USD). */
export function displayToUsdPrice(displayPrice: number, rate: number): number {
  return displayPrice / rate
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd web/default && npx vitest run src/lib/model-pricing-currency.test.ts`
Expected: PASS(4 passed)。

- [ ] **Step 5: 提交**

```bash
git add web/default/src/lib/model-pricing-currency.ts web/default/src/lib/model-pricing-currency.test.ts
git commit -m "feat(pricing): 纯价↔倍率换算模块(单一汇率源,往返防 7.3 倍)"
```

---

## Task 3: 单模型定价抽屉 `model-mutate-drawer.tsx` 接换算模块(显示+录入 ¥)

**Files:**
- Modify: `web/default/src/features/models/components/drawers/model-mutate-drawer.tsx`

**Interfaces:**
- Consumes: `getEffectiveBillingRate`(Task 1)、`ratioToDisplayPrice`/`displayPriceToRatio`/`usdPriceToDisplay`/`displayToUsdPrice`(Task 2)、`getCurrencyLabel`(`lib/currency.ts` 既有)。

> **不变量**:表单字段 `ratio` 存无量纲倍率、`price` 存 USD;局部状态 `promptPrice`/`completionPrice` 存**显示货币**(¥)。`completionRatio = 补全价 ÷ 输入价`(价/价,**无量纲,不含汇率,保持原样**)。所有 `×2`→`ratioToDisplayPrice`、`÷2`→`displayPriceToRatio`,并乘/除同一个 `rate`。

- [ ] **Step 1: 加导入**

在文件顶部 import 区加入:
```ts
import { getEffectiveBillingRate, getCurrencyLabel } from '@/lib/currency'
import {
  ratioToDisplayPrice,
  displayPriceToRatio,
  usdPriceToDisplay,
  displayToUsdPrice,
} from '@/lib/model-pricing-currency'
```

- [ ] **Step 2: 价→倍率(输入)改除以有效汇率**

`:253-261` `handlePromptPriceChange`:
```ts
  const handlePromptPriceChange = (value: string) => {
    setPromptPrice(value)
    if (value && !Number.isNaN(Number.parseFloat(value))) {
      const ratio = displayPriceToRatio(Number.parseFloat(value), getEffectiveBillingRate())
      form.setValue('ratio', ratio.toString())
    } else {
      form.setValue('ratio', '')
    }
  }
```
`:263-277` `handleCompletionPriceChange`:**不改**(`completionRatio = 补全价 / 输入价`,价/价无量纲)。

- [ ] **Step 3: 倍率→价(载入编辑 + 倍率输入联动)改乘以有效汇率**

`:356-358`(载入 per-token 编辑数据):
```ts
          if (ratio !== undefined && ratio !== null) {
            const rate = getEffectiveBillingRate()
            const tokenPrice = ratioToDisplayPrice(ratio, rate)
            setPromptPrice(tokenPrice.toString())
            if (completionRatio !== undefined && completionRatio !== null) {
              const compPrice = tokenPrice * completionRatio
              setCompletionPrice(compPrice.toString())
            }
          }
```
（`compPrice = tokenPrice × completionRatio`:`tokenPrice` 已是 ¥,`completionRatio` 无量纲 → 结果 ¥,**不再另乘 rate**。）

`:1016-1019`(倍率 Input 的 onChange 联动 promptPrice):
```ts
                                    if (value) {
                                      setPromptPrice(
                                        ratioToDisplayPrice(Number.parseFloat(value), getEffectiveBillingRate()).toString()
                                      )
                                    } else {
```

- [ ] **Step 4: 载入按次价 ×rate、提交按次价 ÷rate(存回 USD)**

`:352`(载入 per-request 编辑数据,form.reset 的 price):
```ts
            price: usdPriceToDisplay(price, getEffectiveBillingRate()).toString(),
```
`:517`(`onSubmit` 内 `priceMap` 赋值,把显示 ¥ 折回 USD 存储):
```ts
                priceMap[finalModelName] = displayToUsdPrice(Number.parseFloat(values.price), getEffectiveBillingRate())
```
`:520`(`ratioMap` 赋值):**不改**(`values.ratio` 已由 Step 2 联动折算为无量纲倍率)。

- [ ] **Step 5: 三处 `Calculated ...` 显示串改 ¥ 符号(值已 ¥,不再 ×rate)**

`:1028-1030`(倍率字段的「Calculated price」):
```ts
                              {field.value && !Number.isNaN(Number.parseFloat(field.value))
                                ? `${getCurrencyLabel() === 'USD' ? '$' : '¥'}${ratioToDisplayPrice(Number.parseFloat(field.value), getEffectiveBillingRate()).toFixed(4)} ${t('per 1M tokens', { defaultValue: '/ 1M tokens' })}`
                                : t('Multiplier for prompt tokens.')}
```
`:1070-1072`(补全字段的「Calculated price」,值 = `promptPrice(¥) × completionRatio`,已 ¥):
```ts
                                ? `${getCurrencyLabel() === 'USD' ? '$' : '¥'}${(Number.parseFloat(promptPrice) * Number.parseFloat(field.value)).toFixed(4)} ${t('per 1M tokens', { defaultValue: '/ 1M tokens' })}`
```
`:1092-1094`(输入价字段的「Calculated ratio」,`promptPrice` 已 ¥ → 折回倍率需 ÷rate÷2):
```ts
                            {promptPrice && !Number.isNaN(Number.parseFloat(promptPrice))
                              ? `${t('Calculated ratio', { defaultValue: '换算倍率' })}: ${displayPriceToRatio(Number.parseFloat(promptPrice), getEffectiveBillingRate()).toFixed(4)}`
                              : t('Enter Input price to calculate ratio', { defaultValue: '输入价格以计算倍率' })}
```

- [ ] **Step 6: 文案去币种硬编码(W5 全中文)**

- `:993` `t('Price mode (USD per 1M tokens)')` → `t('Price mode (USD per 1M tokens)', { defaultValue: '价格模式(每 1M tokens)' })`
- `:1082` `t('Prompt price ($/1M tokens)')` → `t('Prompt price ($/1M tokens)', { defaultValue: '输入价格(/ 1M tokens)' })`
- `:1099` `t('Completion price ($/1M tokens)')` → `t('Completion price ($/1M tokens)', { defaultValue: '补全价格(/ 1M tokens)' })`
- 若该文件 `:951` 另有含 `(USD)` 的标签,同法去币种、给中文 `defaultValue`。

- [ ] **Step 7: 验证(typecheck + 手动闭环)**

Run(能跑则本地,否则服务器):`cd web/default && npx tsgo -b`
Expected: 该文件无类型错误。

服务器构建部署后手动闭环(CNY 模式):新建/编辑一个 per-token 模型,输入价填 `¥14.6` → 保存 → 重开该模型,输入价仍显示 `¥14.6`、「Calculated ratio」显示 `1.0000`;编辑一个 per-request 模型,填 `¥0.073` → 保存 → 重开显示 `¥0.073`。DB 中该模型 `model_ratio≈1`、`model_price≈0.01`(与切 USD 模式录入同价时一致)。

- [ ] **Step 8: 提交**

```bash
git add web/default/src/features/models/components/drawers/model-mutate-drawer.tsx
git commit -m "feat(models): 单模型定价抽屉显示+录入按 ¥(接单一汇率源,存库倍率零变)"
```

---

## Task 4: 批量定价表 `model-pricing-sheet.tsx` + 核心 `model-pricing-core.ts` 接换算模块

**Files:**
- Modify: `web/default/src/features/system-settings/models/model-pricing-core.ts`
- Modify: `web/default/src/features/system-settings/models/model-pricing-sheet.tsx`

**Interfaces:**
- Consumes: 同 Task 3 的换算模块 + `getEffectiveBillingRate` + `getCurrencyLabel`。
- Produces(core 签名变更,供 sheet 调用):
  - `createInitialLaneState(data?: ModelRatioData | null, rate: number)`(新增第 2 参 `rate`)
  - `buildPreviewRows(..., currencySymbol: string)`(在末尾新增参 `currencySymbol`;内部把 `$` 换成它)

- [ ] **Step 1: core —— `ratioToBasePrice`/`createInitialLaneState` 加 `rate`**

`model-pricing-core.ts` 顶部 import:
```ts
import { ratioToDisplayPrice } from '@/lib/model-pricing-currency'
```
`:157-161` `ratioToBasePrice`:
```ts
function ratioToBasePrice(ratio: unknown, rate: number): string {
  const num = toNumberOrNull(ratio)
  if (num === null) return ''
  return formatPricingNumber(ratioToDisplayPrice(num, rate))
}
```
`:174` `createInitialLaneState` 签名与调用:
```ts
export function createInitialLaneState(data?: ModelRatioData | null, rate = 1) {
```
其内 `:183` `const promptPrice = ratioToBasePrice(data.ratio, rate)`(其余 `deriveLanePrice` **不改**:lane 价 = 无量纲比 × promptPrice,已随 promptPrice 带上 ¥)。

- [ ] **Step 2: core —— `buildPreviewRows` 把 `$` 换成传入符号**

`:208-216` 在参数表末尾新增 `currencySymbol: string`。`:241-295` 七处 `` `$${...}` `` 全改为 `` `${currencySymbol}${...}` ``(值已是 ¥ 价字符串,仅换符号,不再运算):
```ts
      value: promptPrice ? `${currencySymbol}${promptPrice}` : t('Empty'),
```
(其余 6 处 `completion`/`cache`/`createCache`/`image`/`audio`/`audioCompletion` 同样把 `` `$${lanePrices.x}` `` 改成 `` `${currencySymbol}${lanePrices.x}` ``。)

- [ ] **Step 3: sheet —— 导入 + 计算 rate/symbol**

`model-pricing-sheet.tsx` 顶部 import:
```ts
import { getEffectiveBillingRate, getCurrencyLabel } from '@/lib/currency'
import { displayPriceToRatio, usdPriceToDisplay, displayToUsdPrice } from '@/lib/model-pricing-currency'
```
在组件内(靠近其它 `useMemo`/常量处)取当前符号:
```ts
  const currencySymbol = getCurrencyLabel() === 'USD' ? '$' : '¥'
```

- [ ] **Step 4: sheet —— 载入态传 rate、按次价 ×rate**

`:174-175`(useEffect 首行)与 `:180`:
```ts
    const rate = getEffectiveBillingRate()
    const nextLaneState = createInitialLaneState(editData, rate)

    if (editData) {
      form.reset({
        name: editData.name,
        price: editData.price ? usdPriceToDisplay(Number(editData.price), rate).toString() : '',
        ratio: editData.ratio || '',
        ...
```
（`ratio` 载入保持原样:core 已用 rate 把 `promptPrice` 显示成 ¥,`form.ratio` 仍是无量纲存储值。）

- [ ] **Step 5: sheet —— promptPrice→倍率 ÷rate、预览传符号**

`:253-257` `syncLaneRatios`:
```ts
    const inputPrice = toNumberOrNull(nextPromptPrice)
    setFormValue(
      'ratio',
      inputPrice !== null ? formatPricingNumber(displayPriceToRatio(inputPrice, getEffectiveBillingRate())) : ''
    )
```
（`deriveLaneRatio` 的 `:240 :245` **不改**:lane 比 = 价/价 无量纲。)
找到调用 `buildPreviewRows(...)` 的地方(约 `:390-437` 的 `useMemo`),在实参末尾补 `currencySymbol`。

- [ ] **Step 6: sheet —— 提交按次价 ÷rate、符号角标、文案**

`:444`(`buildSubmitData` 的 price,折回 USD 存储):
```ts
        price: values.price ? displayToUsdPrice(Number(values.price), getEffectiveBillingRate()).toString() : '',
```
（`:445` `ratio` **不改**,已无量纲。)
`:610` 固定价输入角标:
```ts
                                  <InputGroupAddon>{currencySymbol}</InputGroupAddon>
```
文案(W5):`:567` `t('USD price per 1M input tokens.')` → `t('USD price per 1M input tokens.', { defaultValue: '每 1M 输入 tokens 的价格。' })`;`:629` `'Cost in USD per request, regardless of tokens used.'` → 同法加 `defaultValue: '每次请求的固定价格,与 token 用量无关。'`。

- [ ] **Step 7: 验证(typecheck + 手动)**

Run:`cd web/default && npx tsgo -b`(能跑则本地,否则服务器)。
服务器部署后:批量定价表的输入价/各 lane 预览显示 ¥;编辑已有模型回填为 ¥;保存后 DB 倍率/按次价不变(与 Task 3 闭环同理)。

- [ ] **Step 8: 提交**

```bash
git add web/default/src/features/system-settings/models/model-pricing-core.ts web/default/src/features/system-settings/models/model-pricing-sheet.tsx
git commit -m "feat(models): 批量定价表+核心预览按 ¥(接单一汇率源,存库价倍零变)"
```

---

## Task 5: 共享 `agent-format.ts` 的 `usd()`/`quotaToUsd()` 接回中枢(连带修兑换码、我的用户)

**Files:**
- Modify: `web/default/src/lib/agent-format.ts`

**Interfaces:**
- `usd(v)` 与 `quotaToUsd(quota)` 保持同签名(入参 `number|null|undefined`,返回 `string`),内部改走中枢;`cny(v)` **不动**(恒 `¥${v.toFixed(2)}`,红线①)。

> 影响面(自动跟随):`features/redemptions/index.tsx:52`(兑换码面额)、`features/my-users/index.tsx:52`(下级用户额度)、`features/agent-listings/index.tsx` 里经 `usd` 的用量。

- [ ] **Step 1: 改实现**

`:34-40`:
```ts
import { formatBillingCurrencyFromUSD } from '@/lib/currency'

/** $ / ¥ per display currency (was hard-coded $). */
export const usd = (v: number | undefined | null) =>
  formatBillingCurrencyFromUSD(Number(v ?? 0))

/** Raw quota → display currency (was hard-coded $). */
export const quotaToUsd = (quota: number | undefined | null) =>
  formatBillingCurrencyFromUSD(Number(quota ?? 0) / QUOTA_PER_USD)
```
(`cny` 于 `:31-32` 保持不变;`QUOTA_PER_USD` 保留。)

- [ ] **Step 2: 验证**

Run:`cd web/default && npx tsgo -b`。
服务器部署后:兑换码管理面额、我的用户额度显示 ¥(CNY 模式)。注意 `formatBillingCurrencyFromUSD` 对 `null/NaN` 返回 `-`(旧 `usd()` 返回 `$0.00`)——确认这些页对 `-` 展示可接受(通常更好)。

- [ ] **Step 3: 提交**

```bash
git add web/default/src/lib/agent-format.ts
git commit -m "feat(agent-format): usd/quotaToUsd 接回中枢(兑换码/我的用户随显示货币显示 ¥)"
```

---

## Task 6: 套餐月限额 / 订阅额度全链路走中枢(Part B,机械替换)

**Files:**
- Modify: `web/default/src/features/token-plans/token-plans-columns.tsx`
- Modify: `web/default/src/features/tenant-plans/lib/format.ts`、`.../tenant-plans/plan-card.tsx`、`.../tenant-plans/my-subscriptions.tsx`
- Modify: `web/default/src/features/agent-listings/index.tsx`
- Modify: `web/default/src/features/subscription-monitor/lib/index.ts`、`.../subscription-monitor/subscription-monitor-columns.tsx`

**统一改法(pattern)**:凡显示 `month_limit_usd` / `used_usd` / `limit_usd`(美元制)处,把本地 `usd(x)` 调用替换为中枢 `formatBillingCurrencyFromUSD(x)`;**`*_cny` 字段的 `cny(x)` 一律保留不动**(红线①)。各文件顶部按需 `import { formatBillingCurrencyFromUSD } from '@/lib/currency'`。改完删除该文件里已无引用的本地 `usd` 定义(`cny` 若仍被 `*_cny` 使用则保留)。

- [ ] **Step 1: token-plans**

`token-plans-columns.tsx:109` `{usd(row.original.month_limit_usd)}` → `{formatBillingCurrencyFromUSD(row.original.month_limit_usd)}`。保留 `:86 :98` 的 `cny(base_price_cny)`/`cny(anchor_price_cny)`。加中枢 import;`:28-29` 本地 `usd` 若无其它引用则删除,`cny` 保留。

- [ ] **Step 2: tenant-plans**

- `plan-card.tsx:90` `usd(month_limit_usd)` → 中枢;保留 `:78 :82` `cny(retail_price_cny)`/`cny(anchor_price_cny)`。
- `my-subscriptions.tsx:66` `{usd(sub.used_usd)} / {usd(sub.limit_usd)}` → `{formatBillingCurrencyFromUSD(sub.used_usd)} / {formatBillingCurrencyFromUSD(sub.limit_usd)}`。
- `lib/format.ts:22,25`:本地 `usd` 若各页均已改走中枢则删除;`cny` 保留(供 `*_cny`)。

- [ ] **Step 3: agent-listings**

`index.tsx:77` `usd(month_limit_usd)` → 中枢;保留 `:51 :75 :76 :93` 的 `cny(*_cny)`。`:35` 的 import 从 `agent-format` 去掉 `usd`(保留 `cny`),改从 `@/lib/currency` 引 `formatBillingCurrencyFromUSD`。

- [ ] **Step 4: subscription-monitor**

`subscription-monitor-columns.tsx:97` `{usd(sub.used_usd)} / {usd(sub.limit_usd)}` → 中枢两处。`lib/index.ts:24` 本地 `usd` 若无其它引用则删除。

- [ ] **Step 5: 验证 + 提交**

Run:`cd web/default && npx tsgo -b`(无类型错误、无未用 import)。
服务器部署后:后台套餐管理月限额、用户「我的订阅」已用/上限、代理上架月限额、订阅监控 均显示 ¥;抽查套餐售价 `*_cny` 仍是原 ¥ 值(未 ×7.3)。
```bash
git add web/default/src/features/token-plans web/default/src/features/tenant-plans web/default/src/features/agent-listings web/default/src/features/subscription-monitor
git commit -m "feat(plans): 套餐/订阅月限额与额度全链路走中枢显示 ¥(_cny 恒 ¥ 不动)"
```

---

## Task 7: 损耗监控核对 +(可选)前台旁路收敛

**Files:**
- Verify/Modify: `web/default/src/features/breakage-monitor/*`
- (可选)Modify: `web/default/src/features/pricing/components/dynamic-pricing-breakdown.tsx`

- [ ] **Step 1: breakage-monitor**

`grep -rnE "\\\$|\\busd\\(" web/default/src/features/breakage-monitor`。若损耗额用硬编码 `$`/本地 `usd()`,按 Task 6 同法改走 `formatBillingCurrencyFromUSD`;`*_cny` 保留。若已走中枢或本就 ¥,记录「无需改」。

- [ ] **Step 2: (可选,低优先)前台动态定价明细**

`dynamic-pricing-breakdown.tsx:158-169` 自读 store 挑符号、`:292 :368` 手拼 `${symbol}${value*rate}`。当前**已显示 ¥、非 $ 问题**;为贯彻零硬编码可改用 `formatBillingCurrencyFromUSD(value)` 替代手拼(顺带支持 TOKENS)。**若时间紧可跳过**并在提交信息注明遗留。

- [ ] **Step 3: 提交**

```bash
git add web/default/src/features/breakage-monitor web/default/src/features/pricing/components/dynamic-pricing-breakdown.tsx
git commit -m "feat(sweep): 损耗监控接回中枢 + 前台定价明细收敛(全站再无硬编码 \$)"
```

---

## Task 8: 服务器构建 + 全量验收(spec §8)

**Files:** 无代码改动;执行构建、部署测试栈、按验收清单逐项核对。

- [ ] **Step 1: 部署前 scope 核对**

`git status` / `git diff --stat` 确认改动仅限本计划文件;`deploy.sh` 打包前排除无关 WIP([[deploy-scope-parallel-wip]])。

- [ ] **Step 2: 服务器构建 & 单测**

服务器上 `npx vitest run src/lib/currency.test.ts src/lib/model-pricing-currency.test.ts`(2 文件全绿)+ Docker 前端构建成功、`/api/status` 绿。

- [ ] **Step 3: 验收清单(CNY 模式)**

1. 模型定价闭环:设 `¥X/1M` → 保存 → 重开仍 `¥X`;DB `model_ratio`/`model_price` 与 USD 模式同价录入一致。
2. 套餐管理/我的订阅/代理上架/订阅监控:月限额、已用/上限全 ¥。
3. 兑换码面额、我的用户额度、损耗监控:无 `$`。
4. 红线①:套餐售价 `¥6.90`、代理财务、提现等 `*_cny` 数值**未**被放大 7.3 倍。

- [ ] **Step 4: 回归自检(零硬编码证明)**

后台把 `quota_display_type` 临时切 `USD` → 抽象额度类(模型价/月限额/额度/兑换码/用户额度)自动恢复 `$` 且数值 = 原 ¥ ÷7.3;`*_cny` 真实钱款仍显 ¥。核完切回 `CNY`。

- [ ] **Step 5: 收尾**

记录验收结果于 `RETRO.md`(如遇坑)/更新 `doc/tasks/STATUS.md`;确认测试栈健康后再议是否推 `origin/main`。

---

## Self-Review(计划对照 spec)

- **spec §5 Part A(模型定价 UI 显+入 ¥)** → Task 3(drawer)+ Task 4(sheet+core),含 per-token 与 per-request 两路径、载入/提交/预览/Calculated 串全覆盖。✅
- **spec §5 Part B(套餐/订阅月限额全链路)** → Task 6。✅
- **spec §5 Part C(全站扫 $:agent-format/兑换码/我的用户/损耗/前台旁路)** → Task 5 + Task 7。✅
- **spec §3 单一汇率源 + 防 7.3 倍** → Task 1 + Task 2(纯函数往返单测)。✅
- **spec §3 红线①(_cny 恒 ¥、不用 formatLocalCurrencyAmount)** → 各 Task 显式「`cny()` 不动」。✅
- **spec §3 红线②(后端/存库零改)** → Task 3/4 只在 UI 边界 ×/÷rate,`ratioMap`/`ratio` 提交不改。✅
- **spec §6 W5 全中文** → Task 3 Step6、Task 4 Step6 的 `defaultValue` 中文。✅
- **spec §8 验证 + §9 部署纪律** → Task 8。✅
- 类型/命名一致性:`getEffectiveBillingRate`、`ratioToDisplayPrice`/`displayPriceToRatio`/`usdPriceToDisplay`/`displayToUsdPrice`、`createInitialLaneState(data, rate)`、`buildPreviewRows(..., currencySymbol)` 全计划统一。✅
