# Plan — 微信/支付宝：DB表单凭据 + 主站进程内真实SDK + 去mock + 退役auth-service

## 决策（用户已确认）
- 凭据存 DB（管理页表单，类 Stripe）；真实 SDK **主站进程内**执行；回调直达主站；**去 mock**；退役 auth-service（不删码，下线不部署）；覆盖**充值(RCG)+套餐订阅(SUB)**。
- ⚠️ 私钥进库（与 Stripe 同模式，用户接受）。

## 架构变化
旧：mtwire → authServiceClient.CreatePay(HTTP) → auth-service(SDK/mock) → 回调 auth-service → forward /api/internal/order/paid → 入账。
新：mtwire → **进程内 realpay**(DB凭据).CreatePay → 返 code_url/跳转；微信/支付宝回调 **直达主站** /api/pay/{wechat,alipay}/notify → realpay.VerifyNotify → 按 order_no 前缀分发入账(RCG=CreditPaidOrder / SUB=ActivatePaidTokenplanOrder)。
**不变**：买家入口 /api/tenant/wallet/recharge、HandlePurchase；订单台账 payment_orders / mt_subscription_orders；入账(quota/原生订阅/分润)；金额可信(库内为准)+反篡改比对。

## 共享契约（三 Builder 必须一致）
**option keys = setting 变量名**：
- 微信：WechatPayEnabled(bool) WechatPayAppID WechatPayMchID WechatPayAPIv3Key WechatPayCertSerial WechatPayPrivateKey(PEM内容)
- 支付宝：AlipayEnabled(bool) AlipayAppID AlipayPrivateKey(PEM) AlipayPublicKey(PEM) AlipaySellerID(可空) AlipayReturnURL AlipaySandbox(bool)
**provider 取值**：`wxpay` | `alipay`（payment.Provider）。
**回调路由(公开,签名校验)**：`POST /api/pay/wechat/notify`、`POST /api/pay/alipay/notify`。notify_url = MT_PAY_NOTIFY_BASE(或 ServerAddress) + 上述路径。
**买家可用渠道**：enabled(option) && configured(DB凭据齐全)。
**buyer methods 端点**(沿用)：`GET /api/tenant/wallet/recharge/methods` → `{data:{methods:[]}}`。

## 改动分工（文件域不重叠，可并行）
### Builder A — 设置/选项（小）
- 新建 `setting/payment_wxpay_alipay.go`：上述 13 个变量（默认空/false）。
- `model/option.go`：InitOptionMap 注册（var→OptionMap）+ updateOptionMap 各 case（OptionMap→var；bool 用 strconv.ParseBool）。
- 文件域：`setting/`、`model/option.go`。

### Builder B — 主站支付重构（大·核心）
- **realpay 迁移**：`auth-service/realpay/` → `internal/payment/realpay/`；WxpayConfig 增 `PrivateKey`(PEM内容) 字段，加载用 `utils.LoadPrivateKey(content)`（content 非空优先，否则回退 PrivateKeyPath，保 auth-service 不破）。更新 auth-service/server.go 的 import 路径（1 行，保持 dormant 可编译）。
- **providerManager**(新, internal/mtwire)：从 setting.* 读凭据、构造并**缓存** realpay.SDK（按凭据指纹重建——wechatpay-go client 构造含证书下载，**不可每请求重建**）；方法 CreatePay/VerifyNotify/QueryOrder/Configured(provider)。
- **wire.go**：RechargeGateway 的 PaySDK 由 authServiceClient 换成进程内适配器(CreatePay→providerManager；Verify→不支持，回调另走)；保留 notifyBaseURL=MT_PAY_NOTIFY_BASE。删 authClient 装配。
- **http.go subscriptionPayURL**：authClient.CreatePay → providerManager.CreatePay。
- **新建 notify handler**（internal/mtwire）：HandleWechatNotify/HandleAlipayNotify(公开) → providerManager.VerifyNotify(provider, *http.Request) → 成功则按 order_no 前缀：SUB→ActivatePaidTokenplanOrder(orderNo, paidAmount)；否则→RechargeGateway.CreditPaidOrder(orderNo, txnID, paidAmount)。各平台 ack(微信 JSON{code:SUCCESS}/支付宝 "success")。
- **router/mt-router.go**：注册 `/api/pay/wechat/notify`、`/api/pay/alipay/notify`（公开组）；删 internalGroup 的 /order/paid（不再用）。
- **reconcile_loop / sub_reconcile**：QueryOrderStatus(HTTP) → providerManager.QueryOrder(provider, orderNo)；provider 取自 payment_orders / mt_subscription_orders。
- **payment_providers.go 重做**：删 mt_payment_provider_settings 表/store/migration + 删 admin GET/PUT providers 端点（表单接管配置）；保留 HandleTenantRechargeMethods，其 configured=providerManager.Configured、enabled=setting.*Enabled；ensureProviderUsable 同源。
- 删 authServiceClient + loadRechargeConfig 里 authServiceURL/internalSecret + HandleInternalOrderPaid（退役）。
- 文件域：`internal/payment/`、`internal/mtwire/`、`router/mt-router.go`、`auth-service/server.go`(仅 import 行)。+ 测试。

### Builder C — 前端表单（中）
- payment-settings-section.tsx：**删**状态+开关卡（payment-providers-status-section.tsx / payment-providers-api.ts 弃用），**新增**微信/支付宝**凭据表单 section**（仿 Stripe：zod 字段 + Input/Switch/Textarea(PEM) + 加进 onSubmit 的 update map + 渲染）。字段对应上述 13 option key。
- 买家 gating 不变（use-recharge-methods + tenant-recharge-card 仍调 methods 端点）。
- i18n(en/zh) 新键。
- 文件域：`web/default/`。

## 验证
服务器 golang:1.25.1：`go build ./... 之相关包` + `go test`；oven/bun：`tsgo -b` typecheck。然后部署重建测试栈、退役 auth-service(compose/nginx)、配 MT_PAY_NOTIFY_BASE。

## 风险
- wechatpay-go client 构造含网络(证书下载) → providerManager 必须缓存、按凭据指纹重建，勿每请求建。
- 私钥进库：表单字段脱敏回显(已存则显占位/不回明文)。
- 回调公网可达：notify 路由公开、仅签名校验；金额/appid/mchid/seller 校验保留。
- 真实凭据未配时：configured=false → 买家不显示该渠道、下单拒绝（不再有 mock 占位）。
