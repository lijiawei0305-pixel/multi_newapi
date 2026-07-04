# 管理员财务报表精简设计(2026-07-04)

> 继「代理站收益页精简」(`doc/agent-earnings-simplify.md`)之后,把**管理员**财务报表(`/admin` 财务报表,`admin.tsx`)也收敛成和代理「我的收益」一致的干净格式:**概览卡 + 净收入趋势图**,只是比代理**多几张卡 + 保留跨代理排行**。经 2 轮互动确认(布局=保留跨代理排行;净图=3 线)。**代理侧的「我的收益」不动。**

## 一、精简后管理员报表页(`admin.tsx`,从上到下)

1. **控件**:时间区间 + 粒度(天/周/月)+ 刷新。**去掉 lens 透镜选择器**(下面已无按 lens 切换的视图)。
2. **v3 概览 6 卡**(重命名+重排,见 §二)。复用 `OverviewCards scope='admin'`。
3. **净收入趋势图(3 线)**:新组件 `net-income-trend-chart.tsx`(§三)。
4. **跨代理收益排行表**(`AgentRankingTable`,**保留,原样**——含排序/分页)。

**删除**(连带清理不再被引用的孤儿组件/接口):
- 通用 9 张 KPI 卡(`SummaryCards`)
- 旧 lens 趋势图(`TrendChart`)
- 分项来源(`BreakdownBySource`)
- 逐笔明细表(`DetailTable`)
- CSV/PDF 导出(`ExportButtons` + `downloadFinanceDetail`)

## 二、6 卡重命名 + 重排(`summary-cards.tsx` 的 admin 分支)

字段后端全有(`adminFinanceOverviewOut`),只改**显示名 + 顺序**;**agent 4 卡不动**。

| 顺序 | 新显示名(W5 中文) | 后端字段 |
| --- | --- | --- |
| 1 | 主站套餐收入 | `mainsite_tokenplan_revenue_cny` |
| 2 | 代理套餐收入 | `agent_tokenplan_revenue_cny` |
| 3 | 主站api消耗收入 | `mainsite_wallet_consumption_cny` |
| 4 | 代理api消耗收入 | `agent_wallet_consumption_cny` |
| 5 | 代理套餐可提现 | `tokenplan_rebate_cny` |
| 6 | 代理api消耗可提现 | `agent_api_rebate_cny` |

## 三、净收入趋势图(3 线,按天)

**公式**(**不减上游进货成本**,按用户明确要求暂不计):
- **套餐净收入** =(主站套餐收入 + 代理套餐收入)− 代理套餐可提现
- **api净收入** =(主站api消耗 + 代理api消耗)− 代理api消耗可提现
- **总净收入** = 套餐净 + api净(前端相加,第 3 条线)

**对账硬要求**:趋势图的**区间合计**必须与 §二 的 6 卡对得上——即
`Σ套餐净 = 卡1 + 卡2 − 卡5`、`Σapi净 = 卡3 + 卡4 − 卡6`。
因此"扣代理"那两块**必须只算代理、排除主站**,与 overview 的 `tokenplan_rebate`/`agent_api_rebate` 完全同口径(overview 用 `splitByPlatform(..., platformID)` 取代理侧算,见 `internal/mtwire/report.go`)。

## 四、后端:新增管理端净收入趋势

**端点**:`GET /api/admin/finance/net-trend`(AdminAuth,跨租户;入参 `start_timestamp`/`end_timestamp`/`granularity`,与既有 `/api/admin/finance/trend` 同参)。
**响应**(统一信封 `data.series`):每桶 `{bucket, bucket_ts, tokenplan_net_cny, api_net_cny}`(金额边界 `round2`;`bucket`=日历标签,`bucket_ts`=桶起始 epoch)。**总净由前端相加**。

**repo `NetIncomeTrend(ctx, start, end, granularity, platformID)`**(`internal/report/reportrepo/reportrepo.go`),按天复用现成桶做减法:
- `subscriptionPaid[day]` =(全站,含主站)订阅实付 —— 复用 `bucketedSubPaidCost(ctx, nil, …)` 取 paid 分量(= 卡1+卡2 的按天分解)。
- `tokenplanRebate[day]` =(**排除主站**)`agent_earning_logs` `source_type='tokenplan_spread'` 按天和 —— `bucketedSumDatetime(agent_earning_logs, amount, created_at, nil, …, extra=source_type='tokenplan_spread' AND tenant_id<>platformID)`(= 卡5 按天分解)。
- `walletConsumption[day]` =(全站,含主站)`mt_wallet_consume_log` 按天和 —— `bucketedSumDatetime(mt_wallet_consume_log, …, nil, …)` 经 `QuotaToCNY`/既有换算(与 `WalletConsumption` 口径一致)(= 卡3+卡4 按天分解)。
- `apiRebate[day]` =(**排除主站**)`agent_earning_logs` `source_type IN ('ratio_markup','consume_commission')` 按天和,`tenant_id<>platformID`(= 卡6 按天分解)。
- `tokenplan_net = subscriptionPaid − tokenplanRebate`;`api_net = walletConsumption − apiRebate`。
- 桶标签取三/四个 map 的并集后 `sort.Strings`(镜像 `TrendEarnings`/`TrendRecharge` 写法)。

**mtwire 层**(`internal/mtwire/report.go`):`platformID` 由既有 `resolvePlatformTenantID`/`a.platformTenant(ctx)` 解析后传入 repo;DTO `netIncomeTrendOut{bucket,bucket_ts,tokenplan_net_cny,api_net_cny}`;handler `handleAdminNetIncomeTrend`;在既有 admin finance 路由组注册。

**测试**:repo 级(多天桶对齐 + 排除主站后 rebate 不含平台租户)+ handler 级(端点信封 + round2),镜像既有 finance trend 测试。

## 五、前端接口 + 类型

- `types.ts`:`NetIncomeTrendPoint {bucket,bucket_ts,tokenplan_net_cny,api_net_cny}` + 响应类型。
- `api.ts`:`getAdminFinanceNetTrend(range, granularity)` → `GET /api/admin/finance/net-trend`。
- `net-income-trend-chart.tsx`:3 线 VChart,复用 `agent-earnings/components/earnings-trend-chart.tsx` 的主题/样式;线名 `套餐净收入 / api净收入 / 总净收入`。**注意 `'Total'` key 已被 zh.json 占用('总计'),第 3 线用独立 key**(如 `'Total Net Income'` defaultValue `'总净收入'`)避免被覆盖。

## 六、清理孤儿(先 grep 验证零引用再删,存疑就留着,别为清理弄坏构建)

删除 `admin.tsx` 用法后,以下**仅** admin 引用(agent 页已删)→ 逐个 grep 确认无其它 importer 后删除:
- `components/trend-chart.tsx`、`components/breakdown-by-source.tsx`、`components/detail-table.tsx`、`components/export-buttons.tsx`
- `summary-cards.tsx` 里的 `SummaryCards` 导出函数(**保留 `OverviewCards`,保留文件**)
- `api.ts`:`getAdminFinanceDetail`/`getTenantFinanceDetail`/`downloadFinanceDetail`(注意 `getTenantFinanceSummary`/`getTenantFinanceTrend` **仍被 agent-earnings 用,保留**)
- `report-controls.tsx`:把 lens 选择器改为**可选**(admin 不再传),或保留 range+granularity+refresh 版本

**不删**:管理员 finance-report 页与其路由(本次是重建页面内容,不是删页)。后端 `/finance/detail`、`/finance/trend` 端点保留(不再被前端调用,无害)。

## 七、落地

1. **后端**:reportrepo `NetIncomeTrend` + mtwire DTO/handler/路由 + 测试。
2. **前端**:`admin.tsx` 重建布局;`summary-cards.tsx` admin 6 卡改名重排;新 `net-income-trend-chart.tsx`;`types.ts`/`api.ts` 加净趋势;清理孤儿。W5 全中文。
3. **门禁**:`bun run typecheck` 干净、无 `consumptionCaveat` 回退、`go build ./...` + `go test ./internal/report/... ./internal/mtwire/...` 通过;`bun run build` 重生路由树。
4. **禁改锁定文件**:`system-settings/*`、`i18n/locales/*`、`payment_inprocess.go`、`realpay/*`、`option.go`、`payment_wxpay_alipay.go`。提交只 `git add` 显式路径(严禁 `-A`/`.`/`-a`)。
