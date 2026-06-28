package stats

import (
	"net/http"

	"newapi-mt/internal/platform/apperr"
)

// 本模块错误码命名空间（doc/detailed-design.md §2.12 / §6.4）。
// STATS_RANGE_INVALID 见设计文档；STATS_CROSS_TENANT 为本轮补充的同命名空间错误码
// （服务层防御性跨租户隔离），命名前缀对齐 §6.4。
var (
	// ErrRangeInvalid 统计时间范围非法（如 from > to）。本轮三个接口均未带范围参数，
	// 故此码仅占位命名空间；待加入范围过滤后启用（见报告 TODO）。
	ErrRangeInvalid = apperr.New("STATS_RANGE_INVALID", "统计范围非法", http.StatusBadRequest)
	// ErrCrossTenant 非管理员 Principal 试图查看非自身租户的统计。
	// 这是 service 层的防御性隔离（defense-in-depth）；主鉴权应在 handler 前置完成
	// （RequireAdmin / RequireTenantOwner，见报告 TODO）。
	ErrCrossTenant = apperr.New("STATS_CROSS_TENANT", "无权查看其他租户统计", http.StatusForbidden)
)
