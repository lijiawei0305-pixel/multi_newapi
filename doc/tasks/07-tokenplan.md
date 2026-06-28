# 🎟️ TokenPlan 套餐订阅 — 最小可执行任务（MET）★

> **职责**：套餐定义（管理员）、代理上架/改价、购买、订阅实例、**月度计量与到期**、`SubscriptionQuota`。本项目核心增量。
> **依赖**：`PlanRepo/SubscriptionRepo`、`PricingGuard`、`PaymentGateway`、`RiskEngine`、`EarningSink`、`txn`｜ **被依赖**：Billing(QuotaRouter)、Payment(OnPaid)
> **设计参考**：[detailed-design.md](../detailed-design.md) §2.7/§6.2 ｜ [proposal.md](../proposal.md) §8/§2.4
> **关键规则**：固定价买 30 天 → 月限额 USD 封顶 → x1（上游成本×1.0，不叠分组倍率）→ **独立计量不回退** → 用满/到期清零、手动重购。

## A. 数据模型与迁移
- [ ] `token_plans`（含 `anchor_price/discount_label/month_limit_usd/agent_cost_price/min_price/is_recommended/badge/sort`）迁移 ｜ ✅ 迁移跑通
- [ ] `tenant_token_plans`（`enabled/retail_price`，`UNIQUE(tenant_id,plan_id)`）迁移 ｜ ✅ 唯一约束生效
- [ ] `user_subscriptions`（`used_usd/status/expire_at/source_order_id`）+ `subscription_usage_logs` 迁移 ｜ ✅ 迁移跑通，索引齐全
- [x] 种子数据：Trial/Mini/Solo/Lite/Pro/Max 六档（按 proposal §8.2）｜ ✅ `SELECT` 出 6 行

## B. 接口与领域逻辑
- [x] `port.go`：`PlanCatalog`、`PlanRetailService`、`SubscriptionService`、`SubscriptionQuotaFactory` ｜ ✅ `go build` 通过
- [x] `SetListing`（上架/退出 + 改零售价，经 `PricingGuard` 校验 `retail>=min_price`）｜ ✅ 单测（mock Guard）：低于保护线 `RETAIL_BELOW_MIN`
- [x] 订阅状态机 `active→exhausted/expired/refunded`（到期惰性 + 定时双校验）｜ ✅ 单测：迁移合法性
- [x] `Meter`（条件 UPDATE 原子 `used_usd+=`，超额置 exhausted）｜ ✅ **并发测**：N goroutine 累加不击穿 month_limit
- [x] `ActivateFromPayment`（幂等创建 active 实例 + `tokenplan_spread` 收益）｜ ✅ 单测：同 orderID 多次只建 1 实例

## C. 服务与 API
- [ ] 管理员套餐 CRUD `/api/admin/token-plans` ｜ ✅ 接口测：增删改查
- [ ] 代理上架/改价 `/api/tenant/token-plans/manage` ｜ ✅ 接口测：保护线拦截
- [ ] 用户购买 `/api/tenant/token-plans/:id/purchase`（经 `RiskEngine` 限购）｜ ✅ E2E：下单→支付→激活
- [ ] 我的套餐 `/api/tenant/subscriptions`（剩余/到期）｜ ✅ 接口测：`remaining_usd` 正确
- [ ] `SubscriptionQuota.Charge` 适配 `Meter` ｜ ✅ 与 Relay 联调：套餐用户走套餐桶

## D. 验收
- [ ] 套餐内调用按 month_limit 月度封顶；超额/到期拦截提示重购、**不回退钱包** ｜ ✅ E2E：超额后调用被拦截
- [ ] Trial 限购（用户∪实名∪设备 各 1 次）生效 ｜ ✅ 并发购买只成功 1 次
- [ ] tokenplan 收益（差价/分润）入代理可提现余额 ｜ ✅ E2E：收益日志正确
