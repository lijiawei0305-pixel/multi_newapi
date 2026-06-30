// Package authservice 是独立支付网关服务：对接微信/支付宝（原生 new-api 只有 epay 子渠道、
// 无官方 V3），统一以「下单 → 平台验签 → 回调主站内网入账端点」的形态工作。
//
// 形态：独立二进制（cmd/authservice），经 nginx `^~ /auth/` 反代；与主站仅通过
// HTTP 交互（下单：主站→auth-service /auth/order；入账：auth-service→主站 /api/internal/order/paid）。
//
// mock 模式（mock:true，无真实商户凭据时）：用 HMAC stub（复用 internal/payment.StubPaySDK 的
// 签名/验签语义）合成合法回调，跑通「确认页 → 验签 → 幂等 → 调主站入账」全链路。真实凭据到位后
// 填 wxpay/alipay 凭据 + mock:false，换真实 V3 SDK 适配器即可，主站入账侧零改。
package authservice

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config 是 auth-service 配置（config.yaml；敏感项支持环境变量覆盖，见 applyEnvOverrides）。
type Config struct {
	Server struct {
		Addr          string `yaml:"addr"`            // 监听地址，如 ":8080"（仅内部端口）
		PublicBaseURL string `yaml:"public_base_url"` // 拼装支付确认/二维码 URL 的公网基址（经 nginx /auth/ 反代）
	} `yaml:"server"`

	Mock         bool    `yaml:"mock"`            // true=mock 签名+确认页；false=接真实 V3 SDK（顺延）
	USDToCNYRate float64 `yaml:"usd_to_cny_rate"` // 美元→人民币汇率（与主站一致，对账兜底）
	SignSecret   string  `yaml:"sign_secret"`     // mock HMAC 签名密钥（模拟支付平台签名/验签）

	Internal struct {
		CallbackURL  string `yaml:"callback_url"`  // 主站内网入账端点 /api/internal/order/paid
		SharedSecret string `yaml:"shared_secret"` // 调主站内网端点的共享密钥（与主站 MT_INTERNAL_SECRET 同值）
	} `yaml:"internal"`

	// 真实模式凭据（mock:true 时全部留空；接真实 SDK 时填，详见各字段）。
	Wxpay struct {
		MchID          string `yaml:"mch_id"`           // 商户号
		AppID          string `yaml:"app_id"`           // 公众号/小程序 appid
		APIv3Key       string `yaml:"api_v3_key"`       // APIv3 密钥
		CertSerialNo   string `yaml:"cert_serial_no"`   // 商户证书序列号
		PrivateKeyPath string `yaml:"private_key_path"` // 商户私钥路径
	} `yaml:"wxpay"`
	Alipay struct {
		AppID               string `yaml:"app_id"`                 // 应用 appid
		PrivateKeyPath      string `yaml:"private_key_path"`       // 应用私钥路径（PEM 文件）
		AlipayPublicKeyPath string `yaml:"alipay_public_key_path"` // 支付宝公钥路径（PEM 文件）
		SellerID            string `yaml:"seller_id"`              // 可选：收款账号 UID（pid，2088 开头）；非空则校验回调 seller_id
		ReturnURL           string `yaml:"return_url"`             // 同步跳转地址（仅展示，不入账）
		Sandbox             bool   `yaml:"sandbox"`                // true=沙箱网关，false=正式
	} `yaml:"alipay"`
}

// wxpayConfigured 报告微信支付凭据是否齐全（真实模式可用微信）。
func (c *Config) wxpayConfigured() bool {
	w := c.Wxpay
	return w.MchID != "" && w.AppID != "" && w.APIv3Key != "" && w.CertSerialNo != "" && w.PrivateKeyPath != ""
}

// alipayConfigured 报告支付宝凭据是否齐全（真实模式可用支付宝）。
func (c *Config) alipayConfigured() bool {
	a := c.Alipay
	return a.AppID != "" && a.PrivateKeyPath != "" && a.AlipayPublicKeyPath != ""
}

// LoadConfig 读取 yaml 配置并应用环境变量覆盖（敏感项不入库）。
func LoadConfig(path string) (Config, error) {
	var cfg Config
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return cfg, fmt.Errorf("read config %q: %w", path, err)
		}
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return cfg, fmt.Errorf("parse config %q: %w", path, err)
		}
	}
	cfg.applyDefaults()
	cfg.applyEnvOverrides()
	if err := cfg.validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Server.Addr == "" {
		c.Server.Addr = ":8080"
	}
	if c.USDToCNYRate == 0 {
		c.USDToCNYRate = 7.3
	}
}

// applyEnvOverrides 让部署期用环境变量注入敏感/环境相关项（compose/.env），不写进 yaml。
func (c *Config) applyEnvOverrides() {
	if v := os.Getenv("AUTH_SERVER_ADDR"); v != "" {
		c.Server.Addr = v
	}
	if v := os.Getenv("AUTH_PUBLIC_BASE_URL"); v != "" {
		c.Server.PublicBaseURL = v
	}
	if v := os.Getenv("AUTH_SIGN_SECRET"); v != "" {
		c.SignSecret = v
	}
	if v := os.Getenv("AUTH_INTERNAL_CALLBACK_URL"); v != "" {
		c.Internal.CallbackURL = v
	}
	if v := os.Getenv("AUTH_INTERNAL_SHARED_SECRET"); v != "" {
		c.Internal.SharedSecret = v
	}
	if v := os.Getenv("AUTH_MOCK"); v != "" {
		c.Mock = v == "1" || v == "true"
	}

	// 真实支付凭据（敏感）：仅经环境变量/部署期注入，不写进 yaml、不入库。
	if v := os.Getenv("AUTH_WXPAY_APP_ID"); v != "" {
		c.Wxpay.AppID = v
	}
	if v := os.Getenv("AUTH_WXPAY_MCH_ID"); v != "" {
		c.Wxpay.MchID = v
	}
	if v := os.Getenv("AUTH_WXPAY_APIV3_KEY"); v != "" {
		c.Wxpay.APIv3Key = v
	}
	if v := os.Getenv("AUTH_WXPAY_CERT_SERIAL"); v != "" {
		c.Wxpay.CertSerialNo = v
	}
	if v := os.Getenv("AUTH_WXPAY_PRIVATE_KEY_PATH"); v != "" {
		c.Wxpay.PrivateKeyPath = v
	}
	if v := os.Getenv("AUTH_ALIPAY_APP_ID"); v != "" {
		c.Alipay.AppID = v
	}
	if v := os.Getenv("AUTH_ALIPAY_PRIVATE_KEY_PATH"); v != "" {
		c.Alipay.PrivateKeyPath = v
	}
	if v := os.Getenv("AUTH_ALIPAY_PUBLIC_KEY_PATH"); v != "" {
		c.Alipay.AlipayPublicKeyPath = v
	}
	if v := os.Getenv("AUTH_ALIPAY_SELLER_ID"); v != "" {
		c.Alipay.SellerID = v
	}
	if v := os.Getenv("AUTH_ALIPAY_RETURN_URL"); v != "" {
		c.Alipay.ReturnURL = v
	}
	if v := os.Getenv("AUTH_ALIPAY_SANDBOX"); v != "" {
		c.Alipay.Sandbox = v == "1" || v == "true"
	}
}

// validate 校验最小可运行所需字段（mock 模式下也必须有签名密钥与主站回调地址）。
func (c *Config) validate() error {
	if c.Internal.CallbackURL == "" {
		return fmt.Errorf("internal.callback_url is required (主站入账端点)")
	}
	if c.Internal.SharedSecret == "" {
		return fmt.Errorf("internal.shared_secret is required (与主站共享密钥)")
	}
	if c.Mock && c.SignSecret == "" {
		return fmt.Errorf("sign_secret is required in mock mode (mock HMAC 签名密钥)")
	}
	if !c.Mock && !c.wxpayConfigured() && !c.alipayConfigured() {
		// 真实模式至少需配齐微信或支付宝其一的凭据，否则无渠道可下单。
		return fmt.Errorf("real mode requires wxpay and/or alipay credentials (见 config.yaml 模板)")
	}
	return nil
}
