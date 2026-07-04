package agentplan

import "context"

// catalog 是 PlanCatalog 的实现（管理员代理套餐 CRUD）。纯转发 + 入参校验，依赖 PlanRepo 注入。
type catalog struct {
	repo PlanRepo
}

// 编译期断言。
var _ PlanCatalog = (*catalog)(nil)

// NewCatalog 组装 PlanCatalog。
func NewCatalog(repo PlanRepo) PlanCatalog {
	return &catalog{repo: repo}
}

// Create 校验入参后入库；非法返回 AGENT_PLAN_INPUT_INVALID。
func (c *catalog) Create(ctx context.Context, in PlanInput) (*Plan, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	p := in.toPlan()
	if err := c.repo.CreatePlan(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// Update 校验入参后全量更新；不存在返回 AGENT_PLAN_NOT_FOUND。
func (c *catalog) Update(ctx context.Context, id int64, in PlanInput) error {
	if err := in.Validate(); err != nil {
		return err
	}
	return c.repo.UpdatePlan(ctx, id, in)
}

// Get 读取单个代理套餐。
func (c *catalog) Get(ctx context.Context, id int64) (*Plan, error) {
	return c.repo.GetPlan(ctx, id)
}

// List 列出全部代理套餐。
func (c *catalog) List(ctx context.Context) ([]Plan, error) {
	return c.repo.ListPlans(ctx)
}
