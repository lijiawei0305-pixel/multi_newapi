# 代理站收益页精简设计(2026-07-04)

> 代理站现有「我的收益」(`/agent-earnings`)+「财务报表」(`/financial-report`)两个页。精简为**唯一一个「我的收益」页**,删掉代理站的财务报表页。经 3 轮互动确认。**主站(管理员)财务报表不动。**

## 一、精简后「我的收益」页(agent-earnings)

**顶部 7 张卡:**
- 钱包状态(现有 `EarningsSummaryCards`):可提现余额 / 冻结中 / 累计收益。
- v3 收益(复用 `financial-report` 的 `OverviewCards` agent scope 4 卡):套餐收益 / 套餐可提现 / apikey消费收益 / **apikey消费可提现**。
  - **改名**:agent overview 第 4 卡标签「消耗可提现」→「**apikey消费可提现**」(只影响代理 4 卡,不影响管理员 6 卡)。

**收益趋势图(3 线,按天):**
- 套餐可提现(`tokenplan_spread`)
- apikey消费可提现(`ratio_markup` + `consume_commission`)
- **总和**(前两者相加)

**保留:** 收款账户设置(`PayoutAccountCard`)、提现历史(`MyWithdrawalsTable`)、相关 dialog(`WithdrawDialog`/`PayoutAccountDialog`)。

**删除:** 收益明细表(`EarningsTable`)—— 逐笔日志与 v3 卡/趋势重叠。

## 二、删除代理站财务报表

- 代理导航去掉「财务报表」入口(`web/default/src/hooks/use-sidebar-data.ts` 代理段,`/financial-report`)。
- 删除/停用代理 financial-report 页(`features/financial-report/agent.tsx`)+ 其路由(`routes/_authenticated/financial-report`,仅代理可达的那条;若与管理员共用路由则按 scope 分流)。
- **主站(管理员)financial-report 保持不动**(`admin.tsx` + 6 卡 + 趋势 + 分项 + 明细 + 导出 + ranking)。

## 三、趋势图数据

需按天(桶)提供:套餐可提现(`tokenplan_spread`)、apikey消费可提现(`ratio_markup`+`consume_commission`)。前端算总和(第 3 条线)。
- 若现有 finance trend 的 earnings lens 已按天给出这些收益源 → 前端直接映射三线;
- 否则后端 trend(`internal/report/reportrepo` trend + `internal/mtwire/report.go` trend DTO)补上这两条源(按天)。

## 四、落地

1. **前端(主)**:`agent-earnings/index.tsx` 加 v3 4 卡 + 3 线趋势、删收益明细表、第 4 卡改名;删代理 financial-report 页 + 导航入口 + 路由;复用 `OverviewCards`/`TrendChart`(或为三线做一个专用轻量趋势)。取数:`getTenantFinanceSummary`(overview)+ finance trend。W5 中文。
2. **后端(按需)**:finance trend 按天补 `tokenplan_spread` / `ratio_markup`+`consume_commission` 两条源(若尚未提供)。
