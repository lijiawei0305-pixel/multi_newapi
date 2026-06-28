# 🛡️ RiskControl 风控 — 最小可执行任务（MET）

> **职责**：调用前状态/限流/IP 校验、**Trial 限购**、**满额逼近告警**。横切但独立成模块以便单测。
> **依赖**：`KVCache`（Redis）、`RiskRepo`｜ **被依赖**：RelayGateway（CheckCall）、TokenPlan（CheckPurchaseLimit）、Stats
> **设计参考**：[detailed-design.md](../detailed-design.md) §2.13 ｜ [proposal.md](../proposal.md) §2.4/§13
> **一期范围**：基础风控（状态/RPM/IP/并发）+ Trial 限购 + 满额告警标记；规则引擎顺延。

## A. 接口与逻辑
- [x] `port.go`：`RiskEngine` ｜ ✅ `go build` 通过
- [x] `CheckCall`（租户/用户/Token 状态 + RPM 限流 + IP allowlist + 并发）｜ ✅ 单测（mock 时钟+KV）：超限 `RATE_LIMITED/IP_NOT_ALLOWED/STATUS_FORBIDDEN`
- [x] `CheckPurchaseLimit`（Trial = 用户∪实名∪设备 各 1 次）｜ ✅ 单测：三维去重；**并发购买只过 1 次**
- [x] `NoteUsage`（`used/limit` 逼近阈值打标，避免重复告警）｜ ✅ 单测：阈值触发一次

## B. 接入
- [ ] RelayGateway 调用前置 `CheckCall` ｜ ✅ 集成：异常调用被拦
- [ ] TokenPlan 购买前置 `CheckPurchaseLimit` ｜ ✅ 集成：Trial 重复购买被拒 `PURCHASE_LIMIT_EXCEEDED`

## C. 验收
- [ ] Trial 引流款防刷生效（呼应 proposal §2.4 防巨亏）｜ ✅ E2E：同设备/实名二次购买 Trial 失败
- [x] RPM/并发限流在压测下生效 ｜ ✅ 限流计数正确
