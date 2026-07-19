package constant

import "strings"

// IsClaudeMessagesPath reports whether a relay request uses Anthropic's
// Messages protocol. Query parameters are not part of URL.Path, but accepting
// a trailing slash keeps capability checks aligned with tolerant proxies.
func IsClaudeMessagesPath(path string) bool {
	return strings.TrimSuffix(path, "/") == "/v1/messages"
}

// ChannelTypeSupportsClaudeMessages is the declarative routing capability for
// Anthropic Messages requests. These channel adaptors expose normal typed
// unsupported-conversion errors, so automatic selection must exclude them
// before weighted routing. A token-pinned channel may still reach the adaptor
// and receive that explicit error.
func ChannelTypeSupportsClaudeMessages(channelType int) bool {
	switch channelType {
	case ChannelTypePaLM,
		ChannelTypeBaidu,
		ChannelTypeZhipu,
		ChannelTypeXunfei,
		ChannelTypeAIProxyLibrary,
		ChannelTypeTencent,
		ChannelTypeCohere,
		ChannelTypeDify,
		ChannelTypeJina,
		ChannelCloudflare,
		ChannelTypeMistral,
		ChannelTypeMokaAI,
		ChannelTypeXai,
		ChannelTypeCoze,
		ChannelTypeJimeng,
		ChannelTypeSubmodel,
		ChannelTypeReplicate,
		ChannelTypeCodex:
		return false
	default:
		return true
	}
}
