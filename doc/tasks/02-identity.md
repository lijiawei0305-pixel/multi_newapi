# 🔐 Identity & Access 身份与权限 — 最小可执行任务（MET）

> **职责**：用户认证、API Token 鉴权（Token→user+tenant）、角色守卫。复用 New API 用户基座（增量：Token 绑租户）。
> **依赖**：`TokenStore`、`UserStore`（复用包装）、`TenantService`｜ **被依赖**：RelayGateway、各 Handler
> **设计参考**：[detailed-design.md](../detailed-design.md) §2.2 ｜ [proposal.md](../proposal.md) §14
> **一期范围**：复用 new-api 登录/会话；新增 Token 租户绑定与三类守卫。

## A. 数据模型与迁移
- [ ] `tenant_tokens` 增 `tenant_id`、`token_hash`、`model_allowlist`、`ip_allowlist` ｜ ✅ 迁移跑通；旧 Token 兼容
- [ ] Token 反查租户索引 `idx(token_hash)` ｜ ✅ 解释计划走索引

## B. 接口与守卫
- [x] `port.go`：`Authenticator`、`AccessGuard` 接口 ｜ ✅ `go build` 通过
- [x] `AuthenticateToken`（hash 校验 → 返回 `Principal{user,tenant,role}`）｜ ✅ 单测（mock TokenStore）：有效/失效/跨租户
- [x] `RequireAdmin/RequireTenantOwner/RequireTenantActive` ｜ ✅ 表驱动单测：放行/拒绝矩阵全覆盖

## C. 接入
- [ ] Gin 鉴权中间件（注入 Principal，失败 401/403）｜ ✅ E2E：无/错 Token 返回 `UNAUTHORIZED/TOKEN_INVALID`
- [ ] 管理员接口独立鉴权，不与代理权限混用 ｜ ✅ 代理调管理员接口返回 `FORBIDDEN_ADMIN`

## D. 验收
- [x] 跨租户 Token 调用必拒 ｜ ✅ 测试：B 租户 Token 访问 A 租户资源被拦截
- [x] 冻结租户/用户的 Token 不能调用 ｜ ✅ 测试：`TENANT_INACTIVE` 拦截
