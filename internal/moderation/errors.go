package moderation

import (
	"net/http"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

// 本模块错误码命名空间（detailed-design §2.14 / §6.4）。
var (
	// ErrContentBlocked 用户消息命中 block 级违禁词，请求被拦截。
	// relay 层据此映射为原生 ErrorCodeSensitiveWordsDetected 返回。
	ErrContentBlocked = apperr.New("CONTENT_BLOCKED", "消息包含违禁内容，已被拦截", http.StatusBadRequest)
	// ErrWordInvalid 违禁词格式非法（空 / 超长 / 正则编译失败 / 未知 match_type）。
	ErrWordInvalid = apperr.New("MODERATION_WORD_INVALID", "违禁词格式非法", http.StatusBadRequest)
	// ErrWordNotFound 按 id 未找到违禁词（或跨租户访问）。
	ErrWordNotFound = apperr.New("MODERATION_WORD_NOT_FOUND", "违禁词不存在", http.StatusNotFound)
)
