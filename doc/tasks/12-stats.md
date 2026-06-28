# 📊 Stats 统计看板 — 最小可执行任务（MET）

> **职责**：主站全局统计、代理本站统计、**tokenplan 订阅监控与满额预警**。只读聚合，增量精简。
> **依赖**：只读 `BillingRepo`/`SubscriptionRepo`｜ **被依赖**：Admin/代理后台页面
> **设计参考**：[detailed-design.md](../detailed-design.md) §2.12 ｜ [proposal.md](../proposal.md) §7.4/§2.4
> **一期范围**：基础统计 + 满额预警；复杂财务报表顺延三期。

## A. 接口与聚合
- [x] `port.go`：`StatsService` ｜ ✅ `go build` 通过
- [x] `AdminOverview`（全站租户数/活跃数/调用量/扣费/代理收益）｜ ✅ 单测：聚合数值正确
- [x] `TenantOverview`（本站用户数/Token 数/调用量/收入/收益）｜ ✅ 单测：仅本租户范围
- [x] `SubscriptionAlerts`（`used_usd/month_limit` ≥ 阈值如 80% 预警）｜ ✅ 单测：阈值边界正确

## B. API 与隔离
- [ ] 管理员统计接口 `GET /api/admin/stats` ｜ ✅ 接口测：全局数据
- [ ] 代理统计接口 `GET /api/tenant/stats` ｜ ✅ 接口测：代理只见本站
- [ ] 订阅满额监控 `GET /api/admin/subscriptions` ｜ ✅ 接口测：列出逼近满额订阅

## C. 验收
- [x] 代理无法看到其他租户统计 ｜ ✅ 跨租户隔离测试
- [ ] 满额预警可在后台展示（呼应防巨亏）｜ ✅ E2E：构造逼近满额订阅触发预警
