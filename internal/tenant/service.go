package tenant

import (
	"context"
	"errors"
)

type tenantService struct {
	repo TenantRepo
	slug SlugValidator
}

// NewService 组装 TenantService。slug 校验器与持久化以接口注入，便于单测。
func NewService(repo TenantRepo, slug SlugValidator) TenantService {
	return &tenantService{repo: repo, slug: slug}
}

// Get 按 id 读取租户；不存在返回 ErrTenantNotFound。
func (s *tenantService) Get(ctx context.Context, id int64) (*Tenant, error) {
	return s.repo.GetTenant(ctx, id)
}

// Create 校验 slug -> 入库租户 -> 自动写 `<slug>.wedreamhub.com` 域名记录。
// slug 保留/格式非法 -> ErrSlugReserved/ErrSlugInvalid；重复 -> ErrSlugDuplicate。
func (s *tenantService) Create(ctx context.Context, in CreateTenantInput) (*Tenant, error) {
	if err := s.slug.Validate(in.Slug); err != nil {
		return nil, err
	}
	t := &Tenant{
		Slug:             in.Slug,
		Name:             in.Name,
		Status:           StatusActive,
		TokenplanEnabled: in.TokenplanEnabled,
	}
	if err := s.repo.CreateTenant(ctx, t); err != nil {
		return nil, err // 含 ErrSlugDuplicate
	}
	if !in.SkipSubdomain {
		d := &TenantDomain{
			TenantID:  t.ID,
			Domain:    DomainForSlug(in.Slug),
			IsPrimary: true,
		}
		if err := s.repo.CreateDomain(ctx, d); err != nil {
			return nil, err
		}
	}
	return t, nil
}

// EnsureSubdomain 幂等派生二级域名 `<slug>.wedreamhub.com`（管理员升档时调用）。
// 域名已存在（映射到本租户）时 CreateDomain 返回 ErrSlugDuplicate，视为已就绪 → nil。
func (s *tenantService) EnsureSubdomain(ctx context.Context, tenantID int64, slug string) error {
	d := &TenantDomain{TenantID: tenantID, Domain: DomainForSlug(slug), IsPrimary: true}
	if err := s.repo.CreateDomain(ctx, d); err != nil {
		if errors.Is(err, ErrSlugDuplicate) {
			return nil // 已派生：幂等
		}
		return err
	}
	return nil
}

// SetStatus 按状态机迁移租户状态；非法目标/迁移返回 ErrStatusTransition；
// 租户不存在返回 ErrTenantNotFound。
func (s *tenantService) SetStatus(ctx context.Context, id int64, next TenantStatus) error {
	if !next.Valid() {
		return ErrStatusTransition
	}
	cur, err := s.repo.GetTenant(ctx, id)
	if err != nil {
		return err
	}
	if !cur.Status.CanTransitionTo(next) {
		return ErrStatusTransition
	}
	return s.repo.SetTenantStatus(ctx, id, next)
}
