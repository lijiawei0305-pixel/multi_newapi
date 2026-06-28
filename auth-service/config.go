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
		PrivateKeyPath      string `yaml:"private_key_path"`       // 应用私钥路径
		AlipayPublicKeyPath string `yaml:"alipay_public_key_path"` // 支付宝公钥路径
	} `yaml:"alipay"`
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
	if !c.Mock {
		return fmt.Errorf("real wxpay/alipay V3 SDK not wired yet; set mock:true for now (见包注释 TODO)")
	}
	return nil
}
