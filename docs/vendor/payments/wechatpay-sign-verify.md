# 微信支付 APIv3 签名与验签详解

- **Source URL**: https://pay.wechatpay.cn/doc/v3/merchant/4012081606
- **整理时间**: 2026-06-30
- **Purpose**: 微信支付 APIv3 请求签名（发送方）和响应/回调验签（接收方）的完整算法规范。

---

## 整体流程

```
商户签名请求 → 微信支付服务器 → 微信签名响应
                     ↓
              微信支付回调（需验签 + 解密）
```

---

## 一、请求签名（商户端）

### 使用的密钥
- **商户 API 私钥**（RSA 2048）：`apiclient_key.pem`
- 算法：**SHA256WithRSA**

### 构造签名消息串

格式（每行末尾必须有 `\n`，包括最后一行）：

```
HTTP方法\n
URL路径+查询参数\n
请求时间戳\n
请求随机串\n
请求报文主体\n
```

各字段说明：

| 字段 | 说明 |
|------|------|
| HTTP方法 | 大写，如 `POST`、`GET` |
| URL路径+查询参数 | 如 `/v3/pay/transactions/native`；若有查询参数则包含，如 `/v3/refund?offset=0` |
| 请求时间戳 | Unix 时间戳（秒），如 `1554208460` |
| 请求随机串 | 32 位随机字符串，字母+数字 |
| 请求报文主体 | POST 请求为 JSON body；GET 请求为**空字符串** |

### 签名步骤

```go
import (
    "crypto"
    "crypto/rand"
    "crypto/rsa"
    "crypto/sha256"
    "encoding/base64"
    "fmt"
)

func sign(privateKey *rsa.PrivateKey, method, urlPath, timestamp, nonce, body string) (string, error) {
    message := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n", method, urlPath, timestamp, nonce, body)
    h := sha256.New()
    h.Write([]byte(message))
    digest := h.Sum(nil)
    sig, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest)
    if err != nil {
        return "", err
    }
    return base64.StdEncoding.EncodeToString(sig), nil
}
```

### 构造 Authorization Header

```
WECHATPAY2-SHA256-RSA2048 mchid="商户号",nonce_str="随机串",timestamp="时间戳",serial_no="证书序列号",signature="Base64签名"
```

示例：

```
Authorization: WECHATPAY2-SHA256-RSA2048 mchid="1230000109",nonce_str="593BEC0C930BF1AFEB40B4A08C8FB242",timestamp="1554208460",serial_no="1DDE55AD98ED71D6EDD4A4A16996DE7B8F0A",signature="aOJp+VqLnkTMDCIcg..."
```

---

## 二、响应验签（商户端验证微信返回）

### 使用的密钥
- **微信支付公钥**（推荐）或**平台证书**中的公钥

### 响应 Header 中的签名信息

| Header | 说明 |
|--------|------|
| `Wechatpay-Timestamp` | 时间戳 |
| `Wechatpay-Nonce` | 随机串 |
| `Wechatpay-Signature` | Base64 签名 |
| `Wechatpay-Serial` | 平台证书序列号 |

### 构造验签消息串

格式（同请求，但内容不同）：

```
时间戳\n
随机串\n
响应报文主体\n
```

### 验签步骤

```go
import (
    "crypto"
    "crypto/rsa"
    "crypto/sha256"
    "encoding/base64"
)

func verify(publicKey *rsa.PublicKey, timestamp, nonce, body, signature string) error {
    message := fmt.Sprintf("%s\n%s\n%s\n", timestamp, nonce, body)
    h := sha256.New()
    h.Write([]byte(message))
    digest := h.Sum(nil)
    sig, err := base64.StdEncoding.DecodeString(signature)
    if err != nil {
        return err
    }
    return rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest, sig)
}
```

### 防重放攻击

- 检查 `Wechatpay-Timestamp` 与当前时间差不超过 **5 分钟**
- 可缓存 `Wechatpay-Nonce` 防止重放（可选，高安全场景）

---

## 三、回调验签

回调签名验证流程与响应验签完全相同：

1. 取 HTTP Header 中的 `Wechatpay-Timestamp`、`Wechatpay-Nonce`、`Wechatpay-Signature`
2. 读取请求 Body（原始 JSON 字符串）
3. 构造消息串：`时间戳\n随机串\nBody\n`
4. 使用微信支付公钥/平台证书验签

---

## 四、使用 Go SDK 简化签名/验签

`wechatpay-go` SDK 自动处理签名和验签，无需手动实现：

```go
// 初始化时传入私钥和证书序列号
opts := []core.ClientOption{
    option.WithWechatPayAutoAuthCipher(
        mchID,
        mchCertificateSerialNumber,  // 商户证书序列号
        mchPrivateKey,               // *rsa.PrivateKey
        mchAPIv3Key,                 // APIv3 密钥（解密用）
    ),
}
client, _ := core.NewClient(ctx, opts...)
// 之后所有 API 调用自动签名，响应自动验签
```

回调验签：

```go
import (
    "github.com/wechatpay-apiv3/wechatpay-go/core/notify"
    "github.com/wechatpay-apiv3/wechatpay-go/core/auth/verifiers"
    "github.com/wechatpay-apiv3/wechatpay-go/services/payments"
)

handler := notify.NewNotifyHandler(mchAPIv3Key, verifiers.NewSHA256WithRSAVerifier(certificateVisitor))

transaction := new(payments.Transaction)
notifyReq, err := handler.ParseNotifyRequest(ctx, httpRequest, transaction)
// err != nil 则验签失败，应返回 HTTP 500
```

---

## 五、加载私钥

```go
import "github.com/wechatpay-apiv3/wechatpay-go/utils"

// 从文件加载
privateKey, err := utils.LoadPrivateKeyWithPath("/secure/path/apiclient_key.pem")

// 从字符串加载（环境变量场景）
privateKey, err := utils.LoadPrivateKey(os.Getenv("WECHAT_PRIVATE_KEY"))
```

---

## 本项目接入注意事项

1. **不要手动实现签名**：直接使用 `wechatpay-go` SDK，避免手动构造签名出错。
2. **证书序列号**：从 `apiclient_cert.pem` 中提取，或从商户平台 API 安全页面复制。
3. **私钥路径**：通过环境变量 `WECHAT_KEY_PATH` 或 `WECHAT_PRIVATE_KEY` 注入，不写死在代码中。
4. **定期更新平台证书**：如使用平台证书模式，SDK 的 `CertificateDownloader` 支持自动更新。
5. **日志脱敏**：签名串中包含私钥相关信息，日志中不得打印原始签名串。
