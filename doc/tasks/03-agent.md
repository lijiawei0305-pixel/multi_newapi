# 🧑‍💼 Agent 代理体系 — 最小可执行任务（MET）

> **职责**：代理类型/等级、子代理管理、代理钱包（API 额度 + 可提现余额）、收益日志、提现申请与审核。
> **依赖**：`AgentRepo`、`PricingGuard`、`txn`｜ **被依赖**：Billing/Wallet/TokenPlan（经 `EarningSink`）、Admin
> **设计参考**：[detailed-design.md](../detailed-design.md) §2.3 ｜ [proposal.md](../proposal.md) §5/§7
> **一期范围**：三类代理、等级、钱包、收益、提现全流程可演示。

## A. 数据模型与迁移
- [ ] `agent_levels`、`agent_wallets`、`agent_earning_logs`、`agent_withdrawals` 迁移 ｜ ✅ 迁移跑通
- [ ] `agent_earning_logs.source_type` 含 `recharge_spread/consume_commission/tokenplan_spread/tokenplan_commission/manual_adjustment` ｜ ✅ 枚举校验

## B. 接口与领域逻辑
- [ ] `port.go`：`AgentService`、`EarningSink`、`WithdrawalService` ｜ ✅ `go build` 通过
- [ ] `SetAgentType`（设类型/成本价/折扣/分润/等级，经 `PricingGuard`）｜ ✅ 单测：非法类型/击穿保护线被拒
- [ ] `AddEarning`（写日志 + 增 `withdrawable_balance`，幂等）｜ ✅ 单测：重复 source_id 不重复入账
- [ ] 提现状态机 `pending→approved/rejected`（冻结/解冻）｜ ✅ 单测：迁移合法性 + 金额守恒

## C. 服务与 API
- [ ] 管理员：搜索用户、设代理、设等级、启用/禁用 ｜ ✅ 接口测：用户→代理转换成功
- [ ] 代理：查钱包（API 额度/可提现/累计收益）｜ ✅ 接口测：金额正确
- [ ] 提现：申请（冻结）→ 管理员审核（通过=线下打款标记/拒绝=解冻）｜ ✅ E2E：全流程可演示

## D. 验收
- [ ] 提现金额超过可提现余额被拒 `WITHDRAW_INSUFFICIENT` ｜ ✅ 测试红→绿
- [ ] 代理只能看自己钱包/收益 ｜ ✅ 跨租户隔离测试
