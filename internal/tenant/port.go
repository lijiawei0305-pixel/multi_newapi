package tenant

import "context"

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
}

// Cache 是 Host->Tenant 的解析缓存抽象。本轮提供内存假实现（MemCache）；
// 真实 Redis 适配（序列化 + TTL + 写后失效）顺延（见报告 TODO）。
type Cache interface {
	Get(ctx context.Context, host string) (*Tenant, bool)
	Set(ctx context.Context, host string, t *Tenant)
}
