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

	"github.com/QuantumNous/new-api/internal/tenant"
)

// mysqlDupErrNo 是 MySQL "Duplicate entry" 的错误号（唯一键冲突）。
const mysqlDupErrNo = 1062

// tenantRow 是 tenants 表的 GORM 模型。slug 唯一索引保证一期二级域名 label 唯一。
// owner_user_id：代理 owner（new-api users.id），1:1 独占本租户；带索引以支持「按 owner 反查租户」。
type tenantRow struct {
	ID               int64     `gorm:"column:id;primaryKey;autoIncrement"`
	Slug             string    `gorm:"column:slug;type:varchar(63);not null;uniqueIndex:idx_tenants_slug"`
	Name             string    `gorm:"column:name;type:varchar(128);not null"`
	Status           string    `gorm:"column:status;type:varchar(16);not null;default:active"`
	TokenplanEnabled bool      `gorm:"column:tokenplan_enabled;not null;default:false"`
	OwnerUserID      int64     `gorm:"column:owner_user_id;not null;default:0;index:idx_tenants_owner"`
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

// customDomainRow 是 tenant_custom_domains 表的 GORM 模型（OEM 自定义域名，§6.2/§6.3）。
// 与 tenant_domains（wildcard 二级域名）分表：解析热路径不受影响，且"仅 active 自定义域名可解析"
// 这条安全红线由独立表 + status 过滤天然隔离。
//   - tenant_id 唯一索引：每租户至多 1 个自定义域名（业务上限的 DB 兜底）。
//   - domain 唯一索引：自定义域名全局唯一。
type customDomainRow struct {
	ID            int64      `gorm:"column:id;primaryKey;autoIncrement"`
	TenantID      int64      `gorm:"column:tenant_id;not null;uniqueIndex:idx_tcd_tenant"`
	Domain        string     `gorm:"column:domain;type:varchar(255);not null;uniqueIndex:idx_tcd_domain"`
	Status        string     `gorm:"column:status;type:varchar(16);not null;default:pending_dns;index:idx_tcd_status"`
	VerifyToken   string     `gorm:"column:verify_token;type:varchar(64);not null"`
	CertStatus    string     `gorm:"column:cert_status;type:varchar(16);not null;default:''"`
	CertExpiresAt *time.Time `gorm:"column:cert_expires_at"`
	LastError     string     `gorm:"column:last_error;type:varchar(512);not null;default:''"`
	CreatedAt     time.Time  `gorm:"column:created_at"`
	UpdatedAt     time.Time  `gorm:"column:updated_at"`
}

// TableName 固定表名。
func (customDomainRow) TableName() string { return "tenant_custom_domains" }

// Repo 是 tenant.TenantRepo 的 GORM 实现。
type Repo struct {
	db *gorm.DB
}

// 编译期断言：*Repo 同时满足 tenant.TenantRepo 与 tenant.CustomDomainRepo 契约。
var (
	_ tenant.TenantRepo       = (*Repo)(nil)
	_ tenant.CustomDomainRepo = (*Repo)(nil)
)

// New 用已建立连接的 *gorm.DB 构造仓储。
func New(db *gorm.DB) *Repo { return &Repo{db: db} }

// AutoMigrate 建/补 tenants / tenant_domains / tenant_custom_domains 表结构（含唯一/普通索引）。
// 由 cmd/server 在启动时调用；本包不持有迁移时机决策。
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&tenantRow{}, &domainRow{}, &groupRow{}, &customDomainRow{})
}

// CreateTenant 入库租户并回填 ID/时间戳；slug 冲突翻译为 tenant.ErrSlugDuplicate。
func (r *Repo) CreateTenant(ctx context.Context, t *tenant.Tenant) error {
	row := tenantRow{
		ID:               t.ID,
		Slug:             t.Slug,
		Name:             t.Name,
		Status:           string(t.Status),
		TokenplanEnabled: t.TokenplanEnabled,
		OwnerUserID:      t.OwnerUserID,
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

// UpdateName 仅改租户展示名（GORM 自动维护 updated_at）；行不存在返回 ErrTenantNotFound。
func (r *Repo) UpdateName(ctx context.Context, tenantID int64, name string) error {
	res := r.db.WithContext(ctx).Model(&tenantRow{}).
		Where("id = ?", tenantID).
		Update("name", name)
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

// GetTenantByDomain 解析 Host→租户：先查 tenant_domains（wildcard 二级域名热路径），未命中再查
// tenant_custom_domains 中 **status='active'** 的自定义域名；任一命中读对应租户，均未命中返回 ErrTenantNotFound。
//
// 安全红线（DoD §6.4）：未激活（pending_dns/verifying/dns_verified/failed）的自定义域名绝不在此命中——
// status='active' 过滤由独立表 + 显式 WHERE 双重保证，wildcard 查询路径完全不受影响。
func (r *Repo) GetTenantByDomain(ctx context.Context, domain string) (*tenant.Tenant, error) {
	var d domainRow
	err := r.db.WithContext(ctx).Take(&d, "domain = ?", domain).Error
	if err == nil {
		return r.getActiveTenant(ctx, d.TenantID)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	// wildcard 未命中 → 仅命中 active 自定义域名。
	var cd customDomainRow
	cerr := r.db.WithContext(ctx).Take(&cd, "domain = ? AND status = ?", domain, string(tenant.CustomDomainActive)).Error
	if cerr != nil {
		if errors.Is(cerr, gorm.ErrRecordNotFound) {
			return nil, tenant.ErrTenantNotFound
		}
		return nil, cerr
	}
	return r.getActiveTenant(ctx, cd.TenantID)
}

// getActiveTenant 读租户，但已软删（status=deleted）的按「不存在」处理——删除代理后其残留域名绝不再
// 解析到站点（修补软删漏洞：Host 解析此前不看 status，删了站点仍能打开）。
func (r *Repo) getActiveTenant(ctx context.Context, id int64) (*tenant.Tenant, error) {
	t, err := r.GetTenant(ctx, id)
	if err != nil {
		return nil, err
	}
	if t.Status == tenant.StatusDeleted {
		return nil, tenant.ErrTenantNotFound
	}
	return t, nil
}

// DeleteDomainsByTenant 删除某租户全部 tenant_domains 子域名记录，返回被删域名（供失效 Host 缓存）。
func (r *Repo) DeleteDomainsByTenant(ctx context.Context, tenantID int64) ([]string, error) {
	var rows []domainRow
	if err := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	if err := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Delete(&domainRow{}).Error; err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Domain)
	}
	return out, nil
}

// GetPrimaryDomain 返回某租户的主子域名（is_primary，取最近一条）；无则空串。admin 列表/详情回显用。
func (r *Repo) GetPrimaryDomain(ctx context.Context, tenantID int64) string {
	if r == nil {
		return ""
	}
	var row domainRow
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND is_primary = ?", tenantID, true).
		Order("id desc").Take(&row).Error; err != nil {
		return ""
	}
	return row.Domain
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
		OwnerUserID:      row.OwnerUserID,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
	}, nil
}

// SetOwnerUserID 设置/改写租户的代理 owner（设代理流程在建租户后调用）。
// 行不存在返回 ErrTenantNotFound。这是「代理=User+Tenant 1:1」归属落地的写入点。
func (r *Repo) SetOwnerUserID(ctx context.Context, tenantID, ownerUserID int64) error {
	res := r.db.WithContext(ctx).Model(&tenantRow{}).
		Where("id = ?", tenantID).
		Update("owner_user_id", ownerUserID)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return tenant.ErrTenantNotFound
	}
	return nil
}

// OwnerUserID 直读某租户当前 owner_user_id（绕过解析缓存，供 agent_owner 鉴权做权威校验）。
// 行不存在返回 (0, ErrTenantNotFound)。
func (r *Repo) OwnerUserID(ctx context.Context, tenantID int64) (int64, error) {
	var row tenantRow
	if err := r.db.WithContext(ctx).Select("owner_user_id").Take(&row, "id = ?", tenantID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, tenant.ErrTenantNotFound
		}
		return 0, err
	}
	return row.OwnerUserID, nil
}

// TenantByOwner 直读某 owner_user_id 拥有的租户（owner→tenant 反查，OwnerUserID 的逆向；
// 走 idx_tenants_owner 索引）。业务上 owner 1:1 独占一租户（设代理时 ownerTaken 保证唯一）。
// owner_user_id<=0 直接返回 ErrTenantNotFound（主站/未归属租户默认 owner_user_id=0，绝不被空 session 误匹配）。
// 无匹配返回 (nil, ErrTenantNotFound)。供 owner-based 代理自助鉴权（AgentOwnerAuthByUser）做 Host 无关解析。
func (r *Repo) TenantByOwner(ctx context.Context, ownerUserID int64) (*tenant.Tenant, error) {
	if ownerUserID <= 0 {
		return nil, tenant.ErrTenantNotFound
	}
	var row tenantRow
	err := r.db.WithContext(ctx).Take(&row, "owner_user_id = ?", ownerUserID).Error
	return mapTenantResult(&row, err) // ErrRecordNotFound → tenant.ErrTenantNotFound
}

// ============================================================================
// 自定义域名持久化（tenant.CustomDomainRepo 实现，§6.2/§6.3）
// ============================================================================

// CreateCustomDomain 入库自定义域名并回填 ID/时间戳；唯一键冲突翻译为 tenant.ErrDomainTaken。
// （tenant_id 上限冲突由 service 先行 GetCustomDomainByTenant 拦截为 ErrDomainLimit。）
func (r *Repo) CreateCustomDomain(ctx context.Context, d *tenant.CustomDomain) error {
	row := customDomainRow{
		ID:            d.ID,
		TenantID:      d.TenantID,
		Domain:        d.Domain,
		Status:        string(d.Status),
		VerifyToken:   d.VerifyToken,
		CertStatus:    d.CertStatus,
		CertExpiresAt: d.CertExpiresAt,
		LastError:     d.LastError,
		CreatedAt:     d.CreatedAt,
		UpdatedAt:     d.UpdatedAt,
	}
	if row.Status == "" {
		row.Status = string(tenant.CustomDomainPendingDNS)
	}
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		if isDuplicate(err) {
			return tenant.ErrDomainTaken
		}
		return err
	}
	d.ID = row.ID
	d.Status = tenant.CustomDomainStatus(row.Status)
	d.CreatedAt = row.CreatedAt
	d.UpdatedAt = row.UpdatedAt
	return nil
}

// GetCustomDomainByTenant 按租户取唯一绑定；未找到返回 tenant.ErrCustomDomainNotFound。
func (r *Repo) GetCustomDomainByTenant(ctx context.Context, tenantID int64) (*tenant.CustomDomain, error) {
	var row customDomainRow
	err := r.db.WithContext(ctx).Take(&row, "tenant_id = ?", tenantID).Error
	return mapCustomDomainResult(&row, err)
}

// GetCustomDomainByName 按域名取绑定；未找到返回 tenant.ErrCustomDomainNotFound。
func (r *Repo) GetCustomDomainByName(ctx context.Context, domain string) (*tenant.CustomDomain, error) {
	var row customDomainRow
	err := r.db.WithContext(ctx).Take(&row, "domain = ?", domain).Error
	return mapCustomDomainResult(&row, err)
}

// UpdateCustomDomainStatus 改状态机状态与 last_error（GORM 自动维护 updated_at）；行不存在返回 ErrCustomDomainNotFound。
func (r *Repo) UpdateCustomDomainStatus(ctx context.Context, id int64, status tenant.CustomDomainStatus, lastError string) error {
	res := r.db.WithContext(ctx).Model(&customDomainRow{}).
		Where("id = ?", id).
		Updates(map[string]any{"status": string(status), "last_error": lastError})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return tenant.ErrCustomDomainNotFound
	}
	return nil
}

// UpdateCustomDomainCert 按域名回写证书字段并置目标状态（首签/续期）；行不存在返回 ErrCustomDomainNotFound。
func (r *Repo) UpdateCustomDomainCert(ctx context.Context, domain, certStatus string, expiresAt *time.Time, status tenant.CustomDomainStatus) error {
	res := r.db.WithContext(ctx).Model(&customDomainRow{}).
		Where("domain = ?", domain).
		Updates(map[string]any{
			"cert_status":     certStatus,
			"cert_expires_at": expiresAt,
			"status":          string(status),
			"last_error":      "",
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return tenant.ErrCustomDomainNotFound
	}
	return nil
}

// DeleteCustomDomainByTenant 删除租户绑定并返回被删域名；无绑定返回 ErrCustomDomainNotFound。
func (r *Repo) DeleteCustomDomainByTenant(ctx context.Context, tenantID int64) (string, error) {
	var row customDomainRow
	if err := r.db.WithContext(ctx).Take(&row, "tenant_id = ?", tenantID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", tenant.ErrCustomDomainNotFound
		}
		return "", err
	}
	if err := r.db.WithContext(ctx).Delete(&customDomainRow{}, "id = ?", row.ID).Error; err != nil {
		return "", err
	}
	return row.Domain, nil
}

// ListPendingCert 返回 status='dns_verified' 的全部绑定（供签发脚本消费）。
func (r *Repo) ListPendingCert(ctx context.Context) ([]tenant.CustomDomain, error) {
	var rows []customDomainRow
	if err := r.db.WithContext(ctx).
		Where("status = ?", string(tenant.CustomDomainDNSVerified)).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]tenant.CustomDomain, 0, len(rows))
	for i := range rows {
		out = append(out, *mapCustomDomain(&rows[i]))
	}
	return out, nil
}

// ListAllCustomDomains 返回全部自定义域名（admin 跨租户列表，按创建时间倒序）。
func (r *Repo) ListAllCustomDomains(ctx context.Context) ([]tenant.CustomDomain, error) {
	var rows []customDomainRow
	if err := r.db.WithContext(ctx).Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]tenant.CustomDomain, 0, len(rows))
	for i := range rows {
		out = append(out, *mapCustomDomain(&rows[i]))
	}
	return out, nil
}

// DeleteCustomDomainByID 按 id 删除并返回被删域名；不存在返回 ErrCustomDomainNotFound。
func (r *Repo) DeleteCustomDomainByID(ctx context.Context, id int64) (string, error) {
	var row customDomainRow
	if err := r.db.WithContext(ctx).Take(&row, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", tenant.ErrCustomDomainNotFound
		}
		return "", err
	}
	if err := r.db.WithContext(ctx).Delete(&customDomainRow{}, "id = ?", id).Error; err != nil {
		return "", err
	}
	return row.Domain, nil
}

// mapCustomDomainResult 把一次 Take 结果统一翻译为 domain 模型或 tenant 包错误码。
func mapCustomDomainResult(row *customDomainRow, err error) (*tenant.CustomDomain, error) {
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, tenant.ErrCustomDomainNotFound
		}
		return nil, err
	}
	return mapCustomDomain(row), nil
}

// mapCustomDomain 把 GORM 行映射为 domain 模型。
func mapCustomDomain(row *customDomainRow) *tenant.CustomDomain {
	return &tenant.CustomDomain{
		ID:            row.ID,
		TenantID:      row.TenantID,
		Domain:        row.Domain,
		Status:        tenant.CustomDomainStatus(row.Status),
		VerifyToken:   row.VerifyToken,
		CertStatus:    row.CertStatus,
		CertExpiresAt: row.CertExpiresAt,
		LastError:     row.LastError,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
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
