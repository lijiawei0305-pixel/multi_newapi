# 🏢 Tenant 多租户基础 — 最小可执行任务（MET）

> **职责**：租户 CRUD 与状态；Host→租户解析（Redis 缓存）；slug 保留/唯一校验；提供 `scopeByTenant`。系统根基。
> **依赖**：`KVCache`、`TenantRepo`（无业务模块依赖）｜ **被依赖**：几乎所有模块
> **设计参考**：[detailed-design.md](../detailed-design.md) §2.1 ｜ [proposal.md](../proposal.md) §6/§9
> **一期范围**：仅 wildcard 二级域名（`*.wedreamhub.com`）；自定义域名顺延二期。

## A. 数据模型与迁移
- [ ] `tenants` 表（+`tokenplan_enabled`）+ `tenant_domains` 表迁移 ｜ ✅ 迁移跑通，含索引 `idx(slug)`、`idx(domain)`
- [x] 保留 slug 词表（`www/api/admin/root/dashboard/static/cdn/status/support`）落配置 ｜ ✅ 单测可读取

## B. 接口与领域逻辑
- [ ] `port.go`：`TenantResolver/TenantService/SlugValidator/TenantScoper` 接口定义 ｜ ✅ `go build` 通过
- [x] `SlugValidator.Validate`（保留词 + 格式）纯函数实现 ｜ ✅ 表驱动单测：保留词/非法字符/合法 全覆盖
- [ ] `TenantScoper.Scope` 注入 `tenant_id` 条件 ｜ ✅ 单测：生成 SQL 含 `tenant_id=?`

## C. 服务与解析
- [ ] `TenantRepo`（GORM）CRUD + slug 唯一约束 ｜ ✅ 集成测：重复 slug 入库报唯一冲突
- [x] `TenantService.Create`（建租户 + 自动写二级域名记录）｜ ✅ 测试：创建后 `tenant_domains` 有 `<slug>.wedreamhub.com`
- [x] `ResolveByHost`（查 `tenant_domains`/`slug`，Redis 缓存 + 失效）｜ ✅ 单测（mock cache/repo）：命中/穿透/未找到三分支；未找到返回 `TENANT_NOT_FOUND`
- [x] `SetStatus` 状态机（active/suspended/deleted）｜ ✅ 单测：非法迁移被拒
- [ ] 本地开发支持 Header/query 模拟 Host ｜ ✅ 带 `X-Debug-Host` 能解析到指定租户

## D. 接入与验收
- [ ] Gin 中间件 `TenantContext`：解析 Host → 注入 `ctx.tenant`；未开通显示"站点不存在" ｜ ✅ E2E：未知 Host 返回开通提示页
- [ ] 跨租户隔离用例：A 租户不能读 B 租户数据 ｜ ✅ 测试红→绿
