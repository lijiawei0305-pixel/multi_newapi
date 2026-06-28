# 部署 · Nginx · 运维

> **占位文档（stub）。** 由 `CLAUDE.md` 路由表指向，改部署 / 反代 / 证书 / 备份前先读此文。
> 权威来源：`newapi-multitenant-development-plan.md` §12 部署建议。
> **本文是「部署硬约束 C1–C8」的主要素材来源**——这里沉淀的坑应升级为 `CLAUDE.md` 的 C 条目。

## 范围

Docker / Docker Compose 部署、Nginx 反向代理与回调转发、支付回调配置、证书、备份与运维。

## TODO 待填（来自 §12）

- [ ] Docker Compose 编排（Go 后端 / 前端构建产物 / MySQL / Nginx）
- [ ] 支付回调配置（§12.1）
- [ ] Nginx 回调转发与 wildcard / 自定义域名反代（§12.2，与 `doc/domains-ssl.md` 对齐）
- [ ] 环境变量与密钥管理
- [ ] 数据库备份与恢复
- [ ] 上线前检查清单（呼应需求文档 §15 验收清单）

## 关键约束 / 注意（升级候选 → CLAUDE.md C1–C8）

- TODO：MySQL 版本 / 字符集 / 时区一致性。
- TODO：回调地址必须可公网访问且转发正确（呼应 §13.4 入账一致性）。
- TODO：证书与 wildcard / 自定义域名的反代配置。
