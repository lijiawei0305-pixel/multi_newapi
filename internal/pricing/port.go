package pricing

import "context"

// 错误码（apperr.Code 命名空间：PRICING / 成本保护）。
// 详见 doc/detailed-design.md §2.4。
const (
	// CodeRatioBelowFloor 用户组倍率低于主站保护下限（group_floor_ratio）。
	CodeRatioBelowFloor = "RATIO_BELOW_FLOOR"
	// CodePriceBelowProtection 零售价/模型价击穿成本保护线（min_margin / min_floor_price）。
	CodePriceBelowProtection = "PRICE_BELOW_PROTECTION"
)

// ModelPrice 表示某租户下某模型的计费价，并携带成本保护下限。
//
// InputPrice/OutputPrice 为输入/输出单价（USD，单位与上游 ModelCatalog 一致）；
// MinFloorPrice 为成本保护下限：任一单价低于它即视为击穿保护线（PRICE_BELOW_PROTECTION）。
type ModelPrice struct {
	Model         string
	InputPrice    float64
	OutputPrice   float64
	MinFloorPrice float64
}

// PricingService 提供租户维度的倍率与模型价查询（对外接口）。
// 见 doc/detailed-design.md §2.4。
type PricingService interface {
	// GroupRatio 返回租户下指定分组的有效倍率；无分组专属倍率时回退默认倍率。
	GroupRatio(ctx context.Context, tenantID, groupID int64) (float64, error)
	// ModelPrice 返回租户下某模型的价（含 floor）；低于 floor 拦截。
	ModelPrice(ctx context.Context, tenantID int64, model string) (ModelPrice, error)
}

// PricingGuard 是纯函数式的成本保护守卫（无依赖、零 IO）。
// 分组倍率与 tokenplan 零售价共用同一守卫。见 doc/detailed-design.md §2.4 / doc/proposal.md §13.3。
type PricingGuard interface {
	// ValidateGroupRatio 校验用户组倍率不低于保护下限 floor；低于返回 RATIO_BELOW_FLOOR。
	ValidateGroupRatio(ratio, floor float64) error
	// ValidateRetailPrice 校验零售价满足 retail >= cost*(1+minMargin)；否则返回 PRICE_BELOW_PROTECTION。
	ValidateRetailPrice(retail, cost, minMargin float64) error
}

// PricingRepo 是 PricingService 的消费者定义依赖接口（本包声明，main 注入实现）。
// PricingGuard 为纯函数无需此依赖。见 doc/detailed-design.md §1.4。
type PricingRepo interface {
	// GroupRatio 返回租户下指定分组的专属倍率；不存在返回 found=false。
	GroupRatio(ctx context.Context, tenantID, groupID int64) (ratio float64, found bool, err error)
	// DefaultGroupRatio 返回租户的默认倍率（无分组专属时的回退值）。
	DefaultGroupRatio(ctx context.Context, tenantID int64) (float64, error)
	// ModelPrice 返回租户下某模型的价记录（含 MinFloorPrice）。
	ModelPrice(ctx context.Context, tenantID int64, model string) (ModelPrice, error)
}
