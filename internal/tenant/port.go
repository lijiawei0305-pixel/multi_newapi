package tenant

import (
	"context"
	"time"
)

// --- 对外接口（detailed-design §2.1 的 Go 签名）---

// TenantResolver 按 Host 解析租户（缓存 + 回源），未命中返回 ErrTenantNotFound。
type TenantResolver interface {
	ResolveByHost(ctx context.Context, host string) (*Tenant, error)
}

// TenantService 提供租户 CRUD 与状态机。
type TenantService interface {
	Get(ctx context.Context, id int64) (*Tenant, error)
	// Create 建租户并自动写一条二级域名记录 `<slug>.wedreamhub.com`。
	Create(ctx context.Context, in CreateTenantInput) (*Tenant, error)
	// EnsureSubdomain 幂等派生 `<slug>.wedreamhub.com` 域名映射（管理员升档 L0→L1 时调用）；已存在则无操作。
	EnsureSubdomain(ctx context.Context, tenantID int64, slug string) error
	// AddSubdomain 管理员为租户设一个指定 label 的子域名 `<label>.wedreamhub.com`（校验 label + 全局查重 +
	// 替换该租户现有主子域名）。返回新域名与被替换掉的旧域名列表（供装配层失效 Host 缓存）。
	AddSubdomain(ctx context.Context, tenantID int64, label string) (domain string, removed []string, err error)
	SetStatus(ctx context.Context, id int64, s TenantStatus) error
}

// SlugValidator 为纯函数式 slug 校验（保留词 + 格式）。唯一性在 TenantService.Create 处通过 Repo 强制。
type SlugValidator interface {
	Validate(slug string) error
}

// CreateTenantInput 是 TenantService.Create 的入参。
type CreateTenantInput struct {
	Slug             string
	Name             string
	TokenplanEnabled bool
	// SkipSubdomain=true 时不派生 `<slug>.wedreamhub.com` 域名映射（普通档 L0 代理无独立子域名，
	// spec §5.2.2）。零值 false = 保持既有行为（派生子域名）。
	SkipSubdomain bool
}

// --- 消费者定义的依赖接口（本包声明，main 装配具体实现）---

// TenantRepo 是租户持久化抽象。本轮提供内存假实现（MemRepo）；
// 真实 GORM 实现 + slug/domain 唯一约束顺延（见报告 TODO）。
type TenantRepo interface {
	// CreateTenant 入库并回填 t.ID；slug 冲突返回 ErrSlugDuplicate。
	CreateTenant(ctx context.Context, t *Tenant) error
	GetTenant(ctx context.Context, id int64) (*Tenant, error)
	GetTenantBySlug(ctx context.Context, slug string) (*Tenant, error)
	SetTenantStatus(ctx context.Context, id int64, s TenantStatus) error
	// CreateDomain 写入域名映射并回填 d.ID；域名冲突返回 ErrSlugDuplicate。
	CreateDomain(ctx context.Context, d *TenantDomain) error
	GetTenantByDomain(ctx context.Context, domain string) (*Tenant, error)
	// DeleteDomainsByTenant 删除某租户在 tenant_domains 的全部子域名记录，返回被删域名（供失效 Host 缓存）。
	DeleteDomainsByTenant(ctx context.Context, tenantID int64) ([]string, error)
}

// Cache 是 Host->Tenant 的解析缓存抽象。本轮提供内存假实现（MemCache）；
// 真实 Redis 适配（序列化 + TTL）顺延（见报告 TODO）。
type Cache interface {
	Get(ctx context.Context, host string) (*Tenant, bool)
	Set(ctx context.Context, host string, t *Tenant)
	// Invalidate 主动失效一个 Host 的缓存条目（写后失效）。
	// 自定义域名转 active / 解绑后由装配层调用，避免旧解析结果泄露或滞留。
	Invalidate(ctx context.Context, host string)
}

// --- 自定义域名（OEM，二期 §6.2/§6.3）---

// CustomDomainService 是代理自助绑定自定义域名的领域服务（owner 维度）。
// 安全不变量：未 active 的绑定绝不参与 Host 解析（见 ResolveByHost / GetTenantByDomain）。
type CustomDomainService interface {
	// Bind 校验格式/保留词/每租户上限后落一条 pending_dns 记录（不进解析），返回含 verify_token 的绑定。
	Bind(ctx context.Context, tenantID int64, domain string) (*CustomDomain, error)
	// VerifyOwnership 查 TXT 记录校验所有权；通过则转 dns_verified（触发异步发证信号），否则转 failed。
	VerifyOwnership(ctx context.Context, tenantID int64) (*CustomDomain, error)
	// Unbind 删除绑定并返回被删域名（供装配层失效其 Host 缓存）。
	Unbind(ctx context.Context, tenantID int64) (deletedDomain string, err error)
	// GetByTenant 返回当前绑定及状态（未绑定返回 ErrCustomDomainNotFound）。
	GetByTenant(ctx context.Context, tenantID int64) (*CustomDomain, error)
	// ListPendingCert 返回待签发证书（status=dns_verified）的绑定，供服务器侧签发脚本消费。
	ListPendingCert(ctx context.Context) ([]CustomDomain, error)
	// MarkCertIssued 由内网回写端点调用：证书就绪后置 cert 字段并转 active，返回被激活域名（供失效缓存）。
	MarkCertIssued(ctx context.Context, domain, certStatus string, expiresAt *time.Time) (activatedHost string, err error)
	// ListAll 返回全部租户的自定义域名（主站 admin 跨租户视角）。
	ListAll(ctx context.Context) ([]CustomDomain, error)
	// UnbindByID 主站 admin 按记录 id 强制解绑，返回被删域名（供失效缓存）。
	UnbindByID(ctx context.Context, id int64) (deletedDomain string, err error)
}

// CustomDomainRepo 是自定义域名持久化抽象（消费者定义）。生产由 gormrepo.Repo 实现，单测用内存假实现。
type CustomDomainRepo interface {
	// CreateCustomDomain 入库并回填 ID/时间戳；域名全局冲突返回 ErrDomainTaken。
	CreateCustomDomain(ctx context.Context, d *CustomDomain) error
	// GetCustomDomainByTenant 按租户取唯一绑定；无绑定返回 ErrCustomDomainNotFound。
	GetCustomDomainByTenant(ctx context.Context, tenantID int64) (*CustomDomain, error)
	// GetCustomDomainByName 按域名取绑定；未找到返回 ErrCustomDomainNotFound。
	GetCustomDomainByName(ctx context.Context, domain string) (*CustomDomain, error)
	// UpdateCustomDomainStatus 改状态机状态与 last_error（GORM 维护 updated_at）。
	UpdateCustomDomainStatus(ctx context.Context, id int64, status CustomDomainStatus, lastError string) error
	// UpdateCustomDomainCert 按域名回写证书字段并置目标状态（发证/续期）。
	UpdateCustomDomainCert(ctx context.Context, domain, certStatus string, expiresAt *time.Time, status CustomDomainStatus) error
	// DeleteCustomDomainByTenant 删除租户绑定，返回被删域名（无绑定返回 ErrCustomDomainNotFound）。
	DeleteCustomDomainByTenant(ctx context.Context, tenantID int64) (string, error)
	// ListPendingCert 返回 status=dns_verified 的全部绑定。
	ListPendingCert(ctx context.Context) ([]CustomDomain, error)
	// ListAllCustomDomains 返回全部自定义域名（admin 跨租户列表，按创建时间倒序）。
	ListAllCustomDomains(ctx context.Context) ([]CustomDomain, error)
	// DeleteCustomDomainByID 按 id 删除并返回被删域名（不存在返回 ErrCustomDomainNotFound）。
	DeleteCustomDomainByID(ctx context.Context, id int64) (string, error)
}

// DNSVerifier 抽象 TXT 记录查询（默认包裹 net.LookupTXT，单测可注入桩）。
type DNSVerifier interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
}
