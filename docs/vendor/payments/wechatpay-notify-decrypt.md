# 微信支付回调通知与 AES-256-GCM 解密

- **Source URL**: https://pay.wechatpay.cn/doc/v3/merchant/4012081606（通用规则中的回调章节）
- **整理时间**: 2026-06-30
- **Purpose**: 微信支付支付结果通知（Webhook）的完整接收、验签、解密和业务处理规范。

> ⚠️ **需人工补充**：回调通知专项页面未能自动抓取，以下内容基于官方 SDK 文档和 APIv3 通用规则整理，核心参数准确；若有字段出入，请对照 https://pay.wechatpay.cn/doc/v3/merchant/4012081611 人工核对。

---

## 通知触发时机

- 用户支付成功后，微信支付主动推送 HTTP POST 请求到 `notify_url`
- 微信以 **8 小时内、间隔递增**（15s/15s/30s/3min/10min/20min/30min/30min/30min...）重试，直到收到 HTTP 200
- **商户必须在 5 秒内响应**，否则视为失败重试

---

## 通知请求

### 请求 Headers

| Header | 说明 |
|--------|------|
| `Content-Type` | `application/json` |
| `Wechatpay-Timestamp` | 时间戳（Unix 秒） |
| `Wechatpay-Nonce` | 随机串 |
| `Wechatpay-Signature` | Base64 编码的 SHA256WithRSA 签名 |
| `Wechatpay-Serial` | 微信平台证书序列号 |

### 请求 Body 结构

```json
{
  "id": "EV-2018022511223320873",
  "create_time": "2015-05-20T13:29:35+08:00",
  "resource_type": "encrypt-resource",
  "event_type": "TRANSACTION.SUCCESS",
  "summary": "支付成功",
  "resource": {
    "original_type": "transaction",
    "algorithm": "AEAD_AES_256_GCM",
    "ciphertext": "Base64(密文)",
    "associated_data": "transaction",
    "nonce": "12字节随机串"
  }
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | string | 通知唯一 ID |
| `create_time` | string | 通知创建时间（RFC3339） |
| `event_type` | string | 事件类型，支付成功为 `TRANSACTION.SUCCESS` |
| `resource.algorithm` | string | 固定 `AEAD_AES_256_GCM` |
| `resource.ciphertext` | string | Base64 编码的加密报文 |
| `resource.nonce` | string | 12 字节加密随机串 |
| `resource.associated_data` | string | 附加数据（可为空字符串） |

---

## 验签步骤

参见 `wechatpay-sign-verify.md` 第三节"回调验签"。

核心：用微信支付公钥验证 `Wechatpay-Signature`，消息串为：

```
Wechatpay-Timestamp\n
Wechatpay-Nonce\n
RequestBody\n
```

---

## AES-256-GCM 解密步骤

1. 取 `resource.ciphertext` → Base64 解码得到密文字节
2. 取 `resource.nonce`（12 字节 UTF-8）
3. 取 `resource.associated_data`（UTF-8，可为空）
4. 使用 APIv3 密钥（32 字节 UTF-8）作为 AES key
5. 执行 AES-256-GCM 解密

```go
import (
    "crypto/aes"
    "crypto/cipher"
    "encoding/base64"
    "fmt"
)

func decryptAES256GCM(apiV3Key, nonce, associatedData, ciphertextB64 string) ([]byte, error) {
    ciphertext, err := base64.StdEncoding.DecodeString(ciphertextB64)
    if err != nil {
        return nil, fmt.Errorf("base64 decode: %w", err)
    }
    block, err := aes.NewCipher([]byte(apiV3Key))
    if err != nil {
        return nil, fmt.Errorf("new cipher: %w", err)
    }
    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return nil, fmt.Errorf("new gcm: %w", err)
    }
    plaintext, err := gcm.Open(nil, []byte(nonce), ciphertext, []byte(associatedData))
    if err != nil {
        return nil, fmt.Errorf("gcm decrypt: %w", err)
    }
    return plaintext, nil
}
```

解密结果为 JSON 字符串，即 `Transaction` 对象。

---

## 解密后的 Transaction 对象

```json
{
  "appid": "wxd678efh567hg6787",
  "mchid": "1230000109",
  "out_trade_no": "ORDER20240101120000001",
  "transaction_id": "1217752501201407033233368018",
  "trade_type": "NATIVE",
  "trade_state": "SUCCESS",
  "trade_state_desc": "支付成功",
  "bank_type": "CMC",
  "attach": "user_id=123&plan_id=456",
  "success_time": "2018-06-08T10:34:56+08:00",
  "payer": {
    "openid": "oUpF8uMuAJO_M2pxb1Q9zNjWeS6o"
  },
  "amount": {
    "total": 2980,
    "payer_total": 2980,
    "currency": "CNY",
    "payer_currency": "CNY"
  }
}
```

### 关键字段

| 字段 | 说明 |
|------|------|
| `out_trade_no` | 商户内部订单号（用于幂等查找） |
| `transaction_id` | 微信支付流水号（用于对账） |
| `trade_state` | `SUCCESS` / `REFUND` / `NOTPAY` / `CLOSED` / `REVOKED` / `USERPAYING` / `PAYERROR` |
| `attach` | 下单时传入的自定义数据（如 `user_id`/`plan_id`） |
| `amount.total` | 订单总金额（分） |
| `amount.payer_total` | 用户实付金额（分，含优惠后） |
| `success_time` | 支付完成时间（RFC3339） |

---

## 商户响应规范

### 成功响应（必须在 5 秒内返回）

```json
{}
```

HTTP 状态码：**200**

### 失败响应（业务处理失败，触发微信重试）

```json
{
  "code": "FAIL",
  "message": "数据库写入失败，稍后重试"
}
```

HTTP 状态码：**500**（或其他非 200）

---

## 使用 Go SDK 处理回调

```go
import (
    "net/http"
    "github.com/wechatpay-apiv3/wechatpay-go/core/downloader"
    "github.com/wechatpay-apiv3/wechatpay-go/core/notify"
    "github.com/wechatpay-apiv3/wechatpay-go/core/auth/verifiers"
    "github.com/wechatpay-apiv3/wechatpay-go/services/payments"
)

// 初始化（程序启动时执行一次）
func initNotifyHandler(mchPrivateKey *rsa.PrivateKey, mchCertSerial, mchID, mchAPIV3Key string) *notify.Handler {
    ctx := context.Background()
    err := downloader.MgrInstance().RegisterDownloaderWithPrivateKey(
        ctx, mchPrivateKey, mchCertSerial, mchID, mchAPIV3Key,
    )
    if err != nil {
        log.Fatalf("register downloader: %v", err)
    }
    certVisitor := downloader.MgrInstance().GetCertificateVisitor(mchID)
    return notify.NewNotifyHandler(mchAPIV3Key, verifiers.NewSHA256WithRSAVerifier(certVisitor))
}

// HTTP handler
func wechatPayNotifyHandler(handler *notify.Handler) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        transaction := new(payments.Transaction)
        _, err := handler.ParseNotifyRequest(r.Context(), r, transaction)
        if err != nil {
            // 验签失败
            w.WriteHeader(http.StatusInternalServerError)
            json.NewEncoder(w).Encode(map[string]string{"code": "FAIL", "message": err.Error()})
            return
        }

        if *transaction.TradeState == "SUCCESS" {
            // 业务处理：激活订阅、充值余额等
            // 必须幂等：以 out_trade_no 去重
        }

        // 成功响应
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusOK)
        json.NewEncoder(w).Encode(map[string]string{})
    }
}
```

---

## 幂等处理要点

1. 以 `out_trade_no` 为幂等键，先查数据库是否已处理
2. 已处理则直接返回 200，不重复执行业务逻辑
3. 先更新数据库状态，再返回 200（避免未持久化就响应）
4. 业务处理失败时返回非 200，让微信重试

---

## 本项目接入注意事项

1. **`notify_url` 必须 HTTPS**：本地调试需用 ngrok 等工具穿透。
2. **`attach` 字段利用**：在下单时传入 `user_id` 和 `plan_id`，回调时从 `attach` 解析，避免依赖 `out_trade_no` 反查。
3. **主动查单兜底**：不能仅依赖回调，需定期查询 `NOTPAY` 状态订单的实际支付结果。
4. **事务原子性**：订单状态更新和业务激活（如激活套餐）应在同一数据库事务中完成。
