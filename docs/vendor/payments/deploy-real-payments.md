# 真实微信/支付宝支付上线指南（auth-service 真实模式）

- **整理时间**: 2026-06-30
- **Purpose**: 把 auth-service 从 mock 切到真实微信 Native + 支付宝电脑网站支付的服务器上线步骤。
- **前置**: 已拿到商户凭据（见 `CLAUDE.md` 待办看板 🔴）。私钥/证书**绝不入库**，只放服务器。

> 入账侧（主站 internal/payment + internal/mtwire + 原生订阅/钱包）零改；本指南只动 auth-service 部署 + nginx + 凭据。

---

## 1. 代码侧改动（已完成，待服务器构建验证 W4）

- 真实适配器：`auth-service/realpay/{realpay,wxpay,alipay}.go`（微信 `wechatpay-go`、支付宝 `smartwalle/alipay/v3`）。
- auth-service 真实模式接线：`auth-service/{config.go,server.go}`（`mock:false` 放行 + 无状态 verify-and-forward）。
- 依赖：`go.mod` 已加 `wechatpay-apiv3/wechatpay-go`、`smartwalle/alipay/v3`。
- 金额反篡改：主站内网入账比对 `paid_amount` 与库内订单（`internal/payment/credit.go`、`internal/mtwire`）。
- notify 路径修复：微信回调 `/pay/wxpay/notify` 已在 auth-service 与 nginx 注册/转发。

**服务器首次构建（W4）**：
```bash
ssh newapi628
cd /path/to/repo
go mod tidy                      # 拉取两个支付 SDK + go.sum
go build ./...                   # 全量编译
go test ./internal/payment/... ./internal/mtwire/... ./auth-service/...
```
> ⚠️ `auth-service/realpay/alipay.go` 中 `TradePagePay` / `TradeQuery` 标了 `NOTE(W4)`：
> 若 pinned 的 smartwalle/alipay/v3 版本方法签名/字段不同，按注释微调（1-2 行）。

---

## 2. 放置凭据文件（服务器，chmod 600）

```bash
mkdir -p /root/newapi-test/secrets && chmod 700 /root/newapi-test/secrets
# 微信商户私钥
cp apiclient_key.pem            /root/newapi-test/secrets/wx_apiclient_key.pem
# 支付宝应用私钥（PKCS#8）+ 支付宝公钥
cp alipay_app_private_key.pem   /root/newapi-test/secrets/alipay_app_private_key.pem
cp alipay_public_key.pem        /root/newapi-test/secrets/alipay_public_key.pem
chmod 600 /root/newapi-test/secrets/*
```

---

## 3. 填环境变量（服务器 .env，不入库）

参照 `deploy/.env.test.example` 取消注释并填值，关键项：
```ini
AUTH_MOCK=false
MT_PAY_NOTIFY_BASE=https://tokendream.wedreamhub.com      # 与下方 nginx 域名一致
MT_INTERNAL_SECRET=<强随机，主站与 auth-service 同值>
AUTH_USD_TO_CNY_RATE=7.3                                  # 必须与主站 USDExchangeRate 同值（否则金额校验失败）
# 微信
AUTH_WXPAY_APP_ID / AUTH_WXPAY_MCH_ID / AUTH_WXPAY_APIV3_KEY / AUTH_WXPAY_CERT_SERIAL
AUTH_WXPAY_PRIVATE_KEY_PATH=/etc/auth-service/secrets/wx_apiclient_key.pem
# 支付宝
AUTH_ALIPAY_APP_ID
AUTH_ALIPAY_PRIVATE_KEY_PATH=/etc/auth-service/secrets/alipay_app_private_key.pem
AUTH_ALIPAY_PUBLIC_KEY_PATH=/etc/auth-service/secrets/alipay_public_key.pem
AUTH_ALIPAY_RETURN_URL=https://tokendream.wedreamhub.com/order/status
AUTH_ALIPAY_SANDBOX=false                                 # 先 true 沙箱验收，再切 false
```
并在 `deploy/docker-compose.test.yml` 的 auth-service 取消注释挂载 secrets 目录：
```yaml
    volumes:
      - /root/newapi-test/secrets:/etc/auth-service/secrets:ro
```
> 容器内路径 `/etc/auth-service/secrets/...` 须与 `AUTH_*_KEY_PATH` 一致。

---

## 4. nginx（已在 `deploy/nginx/tokendream.wedreamhub.com.conf` 配好，核对即可）

- `^~ /auth/` → 127.0.0.1:8180（支付宝回调 `/auth/alipay/notify`、mock 页）
- `^~ /pay/`  → 127.0.0.1:8180（**微信回调 `/pay/wxpay/notify`**）
- `^~ /api/internal/` → `return 404`（内网入账端点，绝不暴露公网）

商户后台回调地址需登记：
- 微信「支付结果通知」域名 → `https://<域名>/pay/wxpay/notify`
- 支付宝 `notify_url` 由下单参数动态传，无需后台登记；但需 ICP 备案 + 应用网关可达。

---

## 5. 重建并启动

```bash
docker compose -p newapi_test --env-file /root/newapi-test/.env \
  -f deploy/docker-compose.test.yml up -d --build
curl -s https://<域名>/auth/healthz        # 期望 {"success":true,"mock":false}
```

---

## 6. 沙箱/小额验收（先沙箱，后正式）

1. 充值：前端发起 → 微信出二维码 / 支付宝跳转 → 支付 → 回调验签入账 → 余额到账。
2. 套餐：购买 → 激活原生订阅 → `/v1` 走订阅桶。
3. 幂等：人为重推回调（或等平台重推）→ 余额只加一次。
4. 兜底：临时停 app（断回调）→ 支付 → 恢复 → 5min 内对账循环主动查单补入账。
5. 金额：构造金额不一致回调（仅测试环境）→ 主站拒绝入账并告警（PAY_AMOUNT_MISMATCH）。

---

## 7. 回滚

设 `AUTH_MOCK=true` 重新 `up -d` 即退回 mock；主站入账链路不变，不影响 System 1（epay/stripe/waffo）。
