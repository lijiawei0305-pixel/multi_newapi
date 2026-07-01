package siteconfig

import (
	"strings"
	"time"
)

// HomeMode 是首页渲染模式（二期预留）。一期仅放行 default/config；custom_html 锁定。
type HomeMode string

const (
	// HomeModeDefault 渲染 New API 默认皮肤（一期默认）。
	HomeModeDefault HomeMode = "default"
	// HomeModeConfig 按 site config 字段配置化渲染（一期可用）。
	HomeModeConfig HomeMode = "config"
	// HomeModeCustomHTML 自定义 HTML 首页（二期，需审核；一期锁定）。
	HomeModeCustomHTML HomeMode = "custom_html"
)

// Valid 判断是否为已知的合法 home_mode 值。
func (m HomeMode) Valid() bool {
	switch m {
	case HomeModeDefault, HomeModeConfig, HomeModeCustomHTML:
		return true
	default:
		return false
	}
}

// AllowedPhase1 报告该模式是否在一期受控放行集合内（仅 default/config）。
func (m HomeMode) AllowedPhase1() bool {
	return m == HomeModeDefault || m == HomeModeConfig
}

// CustomHTMLStatus 是自定义首页 HTML 的审核位（二期预留）。
type CustomHTMLStatus string

const (
	// CustomHTMLStatusNone 未提交自定义 HTML。
	CustomHTMLStatusNone CustomHTMLStatus = ""
	// CustomHTMLStatusPending 待管理员审核。
	CustomHTMLStatusPending CustomHTMLStatus = "pending"
	// CustomHTMLStatusApproved 审核通过。
	CustomHTMLStatusApproved CustomHTMLStatus = "approved"
	// CustomHTMLStatusRejected 审核驳回。
	CustomHTMLStatusRejected CustomHTMLStatus = "rejected"
)

// SiteConfig 是租户站点装修配置（对应 tenant_site_configs 表，含二期预留字段）。
// 一期只接受受控值（主题色限色板、home_mode 仅 default/config、custom_html 锁定）。
type SiteConfig struct {
	TenantID int64

	// --- 一期可配基础设置（站点名/Logo/Favicon/Hero/标题/公告/客服/页脚）---
	SiteName        string
	LogoURL         string
	FaviconURL      string
	HeroTitle       string
	HeroSubtitle    string
	Announcement    string
	CustomerService string
	Footer          string

	// BrandHidden 为 OEM 开关：代理在自定义域名上隐藏主站品牌、只显示自定 logo/站名（§6 OEM 最小版）。
	BrandHidden bool

	// --- 二期预留字段（字段已建，一期只接受受控值；见 proposal §11）---
	ThemeColor       string           // 主题色，必须命中预设色板
	TemplateKey      string           // 模板 A/B/C（二期）
	HeroImageURL     string           // Hero 大图（二期）
	BannerJSON       string           // 轮播/Banner 配置（JSON 字符串，二期）
	HomeMode         HomeMode         // default/config/custom_html，一期仅 default/config
	CustomHTML       string           // 自定义首页 HTML（一期锁定）
	CustomHTMLStatus CustomHTMLStatus // 审核位（管理员流程，二期）
	EnabledModules   []string         // 模块开关（二期配置驱动渲染）

	CreatedAt time.Time
	UpdatedAt time.Time
}

// SiteConfigPatch 是 SiteConfigService.Patch 的入参（局部更新）。
// 指针/切片为 nil 表示"不修改"该字段；非 nil 表示整体覆盖。
type SiteConfigPatch struct {
	SiteName        *string
	LogoURL         *string
	FaviconURL      *string
	HeroTitle       *string
	HeroSubtitle    *string
	Announcement    *string
	CustomerService *string
	Footer          *string
	BrandHidden     *bool // OEM：隐藏主站品牌开关

	ThemeColor   *string   // 受控：必须命中色板，否则 THEME_NOT_IN_PALETTE
	TemplateKey  *string   // 二期预留
	HeroImageURL *string   // 二期预留
	BannerJSON   *string   // 二期预留
	HomeMode     *HomeMode // 受控：仅 default/config，否则 HOME_MODE_LOCKED
	CustomHTML   *string   // 受控：一期非空即锁定 HOME_MODE_LOCKED

	EnabledModules []string // nil=不改；非 nil（含空切片）=整体覆盖
}

// File 是上传的原始文件（本轮纯校验所需的最小字段）。
type File struct {
	// Name 原始文件名；仅用于取扩展名，存储时不保留（自动重命名）。
	Name string
	// Data 文件内容字节。
	Data []byte
}

// Size 返回文件字节数。
func (f File) Size() int { return len(f.Data) }

// AssetStatus 是素材状态。
type AssetStatus string

const (
	// AssetStatusActive 正常可访问。
	AssetStatusActive AssetStatus = "active"
	// AssetStatusTakenDown 已被管理员下架，不可访问。
	AssetStatusTakenDown AssetStatus = "takendown"
)

// Asset 是素材元数据记录（绑定 tenant_id；Takedown 据 ID 反查 Key 下架）。
type Asset struct {
	ID          int64
	TenantID    int64
	Key         string // 对象存储键（含 tenant 路径前缀）
	URL         string
	ContentType string
	Size        int64
	Status      AssetStatus
	CreatedAt   time.Time
}

// defaultPalette 是主站预设色板（10 色，落于 [8,12] 区间）。主站默认主题色取首项。
// 统一以小写 #rrggbb 存储，便于大小写不敏感比对。
var defaultPalette = []string{
	"#1677ff", // 默认蓝（主站默认）
	"#2f54eb",
	"#722ed1",
	"#13c2c2",
	"#52c41a",
	"#fadb14",
	"#fa8c16",
	"#fa541c",
	"#f5222d",
	"#eb2f96",
}

// defaultThemeColor 是主站默认主题色（色板首项）。
const defaultThemeColor = "#1677ff"

// DefaultPalette 返回预设色板副本（供 theme-options 接口/单测；修改返回值不影响内部）。
func DefaultPalette() []string {
	out := make([]string, len(defaultPalette))
	copy(out, defaultPalette)
	return out
}

// InPalette 报告主题色是否命中预设色板（去空白、大小写不敏感）。
func InPalette(color string) bool {
	c := strings.ToLower(strings.TrimSpace(color))
	for _, p := range defaultPalette {
		if p == c {
			return true
		}
	}
	return false
}

// defaultEnabledModules 是主站默认开启的模块集合（二期配置驱动渲染的基线）。
var defaultEnabledModules = []string{"dashboard", "tokens", "wallet", "logs", "playground"}

// MainSiteDefault 返回主站默认站点配置（未配置租户的回退基线）。
// 每次调用返回全新实例，调用方可安全改写。
func MainSiteDefault() *SiteConfig {
	mods := make([]string, len(defaultEnabledModules))
	copy(mods, defaultEnabledModules)
	return &SiteConfig{
		TenantID:         0,
		SiteName:         "New API",
		ThemeColor:       defaultThemeColor,
		HomeMode:         HomeModeDefault,
		CustomHTMLStatus: CustomHTMLStatusNone,
		EnabledModules:   mods,
	}
}

// applyPatch 把 patch 中非 nil 的字段写入 cfg。
// 调用方须先经 ValidatePatch 校验受控字段。
func applyPatch(cfg *SiteConfig, in SiteConfigPatch) {
	if in.SiteName != nil {
		cfg.SiteName = *in.SiteName
	}
	if in.LogoURL != nil {
		cfg.LogoURL = *in.LogoURL
	}
	if in.FaviconURL != nil {
		cfg.FaviconURL = *in.FaviconURL
	}
	if in.HeroTitle != nil {
		cfg.HeroTitle = *in.HeroTitle
	}
	if in.HeroSubtitle != nil {
		cfg.HeroSubtitle = *in.HeroSubtitle
	}
	if in.Announcement != nil {
		cfg.Announcement = *in.Announcement
	}
	if in.CustomerService != nil {
		cfg.CustomerService = *in.CustomerService
	}
	if in.Footer != nil {
		cfg.Footer = *in.Footer
	}
	if in.BrandHidden != nil {
		cfg.BrandHidden = *in.BrandHidden
	}
	if in.ThemeColor != nil {
		cfg.ThemeColor = strings.ToLower(strings.TrimSpace(*in.ThemeColor))
	}
	if in.TemplateKey != nil {
		cfg.TemplateKey = *in.TemplateKey
	}
	if in.HeroImageURL != nil {
		cfg.HeroImageURL = *in.HeroImageURL
	}
	if in.BannerJSON != nil {
		cfg.BannerJSON = *in.BannerJSON
	}
	if in.HomeMode != nil {
		cfg.HomeMode = *in.HomeMode
	}
	if in.CustomHTML != nil {
		cfg.CustomHTML = *in.CustomHTML
	}
	if in.EnabledModules != nil {
		mods := make([]string, len(in.EnabledModules))
		copy(mods, in.EnabledModules)
		cfg.EnabledModules = mods
	}
}
