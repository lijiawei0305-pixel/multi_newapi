# 域名 · SSL

> **占位文档（stub）。** 由 `CLAUDE.md` 路由表指向，改域名识别 / 绑定 / 证书前先读此文。
> 权威来源：`newapi-multitenant-development-plan.md` §6 域名与租户识别。

## 范围

租户域名方案与 HTTPS 证书：第一阶段 wildcard 二级域名（`*.yourbrand.com`），后续 TOKEN HUB 标准自定义域名绑定与 SSL 配置。

## TODO 待填（来自 §6）

- [ ] 第一阶段域名方案：主站三域名 + wildcard 代理子域（§6.1）
- [ ] TOKEN HUB 自定义域名方案（§6.2）
- [ ] 自定义域名绑定流程：填域名 → DNS A 记录 → 管理员配 SSL（§6.3）
- [ ] 请求路由逻辑：按 Host 解析租户（§6.4，与 `doc/architecture.md` 对齐）
- [ ] HTTPS 证书方案（§6.5）

## 关键约束 / 注意

- TODO：自定义域名与 SSL 配置风险（呼应 §13.7）——证书签发/续期、回源、未配置证书的兜底行为。
