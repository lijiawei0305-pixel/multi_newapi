package setting

// 微信支付 / 支付宝 原生支付凭据配置。
// 这些包级变量与 model.option 的存储键同名，由 InitOptionMap / updateOptionMap
// 在进程内与 DB 双向同步。买家可用渠道判定：configured = 对应凭据齐全（进程内判断）。

// ===== 微信支付（Provider = wxpay） =====
var WechatPayEnabled bool       // 是否启用微信支付
var WechatPayAppID string       // 公众号/小程序/APP 的 AppID
var WechatPayMchID string       // 商户号 mch_id
var WechatPayAPIv3Key string    // APIv3 密钥
var WechatPayCertSerial string  // 商户证书序列号
var WechatPayPrivateKey string  // 商户私钥（PEM 内容）
var WechatPayPublicKeyID string // 微信支付公钥 ID（商户平台-API安全 申请，形如 PUB_KEY_ID_...）
var WechatPayPublicKey string   // 微信支付公钥 PEM 内容（验证微信应答/回调签名）

// ===== 支付宝（Provider = alipay） =====
var AlipayEnabled bool      // 是否启用支付宝
var AlipayAppID string      // 应用 AppID
var AlipayPrivateKey string // 应用私钥（PEM 内容）
var AlipayPublicKey string  // 支付宝公钥（PEM 内容）
var AlipaySellerID string   // 收款方 seller_id（可空）
var AlipayReturnURL string  // 同步跳转回调地址
var AlipaySandbox bool      // 是否使用支付宝沙箱
