package tokenplan

import (
	"net/http"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

// 本模块错误码命名空间（detailed-design §2.7 / §6.4）。
//
// 设计文档 §2.7 列出：PLAN_DISABLED、RETAIL_BELOW_MIN、PURCHASE_LIMIT_EXCEEDED、
// SUBSCRIPTION_EXHAUSTED、SUBSCRIPTION_EXPIRED、PLAN_NOT_LISTED。
//
// SUBSCRIPTION_EXHAUSTED / SUBSCRIPTION_EXPIRED 与 quota / billing 命名空间共用同一字面值
// （由 quota.Source.Charge 上浮，见 billing/port.go），二者必须严格一致。
//
// PLAN_NOT_FOUND / SUBSCRIPTION_NOT_FOUND / PLAN_INPUT_INVALID / TOKENPLAN_AMOUNT_INVALID /
// REFUND_NOT_SUPPORTED 为本轮补充的同命名空间错误码（与 wallet 补充 WALLET_AMOUNT_INVALID 同思路）。
const (
	// CodePlanDisabled 套餐被主站停用，禁止上架/购买。
	CodePlanDisabled = "PLAN_DISABLED"
	// CodePlanNotListed 该套餐未被当前代理上架。
	CodePlanNotListed = "PLAN_NOT_LISTED"
	// CodePlanNotFound 套餐不存在（补充码）。
	CodePlanNotFound = "PLAN_NOT_FOUND"
	// CodeRetailBelowMin 代理零售价击穿成本保护线（retail < min_price）。
	CodeRetailBelowMin = "RETAIL_BELOW_MIN"
	// CodePurchaseLimitExceeded 触发限购（尤其 Trial：用户/实名/设备各 1 次）。
	CodePurchaseLimitExceeded = "PURCHASE_LIMIT_EXCEEDED"
	// CodeSubscriptionExhausted 套餐月额度用尽（终态，需手动重购）。与 quota/billing 一致。
	CodeSubscriptionExhausted = "SUBSCRIPTION_EXHAUSTED"
	// CodeSubscriptionExpired 套餐已过期（expire_at < now）。与 quota/billing 一致。
	CodeSubscriptionExpired = "SUBSCRIPTION_EXPIRED"
	// CodeSubscriptionNotFound 订阅实例/待支付订单不存在（补充码）。
	CodeSubscriptionNotFound = "SUBSCRIPTION_NOT_FOUND"
	// CodePlanInputInvalid 套餐入参非法（补充码）。
	CodePlanInputInvalid = "PLAN_INPUT_INVALID"
	// CodeAmountInvalid 金额非法（负数 / NaN / Inf；防御性补充码）。
	CodeAmountInvalid = "TOKENPLAN_AMOUNT_INVALID"
	// CodeRefundNotSupported 一期不支持套餐退款（§7 默认 #5）。
	CodeRefundNotSupported = "REFUND_NOT_SUPPORTED"
)

var (
	// ErrPlanDisabled 套餐被主站停用。
	ErrPlanDisabled = apperr.New(CodePlanDisabled, "套餐已停用", http.StatusConflict)
	// ErrPlanNotListed 套餐未被当前代理上架。
	ErrPlanNotListed = apperr.New(CodePlanNotListed, "套餐未上架", http.StatusNotFound)
	// ErrPlanNotFound 套餐不存在。
	ErrPlanNotFound = apperr.New(CodePlanNotFound, "套餐不存在", http.StatusNotFound)
	// ErrRetailBelowMin 代理零售价击穿成本保护线。
	ErrRetailBelowMin = apperr.New(CodeRetailBelowMin, "零售价低于成本保护线", http.StatusBadRequest)
	// ErrPurchaseLimitExceeded 触发限购。
	ErrPurchaseLimitExceeded = apperr.New(CodePurchaseLimitExceeded, "已达套餐限购次数", http.StatusConflict)
	// ErrSubscriptionExhausted 套餐月额度用尽。
	ErrSubscriptionExhausted = apperr.New(CodeSubscriptionExhausted, "套餐额度已用尽，请重新购买", http.StatusPaymentRequired)
	// ErrSubscriptionExpired 套餐已过期。
	ErrSubscriptionExpired = apperr.New(CodeSubscriptionExpired, "套餐已过期，请重新购买", http.StatusPaymentRequired)
	// ErrSubscriptionNotFound 订阅实例/待支付订单不存在。
	ErrSubscriptionNotFound = apperr.New(CodeSubscriptionNotFound, "订阅或订单不存在", http.StatusNotFound)
	// ErrPlanInputInvalid 套餐入参非法。
	ErrPlanInputInvalid = apperr.New(CodePlanInputInvalid, "套餐参数非法", http.StatusBadRequest)
	// ErrAmountInvalid 金额非法（负数 / NaN / Inf）。
	ErrAmountInvalid = apperr.New(CodeAmountInvalid, "金额非法", http.StatusBadRequest)
	// ErrRefundNotSupported 一期不支持套餐退款。
	ErrRefundNotSupported = apperr.New(CodeRefundNotSupported, "暂不支持套餐退款", http.StatusNotImplemented)
)
