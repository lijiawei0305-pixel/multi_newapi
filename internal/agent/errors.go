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
)

var (
	// ErrAgentTypeInvalid 代理类型/参数非法。
	ErrAgentTypeInvalid = apperr.New(CodeAgentTypeInvalid, "代理类型或参数非法", http.StatusBadRequest)
	// ErrWithdrawInsufficient 提现金额超过可提现余额。
	ErrWithdrawInsufficient = apperr.New(CodeWithdrawInsufficient, "提现金额超过可提现余额", http.StatusBadRequest)
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
)
