package wallet

import (
	"net/http"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

// 本模块错误码命名空间（detailed-design §2.6 / §6.4）。
//
//   - QUOTA_INSUFFICIENT     —— 钱包桶（WalletQuota）余额不足（与 Billing/quota 命名空间一致，
//     由 quota.Source.Charge 上浮，见 quota.go 注释）。
//   - REDEEM_CODE_INVALID    —— 兑换码不存在 / 已禁用 / 已过期。
//   - REDEEM_CODE_USED       —— 兑换码已被使用（含并发竞态败者）。
//   - RECHARGE_ORDER_INVALID —— 充值/入账参数非法（如未绑定 tenant_id+user_id）。
//
// WALLET_AMOUNT_INVALID 为本轮补充的同命名空间防御性错误码（负数 / NaN / Inf 金额），
// 与 tenant 模块补充 SLUG_INVALID 的做法一致。
const (
	CodeQuotaInsufficient    = "QUOTA_INSUFFICIENT"
	CodeRedeemCodeInvalid    = "REDEEM_CODE_INVALID"
	CodeRedeemCodeUsed       = "REDEEM_CODE_USED"
	CodeRechargeOrderInvalid = "RECHARGE_ORDER_INVALID"
	CodeAmountInvalid        = "WALLET_AMOUNT_INVALID"
)

var (
	// ErrQuotaInsufficient 钱包余额不足以覆盖本次扣费（balance-cost<0）。
	ErrQuotaInsufficient = apperr.New(CodeQuotaInsufficient, "钱包余额不足", http.StatusPaymentRequired)
	// ErrRedeemCodeInvalid 兑换码不存在 / 已禁用 / 已过期。
	ErrRedeemCodeInvalid = apperr.New(CodeRedeemCodeInvalid, "兑换码无效", http.StatusBadRequest)
	// ErrRedeemCodeUsed 兑换码已被使用。
	ErrRedeemCodeUsed = apperr.New(CodeRedeemCodeUsed, "兑换码已被使用", http.StatusConflict)
	// ErrRechargeOrderInvalid 充值/入账参数非法（未绑定租户/用户等）。
	ErrRechargeOrderInvalid = apperr.New(CodeRechargeOrderInvalid, "充值/入账参数非法", http.StatusBadRequest)
	// ErrAmountInvalid 金额非法（负数 / NaN / Inf）。
	ErrAmountInvalid = apperr.New(CodeAmountInvalid, "金额非法", http.StatusBadRequest)
)
