# 🚦 RelayGateway 中继网关 — 最小可执行任务（MET）

> **职责**：`/v1/*` 入口编排 —— 鉴权→租户→风控→模型权限→**桶路由**→转发→扣费→日志（风控先于模型权限）。自身不含业务规则，纯编排。
> **依赖**：`Authenticator`、`AccessGuard`、`RiskEngine`、`ModelPermission`、`BillingService`、`UpstreamPool`（全为接口）｜ **被依赖**：终端用户/API 代理调用
> **设计参考**：[detailed-design.md](../detailed-design.md) §2.11/§3.1 ｜ [proposal.md](../proposal.md) §10.7
> **复用**：上游转发复用 new-api `UpstreamPool`，入口前置租户识别与桶路由。

## A. 接口与编排
- [x] `ModelPermission.Check`（Token `model_allowlist` 校验）｜ ✅ 单测：不允许模型 `MODEL_NOT_ALLOWED`
- [x] `UpstreamPool.Forward` 包装接口（复用 new-api 转发）｜ ✅ 单测（mock）：返回 resp+usage
- [x] `RelayHandler.ChatCompletions` 编排：鉴权→租户活跃→风控→模型→选桶→转发→扣费→日志 ｜ ✅ **全 mock 依赖**单测：编排顺序与短路正确

## B. 关键路径
- [ ] 桶路由集成 `QuotaRouter`（有 active 套餐→套餐桶；否则钱包）｜ ✅ 单测：两路径选桶正确
- [x] 扣费失败处理（套餐超额/过期/余额不足 → 拦截，不回退）｜ ✅ 单测：错误正确返回客户端
- [ ] 兼容入口 `/v1/chat/completions`、`/v1/completions`、`/v1/embeddings`、`GET /v1/models` ｜ ✅ 接口测：OpenAI 风格响应

## C. 验收
- [ ] 端到端：页面 Token 调用 `/v1/chat/completions` → 产生 billing_log + 可选分润 ｜ ✅ E2E 通过
- [x] 短路：鉴权失败不进风控、风控失败不转发 ｜ ✅ 单测断言调用未发生
- [x] 冻结租户/超额套餐的调用被拦截 ｜ ✅ 测试红→绿
