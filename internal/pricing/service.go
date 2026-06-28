package pricing

import (
	"context"
	"net/http"

	"newapi-mt/internal/platform/apperr"
)

// service 是 PricingService 的实现，依赖 PricingRepo（由 main 注入）。
type service struct {
	repo PricingRepo
}

// NewService 构造 PricingService。
func NewService(repo PricingRepo) PricingService {
	return &service{repo: repo}
}

// GroupRatio 返回租户下指定分组的有效倍率：
// 优先返回分组专属倍率；无专属时回退租户默认倍率。
func (s *service) GroupRatio(ctx context.Context, tenantID, groupID int64) (float64, error) {
	ratio, found, err := s.repo.GroupRatio(ctx, tenantID, groupID)
	if err != nil {
		return 0, err
	}
	if found {
		return ratio, nil
	}
	return s.repo.DefaultGroupRatio(ctx, tenantID)
}

// ModelPrice 返回租户下某模型的价；若任一单价低于 MinFloorPrice 则拦截。
func (s *service) ModelPrice(ctx context.Context, tenantID int64, model string) (ModelPrice, error) {
	mp, err := s.repo.ModelPrice(ctx, tenantID, model)
	if err != nil {
		return ModelPrice{}, err
	}
	if mp.InputPrice < mp.MinFloorPrice || mp.OutputPrice < mp.MinFloorPrice {
		return ModelPrice{}, apperr.New(
			CodePriceBelowProtection,
			"模型价低于最低成本保护价",
			http.StatusBadRequest,
		)
	}
	return mp, nil
}
