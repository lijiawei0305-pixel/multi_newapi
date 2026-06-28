package siteconfig

import "context"

// service 是 SiteConfigService 的实现，依赖 SiteConfigRepo（由 main 注入）。
type service struct {
	repo SiteConfigRepo
}

// NewService 构造 SiteConfigService。
func NewService(repo SiteConfigRepo) SiteConfigService {
	return &service{repo: repo}
}

// Get 返回租户站点配置；未配置（found=false）时回退主站默认值，
// 并把 TenantID 回填为请求租户，便于调用方识别"有效配置归属"。
func (s *service) Get(ctx context.Context, tenantID int64) (*SiteConfig, error) {
	cfg, found, err := s.repo.GetConfig(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if !found {
		d := MainSiteDefault()
		d.TenantID = tenantID
		return d, nil
	}
	return cfg, nil
}

// Patch 校验受控字段后，在"现有配置或主站默认"基线上叠加局部更新并落库。
// 校验失败（THEME_NOT_IN_PALETTE / HOME_MODE_LOCKED）时不触碰存储。
func (s *service) Patch(ctx context.Context, tenantID int64, in SiteConfigPatch) error {
	if err := ValidatePatch(in); err != nil {
		return err
	}
	cfg, found, err := s.repo.GetConfig(ctx, tenantID)
	if err != nil {
		return err
	}
	var cur SiteConfig
	if found {
		cur = *cfg
	} else {
		cur = *MainSiteDefault()
	}
	cur.TenantID = tenantID
	applyPatch(&cur, in)
	return s.repo.UpsertConfig(ctx, &cur)
}
