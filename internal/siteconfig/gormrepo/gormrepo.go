// Package gormrepo 用真实 GORM(MySQL) 实现 siteconfig.SiteConfigRepo
// （tenant_site_configs 站点装修配置 + tenant_assets 素材元数据两表）。
//
// 仅负责持久化与 domain<->db 模型映射；上传/校验/受控字段逻辑仍在 siteconfig 包。
// 错误码统一翻译回 siteconfig 命名空间（ASSET_NOT_FOUND），与 MemRepo 行为一致，便于无感切换。
package gormrepo

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/QuantumNous/new-api/internal/siteconfig"
)

// siteConfigRow 是 tenant_site_configs 表的 GORM 模型（每租户 1 行，tenant_id 即主键）。
// logo/favicon/hero_image 列用 longtext：允许存 data: URL（base64 内联，OEM 最小版免对象存储）。
type siteConfigRow struct {
	TenantID         int64     `gorm:"column:tenant_id;primaryKey"`
	SiteName         string    `gorm:"column:site_name;type:varchar(128);not null;default:''"`
	LogoURL          string    `gorm:"column:logo_url;type:longtext"`
	FaviconURL       string    `gorm:"column:favicon_url;type:longtext"`
	HeroTitle        string    `gorm:"column:hero_title;type:varchar(255);not null;default:''"`
	HeroSubtitle     string    `gorm:"column:hero_subtitle;type:varchar(255);not null;default:''"`
	Announcement     string    `gorm:"column:announcement;type:text"`
	CustomerService  string    `gorm:"column:customer_service;type:varchar(255);not null;default:''"`
	Footer           string    `gorm:"column:footer;type:text"`
	BrandHidden      bool      `gorm:"column:brand_hidden;not null;default:false"`
	ThemeColor       string    `gorm:"column:theme_color;type:varchar(16);not null;default:''"`
	TemplateKey      string    `gorm:"column:template_key;type:varchar(32);not null;default:''"`
	HeroImageURL     string    `gorm:"column:hero_image_url;type:longtext"`
	BannerJSON       string    `gorm:"column:banner_json;type:text"`
	HomeMode         string    `gorm:"column:home_mode;type:varchar(16);not null;default:default"`
	CustomHTML       string    `gorm:"column:custom_html;type:mediumtext"`
	CustomHTMLStatus string    `gorm:"column:custom_html_status;type:varchar(16);not null;default:''"`
	EnabledModules   string    `gorm:"column:enabled_modules;type:text"` // JSON 数组字符串
	CreatedAt        time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt        time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (siteConfigRow) TableName() string { return "tenant_site_configs" }

// assetRow 是 tenant_assets 表的 GORM 模型（素材元数据；下架据 ID 反查 Key）。
type assetRow struct {
	ID          int64     `gorm:"column:id;primaryKey;autoIncrement"`
	TenantID    int64     `gorm:"column:tenant_id;not null;index:idx_tsa_tenant"`
	Key         string    `gorm:"column:asset_key;type:varchar(255);not null"`
	URL         string    `gorm:"column:url;type:longtext"`
	ContentType string    `gorm:"column:content_type;type:varchar(64);not null;default:''"`
	Size        int64     `gorm:"column:size;not null;default:0"`
	Status      string    `gorm:"column:status;type:varchar(16);not null;default:active"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (assetRow) TableName() string { return "tenant_assets" }

// Repo 是 siteconfig.SiteConfigRepo 的 GORM 实现。
type Repo struct {
	db *gorm.DB
}

var _ siteconfig.SiteConfigRepo = (*Repo)(nil)

// New 用已建立连接的 *gorm.DB 构造仓储。
func New(db *gorm.DB) *Repo { return &Repo{db: db} }

// AutoMigrate 建/补 tenant_site_configs 与 tenant_assets 表结构。
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&siteConfigRow{}, &assetRow{})
}

// GetConfig 读取租户配置；未配置返回 found=false（由 Service 回退主站默认）。
func (r *Repo) GetConfig(ctx context.Context, tenantID int64) (*siteconfig.SiteConfig, bool, error) {
	var row siteConfigRow
	err := r.db.WithContext(ctx).Take(&row, "tenant_id = ?", tenantID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return rowToConfig(&row), true, nil
}

// UpsertConfig 按 tenant_id 主键 upsert（冲突更新除 created_at 外的全部列）。
func (r *Repo) UpsertConfig(ctx context.Context, cfg *siteconfig.SiteConfig) error {
	row := configToRow(cfg)
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "tenant_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"site_name", "logo_url", "favicon_url", "hero_title", "hero_subtitle",
			"announcement", "customer_service", "footer", "brand_hidden", "theme_color",
			"template_key", "hero_image_url", "banner_json", "home_mode", "custom_html",
			"custom_html_status", "enabled_modules", "updated_at",
		}),
	}).Create(&row).Error
}

// CreateAsset 记录素材并回填 ID/时间戳。
func (r *Repo) CreateAsset(ctx context.Context, a *siteconfig.Asset) error {
	row := assetRow{
		ID:          a.ID,
		TenantID:    a.TenantID,
		Key:         a.Key,
		URL:         a.URL,
		ContentType: a.ContentType,
		Size:        a.Size,
		Status:      string(a.Status),
		CreatedAt:   a.CreatedAt,
	}
	if row.Status == "" {
		row.Status = string(siteconfig.AssetStatusActive)
	}
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return err
	}
	a.ID = row.ID
	a.Status = siteconfig.AssetStatus(row.Status)
	a.CreatedAt = row.CreatedAt
	return nil
}

// GetAsset 按 ID 读取素材；不存在返回 siteconfig.ErrAssetNotFound。
func (r *Repo) GetAsset(ctx context.Context, id int64) (*siteconfig.Asset, error) {
	var row assetRow
	if err := r.db.WithContext(ctx).Take(&row, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, siteconfig.ErrAssetNotFound
		}
		return nil, err
	}
	return &siteconfig.Asset{
		ID:          row.ID,
		TenantID:    row.TenantID,
		Key:         row.Key,
		URL:         row.URL,
		ContentType: row.ContentType,
		Size:        row.Size,
		Status:      siteconfig.AssetStatus(row.Status),
		CreatedAt:   row.CreatedAt,
	}, nil
}

// SetAssetStatus 更新素材状态（如下架）；不存在返回 ErrAssetNotFound。
func (r *Repo) SetAssetStatus(ctx context.Context, id int64, s siteconfig.AssetStatus) error {
	res := r.db.WithContext(ctx).Model(&assetRow{}).Where("id = ?", id).Update("status", string(s))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return siteconfig.ErrAssetNotFound
	}
	return nil
}

// ---- 映射 ----

func configToRow(cfg *siteconfig.SiteConfig) siteConfigRow {
	return siteConfigRow{
		TenantID:         cfg.TenantID,
		SiteName:         cfg.SiteName,
		LogoURL:          cfg.LogoURL,
		FaviconURL:       cfg.FaviconURL,
		HeroTitle:        cfg.HeroTitle,
		HeroSubtitle:     cfg.HeroSubtitle,
		Announcement:     cfg.Announcement,
		CustomerService:  cfg.CustomerService,
		Footer:           cfg.Footer,
		BrandHidden:      cfg.BrandHidden,
		ThemeColor:       cfg.ThemeColor,
		TemplateKey:      cfg.TemplateKey,
		HeroImageURL:     cfg.HeroImageURL,
		BannerJSON:       cfg.BannerJSON,
		HomeMode:         string(cfg.HomeMode),
		CustomHTML:       cfg.CustomHTML,
		CustomHTMLStatus: string(cfg.CustomHTMLStatus),
		EnabledModules:   encodeModules(cfg.EnabledModules),
	}
}

func rowToConfig(row *siteConfigRow) *siteconfig.SiteConfig {
	return &siteconfig.SiteConfig{
		TenantID:         row.TenantID,
		SiteName:         row.SiteName,
		LogoURL:          row.LogoURL,
		FaviconURL:       row.FaviconURL,
		HeroTitle:        row.HeroTitle,
		HeroSubtitle:     row.HeroSubtitle,
		Announcement:     row.Announcement,
		CustomerService:  row.CustomerService,
		Footer:           row.Footer,
		BrandHidden:      row.BrandHidden,
		ThemeColor:       row.ThemeColor,
		TemplateKey:      row.TemplateKey,
		HeroImageURL:     row.HeroImageURL,
		BannerJSON:       row.BannerJSON,
		HomeMode:         siteconfig.HomeMode(row.HomeMode),
		CustomHTML:       row.CustomHTML,
		CustomHTMLStatus: siteconfig.CustomHTMLStatus(row.CustomHTMLStatus),
		EnabledModules:   decodeModules(row.EnabledModules),
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
	}
}

func encodeModules(mods []string) string {
	if len(mods) == 0 {
		return ""
	}
	b, err := json.Marshal(mods)
	if err != nil {
		return ""
	}
	return string(b)
}

func decodeModules(s string) []string {
	if s == "" {
		return nil
	}
	var mods []string
	if err := json.Unmarshal([]byte(s), &mods); err != nil {
		return nil
	}
	return mods
}
