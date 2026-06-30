# 官方支付文档索引

- **整理时间**: 2026-06-30
- **Purpose**: 本目录所有支付文档的导航、用途说明和接入范围规划。

---

## 一、文档清单

### 微信支付文档

| 文件 | 官方链接 | 用途 | 状态 |
|------|----------|------|------|
| [wechatpay-access.md](wechatpay-access.md) | https://pay.weixin.qq.com/doc/global/v3/zh/4012354063 | 接入前提、凭据清单、产品权限 | ✅ 已整理 |
| [wechatpay-apiv3-overview.md](wechatpay-apiv3-overview.md) | https://pay.wechatpay.cn/doc/v3/merchant/4012081606 | APIv3 通用规则：签名/验签/解密/错误格式 | ✅ 已整理 |
| [wechatpay-native-pay.md](wechatpay-native-pay.md) | https://pay.wechatpay.cn/doc/v3/merchant/4012791877 | Native 扫码下单接口（本项目主要支付方式） | ✅ 已整理（官方页面抓取） |
| [wechatpay-h5-pay.md](wechatpay-h5-pay.md) | https://pay.wechatpay.cn/doc/v3/merchant/4012791835 | H5 下单接口（手机端非微信浏览器） | ✅ 已整理（官方页面抓取） |
| [wechatpay-sign-verify.md](wechatpay-sign-verify.md) | https://pay.wechatpay.cn/doc/v3/merchant/4012081606 | 签名构造、Authorization Header、响应验签 | ✅ 已整理 |
| [wechatpay-notify-decrypt.md](wechatpay-notify-decrypt.md) | https://pay.wechatpay.cn/doc/v3/merchant/4012081606 | 回调通知接收、验签、AES-256-GCM 解密 | ✅ 已整理 |
| [wechatpay-go-sdk.md](wechatpay-go-sdk.md) | https://github.com/wechatpay-apiv3/wechatpay-go | 官方 Go SDK 安装、初始化、调用、回调处理 | ✅ 已整理（README 抓取） |

### 支付宝文档

| 文件 | 官方链接 | 用途 | 状态 |
|------|----------|------|------|
| [alipay-access-prepare.md](alipay-access-prepare.md) | https://opendocs.alipay.com/open/270/105898 | 账号资质、密钥生成、签约产品 | ⚠️ 占位，需人工补充 |
| [alipay-page-pay-quickstart.md](alipay-page-pay-quickstart.md) | https://opendocs.alipay.com/open/270/105899 | `alipay.trade.page.pay` 接口参数 | ⚠️ 占位，需人工补充 |
| [alipay-page-pay-notify.md](alipay-page-pay-notify.md) | https://opendocs.alipay.com/open/270/105902 | 电脑网站支付异步通知说明 | ⚠️ 占位，需人工补充 |
| [alipay-common-notify.md](alipay-common-notify.md) | https://opendocs.alipay.com/open/064jha | 通用异步通知机制（重试策略、响应规范） | ⚠️ 占位，需人工补充 |
| [alipay-notify-params.md](alipay-notify-params.md) | https://opendocs.alipay.com/open/04ij9t | 异步通知完整参数字段说明 | ⚠️ 占位，需人工补充 |
| [alipay-sign-verify.md](alipay-sign-verify.md) | （接入文档签名章节） | RSA2 签名算法、请求签名、通知验签 | ✅ 已整理（基于官方通用规范） |

---

## 二、第一阶段建议实现范围

### 目标：最小可用支付闭环

```
微信 Native 扫码支付 + 支付宝电脑网站支付
```

### 实现顺序

| 优先级 | 功能 | 依赖文档 |
|--------|------|----------|
| P0 | 微信 Native 下单 | wechatpay-native-pay.md + wechatpay-sign-verify.md + wechatpay-go-sdk.md |
| P0 | 微信支付回调处理 | wechatpay-notify-decrypt.md |
| P0 | 支付宝电脑网站下单 | alipay-page-pay-quickstart.md + alipay-sign-verify.md |
| P0 | 支付宝异步通知处理 | alipay-notify-params.md + alipay-common-notify.md |
| P1 | 订单查询兜底 | （微信/支付宝各自查单接口） |
| P2 | 微信 H5 支付 | wechatpay-h5-pay.md |
| P3 | 退款 | （未整理，后续扩展） |

---

## 三、微信 Native 扫码支付所用文档

```
wechatpay-access.md          ← 凭据准备
wechatpay-apiv3-overview.md  ← 接口规则（签名/错误码格式）
wechatpay-native-pay.md      ← 下单接口（获取 code_url）
wechatpay-sign-verify.md     ← 签名构造和验签详解
wechatpay-notify-decrypt.md  ← 回调通知处理
wechatpay-go-sdk.md          ← SDK 调用示例
```

---

## 四、支付宝电脑网站支付所用文档

```
alipay-access-prepare.md      ← 账号和密钥准备
alipay-page-pay-quickstart.md ← alipay.trade.page.pay 接口
alipay-sign-verify.md         ← 签名和验签
alipay-page-pay-notify.md     ← 电脑网站支付通知说明
alipay-common-notify.md       ← 通用通知机制
alipay-notify-params.md       ← 通知参数字段
```

---

## 五、后续扩展备用文档（暂不实现）

| 功能 | 状态 | 说明 |
|------|------|------|
| 微信 H5 支付 | `wechatpay-h5-pay.md` 已整理 | 手机端非微信浏览器；需申请独立权限和配置 H5 域名 |
| 微信 JSAPI 支付 | 未整理 | 微信内网页支付，需 openid，复杂度高 |
| 支付宝手机网站支付 | 未整理 | `alipay.trade.wap.pay`，H5 场景 |
| 微信退款 | 未整理 | `/v3/refund/domestic/refunds` |
| 支付宝退款 | 未整理 | `alipay.trade.refund` |
| 对账账单 | 未整理 | 微信/支付宝均有账单下载接口 |

---

## 六、待补充的官方原文（需人工操作）

以下 5 个支付宝文档页面因 JS 渲染无法自动抓取，需人工访问复制正文：

| 优先级 | 文档 | 官方链接 |
|--------|------|----------|
| P0 | alipay-notify-params.md | https://opendocs.alipay.com/open/04ij9t |
| P0 | alipay-page-pay-quickstart.md | https://opendocs.alipay.com/open/270/105899 |
| P0 | alipay-common-notify.md | https://opendocs.alipay.com/open/064jha |
| P1 | alipay-page-pay-notify.md | https://opendocs.alipay.com/open/270/105902 |
| P1 | alipay-access-prepare.md | https://opendocs.alipay.com/open/270/105898 |

**操作方法**：
1. 在浏览器打开对应链接
2. 全选（`Ctrl+A`）→ 复制（`Ctrl+C`）页面正文
3. 在对应 `.md` 文件的「手工复制区」粘贴

> 当前占位文档已包含基于官方通用规范整理的技术参数，足以支撑初步开发；原文补充后可做精确核对。

---

## 七、SDK 推荐

| 平台 | SDK | 版本 | import |
|------|-----|------|--------|
| 微信支付 | 官方 | latest | `github.com/wechatpay-apiv3/wechatpay-go` |
| 支付宝 | 社区 | v3 | `github.com/smartwalle/alipay/v3` |

> 支付宝官方暂无维护良好的 Go SDK；`smartwalle/alipay` 是 Go 生态中最广泛使用的第三方实现，与官方文档对齐。
