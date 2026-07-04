package tenant

import (
	"context"
	"sync"
	"time"
)

// MemRepo 是 TenantRepo 的并发安全内存假实现，用于本轮纯逻辑开发与单测。
// 真实 GORM 实现（迁移 + slug/domain 唯一约束 + scopeByTenant）顺延（见报告 TODO）。
type MemRepo struct {
	mu           sync.RWMutex
	tenants      map[int64]Tenant
	slugIndex    map[string]int64
	domainIndex  map[string]int64 // 归一化域名 -> tenantID
	nextTenantID int64
	nextDomainID int64
	now          func() time.Time
}

// NewMemRepo 构造空的内存仓储。
func NewMemRepo() *MemRepo {
	return &MemRepo{
		tenants:     make(map[int64]Tenant),
		slugIndex:   make(map[string]int64),
		domainIndex: make(map[string]int64),
		now:         time.Now,
	}
}

func (r *MemRepo) CreateTenant(_ context.Context, t *Tenant) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.slugIndex[t.Slug]; ok {
		return ErrSlugDuplicate
	}
	r.nextTenantID++
	t.ID = r.nextTenantID
	ts := r.now()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = ts
	}
	t.UpdatedAt = ts
	if t.Status == "" {
		t.Status = StatusActive
	}
	r.tenants[t.ID] = *t
	r.slugIndex[t.Slug] = t.ID
	return nil
}

func (r *MemRepo) GetTenant(_ context.Context, id int64) (*Tenant, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tenants[id]
	if !ok {
		return nil, ErrTenantNotFound
	}
	cp := t
	return &cp, nil
}

func (r *MemRepo) GetTenantBySlug(_ context.Context, slug string) (*Tenant, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.slugIndex[slug]
	if !ok {
		return nil, ErrTenantNotFound
	}
	t := r.tenants[id]
	cp := t
	return &cp, nil
}

func (r *MemRepo) SetTenantStatus(_ context.Context, id int64, s TenantStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tenants[id]
	if !ok {
		return ErrTenantNotFound
	}
	t.Status = s
	t.UpdatedAt = r.now()
	r.tenants[id] = t
	return nil
}

func (r *MemRepo) CreateDomain(_ context.Context, d *TenantDomain) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.domainIndex[d.Domain]; ok {
		return ErrSlugDuplicate
	}
	r.nextDomainID++
	d.ID = r.nextDomainID
	if d.CreatedAt.IsZero() {
		d.CreatedAt = r.now()
	}
	r.domainIndex[d.Domain] = d.TenantID
	return nil
}

func (r *MemRepo) GetTenantByDomain(_ context.Context, domain string) (*Tenant, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.domainIndex[domain]
	if !ok {
		return nil, ErrTenantNotFound
	}
	t, ok := r.tenants[id]
	if !ok {
		return nil, ErrTenantNotFound
	}
	if t.Status == StatusDeleted {
		return nil, ErrTenantNotFound
	}
	cp := t
	return &cp, nil
}

// DeleteDomainsByTenant 删除某租户全部子域名映射，返回被删域名（内存实现，接口一致性 + 单测）。
func (r *MemRepo) DeleteDomainsByTenant(_ context.Context, tenantID int64) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var removed []string
	for domain, tid := range r.domainIndex {
		if tid == tenantID {
			removed = append(removed, domain)
			delete(r.domainIndex, domain)
		}
	}
	return removed, nil
}
