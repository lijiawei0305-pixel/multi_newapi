// Package gormrepo 用真实 GORM(MySQL) 实现 tenant.TenantRepo（tenants / tenant_domains 两表）。
//
// 仅负责持久化与 domain<->db 模型映射；缓存仍复用 tenant 包的内存实现（MemCache），
// Redis 适配顺延后续 Slice。错误码统一翻译回 tenant 包命名空间（TENANT_NOT_FOUND /
// SLUG_DUPLICATE），保持与 MemRepo 行为一致，便于 resolver/service 无感切换。
package gormrepo

import (
	"context"
	"errors"
	"time"

	driver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	"newapi-mt/internal/tenant"
)

// mysqlDupErrNo 是 MySQL "Duplicate entry" 的错误号（唯一键冲突）。
const mysqlDupErrNo = 1062

// tenantRow 是 tenants 表的 GORM 模型。slug 唯一索引保证一期二级域名 label 唯一。
type tenantRow struct {
	ID               int64     `gorm:"column:id;primaryKey;autoIncrement"`
	Slug             string    `gorm:"column:slug;type:varchar(63);not null;uniqueIndex:idx_tenants_slug"`
	Name             string    `gorm:"column:name;type:varchar(128);not null"`
	Status           string    `gorm:"column:status;type:varchar(16);not null;default:active"`
	TokenplanEnabled bool      `gorm:"column:tokenplan_enabled;not null;default:false"`
	CreatedAt        time.Time `gorm:"column:created_at"`
	UpdatedAt        time.Time `gorm:"column:updated_at"`
}

// TableName 固定表名，避免 GORM 复数推断带来的歧义。
func (tenantRow) TableName() string { return "tenants" }

// domainRow 是 tenant_domains 表的 GORM 模型。domain 唯一索引保证 Host->租户一对一。
type domainRow struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement"`
	TenantID  int64     `gorm:"column:tenant_id;not null;index:idx_tenant_domains_tenant"`
	Domain    string    `gorm:"column:domain;type:varchar(255);not null;uniqueIndex:idx_tenant_domains_domain"`
	IsPrimary bool      `gorm:"column:is_primary;not null;default:false"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName 固定表名。
func (domainRow) TableName() string { return "tenant_domains" }

// Repo 是 tenant.TenantRepo 的 GORM 实现。
type Repo struct {
	db *gorm.DB
}

// 编译期断言：*Repo 满足 tenant.TenantRepo 契约。
var _ tenant.TenantRepo = (*Repo)(nil)

// New 用已建立连接的 *gorm.DB 构造仓储。
func New(db *gorm.DB) *Repo { return &Repo{db: db} }

// AutoMigrate 建/补 tenants 与 tenant_domains 表结构（含唯一/普通索引）。
// 由 cmd/server 在启动时调用；本包不持有迁移时机决策。
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&tenantRow{}, &domainRow{})
}

// CreateTenant 入库租户并回填 ID/时间戳；slug 冲突翻译为 tenant.ErrSlugDuplicate。
func (r *Repo) CreateTenant(ctx context.Context, t *tenant.Tenant) error {
	row := tenantRow{
		ID:               t.ID,
		Slug:             t.Slug,
		Name:             t.Name,
		Status:           string(t.Status),
		TokenplanEnabled: t.TokenplanEnabled,
		CreatedAt:        t.CreatedAt,
		UpdatedAt:        t.UpdatedAt,
	}
	if row.Status == "" {
		row.Status = string(tenant.StatusActive)
	}
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		if isDuplicate(err) {
			return tenant.ErrSlugDuplicate
		}
		return err
	}
	t.ID = row.ID
	t.Status = tenant.TenantStatus(row.Status)
	t.CreatedAt = row.CreatedAt
	t.UpdatedAt = row.UpdatedAt
	return nil
}

// GetTenant 按 id 读取；未找到翻译为 tenant.ErrTenantNotFound。
func (r *Repo) GetTenant(ctx context.Context, id int64) (*tenant.Tenant, error) {
	var row tenantRow
	err := r.db.WithContext(ctx).Take(&row, "id = ?", id).Error
	return mapTenantResult(&row, err)
}

// GetTenantBySlug 按 slug 读取；未找到翻译为 tenant.ErrTenantNotFound。
func (r *Repo) GetTenantBySlug(ctx context.Context, slug string) (*tenant.Tenant, error) {
	var row tenantRow
	err := r.db.WithContext(ctx).Take(&row, "slug = ?", slug).Error
	return mapTenantResult(&row, err)
}

// SetTenantStatus 仅改 status 列（GORM 自动维护 updated_at）；行不存在返回 ErrTenantNotFound。
func (r *Repo) SetTenantStatus(ctx context.Context, id int64, s tenant.TenantStatus) error {
	res := r.db.WithContext(ctx).Model(&tenantRow{}).
		Where("id = ?", id).
		Update("status", string(s))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return tenant.ErrTenantNotFound
	}
	return nil
}

// CreateDomain 写入域名映射并回填 ID/时间戳；域名冲突翻译为 tenant.ErrSlugDuplicate。
func (r *Repo) CreateDomain(ctx context.Context, d *tenant.TenantDomain) error {
	row := domainRow{
		ID:        d.ID,
		TenantID:  d.TenantID,
		Domain:    d.Domain,
		IsPrimary: d.IsPrimary,
		CreatedAt: d.CreatedAt,
	}
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		if isDuplicate(err) {
			return tenant.ErrSlugDuplicate
		}
		return err
	}
	d.ID = row.ID
	d.CreatedAt = row.CreatedAt
	return nil
}

// GetTenantByDomain 先按 domain 命中映射，再读对应租户；任一步未找到均返回 ErrTenantNotFound。
func (r *Repo) GetTenantByDomain(ctx context.Context, domain string) (*tenant.Tenant, error) {
	var d domainRow
	if err := r.db.WithContext(ctx).Take(&d, "domain = ?", domain).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, tenant.ErrTenantNotFound
		}
		return nil, err
	}
	return r.GetTenant(ctx, d.TenantID)
}

// mapTenantResult 把一次 Take 的结果统一翻译为 domain 模型或 tenant 包错误码。
func mapTenantResult(row *tenantRow, err error) (*tenant.Tenant, error) {
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, tenant.ErrTenantNotFound
		}
		return nil, err
	}
	return &tenant.Tenant{
		ID:               row.ID,
		Slug:             row.Slug,
		Name:             row.Name,
		Status:           tenant.TenantStatus(row.Status),
		TokenplanEnabled: row.TokenplanEnabled,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
	}, nil
}

// isDuplicate 判断是否唯一键冲突：优先用 GORM TranslateError 归一化的 ErrDuplicatedKey，
// 兜底再看 MySQL 原生错误号 1062，兼容未开启 TranslateError 的场景。
func isDuplicate(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	var myErr *driver.MySQLError
	if errors.As(err, &myErr) {
		return myErr.Number == mysqlDupErrNo
	}
	return false
}
