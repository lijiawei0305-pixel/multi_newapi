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

// AddSubdomain 管理员为租户设一个指定 label 的子域名（doc/agent-subdomain-and-delete.md §一）：
// 校验 label（格式/保留词）→ 全局查重（已被他租户占用 → ErrDomainTaken；已是本租户该域名 → 幂等返回）
// → 替换本租户现有主子域名（删旧建新）。返回新域名 + 被删旧域名（装配层据此失效 Host 缓存）。
func (s *tenantService) AddSubdomain(ctx context.Context, tenantID int64, label string) (string, []string, error) {
	if err := s.slug.Validate(label); err != nil {
		return "", nil, err
	}
	domain := DomainForSlug(label)
	existing, err := s.repo.GetTenantByDomain(ctx, domain)
	if err == nil && existing != nil {
		if existing.ID == tenantID {
			return domain, nil, nil // 已是本租户该子域名：幂等
		}
		return "", nil, ErrDomainTaken
	}
	if err != nil && !errors.Is(err, ErrTenantNotFound) {
		return "", nil, err
	}
	removed, err := s.repo.DeleteDomainsByTenant(ctx, tenantID)
	if err != nil {
		return "", nil, err
	}
	if err := s.repo.CreateDomain(ctx, &TenantDomain{TenantID: tenantID, Domain: domain, IsPrimary: true}); err != nil {
		return "", nil, err
	}
	return domain, removed, nil
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
