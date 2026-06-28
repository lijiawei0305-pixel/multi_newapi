package agent

import (
	"net/http"

	"newapi-mt/internal/platform/apperr"
)

// 本模块错误码命名空间（detailed-design §2.3 / §6.4）。
// AGENT_TYPE_INVALID / WITHDRAW_INSUFFICIENT / WITHDRAW_NOT_PENDING 见设计文档；
// WITHDRAW_NOT_FOUND（提现单不存在）与 EARNING_INVALID（收益条目非法）为本轮补充的同命名空间错误码。
const (
	// CodeAgentTypeInvalid 代理类型或 AgentParams 参数非法。
	CodeAgentTypeInvalid = "AGENT_TYPE_INVALID"
	// CodeWithdrawInsufficient 提现金额超过可提现余额（或非正）。
	CodeWithdrawInsufficient = "WITHDRAW_INSUFFICIENT"
	// CodeWithdrawNotPending 提现单非 pending，不可再次审核。
	CodeWithdrawNotPending = "WITHDRAW_NOT_PENDING"
	// CodeWithdrawNotFound 提现单不存在（补充码）。
	CodeWithdrawNotFound = "WITHDRAW_NOT_FOUND"
	// CodeEarningInvalid 收益条目非法：来源类型不合法或来源 ID 缺失（补充码）。
	CodeEarningInvalid = "EARNING_INVALID"
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
)
