# 支付宝电脑网站支付 — 异步通知参数

- **Source URL**: https://opendocs.alipay.com/open/270/105902
- **整理时间**: 2026-06-30（官方原文已复制）
- **Purpose**: PC 网站支付结果异步通知（Webhook）完整参数说明、交易状态、验签规则。

---

## 通知触发

对于 PC 网站支付交易，用户支付完成后，支付宝通过 **POST 请求**将支付结果推送到 `notify_url`。

> 同时需接入 `alipay.trade.query` 查询接口：当通知丢失时，必须主动查询订单结果。

---

## 公共参数

| 参数 | 类型 | 必选 | 最大长度 | 说明 | 示例值 |
|------|------|------|---------|------|--------|
| `notify_time` | Date | 是 | - | 通知发送时间，`yyyy-MM-dd HH:mm:ss` | `2018-10-21 15:45:22` |
| `notify_type` | String | 是 | 64 | 通知类型 | `trade_status_sync` |
| `notify_id` | String | 是 | 128 | 通知校验 ID | `ac05099524730693a8b330c45cf72da943` |
| `charset` | String | 是 | 10 | 编码，如 `utf-8` | `utf-8` |
| `version` | String | 是 | 3 | 接口版本，固定 `1.0` | `1.0` |
| `sign_type` | String | 是 | 10 | 签名算法，推荐 `RSA2` | `RSA2` |
| `sign` | String | 是 | 344 | 签名值 | `601510b7970e52cc...` |
| `auth_app_id` | String | 是 | 32 | 授权方 APPID；直连模式 = `app_id` | `2018072300007418` |

---

## 业务参数

| 参数 | 类型 | 必选 | 最大长度 | 说明 | 示例值 |
|------|------|------|---------|------|--------|
| `trade_no` | String | 是 | 64 | **支付宝交易号**（用于对账和退款） | `2013112011001004330000121536` |
| `app_id` | String | 是 | 32 | 支付宝应用 APPID | `2019082200007148` |
| `out_trade_no` | String | 是 | 64 | **商户订单号**（用于幂等查找） | `6823789339978248` |
| `out_biz_no` | String | 否 | 64 | 商户业务号（退款通知中为退款申请流水号） | `HZRF001` |
| `buyer_id` | String | 否 | 128 | 买家支付宝 UID（新商户建议用 `buyer_open_id`） | - |
| `seller_id` | String | 否 | 30 | 卖家支付宝账号 UID，以 2088 开头 | `20881***2239364` |
| `trade_status` | String | 否 | 32 | **交易状态**（见下表） | `TRADE_SUCCESS` |
| `total_amount` | Number | 否 | 11 | 订单总金额，单位：**元**，精确到小数点后 2 位 | `20.00` |
| `receipt_amount` | Number | 否 | 11 | 商家实收金额（元） | `15.00` |
| `invoice_amount` | Number | 否 | 11 | 开票金额（元） | `13.88` |
| `buyer_pay_amount` | Number | 否 | 11 | 用户实付金额（元，含优惠后） | `12.00` |
| `point_amount` | Number | 否 | 11 | 集分宝支付金额（元） | `0.00` |
| `refund_fee` | Number | 否 | 11 | 总退款金额（退款通知中返回） | `2.58` |
| `subject` | String | 否 | 256 | 商品标题（与下单时一致） | `XXXX交易` |
| `body` | String | 否 | 400 | 商品描述 | `XXX交易内容` |
| `gmt_create` | Date | 否 | - | 交易创建时间，`yyyy-MM-dd HH:mm:ss` | `2018-08-25 15:34:42` |
| `gmt_payment` | Date | 否 | - | 交易付款时间 | `2018-08-25 15:34:42` |
| `gmt_refund` | Date | 否 | - | 交易退款时间，`yyyy-MM-dd HH:mm:ss.S` | `2018-08-26 10:34:44.340` |
| `gmt_close` | Date | 否 | - | 交易结束时间 | `2018-08-26 16:32:30` |
| `fund_bill_list` | String | 否 | 512 | 支付成功各渠道金额信息（JSON） | `[{"amount":"15.00","fundChannel":"ALIPAYACCOUNT"}]` |
| `voucher_detail_list` | String | 否 | 512 | 优惠券信息（JSON，字段名为 `vocher_detail_list`） | - |
| `passback_params` | String | 否 | 512 | 公用回传参数（URL 编码，原样返回） | `merchantBizType%3d3C...` |

---

## 交易状态（trade_status）

| 状态值 | 说明 | 默认触发通知 | 业务处理 |
|--------|------|------------|----------|
| `WAIT_BUYER_PAY` | 交易创建，等待买家付款 | 否 | 不处理 |
| `TRADE_CLOSED` | 未付款超时关闭，或全额退款后关闭 | 否 | 可记录 |
| `TRADE_SUCCESS` | **交易支付成功** | **是** | **核心处理** |
| `TRADE_FINISHED` | 交易结束，不可退款 | 否 | 可记录 |

---

## 资金明细（fund_bill_list 字段）

| 参数 | 类型 | 说明 |
|------|------|------|
| `fundChannel` | String | 支付渠道（如 `ALIPAYACCOUNT`、`MDISCOUNT`） |
| `amount` | String | 该渠道支付金额（元） |

---

## 优惠券信息（voucher_detail_list 字段）

| 参数 | 类型 | 说明 |
|------|------|------|
| `voucherId` | String | 券 ID |
| `name` | String | 券名称 |
| `type` | String | 优惠类型（`ALIPAY_BIZ_VOUCHER` 等） |
| `amount` | Number | 优惠金额（元） |
| `merchantContribute` | Number | 商家出资金额（元） |
| `otherContribute` | Number | 其他出资方金额（元） |
| `memo` | String | 优惠券备注 |

---

## 通知特性

1. **重试策略**：支付宝未收到 `success` 会重发，间隔 `4m / 10m / 10m / 1h / 2h / 6h / 15h`，总计约 25 小时。
2. **notify_id 不变**：同一条通知重试时 `notify_id` 不变（可用于幂等判断）。
3. **`notify_url` 要求**：
   - 公网可访问，无重定向
   - 页面无任何多余字符（空格、HTML 标签）
   - 支付宝 POST 方式发送，`application/x-www-form-urlencoded`
   - Session / Cookie 在此页面失效，无法获取
4. **必须打印 `success`**：处理完成后必须输出纯文本 `success`，否则支付宝判定失败并重试。

---

## 验签步骤

**步骤一**：除去 `sign`、`sign_type` 参数外，所有通知返回参数均参与验签。

**步骤二**：剩余参数进行 URLDecode，然后**字典排序**，拼接为 `key=value&key=value` 字符串。

**步骤三**：对 `sign` 参数执行 Base64 解码。

**步骤四**：使用**支付宝公钥**对签名字符串执行 RSA2（SHA256WithRSA）验签。

**步骤五**：验签通过后，核对以下字段：
- `app_id` 与本应用一致
- `out_trade_no` 存在于本地订单库
- `total_amount` 与本地订单金额一致
- `seller_id` 为本商户账号

以上任意一项不通过，忽略本次通知。

---

## 通知示例（URL 解码后）

```
trade_no=2016101221001004580200203978
out_trade_no=mobile_rdm862016-10-12213600
app_id=2016092101248425
trade_status=TRADE_SUCCESS
total_amount=1.00
receipt_amount=0.80
buyer_pay_amount=0.80
subject=PC网站支付交易
gmt_payment=2016-10-12 21:37:19
notify_time=2016-10-12 21:41:23
notify_type=trade_status_sync
notify_id=7676a2e1e4e737cff30015c4b7b55e3kh6
sign_type=RSA2
sign=***
passback_params=passback_params123
```

---

## Go 处理代码

```go
func alipayNotifyHandler(client *alipay.Client) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        r.ParseForm()
        
        // 1. 验签
        ok, err := client.VerifySign(r.Form)
        if err != nil || !ok {
            log.Printf("alipay notify verify failed: %v", err)
            w.Write([]byte("fail"))
            return
        }
        
        // 2. 提取关键字段
        appID := r.FormValue("app_id")
        outTradeNo := r.FormValue("out_trade_no")
        tradeNo := r.FormValue("trade_no")
        tradeStatus := r.FormValue("trade_status")
        totalAmount := r.FormValue("total_amount")
        passback, _ := url.QueryUnescape(r.FormValue("passback_params"))
        
        // 3. 安全校验
        if appID != "YOUR_APP_ID" {
            w.Write([]byte("fail"))
            return
        }
        
        // 4. 业务处理（仅处理 TRADE_SUCCESS）
        if tradeStatus == "TRADE_SUCCESS" {
            // 幂等：检查 outTradeNo 是否已处理
            // 金额验证：totalAmount 与本地订单金额一致
            // 激活订阅、充值余额等业务操作
            // 记录 tradeNo 用于对账
            _ = tradeNo
            _ = totalAmount
            _ = passback
        }
        
        // 5. 返回 success
        w.Write([]byte("success"))
    }
}
```

---

## 关键注意事项

1. **`passback_params` 是 URL 编码的**：需 `url.QueryUnescape()` 解码后再解析 `user_id`/`plan_id`。
2. **`total_amount` 是字符串**：用字符串比较或解析为 `decimal` 比对，避免浮点问题。
3. **幂等**：同一 `out_trade_no` 可能收到多次通知，以第一次为准，后续直接返回 `success`。
4. **主动查单兜底**：25 小时后若仍未收到通知，调用 `alipay.trade.query` 确认结果。
5. **事务原子性**：订单状态更新和业务激活（如套餐激活）应在同一数据库事务中完成。
