# 支付宝异步通知参数说明

- **Source URL**: https://opendocs.alipay.com/open/04ij9t
- **整理时间**: 2026-06-30（官方原文已复制）
- **Purpose**: 支付宝异步通知验签算法（通用），以及资金预授权场景的通知参数参考。

> **注意**：官方此页面（04ij9t）的通知参数示例为**资金预授权**（fund_auth）场景。
> **PC 网站支付（trade）的通知参数见** `alipay-page-pay-notify.md`。
> 验签算法对所有场景通用，见下文第二节。

---

## 一、通知基础说明

异步通知是指一笔订单支付完成后，支付宝将订单变更信息，沿商家调用支付请求时传入的 `notify_url`，通过 **POST 请求**的形式将支付结果作为参数通知到商家系统。

- HTTP 状态码 **200** = 异步通知成功
- HTTP 状态码 **404 / 500** = 服务器内部错误，需商家排查
- 重试策略：`4m / 10m / 10m / 1h / 2h / 6h / 15h`（详见 `alipay-common-notify.md`）

---

## 二、验签算法（通用，所有通知场景适用）

### 步骤

1. 取出通知参数中的 `sign` 值（Base64 编码的签名）
2. **移除** `sign` 和 `sign_type` 参数
3. 其余参数**过滤空值**
4. 对参数值执行 **URLDecode**
5. 按参数名 **ASCII 升序**排列，拼接为 `key=value&key=value` 字符串
6. 对 `sign` 执行 Base64 解码，得到签名字节串
7. 用**支付宝公钥**执行 SHA256WithRSA（RSA2）验签

### 拼接示例

```
amount=99.00&app_id=2021002110681111&auth_no=2021120610002001030501221111&charset=utf-8&credit_amount=99.00&fund_amount=0.00&gmt_create=2021-12-06 23:59:55&gmt_trans=2021-12-06 23:59:59&notify_id=2021120700222000000090241427601111&notify_time=2021-12-07 00:00:00&notify_type=fund_auth_freeze&operation_id=20211206696407711111&operation_type=FREEZE&out_order_no=2107811467886528557601111&out_request_no=210781146788652855760491111&...&version=1.0
```

### Go 实现

```go
import "github.com/smartwalle/alipay/v3"

// 使用 SDK（推荐）
ok, err := client.VerifySign(r.Form)

// 手动实现
func verifyAlipaySign(params url.Values, alipayPublicKey *rsa.PublicKey) error {
    sign := params.Get("sign")
    sig, err := base64.StdEncoding.DecodeString(sign)
    if err != nil {
        return err
    }
    
    keys := make([]string, 0)
    for k, v := range params {
        if k == "sign" || k == "sign_type" || (len(v) > 0 && v[0] == "") {
            continue
        }
        keys = append(keys, k)
    }
    sort.Strings(keys)
    
    parts := make([]string, 0, len(keys))
    for _, k := range keys {
        parts = append(parts, k+"="+params.Get(k))
    }
    message := strings.Join(parts, "&")
    
    h := sha256.New()
    h.Write([]byte(message))
    digest := h.Sum(nil)
    return rsa.VerifyPKCS1v15(alipayPublicKey, crypto.SHA256, digest, sig)
}
```

### 验签后必须校验

```go
// 通用校验（所有通知场景）
if r.FormValue("app_id") != YOUR_APP_ID {
    // 非本应用通知，忽略
}

// 金额校验（PC 支付场景）
if r.FormValue("total_amount") != expectedAmount {
    // 金额不符，可能是篡改攻击
}
```

---

## 三、资金预授权通知参数（fund_auth 场景，仅供参考）

> 本项目不使用预授权功能，以下参数仅作参考。PC 网站支付参数见 `alipay-page-pay-notify.md`。

### 接口与通知对应

| 接口 | 通知类型 | 默认开启 |
|------|----------|----------|
| `alipay.fund.auth.order.freeze` | `fund_auth_freeze` | 是 |
| `alipay.fund.auth.order.unfreeze` | `fund_auth_unfreeze` | 是 |
| `alipay.fund.auth.operation.cancel` | `fund_auth_operation_cancel` | 是 |

### 预授权通知参数表

| 参数 | 类型 | 必填 | 最大长度 | 说明 |
|------|------|------|---------|------|
| `auth_no` | String | 必须 | 64 | 支付宝资金授权订单号 |
| `notify_type` | String | 必须 | 64 | 通知类型（如 `fund_auth_freeze`） |
| `out_order_no` | String | 必须 | 64 | 商家资金授权订单号 |
| `operation_id` | String | 必须 | 64 | 支付宝资金操作流水号 |
| `out_request_no` | String | 必须 | 64 | 商家资金操作流水号 |
| `operation_type` | String | 必须 | 16 | 操作类型：`FREEZE`（冻结）/ `UNFREEZE`（解冻）/ `PAY`（转交易） |
| `amount` | String | 必须 | 11 | 本次操作金额（元） |
| `status` | String | 必须 | 20 | 状态：`INIT` / `SUCCESS` / `CLOSED` |
| `gmt_create` | String | 必须 | 20 | 明细创建时间（`yyyy-MM-dd HH:mm:ss`） |
| `gmt_trans` | String | 必须 | 20 | 明细处理完成时间 |
| `payer_logon_id` | String | 必须 | 100 | 付款方支付宝账号（脱敏） |
| `payer_user_id` | String | 必须 | 32 | 付款方支付宝 UID |
| `payee_logon_id` | String | 可选 | 100 | 收款方支付宝账号（脱敏） |
| `payee_user_id` | String | 可选 | 32 | 收款方支付宝 UID |
| `total_freeze_amount` | String | 必须 | 11 | 累计冻结金额（元） |
| `total_unfreeze_amount` | String | 必须 | 11 | 累计解冻金额（元） |
| `total_pay_amount` | String | 必须 | 11 | 累计支付金额（元） |
| `rest_amount` | String | 必须 | 11 | 剩余冻结金额（元） |
| `pre_auth_type` | String | 可选 | 20 | 预授权类型（`CREDIT_AUTH`=信用预授权，无真实冻结资金） |

### 预授权通知示例

```
https://merchant.com/receive_notify.htm
  ?gmt_create=2021-12-06+23%3A59%3A55
  &auth_no=2021120610002001030501221111
  &notify_type=fund_auth_freeze
  &operation_type=FREEZE
  &amount=99.00
  &status=SUCCESS
  &payer_user_id=2088122536931111
  &app_id=2021002110681111
  &sign_type=RSA2
  &sign=$$$
  ...
```

---

## 四、关联文档

| 文档 | 用途 |
|------|------|
| `alipay-page-pay-notify.md` | PC 网站支付通知参数（本项目使用） |
| `alipay-common-notify.md` | 通用通知机制（重试、响应、验签流程） |
| `alipay-sign-verify.md` | RSA2 签名算法详解 |
