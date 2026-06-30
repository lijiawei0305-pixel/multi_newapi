# 微信支付 APIv3 概述与通用规则

- **Source URL**: https://pay.wechatpay.cn/doc/v3/merchant/4012081606
- **整理时间**: 2026-06-30
- **Purpose**: APIv3 接口规则总纲；所有请求/响应/验签/解密的基础规范。

---

## 与旧版 API 的核心差异

| 维度 | APIv3 | 旧版 |
|------|-------|------|
| 架构风格 | REST + JSON | SOAP / XML |
| 数据格式 | JSON | XML |
| 签名算法 | SHA256-RSA（非对称） | MD5 / HMAC-SHA256 |
| 证书要求 | 必须持有商户 API 证书 | 不强制 |
| 敏感字段加密 | AES-256-GCM | 无 |

---

## 接入必备材料

| 材料 | 说明 |
|------|------|
| `mchid` | 商户号，由微信支付分配 |
| `appid` | 公众号 / 开放平台 App 的 AppID |
| 商户 API 证书 | 含证书序列号（Serial Number）和公钥，用于请求签名 |
| 商户 API 私钥 | `apiclient_key.pem`，**必须保密，绝不入库** |
| APIv3 密钥 | 32 字节字符串，用于 AES-256-GCM 解密回调报文 |
| 微信支付公钥 / 平台证书 | 用于验证响应和回调签名 |

---

## 签名验签方案（二选一）

### 方案一：微信支付公钥模式（推荐）
- 使用微信支付提供的公钥验证 API 响应和回调签名
- 支持按需更新公钥，维护成本低
- 推荐新接入商户使用

### 方案二：平台证书模式
- 使用平台证书（有效期 5 年）
- 每 5 年需更新一次证书
- 实现复杂度略高

---

## 请求规范

### 请求 URL

- 主域：`https://api.mch.weixin.qq.com`
- 备域：`https://api2.mch.weixin.qq.com`（主域不可用时切换）

### 请求 Headers（通用）

| Header | 必选 | 说明 |
|--------|------|------|
| `Authorization` | 是 | 签名鉴权信息，见下方格式 |
| `Accept` | 是 | `application/json` |
| `Content-Type` | 是 | `application/json`（POST/PATCH） |

---

## Authorization Header 构造

格式：

```
Authorization: WECHATPAY2-SHA256-RSA2048 mchid="商户号",nonce_str="随机串",timestamp="时间戳",serial_no="证书序列号",signature="签名值"
```

### 构造签名串（message）

```
HTTP方法\n
URL（含路径和查询参数）\n
时间戳\n
随机串\n
请求体（GET 请求为空字符串）\n
```

示例：

```
POST\n
/v3/pay/transactions/native\n
1554208460\n
593BEC0C930BF1AFEB40B4A08C8FB242\n
{"appid":"wx...","mchid":"...","description":"...","out_trade_no":"...","notify_url":"...","amount":{"total":100,"currency":"CNY"}}\n
```

### 签名步骤

1. 用商户 API 私钥（RSA 2048）对消息串执行 SHA256WithRSA 签名
2. 对签名结果执行 Base64 编码
3. 将 Base64 编码值填入 `signature` 字段

---

## 响应验签

响应头中包含：

| Header | 说明 |
|--------|------|
| `Wechatpay-Timestamp` | 时间戳 |
| `Wechatpay-Nonce` | 随机串 |
| `Wechatpay-Signature` | Base64 签名 |
| `Wechatpay-Serial` | 平台证书序列号 |

验签消息串格式：

```
时间戳\n
随机串\n
响应体\n
```

使用微信支付公钥 / 平台证书公钥执行 SHA256WithRSA 验签。

---

## 回调解密（AES-256-GCM）

回调通知的 `resource` 字段结构：

```json
{
  "resource": {
    "algorithm": "AEAD_AES_256_GCM",
    "ciphertext": "Base64编码的密文",
    "associated_data": "附加数据",
    "nonce": "随机串（12字节）",
    "original_type": "transaction"
  }
}
```

解密步骤：

1. `key` = APIv3 密钥（32 字节 UTF-8）
2. `nonce` = `resource.nonce`（12 字节）
3. `associated_data` = `resource.associated_data`（可为空字符串）
4. `ciphertext` = Base64 解码 `resource.ciphertext`
5. 执行 AES-256-GCM 解密，得到明文 JSON

Go 伪代码：

```go
import (
    "crypto/aes"
    "crypto/cipher"
    "encoding/base64"
)

func decryptNotify(apiV3Key, nonce, associatedData, ciphertext string) ([]byte, error) {
    ct, _ := base64.StdEncoding.DecodeString(ciphertext)
    block, _ := aes.NewCipher([]byte(apiV3Key))
    gcm, _ := cipher.NewGCM(block)
    return gcm.Open(nil, []byte(nonce), ct, []byte(associatedData))
}
```

---

## 错误响应格式

```json
{
  "code": "PARAM_ERROR",
  "message": "参数错误",
  "detail": {
    "field": "/amount/total",
    "value": "-1",
    "issue": "金额不能为负数"
  }
}
```

---

## 官方 SDK

| 语言 | 地址 |
|------|------|
| Go | https://github.com/wechatpay-apiv3/wechatpay-go |
| Java | https://github.com/wechatpay-apiv3/wechatpay-java |

---

## 本项目接入注意事项

1. **私钥文件**：`apiclient_key.pem` 存储在服务器安全路径，环境变量注入，绝不写入代码或 git。
2. **APIv3 密钥**：同上，通过 `config.yaml` 读取，不硬编码。
3. **推荐使用 Go SDK**：`wechatpay-go` 封装了签名、验签、证书管理，避免手动实现错误。
4. **时间戳防重放**：验证回调时检查 `Wechatpay-Timestamp` 与当前时间差不超过 5 分钟。
5. **回调幂等**：同一笔订单可能收到多次回调，以 `out_trade_no` 做幂等。
