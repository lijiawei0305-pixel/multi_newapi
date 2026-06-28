# 数据模型 · 迁移

> **占位文档（stub）。** 由 `CLAUDE.md` 路由表指向，改表结构 / 加字段 / 写迁移前先读此文。
> 权威来源：`newapi-multitenant-development-plan.md` §4 核心数据模型。

## 范围

第一期落地的租户与代理相关表结构及迁移策略（字段按 New API 现有表适配）。

## 表清单（来自 §4，待逐表细化）

- [ ] `tenants` — 代理站/租户主表（slug、agent_type、custom_domain、cost_price 等）
- [ ] `tenant_site_configs` — 站点品牌/装修配置
- [ ] `tenant_domains` — 自定义域名绑定
- [ ] `tenant_users` — 租户下级用户归属
- [ ] `tenant_groups` — 用户组
- [ ] `tenant_tokens` — 租户 Token
- [ ] `tenant_pricing` / `tenant_group_pricing` — 定价 / 分组倍率
- [ ] `tenant_billing_logs` — 计费日志
- [ ] `agent_levels` — 代理等级
- [ ] `agent_wallets` / `agent_earning_logs` / `agent_withdrawals` — 钱包 / 收益 / 提现
- [ ] `agent_promotion_channels` — 推广渠道
- [ ] `agent_redemption_codes` — 兑换码
- [ ] `agent_open_api_keys` — 开放 API 密钥

## 关键约束 / 注意

- TODO：与 New API 原有表的兼容/扩展方式（新增表 vs 改表）。
- TODO：迁移脚本规范与回滚策略。
- TODO：所有租户维度表的隔离字段与索引。
