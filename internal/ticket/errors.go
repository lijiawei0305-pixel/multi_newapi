package ticket

import (
	"net/http"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

// 本模块错误码命名空间（全部 TICKET_ 前缀；登记于 doc/api-contract.md §1.1，前端按 code 映射文案）。
var (
	// ErrNotFound 工单不存在，或跨用户/跨租户访问（IDOR 防线坍缩为「不存在」，不泄漏存在性；moderation 先例）。
	ErrNotFound = apperr.New("TICKET_NOT_FOUND", "工单不存在", http.StatusNotFound)
	// ErrInputInvalid 请求体非法（缺标题/正文，或标题超长）。
	ErrInputInvalid = apperr.New("TICKET_INPUT_INVALID", "工单标题或内容非法", http.StatusBadRequest)
	// ErrPriorityInvalid 优先级不在 low|normal|high|urgent。
	ErrPriorityInvalid = apperr.New("TICKET_PRIORITY_INVALID", "工单优先级非法", http.StatusBadRequest)
	// ErrStatusInvalid 目标状态非法，或不允许的状态流转（如 closed→resolved）。
	ErrStatusInvalid = apperr.New("TICKET_STATUS_INVALID", "工单状态或流转非法", http.StatusBadRequest)
	// ErrClosed 向已关闭工单回复（需先重开）。
	ErrClosed = apperr.New("TICKET_CLOSED", "工单已关闭，无法回复", http.StatusConflict)
	// ErrReplyEmpty 回复内容为空。
	ErrReplyEmpty = apperr.New("TICKET_REPLY_EMPTY", "回复内容不能为空", http.StatusBadRequest)
	// ErrTenantLookup 建单时归属租户查询失败（区别于「用户 tenant_id=0」的合法平台工单）：拒绝而非静默
	// 落为平台工单，避免瞬时故障把租户用户工单归错、对拥有的代理不可见。可重试。
	ErrTenantLookup = apperr.New("TICKET_TENANT_LOOKUP", "工单归属租户查询失败，请重试", http.StatusServiceUnavailable)
)
