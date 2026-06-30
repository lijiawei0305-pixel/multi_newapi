# 支付宝签名与验签规范

- **Source URL**: https://opendocs.alipay.com/open/270/105898（接入准备中的安全说明）
- **整理时间**: 2026-06-30
- **Purpose**: 支付宝 RSA2（SHA256WithRSA）签名算法和验签完整规范，适用于请求签名和通知验签。

---

## 密钥体系

| 密钥 | 持有方 | 用途 |
|------|--------|------|
| 应用私钥（RSA2） | 商户 | 对请求参数签名 |
| 应用公钥（RSA2） | 上传至支付宝开放平台 | 支付宝用于验证请求来源 |
| 支付宝公钥（RSA2） | 从开放平台下载 | 商户用于验证支付宝通知/响应签名 |

---

## 一、请求签名（商户端）

### 签名算法

`RSA2`（即 SHA256WithRSA / RSASSA-PKCS1-v1_5 with SHA-256）

### 步骤

1. **收集参数**：除 `sign` 外的所有请求参数（公共参数 + 业务参数）
2. **过滤空值**：移除值为空（`""`）的参数
3. **参数排序**：按参数名 **ASCII 升序**排序
4. **拼接字符串**：`key=value&key=value`（**值不做 URL 编码**）
5. **签名**：用应用私钥（RSA2）对字符串执行 SHA256WithRSA 签名
6. **编码**：签名结果 Base64 编码，赋值给 `sign` 参数
7. **URL 编码**：最终提交时对参数值做 URL 编码

### 拼接示例

原始参数（已排序）：

```
app_id=2021000122671234
biz_content={"out_trade_no":"ORDER001","product_code":"FAST_INSTANT_TRADE_PAY","total_amount":"29.80","subject":"月度套餐"}
charset=utf-8
format=JSON
method=alipay.trade.page.pay
notify_url=https://your-domain.com/api/payment/alipay/notify
return_url=https://your-domain.com/order/status
sign_type=RSA2
timestamp=2024-01-01 12:00:00
version=1.0
```

待签名字符串（`\n` 仅为可读性换行，实际是连续一行）：

```
app_id=2021000122671234&biz_content={"out_trade_no":"ORDER001",...}&charset=utf-8&...&version=1.0
```

### Go 签名代码

```go
import (
    "crypto"
    "crypto/rand"
    "crypto/rsa"
    "crypto/sha256"
    "encoding/base64"
    "sort"
    "strings"
)

func sign(params map[string]string, privateKey *rsa.PrivateKey) (string, error) {
    // 过滤空值、sign、sign_type
    keys := make([]string, 0, len(params))
    for k, v := range params {
        if k == "sign" || k == "sign_type" || v == "" {
            continue
        }
        keys = append(keys, k)
    }
    // ASCII 升序
    sort.Strings(keys)

    parts := make([]string, 0, len(keys))
    for _, k := range keys {
        parts = append(parts, k+"="+params[k])
    }
    message := strings.Join(parts, "&")

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

---

## 二、验签（验证支付宝通知）

### 使用密钥

**支付宝公钥**（从开放平台下载）

### 步骤

1. 从通知参数中取出 `sign`（Base64 编码的签名）
2. 移除 `sign` 和 `sign_type` 参数
3. 过滤空值参数
4. 剩余参数按 ASCII 升序排序，拼接为 `key=value&key=value` 字符串
5. 对 `sign` 执行 Base64 解码
6. 用支付宝公钥执行 SHA256WithRSA 验签

### Go 验签代码

```go
import (
    "crypto"
    "crypto/rsa"
    "crypto/sha256"
    "encoding/base64"
)

func verifySign(params map[string]string, alipayPublicKey *rsa.PublicKey) error {
    signStr := params["sign"]
    sig, err := base64.StdEncoding.DecodeString(signStr)
    if err != nil {
        return err
    }

    keys := make([]string, 0, len(params))
    for k, v := range params {
        if k == "sign" || k == "sign_type" || v == "" {
            continue
        }
        keys = append(keys, k)
    }
    sort.Strings(keys)

    parts := make([]string, 0, len(keys))
    for _, k := range keys {
        parts = append(parts, k+"="+params[k])
    }
    message := strings.Join(parts, "&")

    h := sha256.New()
    h.Write([]byte(message))
    digest := h.Sum(nil)

    return rsa.VerifyPKCS1v15(alipayPublicKey, crypto.SHA256, digest, sig)
}
```

---

## 三、使用 Go SDK 简化签名/验签

推荐使用 `github.com/smartwalle/alipay/v3`，自动处理签名和验签：

```go
import "github.com/smartwalle/alipay/v3"

// 初始化（正式环境）
client, err := alipay.New(appID, privateKey, true)
if err != nil {
    log.Fatal(err)
}
// 加载支付宝公钥（用于验签）
err = client.LoadAliPayPublicKey(alipayPublicKey)

// 验签（通知处理时）
ok, err := client.VerifySign(r.Form)
```

---

## 四、密钥格式

### 应用私钥（PKCS#8 格式，推荐）

```
-----BEGIN PRIVATE KEY-----
MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQC...
-----END PRIVATE KEY-----
```

### 支付宝公钥（PKCS#8 格式）

```
-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA...
-----END PUBLIC KEY-----
```

> **注意**：支付宝开放平台下载的公钥可能是裸 Base64，需手动加上 PEM 头尾。

---

## 五、常见错误

| 错误 | 原因 | 解决 |
|------|------|------|
| 验签失败 | 参数排序错误 | 确认按 ASCII 升序，不是按值排序 |
| 验签失败 | 包含了 `sign`/`sign_type` | 排除这两个参数再排序签名 |
| 验签失败 | 值做了 URL 编码 | 签名时值不编码，提交时才编码 |
| 验签失败 | 使用了 RSA 而非 RSA2 | 确认算法为 SHA256WithRSA |
| 签名异常 | 私钥格式错误 | 使用 PKCS#8 格式（非 PKCS#1） |

---

## 本项目接入注意事项

1. **推荐使用 `smartwalle/alipay` SDK**，避免手动实现签名。
2. **私钥不入库**：通过环境变量 `ALIPAY_PRIVATE_KEY` 注入，值为 PEM 字符串（换行替换为 `\n`）。
3. **支付宝公钥更新**：公钥版本更新时需同步更新配置。
4. **沙箱密钥与正式密钥分离**：沙箱和正式环境使用不同的应用和密钥对。
