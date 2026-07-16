package agentplan

import (
	"net/http"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

// 本模块错误码命名空间（与 tokenplan 分开，避免语义混淆）。
const (
	// CodePlanNotFound 代理套餐不存在。
	CodePlanNotFound = "AGENT_PLAN_NOT_FOUND"
	// CodePlanInputInvalid 代理套餐入参非法。
	CodePlanInputInvalid = "AGENT_PLAN_INPUT_INVALID"
	// CodePlanDisabled 代理套餐已停用，禁止购买。
	CodePlanDisabled = "AGENT_PLAN_DISABLED"
	// CodeOrderNotFound 代理套餐待支付订单不存在。
	CodeOrderNotFound = "AGENT_PLAN_ORDER_NOT_FOUND"
	// CodeAgentSuspended 买家名下代理站已被管理员停用，禁止购买/升级/续费。
	CodeAgentSuspended = "AGENT_SUSPENDED"
)

var (
	// ErrPlanNotFound 代理套餐不存在。
	ErrPlanNotFound = apperr.New(CodePlanNotFound, "代理套餐不存在", http.StatusNotFound)
	// ErrPlanInputInvalid 代理套餐入参非法。
	ErrPlanInputInvalid = apperr.New(CodePlanInputInvalid, "代理套餐参数非法", http.StatusBadRequest)
	// ErrPlanDisabled 代理套餐已停用。
	ErrPlanDisabled = apperr.New(CodePlanDisabled, "代理套餐已停用", http.StatusConflict)
	// ErrOrderNotFound 代理套餐待支付订单不存在。
	ErrOrderNotFound = apperr.New(CodeOrderNotFound, "代理套餐订单不存在", http.StatusNotFound)
	// ErrAgentSuspended 买家名下代理站已被管理员停用——付款前拒单（fail-closed），避免向停用代理收钱后
	// 仍无法交付（agent 自助鉴权 TenantByOwner 排除 suspended，付了钱仍登不进后台）。解封须管理员显式执行。
	ErrAgentSuspended = apperr.New(CodeAgentSuspended, "你的代理站已被停用，请联系客服", http.StatusForbidden)
)
