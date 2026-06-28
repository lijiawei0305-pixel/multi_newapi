# 📣 Promotion 推广归属 — 最小可执行任务（MET）

> **职责**：推广渠道、专属注册链接、用户经链接/域名注册后归属代理与渠道。复用度高，增量精简。
> **依赖**：`PromotionRepo`、`TenantService`｜ **被依赖**：注册流程、Stats
> **设计参考**：[detailed-design.md](../detailed-design.md) §2.9 ｜ [proposal.md](../proposal.md) §3/§4.14
> **一期范围**：渠道创建 + 链接生成 + 注册归属。

## A. 数据模型与迁移
- [ ] `agent_promotion_channels`（`prefix/channel_code/signup_url/registered_count`）迁移 ｜ ✅ 迁移跑通

## B. 接口与逻辑
- [x] `port.go`：`PromotionService` ｜ ✅ `go build` 通过
- [x] `CreateChannel`（名称 + 前缀，前缀唯一）｜ ✅ 单测：重复前缀 `CHANNEL_PREFIX_DUP`
- [x] 生成链接 `/sign-up?channel=<prefix>_<rand>` ｜ ✅ 单测：格式正确、可解析回 channel_code
- [x] `AttributeOnSignup`（注册时绑 tenant+channel，计数+1）｜ ✅ 单测：归属落库、计数递增

## C. 服务与验收
- [ ] 渠道列表/创建接口 ｜ ✅ 接口测
- [ ] 经代理域名注册的用户也归属对应代理 ｜ ✅ E2E：`aaa.wedreamhub.com` 注册→归属 aaa
