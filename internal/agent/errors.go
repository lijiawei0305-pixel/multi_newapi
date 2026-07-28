package agent

import (
	"net/http"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

// 本模块错误码命名空间（detailed-design §2.3 / §6.4）。
// AGENT_TYPE_INVALID / WITHDRAW_INSUFFICIENT / WITHDRAW_NOT_PENDING 见设计文档；
// WITHDRAW_NOT_FOUND（提现单不存在）与 EARNING_INVALID（收益条目非法）为本轮补充的同命名空间错误码。
// WITHDRAW_NOT_APPROVED / PAYOUT_ACCOUNT_REQUIRED / PAYOUT_ACCOUNT_INVALID / PAYOUT_REF_REQUIRED
// 为提现闭环补强（收款账户 + 已打款状态凭证 + 驳回理由）新增。
const (
	// CodeAgentTypeInvalid 代理类型或 AgentParams 参数非法。
	CodeAgentTypeInvalid = "AGENT_TYPE_INVALID"
	// CodeWithdrawInsufficient 提现金额超过可提现余额（或非正）。
	CodeWithdrawInsufficient = "WITHDRAW_INSUFFICIENT"
	// CodeWithdrawAmountInvalid 提现金额不是人民币“分”的整数倍。
	CodeWithdrawAmountInvalid = "WITHDRAW_AMOUNT_INVALID"
	// CodeWithdrawRequestKeyInvalid 提现幂等键过长。
	CodeWithdrawRequestKeyInvalid = "WITHDRAW_REQUEST_KEY_INVALID"
	// CodeWithdrawIdempotencyConflict 同一幂等键被复用于不同提现参数。
	CodeWithdrawIdempotencyConflict = "WITHDRAW_IDEMPOTENCY_CONFLICT"
	// CodeWithdrawNotPending 提现单非 pending，不可再次审核（approve/reject）。
	CodeWithdrawNotPending = "WITHDRAW_NOT_PENDING"
	// CodeWithdrawNotFound 提现单不存在（补充码）。
	CodeWithdrawNotFound = "WITHDRAW_NOT_FOUND"
	// CodeEarningInvalid 收益条目非法：来源类型不合法或来源 ID 缺失（补充码）。
	CodeEarningInvalid = "EARNING_INVALID"
	// CodeWithdrawNotApproved 提现单非 approved，不能标记已打款（mark-paid 的 CAS 失败码）。
	CodeWithdrawNotApproved = "WITHDRAW_NOT_APPROVED"
	// CodePayoutAccountRequired 申请提现前尚未设置收款账户。
	CodePayoutAccountRequired = "PAYOUT_ACCOUNT_REQUIRED"
	// CodePayoutAccountInvalid 收款账户参数非法（方式/账号/姓名/开户行）。
	CodePayoutAccountInvalid = "PAYOUT_ACCOUNT_INVALID"
	// CodePayoutRefRequired 标记已打款时打款单号/凭证缺失。
	CodePayoutRefRequired = "PAYOUT_REF_REQUIRED"
	// CodeWithdrawalRemarkInvalid 审核备注超过持久化字段长度限制。
	CodeWithdrawalRemarkInvalid = "WITHDRAW_REMARK_INVALID"
	// CodePayoutRefInvalid 打款单号/凭证超过持久化字段长度限制。
	CodePayoutRefInvalid = "PAYOUT_REF_INVALID"
	// CodePayoutRefDuplicate 打款凭证已绑定其他提现单。
	CodePayoutRefDuplicate = "PAYOUT_REF_DUPLICATE"
	// CodeWalletInvariant 账务数据不满足钱包/冻结余额不变量。
	CodeWalletInvariant = "AGENT_WALLET_INVARIANT"
)

var (
	// ErrAgentParamsInvalid 代理参数非法（成本价/分润/折扣/等级等 AgentParams 校验失败）。
	// 错误码常量保持 CodeAgentTypeInvalid（"AGENT_TYPE_INVALID"）不变，对外契约稳定。
	ErrAgentParamsInvalid = apperr.New(CodeAgentTypeInvalid, "代理参数非法", http.StatusBadRequest)
	// ErrWithdrawInsufficient 提现金额超过可提现余额。
	ErrWithdrawInsufficient = apperr.New(CodeWithdrawInsufficient, "提现金额超过可提现余额", http.StatusBadRequest)
	// ErrWithdrawAmountInvalid 提现金额必须精确到分，避免界面两位金额与线下打款金额不一致。
	ErrWithdrawAmountInvalid = apperr.New(CodeWithdrawAmountInvalid, "提现金额最多保留两位小数", http.StatusBadRequest)
	// ErrWithdrawRequestKeyInvalid 提现请求幂等键非法。
	ErrWithdrawRequestKeyInvalid = apperr.New(CodeWithdrawRequestKeyInvalid, "提现请求幂等键非法", http.StatusBadRequest)
	// ErrWithdrawIdempotencyConflict 同一幂等键对应的请求参数不一致。
	ErrWithdrawIdempotencyConflict = apperr.New(CodeWithdrawIdempotencyConflict, "提现幂等键已用于另一笔请求", http.StatusConflict)
	// ErrWithdrawNotPending 提现单非 pending，不可再次审核。
	ErrWithdrawNotPending = apperr.New(CodeWithdrawNotPending, "提现单非待审核状态", http.StatusConflict)
	// ErrWithdrawNotFound 提现单不存在。
	ErrWithdrawNotFound = apperr.New(CodeWithdrawNotFound, "提现单不存在", http.StatusNotFound)
	// ErrEarningInvalid 收益条目非法（来源类型或来源 ID 无效）。
	ErrEarningInvalid = apperr.New(CodeEarningInvalid, "收益条目非法（来源类型或来源 ID 无效）", http.StatusBadRequest)
	// ErrWithdrawNotApproved 提现单非已通过状态，不能标记已打款。
	ErrWithdrawNotApproved = apperr.New(CodeWithdrawNotApproved, "提现单非已通过状态，不能标记已打款", http.StatusConflict)
	// ErrPayoutAccountRequired 申请提现前尚未设置收款账户。
	ErrPayoutAccountRequired = apperr.New(CodePayoutAccountRequired, "请先设置收款账户后再申请提现", http.StatusBadRequest)
	// ErrPayoutAccountInvalid 收款账户信息非法。
	ErrPayoutAccountInvalid = apperr.New(CodePayoutAccountInvalid, "收款账户信息非法", http.StatusBadRequest)
	// ErrPayoutRefRequired 标记已打款时打款单号/凭证缺失。
	ErrPayoutRefRequired = apperr.New(CodePayoutRefRequired, "请填写打款单号/凭证", http.StatusBadRequest)
	// ErrWithdrawalRemarkInvalid 审核备注过长。
	ErrWithdrawalRemarkInvalid = apperr.New(CodeWithdrawalRemarkInvalid, "审核备注过长", http.StatusBadRequest)
	// ErrPayoutRefInvalid 打款单号/凭证过长。
	ErrPayoutRefInvalid = apperr.New(CodePayoutRefInvalid, "打款单号/凭证过长", http.StatusBadRequest)
	// ErrPayoutRefDuplicate 打款单号/凭证不可跨提现单重复使用。
	ErrPayoutRefDuplicate = apperr.New(CodePayoutRefDuplicate, "打款单号/凭证已用于其他提现单", http.StatusConflict)
	// ErrWalletInvariant 钱包或提现账务数据已损坏；必须回滚并由运维对账处理。
	ErrWalletInvariant = apperr.New(CodeWalletInvariant, "代理钱包账务不一致", http.StatusInternalServerError)
)
