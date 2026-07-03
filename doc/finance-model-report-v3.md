# 财务模型 + 报表 v3 设计(2026-07-04)

> 用户重新理清金流后确认的口径。现有机制**两种提现时机已正确**(套餐=购买时、消耗=消耗时),本次主要**重组财务报表展示**并**删一处多余分成**。经 4 轮互动确认 + Explore 核对现有代码后定稿。

## 一、金流模型(两条,互不重叠)

### 金流 A · 套餐(tokenplan)—— 代理「购买即得差价、即可提现」
- 定价:平台按套餐设「内部成本」+「代理底价 `AgentCostPrice`」;代理按上架自设「售价 `RetailPrice`」(底价之上,可设上限)。同一套餐、不同代理可不同售价。
- 代理套餐收益 = **`tokenplan_spread = 售价 − 底价`**,在**购买(`ActivateFromPayment`)时**入账、**立即可提现**。
- **删除 `tokenplan_commission`**(套餐内额度被消耗时的二次分成)—— 套餐的钱在购买时一次性分完,后续消耗套餐额度**不再**给代理分成。
- 套餐**不支持退款**(购买即终态,提现无追回)。

### 金流 B · 钱包充值 → apikey 消耗 —— 代理「消耗时得差价、即可提现」
- 充值 **1:1** 进钱包(无充值差价;现有 `recharge_spread` 是死代码,可清理)。
- 消耗按倍率:用户倍率 `chargedGroupRatio` / 代理底价 `BottomPriceRatio` / 上游成本。
- 代理消耗收益 = **`ratio_markup`(L1)/ `consume_commission`(L0)**,**消耗时逐笔实时**入账、**立即可提现**。

### 主站(平台租户)
- 平台直营:主站(www 直接注册)用户的套餐 + 消耗收入**全归平台,无代理返现**。

### 提现
- 现有机制:每笔 earning 立即计入 `withdrawable_balance`;代理申请提现 → 冻结 → 管理员审核(通过=线下打款/驳回=解冻)。**本次不改提现流程**,只改报表口径。

## 二、报表

### 代理报表(4 项)
| 字段 | 含义 | 数据源 |
|---|---|---|
| 套餐收益 | 用户为套餐支付的总额(售价×份) | `subscription_paid` |
| 套餐可提现 | 代理套餐差价 | `tokenplan_spread` |
| apikey 消费收益 | 用户消耗的**钱包余额**(区间内) | `consumption.used_cost` |
| 消耗可提现 | 代理消耗差价 | `ratio_markup` + `consume_commission` |

> 套餐额度的消耗**不计入** apikey 消费收益(那是钱包余额的消耗),两条金流不重叠。

### 管理员报表(6 项,主站 / 代理站各自独立成列)
| 字段 | 含义 |
|---|---|
| 主站套餐收益 | 主站(平台直营)用户买套餐的支付总额 |
| 代理站套餐收益 | 各代理站用户买套餐的支付总额(合计) |
| 给代理的套餐返现 | Σ 各代理 `tokenplan_spread` |
| 主站钱包消耗 | 主站用户消耗的钱包余额 |
| 代理站钱包消耗 | 各代理站用户消耗的钱包余额(合计) |
| 需返现代理的 api 消耗金额 | Σ 各代理 `ratio_markup` + `consume_commission` |

- 主站 vs 代理站按 `tenant_id` 区分:平台租户(slug=`platform`)= 主站;其余 = 代理站。

### 时间口径
报表跟随现有时间范围选择器(今天 / 本周 / 自定义);"今天"仅为默认值。

## 三、落地改动
1. **后端·模型**:删除 `tokenplan_commission` 的入账(`internal/mtwire/agent.go` billingSource=="subscription" 分支)—— 套餐消耗不再给代理分成。
2. **后端·报表**:`internal/report/reportrepo/reportrepo.go` + DTO(`internal/mtwire/report.go`)按上述口径重组:代理 4 项;管理员 6 项(套餐收益、钱包消耗各按主站/代理站拆列)。
3. **前端·报表**:`web/default/src/features/financial-report/` 报表卡/DTO 按新字段重组(代理 4、管理员 6)。前端文案中文(W5)。
4. **(可选)清理**:死代码 `recharge_spread`(source + `wallet` module + 报表字段),YAGNI。
