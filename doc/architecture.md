# 架构 · 多租户识别 · 隔离

> **占位文档（stub）。** 由 `CLAUDE.md` 路由表指向，动手改本模块前先读此文，内容随开发填充。
> 权威来源：`newapi-multitenant-development-plan.md` §2 总体架构、§6.4 请求路由逻辑、§11 权限与隔离要求。

## 范围

单套后端、多租户隔离的整体架构与请求链路：`Nginx → Tenant Router（按 Host 识别租户）→ 前台/控制台 → 统一 API 网关（鉴权/扣费/限流/风控）→ 主站上游渠道池`。

## 关键约束 / 注意

- TODO：租户识别规则（Host / slug / 自定义域名）。
- TODO：数据隔离红线——任何查询/写入都必须带租户边界，禁止跨租户读写。
- TODO：代理商不可绕过主站统一扣费与风控。

## TODO 待填

- [ ] 请求生命周期时序图（落地版）
- [ ] Tenant Router 识别逻辑与兜底（未知 Host 行为）
- [ ] 统一 API 网关分层职责
- [ ] 越权与数据隔离的强制校验点清单（呼应 §11）
- [ ] 与 `doc/domains-ssl.md`、`doc/relay-channels.md` 的边界
