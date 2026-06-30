# 💳 Payment 支付与回调 — 最小可执行任务（MET）

> **职责**：下单（微信/支付宝）、回调验签、**幂等入账**并分发（钱包充值 or 激活订阅）。内置于主应用进程内（`internal/payment/realpay` + `internal/mtwire/payment_inprocess.go`，凭据存 DB）。
> **依赖**：支付 SDK、`OrderRepo`、`OrderSink`（Wallet/TokenPlan 实现）｜ **被依赖**：Wallet、TokenPlan
> **设计参考**：[detailed-design.md](../detailed-design.md) §2.8/§3.3 ｜ [proposal.md](../proposal.md) §10.5/§12.1/§12.2/§13.4
> **现网**：真实支付已内置于主站进程内（真实 SDK 已落地，凭据存 DB、后台「系统设置 → 支付」选项卡表单填写并启用）；回调经主站 nginx `location /` 反代到 app，无独立 auth-service 块。早期独立 auth-service 已退役。若线上支付未通，先做管理员人工入账兜底（见 06-wallet）。

## A. 数据模型与迁移
- [ ] 支付订单表（`order_no` 唯一约束做幂等，`type=recharge|subscription`，绑 `tenant_id/user_id`）｜ ✅ 迁移跑通；重复 order_no 入库冲突
- [x] 订单状态机 `created→paid→credited / failed` ｜ ✅ 单测：迁移合法性

## B. 接口与逻辑
- [x] `port.go`：`PaymentGateway`、`CallbackHandler`、`OrderSink` ｜ ✅ `go build`；Payment 不 import Wallet/TokenPlan
- [x] `CreateOrder`（微信/支付宝下单，回填 notify_url）｜ ✅ 单测（mock SDK）：订单落库
- [x] `HandleWxpay/HandleAlipay`（验签 → 幂等 → `OnPaid` 分发）｜ ✅ 单测：错签 `SIGN_INVALID`；重复回调不重复入账
- [x] `OnPaid` 按 `type` 分发到 Wallet.Credit 或 TokenPlan.ActivateFromPayment ｜ ✅ 单测（mock Sink）：分发目标正确

## C. 部署与配置
- [ ] 后台「系统设置 → 支付」选项卡填 wxpay/alipay 凭据（存 DB，参考 proposal §12.1）｜ ✅ 填后启用，官方支付对买家显示
- [ ] 回调经主站 nginx `location /` 反代到 app（`/api/pay/wechat/notify`、`/api/pay/alipay/notify`），无需独立 `/pay/ /auth/` 块 ｜ ✅ 公网可达回调地址
- [ ] 回调入账绑定 `tenant_id/user_id/order_no/额度` ｜ ✅ E2E：入账记录可按租户筛

## D. 验收
- [ ] 微信回调 `/api/pay/wechat/notify` 正确入账 ｜ ✅ 沙箱/真实支付到账
- [ ] 支付宝回调 `/api/pay/alipay/notify` 正确入账 ｜ ✅ 到账
- [x] 重复/伪造回调被幂等/验签拦截 ｜ ✅ 测试红→绿
