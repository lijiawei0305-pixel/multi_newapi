# 微信支付 H5 下单

- **Source URL（下单接口）**: https://pay.wechatpay.cn/doc/v3/merchant/4012791834
- **Source URL（调起支付）**: https://pay.wechatpay.cn/doc/v3/merchant/4012791835
- **整理时间**: 2026-06-30
- **Purpose**: H5 支付下单接口——在非微信浏览器的手机端网页中唤起微信支付。

---

## 接口规格（H5 下单）

| 项 | 值 |
|----|----|
| 请求方式 | POST |
| 接口路径 | `/v3/pay/transactions/h5` |
| 主域 | `https://api.mch.weixin.qq.com` |
| 备域 | `https://api2.mch.weixin.qq.com` |
| 适用商户 | 普通商户 |

---

## 请求 Headers

| Header | 必选 | 类型 | 说明 |
|--------|------|------|------|
| `Authorization` | 是 | string | 按签名规范构造的鉴权信息 |
| `Accept` | 是 | string | `application/json` |
| `Content-Type` | 是 | string | `application/json` |

---

## 请求体参数

### 核心字段

| 参数 | 必选 | 类型 | 最大长度 | 说明 |
|------|------|------|---------|------|
| `appid` | 是 | string | 32 | 微信分配的 AppID |
| `mchid` | 是 | string | 32 | 商户号 |
| `description` | 是 | string | 127 | 商品描述 |
| `out_trade_no` | 是 | string | 32 | 商户内部订单号 |
| `notify_url` | 是 | string | 255 | 支付成功回调地址（HTTPS） |
| `time_expire` | 否 | string | 64 | 支付截止时间（RFC3339） |
| `attach` | 否 | string | 128 | 商户自定义数据 |
| `goods_tag` | 否 | string | 32 | 订单优惠标记 |
| `support_fapiao` | 否 | boolean | - | 是否开启电子发票 |

### amount 对象（必选）

| 参数 | 必选 | 类型 | 说明 |
|------|------|------|------|
| `total` | 是 | integer | 金额（分），必须 > 0 |
| `currency` | 否 | string | 固定 `CNY` |

### scene_info 对象（**必选**，H5 支付特有）

| 参数 | 必选 | 类型 | 最大长度 | 说明 |
|------|------|------|---------|------|
| `payer_client_ip` | **是** | string | 45 | 用户端 IP（IPv4/IPv6），H5 必传 |
| `device_id` | 否 | string | 32 | 商户设备 ID |
| `store_info.id` | 否 | string | 32 | 门店 ID |

### h5_info 对象（**必选**，H5 支付特有）

| 参数 | 必选 | 类型 | 最大长度 | 说明 |
|------|------|------|---------|------|
| `type` | **是** | string | 32 | 场景类型：`Wap`、`iOS`、`Android` |
| `app_name` | 否 | string | 64 | 应用名称 |
| `app_url` | 否 | string | 128 | 网站 URL |
| `bundle_id` | 否 | string | 128 | iOS BundleID |
| `package_name` | 否 | string | 128 | Android PackageName |

---

## 响应参数

### 成功响应（HTTP 200）

```json
{
  "h5_url": "https://wx.tenpay.com/cgi-bin/mmpayweb-bin/checkmweb?prepay_id=wx...&package=..."
}
```

| 参数 | 类型 | 最大长度 | 说明 |
|------|------|---------|------|
| `h5_url` | string | 256 | 唤起微信支付的链接，**有效期 5 分钟** |

---

## 调起支付流程

### 步骤

1. 调用 H5 下单接口获取 `h5_url`
2. 在**已配置 H5 支付域名**的网页中跳转 `h5_url`
3. 微信完成安全校验，用户支付
4. 用户返回商户页面（可附带 `redirect_url`）

### redirect_url 配置

在 `h5_url` 后拼接 `redirect_url` 参数（需 URL 编码）：

```
https://wx.tenpay.com/cgi-bin/mmpayweb-bin/checkmweb?prepay_id=xxx&package=xxx&redirect_url=https%3A%2F%2Fyour-domain.com%2Forder%2Fstatus
```

**约束**：
- `redirect_url` 的域名**必须**是商户平台中配置的 H5 支付域名
- 用户支付完成或取消后跳转到该页面

---

## 错误码

### 通用错误

| HTTP 状态 | code | 说明 | 处理 |
|-----------|------|------|------|
| 400 | `PARAM_ERROR` | 参数错误 | 检查参数 |
| 400 | `INVALID_REQUEST` | 请求不合规 | 查看接口规则 |
| 401 | `SIGN_ERROR` | 签名失败 | 检查签名 |
| 500 | `SYSTEM_ERROR` | 系统异常 | 重试 |

### 业务错误

| HTTP 状态 | code | 说明 | 处理 |
|-----------|------|------|------|
| 400 | `APPID_MCHID_NOT_MATCH` | AppID 与商户号不匹配 | 核实绑定 |
| 400 | `MCH_NOT_EXISTS` | 商户号不存在 | 核实商户号 |
| 403 | `NO_AUTH` | 权限不足 | 申请权限 |
| 403 | `OUT_TRADE_NO_USED` | 订单号重复 | 检查重复 |
| 403 | `RULE_LIMIT` | 业务规则限制 | 查看响应详情 |
| 429 | `FREQUENCY_LIMITED` | 频率超限 | 降低频率 |

---

## 关键实现注意事项

1. **H5 支付域名**：需在微信商户平台「产品中心→H5 支付→申请」中配置，`redirect_url` 域名必须与之一致。
2. **`payer_client_ip` 必填**：H5 下单时必须传用户端真实 IP，不能传服务端 IP。
3. **h5_url 有效期**：仅 5 分钟，过期须重新下单。
4. **不可修改 h5_url**：不能截断、拆分或修改参数，只能在末尾追加 `redirect_url`。
5. **支付环境**：H5 支付在微信 App 内无法使用，只能在外部浏览器中唤起。
6. **回调同 Native**：支付结果通知机制与 Native 完全一致，参见 `wechatpay-notify-decrypt.md`。
7. **本项目应用**：本项目主要使用 Native 扫码支付；H5 支付作为备选，需额外申请权限。

---

## Go SDK 调用示例

```go
import (
    "github.com/wechatpay-apiv3/wechatpay-go/core"
    "github.com/wechatpay-apiv3/wechatpay-go/services/payments/h5"
)

svc := h5.H5ApiService{Client: client}
resp, result, err := svc.Prepay(ctx, h5.PrepayRequest{
    Appid:       core.String("wxd678efh567hg6787"),
    Mchid:       core.String("1230000109"),
    Description: core.String("充值套餐-月度版"),
    OutTradeNo:  core.String("ORDER20240101120000001"),
    NotifyUrl:   core.String("https://your-domain.com/api/payment/wechat/notify"),
    Amount: &h5.Amount{
        Total: core.Int64(2980),
    },
    SceneInfo: &h5.SceneInfo{
        PayerClientIp: core.String("14.23.150.211"),
        H5Info: &h5.H5Info{
            Type: core.String("Wap"),
        },
    },
})
if err != nil {
    // 处理错误
}
h5Url := resp.H5Url
```
