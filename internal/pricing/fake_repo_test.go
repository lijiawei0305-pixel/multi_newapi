package pricing

import "context"

// fakeRepo 是 PricingRepo 的内存假实现，仅供单测使用（零外部依赖）。
type fakeRepo struct {
	// groupRatios: tenantID -> groupID -> 专属倍率。
	groupRatios map[int64]map[int64]float64
	// defaultRatios: tenantID -> 默认倍率（回退值）。
	defaultRatios map[int64]float64
	// modelPrices: tenantID -> model -> 价记录。
	modelPrices map[int64]map[string]ModelPrice

	// 可选注入错误，用于覆盖错误传播路径。
	groupErr   error
	defaultErr error
	modelErr   error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		groupRatios:   map[int64]map[int64]float64{},
		defaultRatios: map[int64]float64{},
		modelPrices:   map[int64]map[string]ModelPrice{},
	}
}

func (f *fakeRepo) setGroupRatio(tenantID, groupID int64, ratio float64) {
	if f.groupRatios[tenantID] == nil {
		f.groupRatios[tenantID] = map[int64]float64{}
	}
	f.groupRatios[tenantID][groupID] = ratio
}

func (f *fakeRepo) setDefaultRatio(tenantID int64, ratio float64) {
	f.defaultRatios[tenantID] = ratio
}

func (f *fakeRepo) setModelPrice(tenantID int64, mp ModelPrice) {
	if f.modelPrices[tenantID] == nil {
		f.modelPrices[tenantID] = map[string]ModelPrice{}
	}
	f.modelPrices[tenantID][mp.Model] = mp
}

func (f *fakeRepo) GroupRatio(_ context.Context, tenantID, groupID int64) (float64, bool, error) {
	if f.groupErr != nil {
		return 0, false, f.groupErr
	}
	if byGroup, ok := f.groupRatios[tenantID]; ok {
		if r, ok := byGroup[groupID]; ok {
			return r, true, nil
		}
	}
	return 0, false, nil
}

func (f *fakeRepo) DefaultGroupRatio(_ context.Context, tenantID int64) (float64, error) {
	if f.defaultErr != nil {
		return 0, f.defaultErr
	}
	return f.defaultRatios[tenantID], nil
}

func (f *fakeRepo) ModelPrice(_ context.Context, tenantID int64, model string) (ModelPrice, error) {
	if f.modelErr != nil {
		return ModelPrice{}, f.modelErr
	}
	if byModel, ok := f.modelPrices[tenantID]; ok {
		if mp, ok := byModel[model]; ok {
			return mp, nil
		}
	}
	return ModelPrice{}, nil
}
