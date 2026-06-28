# 🧮 Billing & Quota 计费与额度桶 — 最小可执行任务（MET）

> **职责**：`QuotaSource` 抽象 + `QuotaRouter`（双桶路由）；扣费、写计费日志、触发消耗分润。计费中枢。
> **依赖**：`QuotaRouter`、`ModelCatalog`、`EarningSink`、`CallLogWriter`、`txn`｜ **被依赖**：RelayGateway
> **设计参考**：[detailed-design.md](../detailed-design.md) §2.5/§6.2 ｜ [proposal.md](../proposal.md) §7/§8.1
> **关键规则**：**独立计量不回退** —— 有 active 套餐走套餐桶，超额/过期拦截不切钱包。

## A. 接口与抽象
- [ ] `port.go`：`QuotaSource`、`QuotaRouter`、`BillingService` 接口 ｜ ✅ `go build` 通过；注意 Billing 不 import Wallet/TokenPlan
- [ ] `QuotaRouter.Select`（有 active 套餐→SubscriptionQuota 否则 WalletQuota）｜ ✅ 单测（mock SubscriptionService）：两分支正确

## B. 计费逻辑
- [ ] `BillingService.Charge`：算上游成本 → 路由桶 → 原子扣减 → 写 `tenant_billing_logs` → `AddEarning(consume_commission)` ｜ ✅ 单测（mock QuotaSource/EarningSink）：扣费→日志→分润顺序正确
- [ ] 错误传播：桶不足/超额/过期 → 返回 `QUOTA_INSUFFICIENT/SUBSCRIPTION_EXHAUSTED/SUBSCRIPTION_EXPIRED` ｜ ✅ 单测：mock 各错误均正确上浮、不吞码
- [ ] 计费与日志/分润在同一事务 ｜ ✅ 单测：扣减失败则日志/分润不写

## C. 数据模型
- [ ] `tenant_billing_logs`（`upstream_cost/charged_quota/gross_profit/group_key`）迁移 ｜ ✅ 迁移跑通
- [ ] `gross_profit = charged_quota - upstream_cost` 计算 ｜ ✅ 单测：数值正确

## D. 验收
- [ ] 用 mock QuotaSource 注入"成功/不足/超额"覆盖核心分支（无需起 DB）｜ ✅ 覆盖率达标
- [ ] 与 Relay 联调：一次调用产生一条 billing_log + 可选分润 ｜ ✅ E2E 通过
