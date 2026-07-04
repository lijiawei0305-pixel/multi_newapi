package agentplan

import "context"

// PlanCatalog 是管理员代理套餐 CRUD（纯转发 + 入参校验）。
type PlanCatalog interface {
	// Create 新建代理套餐；入参非法返回 AGENT_PLAN_INPUT_INVALID。
	Create(ctx context.Context, in PlanInput) (*Plan, error)
	// Update 全量更新；不存在返回 AGENT_PLAN_NOT_FOUND，入参非法返回 AGENT_PLAN_INPUT_INVALID。
	Update(ctx context.Context, id int64, in PlanInput) error
	// Get 读取单个代理套餐；不存在返回 AGENT_PLAN_NOT_FOUND。
	Get(ctx context.Context, id int64) (*Plan, error)
	// List 列出全部代理套餐（按 Sort 升序）。
	List(ctx context.Context) ([]Plan, error)
}

// PlanRepo 是代理套餐定义的持久化抽象（真实实现在 gormrepo；本包只声明契约）。
type PlanRepo interface {
	// CreatePlan 入库并回填 p.ID / 时间戳；code 冲突返回 ErrPlanInputInvalid。
	CreatePlan(ctx context.Context, p *Plan) error
	// UpdatePlan 全量更新；不存在返回 ErrPlanNotFound。
	UpdatePlan(ctx context.Context, id int64, in PlanInput) error
	// GetPlan 按 id 读取；不存在返回 ErrPlanNotFound。
	GetPlan(ctx context.Context, id int64) (*Plan, error)
	// ListPlans 返回全部套餐（按 Sort 升序）。
	ListPlans(ctx context.Context) ([]Plan, error)
}
