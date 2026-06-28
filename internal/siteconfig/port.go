package siteconfig

import "context"

// --- 对外接口（detailed-design §2.10 的 Go 签名）---

// SiteConfigService 提供租户装修配置的读写。
type SiteConfigService interface {
	// Get 返回租户站点配置；未配置时回退主站默认值。
	Get(ctx context.Context, tenantID int64) (*SiteConfig, error)
	// Patch 局部更新租户配置：主题色限色板（THEME_NOT_IN_PALETTE）；
	// home_mode 一期仅 default/config，custom_html 锁定（HOME_MODE_LOCKED）。
	Patch(ctx context.Context, tenantID int64, in SiteConfigPatch) error
}

// AssetService 提供素材上传与下架（图片安全限制）。
type AssetService interface {
	// Upload 校验并上传素材：限 jpg/png/webp、≤2MB、绑 tenant_id、自动重命名；
	// 返回可访问 URL。违规返回 ASSET_TYPE_FORBIDDEN / ASSET_TOO_LARGE。
	Upload(ctx context.Context, tenantID int64, f File) (url string, err error)
	// Takedown 管理员下架违规素材（删对象 + 置状态），下架后不可访问。
	Takedown(ctx context.Context, assetID int64) error
}

// --- 消费者定义的依赖接口（本包声明，main 装配具体实现）---

// SiteConfigRepo 是站点配置与素材元数据的持久化抽象。
// 本轮提供内存假实现（MemRepo）；真实 GORM 实现 + 迁移顺延（见报告 TODO）。
type SiteConfigRepo interface {
	// GetConfig 读取租户配置；未配置返回 found=false（由 Service 回退主站默认）。
	GetConfig(ctx context.Context, tenantID int64) (cfg *SiteConfig, found bool, err error)
	// UpsertConfig 写入/更新租户配置（按 TenantID upsert）。
	UpsertConfig(ctx context.Context, cfg *SiteConfig) error
	// CreateAsset 记录素材元数据并回填 a.ID。
	CreateAsset(ctx context.Context, a *Asset) error
	// GetAsset 按 ID 读取素材；不存在返回 ErrAssetNotFound。
	GetAsset(ctx context.Context, id int64) (*Asset, error)
	// SetAssetStatus 更新素材状态（如下架）；不存在返回 ErrAssetNotFound。
	SetAssetStatus(ctx context.Context, id int64, s AssetStatus) error
}

// Blob 是对象存储抽象。本轮提供内存假实现（MemBlob）；
// 真实对象存储适配（S3/OSS/COS：分桶、签名 URL、CDN）顺延（见报告 TODO）。
type Blob interface {
	// Put 以 key 存储对象，返回可访问 URL。
	Put(ctx context.Context, key string, data []byte, contentType string) (url string, err error)
	// Delete 删除对象（下架）。
	Delete(ctx context.Context, key string) error
}
