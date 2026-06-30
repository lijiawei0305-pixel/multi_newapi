# 微信支付 Native 下单（扫码支付）

- **Source URL**: https://pay.wechatpay.cn/doc/v3/merchant/4012791877
- **整理时间**: 2026-06-30
- **Purpose**: Native 扫码支付下单接口——服务端调用后获取 `code_url`，前端渲染为二维码供用户扫描。

---

## 接口规格

| 项 | 值 |
|----|----|
| 请求方式 | POST |
| 接口路径 | `/v3/pay/transactions/native` |
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
| `appid` | 是 | string | 32 | 微信开放平台或公众号 AppID |
| `mchid` | 是 | string | 32 | 微信支付分配的商户号 |
| `description` | 是 | string | 127 | 商品描述，显示在用户微信账单 |
| `out_trade_no` | 是 | string | 32 | 商户内部订单号，6-32 位，字母数字 `-_\|*` |
| `notify_url` | 是 | string | 255 | 支付成功后微信推送通知的回调 URL（HTTPS） |
| `time_expire` | 否 | string | 64 | 支付截止时间，RFC3339 格式（如 `2025-01-01T10:00:00+08:00`） |
| `attach` | 否 | string | 128 | 商户自定义数据，会在回调中原样返回 |
| `goods_tag` | 否 | string | 32 | 订单优惠标记（优惠券使用） |
| `support_fapiao` | 否 | boolean | - | 是否开启电子发票入口，`true`/`false` |

### amount 对象（必选）

| 参数 | 必选 | 类型 | 说明 |
|------|------|------|------|
| `total` | 是 | integer | 订单总金额，单位：分，必须 > 0 |
| `currency` | 否 | string | 货币类型，固定 `CNY`（人民币） |

### detail 对象（可选）

| 参数 | 必选 | 类型 | 说明 |
|------|------|------|------|
| `cost_price` | 否 | integer | 原始商品价格（分） |
| `invoice_id` | 否 | string | 商户小票 ID |
| `goods_detail` | 否 | array | 商品列表（至少 1 项） |
| `goods_detail[].merchant_goods_id` | 是 | string | 商户商品编码 |
| `goods_detail[].wechatpay_goods_id` | 否 | string | 微信侧商品编码 |
| `goods_detail[].goods_name` | 否 | string | 商品名称 |
| `goods_detail[].quantity` | 是 | integer | 购买数量 |
| `goods_detail[].unit_price` | 是 | integer | 单价（分） |

### scene_info 对象（可选）

| 参数 | 必选 | 类型 | 说明 |
|------|------|------|------|
| `payer_client_ip` | 否 | string | 用户客户端 IP（IPv4/IPv6） |
| `device_id` | 否 | string | 商户设备 ID |
| `store_info.id` | 否 | string | 门店 ID |
| `store_info.name` | 否 | string | 门店名称 |
| `store_info.area_code` | 否 | string | 省市编码 |
| `store_info.address` | 否 | string | 详细地址 |

### settle_info 对象（可选）

| 参数 | 必选 | 类型 | 说明 |
|------|------|------|------|
| `profit_sharing` | 否 | boolean | `true` 表示需要分账，资金冻结至分账或 30 天后自动解冻 |

---

## 请求示例

```bash
curl -X POST \
  https://api.mch.weixin.qq.com/v3/pay/transactions/native \
  -H "Authorization: WECHATPAY2-SHA256-RSA2048 mchid=\"1900000001\",..." \
  -H "Accept: application/json" \
  -H "Content-Type: application/json" \
  -d '{
    "appid": "wxd678efh567hg6787",
    "mchid": "1230000109",
    "description": "充值套餐-月度版",
    "out_trade_no": "ORDER20240101120000001",
    "time_expire": "2024-01-01T13:00:00+08:00",
    "attach": "user_id=123&plan_id=456",
    "notify_url": "https://your-domain.com/api/payment/wechat/notify",
    "amount": {
      "total": 2980,
      "currency": "CNY"
    }
  }'
```

---

## 响应参数

### 成功响应（HTTP 200）

```json
{
  "code_url": "weixin://wxpay/bizpayurl/up?pr=NwY5Mz9&groupid=00"
}
```

| 参数 | 类型 | 说明 |
|------|------|------|
| `code_url` | string | 二维码链接，有效期 **2 小时**；过期需重新调用接口 |

### 错误响应格式

```json
{
  "code": "PARAM_ERROR",
  "message": "out_trade_no格式错误",
  "detail": { "field": "/out_trade_no", "issue": "格式不合规" }
}
```

---

## 错误码

### 通用错误

| HTTP 状态 | code | 说明 | 处理 |
|-----------|------|------|------|
| 400 | `PARAM_ERROR` | 参数错误 | 检查参数 |
| 400 | `INVALID_REQUEST` | 请求不合规 | 查看接口规则 |
| 401 | `SIGN_ERROR` | 签名失败 | 检查签名流程 |
| 500 | `SYSTEM_ERROR` | 系统异常 | 用相同参数重试 |

### 业务错误

| HTTP 状态 | code | 说明 | 处理 |
|-----------|------|------|------|
| 400 | `APPID_MCHID_NOT_MATCH` | AppID 与商户号不匹配 | 核实绑定关系 |
| 400 | `MCH_NOT_EXISTS` | 商户号不存在 | 核实商户号 |
| 400 | `ORDER_CLOSED` | 订单已关闭 | 重新下单 |
| 403 | `NO_AUTH` | 权限不足 | 在商户平台申请权限 |
| 403 | `OUT_TRADE_NO_USED` | 订单号重复 | 检查是否重复提交 |
| 429 | `FREQUENCY_LIMITED` | 频率超限 | 降低请求频率 |

---

## 关键实现注意事项

1. **有效期**：不传 `time_expire` 默认 7 天，最长 15 天；`code_url` 本身有效期只有 2 小时。
2. **金额单位**：全部使用分（整数），最小值 1 分。
3. **`out_trade_no` 唯一性**：同一商户号下不可重复；重复会返回 `OUT_TRADE_NO_USED`。
4. **回调地址**：`notify_url` 必须是 HTTPS，且公网可达。
5. **订单查询兜底**：不要仅依赖回调确认，需实现主动查单轮询作为兜底。
6. **分账场景**：若套餐需要佣金分账，设置 `settle_info.profit_sharing: true`。
7. **`attach` 字段**：推荐携带业务标识（如 `user_id`/`plan_id`），回调原样返回，便于业务处理。

---

## Go SDK 调用示例

```go
import (
    "github.com/wechatpay-apiv3/wechatpay-go/core"
    "github.com/wechatpay-apiv3/wechatpay-go/services/payments/native"
)

svc := native.NativeApiService{Client: client}
resp, result, err := svc.Prepay(ctx, native.PrepayRequest{
    Appid:       core.String("wxd678efh567hg6787"),
    Mchid:       core.String("1230000109"),
    Description: core.String("充值套餐-月度版"),
    OutTradeNo:  core.String("ORDER20240101120000001"),
    NotifyUrl:   core.String("https://your-domain.com/api/payment/wechat/notify"),
    Amount: &native.Amount{
        Total:    core.Int64(2980),
        Currency: core.String("CNY"),
    },
})
if err != nil {
    // 处理错误
}
codeUrl := resp.CodeUrl // 用于生成二维码
```
