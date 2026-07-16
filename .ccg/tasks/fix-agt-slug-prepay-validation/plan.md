# 实施计划 — AGT slug 付款前校验 + 软删复活

## 需求
AGT 代理套餐购买（`HandlePurchaseAgentPlan`）当前把买家填的 `slug` 零校验落单、立刻向微信/支付宝真实收钱，slug 合法性直到**付款成功后的回调激活**才校验 → 非法/保留/占用 slug 一律「钱已离账却确定性永久激活失败、订单永停 pending」。三类触发：① 买家填中文/过短/保留词/占用 slug ② 无需输入的软删占位黑洞（owner 曾被软删，slug 唯一索引仍占位，重购走新建必撞）。

## 目标 / 验收
- **付款前 fail-closed**：非法/保留/占用 slug 在**下单收钱之前**返回 `SLUG_INVALID/SLUG_RESERVED/SLUG_DUPLICATE`，绝不产生支付订单（对齐 tokenplan `http.go:258` 先校验再出凭据）。
- **软删复活**：owner 曾被软删的代理重购 → 复活其旧租户（拿回原 slug/子域名/下级/钱包），而非新建撞索引。
- **前端预校验**：slug 输入框加 maxLength/pattern/onSubmit 拦截 + 中文错误提示（W5），中文输入「我的小店」提交前即拦。
- **不扩大范围**：不动 AGT 对账（#9 / C5 已由 `agent_plan_reconcile.go` 另修）。

## 约束
- 复用 `internal/tenant` 权威校验（`NewSlugValidator().Validate` = 格式+保留词；`ErrSlug*` 已是 apperr，`respondErr` 直出前端）。
- `StatusDeleted` 是终态（状态机禁 deleted→active）→ 复活走 repo 级 `TenantRepo.SetTenantStatus` 直写，**刻意绕过状态机**（复活是回调确定性重入，非用户态迁移），加注释说明。
- 预检分支决策必须与 `provisionAgentFromOrder` **一一同构**，否则「预检过但激活失败」重新制造黑洞。

## 方案
预检与激活共享同一分支决策树：
```
已是代理(active, agentTenantByOwner)     → 升级路径，忽略 slug        → 放行
owner 有软删租户(agentDeletedTenantByOwner) → 复活路径，复用旧 slug      → 放行
全新代理                                  → normalize + 格式/保留词 + 状态盲查重 → 命中即 ErrSlug*
```
复活实现：翻转 deleted→active 后设 existing=true，落入现有升级分支（自带 EnsureSubdomain 重建域名 + SetAgentType + upsertMembership，全幂等）。

## 步骤
1. **`internal/mtwire/agent_plan_bridge.go`**
   - 新增 `agentDeletedTenantByOwner(ctx, uid) (int64,bool,error)`：`status = deleted` 的对称查询。
   - 新增 `precheckAgentPurchaseSlug(ctx, ownerUserID, rawSlug) error`：上述分支树，全新分支做 `normalizeAgentSlug` → `tenant.NewSlugValidator().Validate` → `Table("tenants").Where("slug=?").Count` 状态盲查重（>0 → `ErrSlugDuplicate`）。
   - `provisionAgentFromOrder`：在 `agentTenantByOwner` 之后、`if existing` 之前插入复活块——`!existing && hasDeleted` → `TenantRepo.SetTenantStatus(delID, StatusActive)` → `tenantID,existing = delID,true`（落入升级分支）。
2. **`internal/mtwire/http_agentplan.go`**
   - `HandlePurchaseAgentPlan`：解析 userID 后（:306）、`create` 落单前（:308），插入 `if err := a.precheckAgentPurchaseSlug(ctx, userID, body.Slug); err != nil { respondErr(c, err); return }`。
3. **`web/default/src/features/agent-plans/index.tsx`**
   - `!isExistingAgent` 时对 slug 做客户端校验（lowercase、3–63、`[a-z0-9-]` 且不首尾连字符、非保留词）；输入框 `maxLength={63}` + onChange 归一小写；错误中文提示；有错时禁用购买按钮 / 阻断 `purchase.mutate`。
   - `purchase` mutation 加 `onError` → 中文 toast 呈现后端 `SLUG_*` 错误（防御纵深；后端已 fail-closed 不收钱）。
4. **测试（`internal/mtwire`）**
   - `precheckAgentPurchaseSlug`：已是代理/软删 owner → nil；全新 invalid/reserved/duplicate/valid → 对应 `ErrSlug*` / nil。
   - `provisionAgentFromOrder` 复活回归：软删租户 owner 重购 → 复活同一 tenantID（不再 `ErrSlugDuplicate`）。
   - 可选 handler 级：非法 slug 购买返回 400/409 且未落 AGT 订单。

## 影响范围
- 修改：`agent_plan_bridge.go`、`http_agentplan.go`、`web/default/src/features/agent-plans/index.tsx`
- 新增：`internal/mtwire/agent_plan_slug_precheck_test.go`（或并入既有 agent_plan 测试）
- 服务器验证：`go test ./internal/...` + 构建部署（W4：构建/迁移/E2E 在服务器）
