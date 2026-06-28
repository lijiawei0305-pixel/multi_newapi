# 💱 Pricing & CostGuard 定价与成本保护 — 最小可执行任务（MET）

> **职责**：用户组倍率、模型价、**成本保护线校验**（分组倍率与 tokenplan 零售价共用同一守卫）。纯规则模块，最易单测。
> **依赖**：`PricingRepo`（仅 `PricingService` 需要；`PricingGuard` 纯函数无依赖）｜ **被依赖**：Agent、Wallet、TokenPlan、Billing、Relay
> **设计参考**：[detailed-design.md](../detailed-design.md) §2.4 ｜ [proposal.md](../proposal.md) §7/§13.3
> **一期范围**：`model_ratio` + 分组倍率 + 保护线；复杂 fixed price 顺延。

## A. 数据模型与迁移
- [ ] `tenant_pricing`、`tenant_group_pricing`、`tenant_groups`（含 `min/max_group_ratio`）迁移 ｜ ✅ 迁移跑通
- [ ] 主站全局保护配置（`group_floor_ratio`、最低成本价、`min_margin_ratio`）落表/配置 ｜ ✅ 可读取

## B. 纯逻辑（零 mock，TDD 优先）
- [ ] `port.go`：`PricingService`、`PricingGuard` 接口 ｜ ✅ `go build` 通过
- [ ] `ValidateGroupRatio(ratio, floor)` ｜ ✅ 表驱动：等于放行、低于拦截 `RATIO_BELOW_FLOOR`
- [ ] `ValidateRetailPrice(retail, cost, minMargin)`（`retail >= cost*(1+minMargin)`）｜ ✅ 表驱动：边界放行、低于/负利润拦截 `PRICE_BELOW_PROTECTION`

## C. 服务
- [ ] `GroupRatio(tenant, group)`（无分组专属则回退模型默认倍率）｜ ✅ 单测：覆盖/回退两路径
- [ ] `ModelPrice(tenant, model)`（含 `min_floor_price`）｜ ✅ 单测：低于 floor 被拦截

## D. 验收
- [ ] 被 Agent/TokenPlan/Wallet 调用的保护线校验生效 ｜ ✅ 集成测：代理改价/改倍率击穿保护线被拒
