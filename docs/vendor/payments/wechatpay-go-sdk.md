# 微信支付官方 Go SDK（wechatpay-go）

- **Source URL**: https://github.com/wechatpay-apiv3/wechatpay-go
- **整理时间**: 2026-06-30
- **Purpose**: 官方 Go SDK 的安装、初始化、调用和回调处理完整参考。

---

## 安装

```shell
go get -u github.com/wechatpay-apiv3/wechatpay-go
```

---

## 初始化 Client

```go
package wechatpay

import (
    "context"
    "log"

    "github.com/wechatpay-apiv3/wechatpay-go/core"
    "github.com/wechatpay-apiv3/wechatpay-go/core/option"
    "github.com/wechatpay-apiv3/wechatpay-go/utils"
)

var Client *core.Client

func InitClient(mchID, mchCertSerial, apiV3Key, privateKeyPath string) {
    privateKey, err := utils.LoadPrivateKeyWithPath(privateKeyPath)
    if err != nil {
        log.Fatalf("load private key: %v", err)
    }

    ctx := context.Background()
    opts := []core.ClientOption{
        option.WithWechatPayAutoAuthCipher(mchID, mchCertSerial, privateKey, apiV3Key),
    }
    Client, err = core.NewClient(ctx, opts...)
    if err != nil {
        log.Fatalf("new wechat pay client: %v", err)
    }
}
```

### 凭据说明

| 参数 | 来源 | 说明 |
|------|------|------|
| `mchID` | 商户平台 | 商户号，如 `1230000109` |
| `mchCertSerial` | 商户平台 API 安全页 | 商户 API 证书序列号（大写十六进制） |
| `apiV3Key` | 商户平台 API 安全页 | 32 位 APIv3 密钥 |
| `privateKeyPath` | 本地安全路径 | `apiclient_key.pem` 路径 |

---

## Native 扫码支付

```go
import (
    "github.com/wechatpay-apiv3/wechatpay-go/core"
    "github.com/wechatpay-apiv3/wechatpay-go/services/payments/native"
)

func CreateNativeOrder(ctx context.Context, outTradeNo, description, notifyURL string, totalFen int64, attach string) (string, error) {
    svc := native.NativeApiService{Client: Client}
    resp, _, err := svc.Prepay(ctx, native.PrepayRequest{
        Appid:       core.String("YOUR_APPID"),
        Mchid:       core.String("YOUR_MCHID"),
        Description: core.String(description),
        OutTradeNo:  core.String(outTradeNo),
        NotifyUrl:   core.String(notifyURL),
        Attach:      core.String(attach),
        Amount: &native.Amount{
            Total:    core.Int64(totalFen),
            Currency: core.String("CNY"),
        },
    })
    if err != nil {
        return "", err
    }
    return *resp.CodeUrl, nil
}
```

---

## H5 支付

```go
import (
    "github.com/wechatpay-apiv3/wechatpay-go/core"
    "github.com/wechatpay-apiv3/wechatpay-go/services/payments/h5"
)

func CreateH5Order(ctx context.Context, outTradeNo, description, notifyURL, clientIP string, totalFen int64) (string, error) {
    svc := h5.H5ApiService{Client: Client}
    resp, _, err := svc.Prepay(ctx, h5.PrepayRequest{
        Appid:       core.String("YOUR_APPID"),
        Mchid:       core.String("YOUR_MCHID"),
        Description: core.String(description),
        OutTradeNo:  core.String(outTradeNo),
        NotifyUrl:   core.String(notifyURL),
        Amount: &h5.Amount{
            Total: core.Int64(totalFen),
        },
        SceneInfo: &h5.SceneInfo{
            PayerClientIp: core.String(clientIP),
            H5Info: &h5.H5Info{
                Type: core.String("Wap"),
            },
        },
    })
    if err != nil {
        return "", err
    }
    return *resp.H5Url, nil
}
```

---

## 查询订单

```go
import "github.com/wechatpay-apiv3/wechatpay-go/services/payments/native"

func QueryOrderByOutTradeNo(ctx context.Context, outTradeNo string) (*payments.Transaction, error) {
    svc := native.NativeApiService{Client: Client}
    resp, _, err := svc.QueryOrderByOutTradeNo(ctx, native.QueryOrderByOutTradeNoRequest{
        OutTradeNo: core.String(outTradeNo),
        Mchid:      core.String("YOUR_MCHID"),
    })
    return resp, err
}
```

---

## 回调通知处理

```go
import (
    "github.com/wechatpay-apiv3/wechatpay-go/core/downloader"
    "github.com/wechatpay-apiv3/wechatpay-go/core/notify"
    "github.com/wechatpay-apiv3/wechatpay-go/core/auth/verifiers"
    "github.com/wechatpay-apiv3/wechatpay-go/services/payments"
)

func InitNotifyHandler(mchID, mchAPIV3Key string) (*notify.Handler, error) {
    ctx := context.Background()
    // 需要先注册 CertificateDownloader
    err := downloader.MgrInstance().RegisterDownloaderWithPrivateKey(
        ctx,
        privateKey,        // *rsa.PrivateKey
        mchCertSerial,    // 证书序列号
        mchID,
        mchAPIV3Key,
    )
    if err != nil {
        return nil, err
    }
    certVisitor := downloader.MgrInstance().GetCertificateVisitor(mchID)
    handler := notify.NewNotifyHandler(
        mchAPIV3Key,
        verifiers.NewSHA256WithRSAVerifier(certVisitor),
    )
    return handler, nil
}

// 在 HTTP 路由中使用
func HandleNotify(handler *notify.Handler, w http.ResponseWriter, r *http.Request) {
    transaction := new(payments.Transaction)
    _, err := handler.ParseNotifyRequest(r.Context(), r, transaction)
    if err != nil {
        w.WriteHeader(http.StatusInternalServerError)
        return
    }
    // 处理支付结果...
    w.WriteHeader(http.StatusOK)
    w.Write([]byte("{}"))
}
```

---

## 错误处理

```go
import "github.com/wechatpay-apiv3/wechatpay-go/core"

_, result, err := svc.Prepay(ctx, req)
if err != nil {
    if core.IsAPIError(err, "OUT_TRADE_NO_USED") {
        // 订单号重复
    } else if core.IsAPIError(err, "SYSTEM_ERROR") {
        // 系统错误，可重试
    }
    // result.Response 包含原始 HTTP 响应
    log.Printf("HTTP %d: %v", result.Response.StatusCode, err)
}
```

---

## 工具函数

```go
import "github.com/wechatpay-apiv3/wechatpay-go/utils"

// 从文件加载私钥
key, err := utils.LoadPrivateKeyWithPath("/path/to/apiclient_key.pem")

// 从 PEM 字符串加载
key, err := utils.LoadPrivateKey(pemString)

// 生成随机串
nonce := utils.GenerateNonce()  // 若 SDK 提供

// 时间戳
timestamp := strconv.FormatInt(time.Now().Unix(), 10)
```

---

## 包结构参考

```
github.com/wechatpay-apiv3/wechatpay-go/
├── core/                  # Client、选项、错误
│   ├── option/            # WithWechatPayAutoAuthCipher 等选项
│   ├── auth/verifiers/    # SHA256WithRSAVerifier
│   ├── downloader/        # 平台证书下载器
│   └── notify/            # 回调通知处理
├── services/
│   └── payments/
│       ├── native/        # NativeApiService（扫码支付）
│       ├── h5/            # H5ApiService（H5 支付）
│       └── jsapi/         # JsapiApiService（JSAPI 支付）
└── utils/                 # 密钥加载等工具
```

---

## 本项目接入配置示例

本项目不通过仓库 YAML 组装 SDK。管理员在「系统设置 → 集成 → 支付 → 微信」维护并验证 AppID、商户号、
API v3 key、证书序列号和商户私钥；主站进程内 provider manager 从现有 options 配置初始化 SDK。数据库和备份必须限制访问。

异步通知固定为：

```text
https://your-domain.com/api/pay/wechat/notify
```

完整上线步骤见 [`deploy-real-payments.md`](deploy-real-payments.md)。

---

## 注意事项

1. **不要将 `Client` 作为全局变量在并发中裸写**：SDK Client 是并发安全的，可以共享。
2. **私钥文件权限**：`chmod 600 apiclient_key.pem`，确保只有运行用户可读。
3. **平台证书自动更新**：`WithWechatPayAutoAuthCipher` 会自动下载和更新平台证书，无需手动处理。
4. **Gin/Echo 集成**：`handler.ParseNotifyRequest` 接受标准 `*http.Request`，与任何框架兼容。
