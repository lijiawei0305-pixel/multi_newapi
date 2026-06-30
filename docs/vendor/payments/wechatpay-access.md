# 微信支付接入准备

- **Source URL**: https://pay.weixin.qq.com/doc/global/v3/zh/4012354063
- **整理时间**: 2026-06-30
- **Purpose**: 微信支付商户接入前置条件、材料清单和资质申请流程。

> ⚠️ **部分需人工补充**：全局开发者中心页面已抓取到 API 列表；商户国内版详细接入步骤请对照 https://pay.wechatpay.cn/doc/v3/merchant/4012080400 人工核对和补充。

---

## 接入前提条件

### 商户资质

| 资质 | 说明 |
|------|------|
| 营业执照 | 有效的企业/个体工商户营业执照 |
| 法人身份证 | 法定代表人二代身份证 |
| 银行账户 | 对公银行账户（企业主体必须）或个人银行卡 |
| 微信公众号/开放平台 | 需与商户号绑定的 AppID |

### 产品权限申请

| 支付产品 | 申请位置 | 说明 |
|----------|----------|------|
| Native 扫码支付 | 商户平台 → 产品中心 | 本项目主要使用 |
| H5 支付 | 商户平台 → 产品中心 | 需额外申请，需配置 H5 支付域名 |
| JSAPI 支付 | 商户平台 → 产品中心 | 微信内网页支付，可选 |

---

## 必须准备的凭据

### 1. 商户号（mchid）

- 格式：10 位纯数字，如 `1230000109`
- 来源：微信支付商户平台注册成功后分配

### 2. AppID

- 格式：`wx` 开头的字符串，如 `wxd678efh567hg6787`
- 来源：微信公众平台或开放平台
- 需在商户平台绑定 AppID 与商户号

### 3. 商户 API 证书

包含两个文件：
- `apiclient_cert.pem`：公钥证书（含证书序列号）
- `apiclient_key.pem`：私钥（**必须妥善保管，绝不入库**）

下载位置：商户平台 → 账户中心 → API 安全 → 申请/下载 API 证书

提取证书序列号：

```bash
openssl x509 -in apiclient_cert.pem -noout -serial
# 输出类似：serial=1DDE55AD98ED71D6EDD4A4A16996DE7B8F0A
```

### 4. APIv3 密钥

- 格式：32 字节字符串（字母 + 数字）
- 用途：AES-256-GCM 解密回调通知
- 设置位置：商户平台 → 账户中心 → API 安全 → 设置 APIv3 密钥

### 5. 微信支付公钥（推荐）

- 格式：RSA 公钥 PEM
- 用途：验证微信支付响应和回调签名
- 获取方式：商户平台下载，或使用 SDK 自动管理（平台证书模式）

---

## API 能力列表（Native 支付）

| API | 说明 |
|-----|------|
| Native 下单 | 创建扫码支付订单，获取 `code_url` |
| 订单查询 | 按商户订单号或微信流水号查询 |
| 申请退款 | 发起退款请求 |
| 退款查询 | 查询退款状态 |
| 关闭订单 | 取消未支付订单 |
| 支付成功通知 | 微信主动推送支付结果 |
| 退款结果通知 | 微信主动推送退款结果 |
| 申请账单 | 获取交易/资金账单 |
| 下载账单 | 按日期下载账单文件 |
| 平台证书下载 | 获取微信支付平台证书 |

---

## 接入步骤概览

```
1. 注册商户 → 商户号
2. 绑定 AppID → appid + mchid 绑定
3. 申请产品权限 → Native 支付、H5 支付
4. 下载 API 证书 → apiclient_key.pem / apiclient_cert.pem
5. 设置 APIv3 密钥
6. 配置 IP 白名单（如有要求）
7. 配置回调域名（notify_url 域名）
8. 集成 SDK → 参见 wechatpay-go-sdk.md
9. 沙箱联调 → 真实小额验收
```

---

## 商户平台地址

| 环境 | 地址 |
|------|------|
| 商户平台（正式） | https://pay.weixin.qq.com |
| API 接口（正式） | https://api.mch.weixin.qq.com |
| API 接口（备用） | https://api2.mch.weixin.qq.com |

---

## 本项目接入配置

填入 `auth-service/config.yaml`：

```yaml
wxpay:
  mock: false
  app_id: ""           # 待填：微信公众号/开放平台 AppID
  mch_id: ""           # 待填：商户号
  api_v3_key: ""       # 待填：APIv3 密钥
  cert_serial: ""      # 待填：证书序列号
  private_key_path: "" # 待填：apiclient_key.pem 在服务器上的绝对路径
  notify_url: "https://your-domain.com/api/payment/wechat/notify"
```

---

## 注意事项

1. **私钥不入库**：`apiclient_key.pem` 通过 scp 或 CI Secret 上传到服务器，路径通过配置文件引用。
2. **IP 白名单**：部分商户需要在平台配置调用 API 的服务器 IP，确认是否需要。
3. **证书有效期**：商户 API 证书有效期通常 5 年，到期前需更新。
4. **权限开通**：Native 支付和 H5 支付需分别申请，审核通过后才能调用对应接口。
