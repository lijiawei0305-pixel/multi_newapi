package promotion

import (
	"net/http"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

// 本模块错误码命名空间（detailed-design §2.9 / §6.4）。
// CHANNEL_PREFIX_DUP / CHANNEL_NOT_FOUND 见设计文档；
// CHANNEL_PREFIX_INVALID（前缀格式非法）与 CHANNEL_CODE_GEN（随机码生成失败）
// 为本轮补充的同命名空间错误码。
var (
	// ErrChannelPrefixDup 渠道前缀已被占用（前缀为业务唯一键）。
	ErrChannelPrefixDup = apperr.New("CHANNEL_PREFIX_DUP", "渠道前缀已被占用", http.StatusConflict)
	// ErrChannelNotFound 按 channel_code / id 未找到推广渠道。
	ErrChannelNotFound = apperr.New("CHANNEL_NOT_FOUND", "推广渠道不存在", http.StatusNotFound)
	// ErrChannelPrefixInvalid 渠道前缀格式非法（空 / 超长 / 非法字符）。
	ErrChannelPrefixInvalid = apperr.New("CHANNEL_PREFIX_INVALID", "渠道前缀格式非法", http.StatusBadRequest)
	// ErrCodeGen 随机渠道码生成失败（熵源错误，理论上罕见）。
	ErrCodeGen = apperr.New("CHANNEL_CODE_GEN", "渠道码生成失败", http.StatusInternalServerError)
)
