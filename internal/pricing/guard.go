package pricing

import (
	"net/http"

	"newapi-mt/internal/platform/apperr"
)

// guard 是 PricingGuard 的纯函数实现（无状态、无依赖）。
type guard struct{}

// NewGuard 构造成本保护守卫。可被 Agent / TokenPlan / Wallet 等模块复用。
func NewGuard() PricingGuard { return guard{} }

// ValidateGroupRatio 校验用户组倍率不低于主站保护下限 floor。
// 等于 floor 放行；低于返回 RATIO_BELOW_FLOOR。
func (guard) ValidateGroupRatio(ratio, floor float64) error {
	if ratio < floor {
		return apperr.New(
			CodeRatioBelowFloor,
			"用户组倍率低于主站保护下限",
			http.StatusBadRequest,
		)
	}
	return nil
}

// ValidateRetailPrice 校验零售价满足 retail >= cost*(1+minMargin)。
// 边界（恰好等于最低保护价）放行；低于或负利润返回 PRICE_BELOW_PROTECTION。
func (guard) ValidateRetailPrice(retail, cost, minMargin float64) error {
	floor := cost * (1 + minMargin)
	if retail < floor {
		return apperr.New(
			CodePriceBelowProtection,
			"零售价击穿成本保护线",
			http.StatusBadRequest,
		)
	}
	return nil
}
