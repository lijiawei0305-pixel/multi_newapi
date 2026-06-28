# 👛 Wallet & Recharge 钱包/充值/兑换码 — 最小可执行任务（MET）

> **职责**：用户 API 余额、充值订单、兑换码、`WalletQuota`（`QuotaSource` 实现之一）。复用 New API 充值能力 + 租户维度。
> **依赖**：`WalletRepo`、`PricingService`、`EarningSink`、`txn`、`PaymentGateway`｜ **被依赖**：Billing(QuotaRouter)、Payment(OrderSink)
> **设计参考**：[detailed-design.md](../detailed-design.md) §2.6/§6.2 ｜ [proposal.md](../proposal.md) §5/§7
> **一期范围**：充值/人工入账/兑换码/订单历史；WalletQuota 扣减。

## A. 数据模型与迁移
- [ ] 充值订单表 + `agent_redemption_codes` 迁移（绑 `tenant_id`）｜ ✅ 迁移跑通
- [ ] 用户余额字段（`tenant_users.balance`）入账绑租户 ｜ ✅ 单测：入账必带 tenant_id+user_id

## B. 核心逻辑
- [ ] `port.go`：`WalletService`、`WalletQuotaFactory` ｜ ✅ `go build` 通过
- [ ] `Credit`（充值/人工入账 → 增余额 → 算充值差价 `AddEarning(recharge_spread)`）｜ ✅ 单测（mock Pricing/EarningSink）：差价=实付−成本
- [ ] `Redeem`（兑换码入账 + 状态机 enabled→used）｜ ✅ 单测：已用/过期/无效码被拒
- [ ] `WalletQuota.Charge`（条件 UPDATE `balance-cost>=0`）｜ ✅ **并发测**：N goroutine 扣减不透支

## C. 服务与 API
- [ ] 钱包页接口：当前余额/充值入口/订单历史/兑换 ｜ ✅ 接口测：字段齐全
- [ ] 管理员人工入账（选租户/用户/额度 + 日志）｜ ✅ E2E：入账可演示（线上支付未通时的兜底）

## D. 验收
- [ ] 充值成功后代理充值差价收益正确入账 ｜ ✅ E2E：代理可提现余额增加
- [ ] 余额不足无法继续调用 ｜ ✅ 测试：`QUOTA_INSUFFICIENT`
