# Plan — 管理页加微信/支付宝渠道（状态+开关）+ 买家页 gating

## 目标
在 admin `system-settings/billing/payment` 页加「微信支付(Native)」「支付宝(电脑网站)」两张卡：显示**已配置/未配置**状态 + **启用开关** + 回调地址说明；凭据仍留 auth-service env（不入库）。买家充值页按"启用且已配置"过滤渠道。

## 现状（研究结论）
- ✅ **买家付款 UI 已存在且可用**：`TenantRechargeCard`（已挂载于 `recharge-form-card.tsx:212`）→ `/api/tenant/wallet/recharge`；微信走 `QRCodeSVG` 渲染真实 `code_url` 二维码，支付宝跳转 `alipay_url`。真实响应结构已适配。
- ❌ admin 页无微信/支付宝卡（只有 epay/stripe/creem/waffo）。
- ❌ 无"启用开关"与"已配置状态"后端来源；买家卡无条件显示两渠道（未按启用/配置过滤）。

## 改动方案

### 后端
1. **auth-service**：`/auth/healthz` 响应增 `providers:{wxpay:bool,alipay:bool}`（真实模式取 `s.real.Available(provider)`，mock 模式均 true）。— `auth-service/server.go`
2. **mtwire 启用开关存储**：新增持久化「渠道启用」标志（推荐用 new-api option key `WechatPayEnabled`/`AlipayEnabled`，与现有支付设置存储一致；或 mtwire 自存）。
3. **mtwire 客户端**：`authServiceClient.GetProviders(ctx)` 调 `/auth/healthz` 取 configured 状态。— `internal/mtwire/recharge.go`
4. **admin 端点**（AdminAuth，`mt-router.go` `/api/admin/payment/...`）：
   - `GET /api/admin/payment/providers` → `{wxpay:{configured,enabled}, alipay:{configured,enabled}}`
   - `PUT /api/admin/payment/providers/:provider` body `{enabled:bool}` → 写启用标志
5. **buyer 端点 + gating**：
   - `GET /api/tenant/wallet/recharge/methods`（UserAuth+Host）→ 返回允许的 provider 列表（enabled && configured）
   - `HandleWalletRecharge` 增 enabled&&configured 校验（纵深防御：禁用渠道下单拒绝）

### 前端 — admin（`features/system-settings/integrations/`）
6. 新增 `payment-providers-status-section.tsx`（或并入 `payment-settings-section.tsx`）：2 张卡 = 状态徽章(已配置/未配置，调 `GET /api/admin/payment/providers`) + 启用开关(调 PUT) + 回调路径只读说明（`/pay/wxpay/notify`、`/auth/alipay/notify`）。
7. 在 billing/payment section 渲染该组件。

### 前端 — buyer（`features/wallet/`）
8. `use-tenant-recharge.ts` / `tenant-recharge-card.tsx`：拉 `GET /api/tenant/wallet/recharge/methods`，只渲染允许的 provider 按钮；两者都不可用则隐藏整卡。

## 测试
- 后端：mtwire admin providers 端点（configured/enabled 组合）、HandleWalletRecharge 禁用渠道拒绝、auth-service healthz providers 字段。
- 前端：admin 卡状态/开关交互；buyer 卡 gating（禁用→不显示）。

## 关键文件
后端：`auth-service/server.go`、`internal/mtwire/recharge.go`、`internal/mtwire/http.go`、`router/mt-router.go`（+ 启用标志存储：`setting/` 或 mtwire）。
前端：`web/default/src/features/system-settings/integrations/payment-settings-section.tsx`(或新子组件)、`web/default/src/features/wallet/{api.ts,hooks/use-tenant-recharge.ts,components/tenant-recharge-card.tsx}`、i18n。

## 验证
服务器 `golang:1.25.1` 容器 `go build ./internal/mtwire/... ./auth-service/...` + `go test`；前端 `bun run build`（web/default）。分支 500L。

## 风险/缓解
- 启用标志存储位置（new-api option vs mtwire）需定 —— 见审批问题。
- admin 页是 System1 前端，新端点在 mtwire(System2)；AdminAuth 已有（复用 reconcile 端点同款）。
- 凭据始终不入库（auth-service env），admin 页只读状态、不收凭据。
