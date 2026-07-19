package channel_test

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/baidu"
	"github.com/QuantumNous/new-api/relay/channel/cloudflare"
	"github.com/QuantumNous/new-api/relay/channel/codex"
	"github.com/QuantumNous/new-api/relay/channel/cohere"
	"github.com/QuantumNous/new-api/relay/channel/coze"
	"github.com/QuantumNous/new-api/relay/channel/dify"
	"github.com/QuantumNous/new-api/relay/channel/jimeng"
	"github.com/QuantumNous/new-api/relay/channel/jina"
	"github.com/QuantumNous/new-api/relay/channel/mistral"
	"github.com/QuantumNous/new-api/relay/channel/mokaai"
	"github.com/QuantumNous/new-api/relay/channel/palm"
	"github.com/QuantumNous/new-api/relay/channel/replicate"
	"github.com/QuantumNous/new-api/relay/channel/submodel"
	"github.com/QuantumNous/new-api/relay/channel/tencent"
	"github.com/QuantumNous/new-api/relay/channel/xai"
	"github.com/QuantumNous/new-api/relay/channel/xunfei"
	"github.com/QuantumNous/new-api/relay/channel/zhipu"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnsupportedClaudeConversionsReturnTypedErrors(t *testing.T) {
	tests := []struct {
		name        string
		channelType int
		convert     func() (any, error)
	}{
		{name: "Baidu", channelType: constant.ChannelTypeBaidu, convert: func() (any, error) { return (&baidu.Adaptor{}).ConvertClaudeRequest(nil, nil, nil) }},
		{name: "PaLM", channelType: constant.ChannelTypePaLM, convert: func() (any, error) { return (&palm.Adaptor{}).ConvertClaudeRequest(nil, nil, nil) }},
		{name: "Cohere", channelType: constant.ChannelTypeCohere, convert: func() (any, error) { return (&cohere.Adaptor{}).ConvertClaudeRequest(nil, nil, nil) }},
		{name: "Cloudflare", channelType: constant.ChannelCloudflare, convert: func() (any, error) { return (&cloudflare.Adaptor{}).ConvertClaudeRequest(nil, nil, nil) }},
		{name: "Zhipu", channelType: constant.ChannelTypeZhipu, convert: func() (any, error) { return (&zhipu.Adaptor{}).ConvertClaudeRequest(nil, nil, nil) }},
		{name: "Xunfei", channelType: constant.ChannelTypeXunfei, convert: func() (any, error) { return (&xunfei.Adaptor{}).ConvertClaudeRequest(nil, nil, nil) }},
		{name: "MokaAI", channelType: constant.ChannelTypeMokaAI, convert: func() (any, error) { return (&mokaai.Adaptor{}).ConvertClaudeRequest(nil, nil, nil) }},
		{name: "Mistral", channelType: constant.ChannelTypeMistral, convert: func() (any, error) { return (&mistral.Adaptor{}).ConvertClaudeRequest(nil, nil, nil) }},
		{name: "Tencent", channelType: constant.ChannelTypeTencent, convert: func() (any, error) { return (&tencent.Adaptor{}).ConvertClaudeRequest(nil, nil, nil) }},
		{name: "Jina", channelType: constant.ChannelTypeJina, convert: func() (any, error) { return (&jina.Adaptor{}).ConvertClaudeRequest(nil, nil, nil) }},
		{name: "Dify", channelType: constant.ChannelTypeDify, convert: func() (any, error) { return (&dify.Adaptor{}).ConvertClaudeRequest(nil, nil, nil) }},
		{name: "Codex", channelType: constant.ChannelTypeCodex, convert: func() (any, error) { return (&codex.Adaptor{}).ConvertClaudeRequest(nil, nil, nil) }},
		{name: "Coze", channelType: constant.ChannelTypeCoze, convert: func() (any, error) { return (&coze.Adaptor{}).ConvertClaudeRequest(nil, nil, nil) }},
		{name: "Jimeng", channelType: constant.ChannelTypeJimeng, convert: func() (any, error) { return (&jimeng.Adaptor{}).ConvertClaudeRequest(nil, nil, nil) }},
		{name: "Replicate", channelType: constant.ChannelTypeReplicate, convert: func() (any, error) { return (&replicate.Adaptor{}).ConvertClaudeRequest(nil, nil, nil) }},
		{name: "Submodel", channelType: constant.ChannelTypeSubmodel, convert: func() (any, error) { return (&submodel.Adaptor{}).ConvertClaudeRequest(nil, nil, nil) }},
		{name: "xAI", channelType: constant.ChannelTypeXai, convert: func() (any, error) { return (&xai.Adaptor{}).ConvertClaudeRequest(nil, nil, nil) }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.False(t, constant.ChannelTypeSupportsClaudeMessages(test.channelType), "unsupported conversion must be excluded by automatic routing")
			var converted any
			var err error
			require.NotPanics(t, func() {
				converted, err = test.convert()
			})
			assert.Nil(t, converted)
			require.Error(t, err)

			var unsupported *channel.UnsupportedConversionError
			require.True(t, errors.As(err, &unsupported))
			assert.Equal(t, test.name, unsupported.Provider)
			assert.Equal(t, "Claude", unsupported.RequestFormat)
			assert.Contains(t, err.Error(), "Claude request conversion is unsupported")
		})
	}
}

func TestImplementedClaudeConversionsRemainRoutable(t *testing.T) {
	for _, channelType := range []int{
		constant.ChannelTypeOpenAI,
		constant.ChannelTypeAnthropic,
		constant.ChannelTypeAli,
		constant.ChannelTypeGemini,
		constant.ChannelTypeMoonshot,
		constant.ChannelTypeMiniMax,
		constant.ChannelTypeAdvancedCustom,
	} {
		assert.True(t, constant.ChannelTypeSupportsClaudeMessages(channelType), constant.GetChannelTypeName(channelType))
	}
}
