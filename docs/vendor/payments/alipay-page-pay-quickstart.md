# 支付宝电脑网站支付 — 快速接入

- **Source URL**: https://opendocs.alipay.com/open/270/105899
- **整理时间**: 2026-06-30（官方原文已复制）
- **Purpose**: `alipay.trade.page.pay` 完整调用流程：支付、查单、退款、关闭交易、对账。

---

## 支付流程

### 系统时序

```
用户 → 商家: 下单
商家 → 支付宝: 调用 alipay.trade.page.pay 发起支付请求
用户 → 支付宝: 登录 + 选择支付渠道 + 输入密码 + 确认支付
支付宝 → 商家: GET 请求 return_url（同步返回，不可信）
支付宝 → 商家: POST 请求 notify_url（异步通知，权威结果）
商家 → 支付宝: 主动调用 alipay.trade.query 查单兜底
```

**重要原则**：
- 支付结果必须以**异步通知**或**查询接口**为准，不能依赖同步跳转 `return_url`
- 收到异步通知后必须**验签**，并核对 `app_id`、`out_trade_no`、`total_amount`
- 同一 `out_trade_no` 在支付宝侧唯一对应一笔单据，不可重复使用不同订单

---

## 接口规格

| 项 | 值 |
|----|----|
| 接口名 | `alipay.trade.page.pay` |
| 调用方式 | 构造 POST/GET 表单，输出 HTML 让浏览器提交 |
| 正式网关 | `https://openapi.alipay.com/gateway.do` |
| 沙箱网关 | `https://openapi-sandbox.dl.alipaydev.com/gateway.do` |

---

## 公共请求参数

| 参数 | 必选 | 说明 |
|------|------|------|
| `app_id` | 是 | 支付宝应用 ID |
| `method` | 是 | 固定：`alipay.trade.page.pay` |
| `format` | 否 | 固定：`JSON` |
| `return_url` | 否 | 同步跳转地址（支付完成后浏览器 GET 跳转） |
| `charset` | 是 | 固定：`utf-8` |
| `sign_type` | 是 | 固定：`RSA2` |
| `sign` | 是 | 请求参数签名值 |
| `timestamp` | 是 | 发送时间，格式 `yyyy-MM-dd HH:mm:ss` |
| `version` | 是 | 固定：`1.0` |
| `notify_url` | 否 | 支付结果异步通知 URL（服务端接收，POST） |
| `biz_content` | 是 | 业务参数 JSON 字符串 |

---

## 业务参数（biz_content）

### 必填参数

| 参数 | 类型 | 说明 |
|------|------|------|
| `out_trade_no` | string(64) | 商户订单号，字母/数字/下划线，同账号下唯一 |
| `product_code` | string(64) | 固定：`FAST_INSTANT_TRADE_PAY` |
| `total_amount` | string(11) | 订单总金额，单位：**元**，范围 [0.01, 100000000]，不能为 0 |
| `subject` | string(256) | 商品标题，不可含 `/=&` 等特殊字符 |

### 常用可选参数

| 参数 | 类型 | 说明 |
|------|------|------|
| `body` | string(128) | 商品描述 |
| `time_expire` | string(32) | 绝对超时时间，格式 `yyyy-MM-dd HH:mm:ss` |
| `passback_params` | string(512) | 公用回传参数，通知时原样返回，**需 URL 编码** |
| `qr_pay_mode` | string | PC 扫码支付方式：`0`=订单码展示，`1`=嵌入式（推荐），`4`=跳转码 |
| `goods_detail` | array | 商品详情列表 |

> **关键差异**：支付宝金额单位是**元**，微信支付是**分**。

---

## Go SDK 调用示例

官方暂无 Go SDK，推荐使用 `github.com/smartwalle/alipay/v3`：

```go
import (
    "net/url"
    "github.com/smartwalle/alipay/v3"
)

// 初始化（true=正式环境，false=沙箱）
client, err := alipay.New(appID, privateKey, true)
if err != nil {
    log.Fatal(err)
}
err = client.LoadAliPayPublicKey(alipayPublicKey)

// 构造支付请求
p := alipay.TradePagePay{}
p.NotifyURL = "https://your-domain.com/api/pay/alipay/notify"
p.ReturnURL = "https://your-domain.com/order/status"
p.Subject = "月度套餐"
p.OutTradeNo = "ORDER20240101120000001"
p.TotalAmount = "29.80"
p.ProductCode = "FAST_INSTANT_TRADE_PAY"
p.PassbackParams = url.QueryEscape("user_id=123&plan_id=456")

// 获取支付页面 URL（GET 方式，前端跳转）
payURL, err := client.TradePagePay(p)
// 将 payURL 返回给前端跳转

// 或获取 HTML 表单（POST 方式）
// form, err := client.TradePagePayForm(p)
```

---

## 订单查询（兜底）

```go
p := alipay.TradeQuery{OutTradeNo: "ORDER20240101120000001"}
result, err := client.TradeQuery(p)
if err == nil && result.TradeStatus == "TRADE_SUCCESS" {
    // 支付成功，激活业务
}
```

---

## 退款

```go
p := alipay.TradeRefund{}
p.OutTradeNo = "ORDER20240101120000001"
p.RefundAmount = "29.80"  // 退款金额（元）
p.RefundReason = "用户申请退款"
p.OutRequestNo = "REFUND20240101120000001"  // 退款单号，同一订单多次退款不同

result, err := client.TradeRefund(p)
```

退款规则：
- 退款周期：12 个月内
- 退款费：手续费不退
- 一笔退款失败后重新提交，**使用相同退款单号**
- 总退款金额不超过实付金额

---

## 关闭交易

```go
p := alipay.TradeClose{OutTradeNo: "ORDER20240101120000001"}
result, err := client.TradeClose(p)
```

适用场景：用户长时间未支付，主动关闭交易；成功关闭后不可再支付。

---

## 对账账单下载

```go
p := alipay.DataDataserviceBillDownloadurlQuery{
    BillType: "trade",           // 账单类型
    BillDate: "2024-01-01",      // 账单日期 yyyy-MM-dd
}
result, err := client.DataDataserviceBillDownloadurlQuery(p)
// result.BillDownloadUrl 为账单下载地址
```

---

## 同步返回参数（return_url GET 参数）

| 参数 | 说明 |
|------|------|
| `out_trade_no` | 商户订单号 |
| `trade_no` | 支付宝交易号 |
| `total_amount` | 订单金额（元） |
| `seller_id` | 收款账号 UID |
| `sign` / `sign_type` | 签名（需验签，但**不作为成功依据**） |

> 同步返回仅用于展示"处理中"页面，最终结果以异步通知为准。

---

## 验签示例（异步通知验签）

```go
// 接收到 notify 后
r.ParseForm()
ok, err := client.VerifySign(r.Form)
if err != nil || !ok {
    w.Write([]byte("fail"))
    return
}
// 验签通过，继续业务处理
```

---

## 关键注意事项

1. **产品码固定**：`product_code = "FAST_INSTANT_TRADE_PAY"`，不可变更。
2. **金额单位是元**：`total_amount = "29.80"`（字符串格式）。
3. **`passback_params` 需 URL 编码**：下单时编码，通知时原样返回，需解码后解析。
4. **付款码 2 分钟刷新一次**：页面生成的付款码每 2 分钟自动刷新。
5. **同步返回不可信**：支付结果以异步通知或主动查单为准。
6. **重复订单号**：支付宝会关联到原单据，相同 `out_trade_no` 只能对应一笔支付。
