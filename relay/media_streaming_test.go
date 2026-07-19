package relay

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateMediaStreamingCapabilityUsesServerChannelCapability(t *testing.T) {
	tests := []struct {
		name        string
		relayMode   int
		channelType int
		baseURL     string
		stream      bool
		wantError   bool
	}{
		{name: "non-stream media is unchanged", relayMode: relayconstant.RelayModeImagesGenerations, channelType: constant.ChannelTypeGemini},
		{name: "native OpenAI image stream", relayMode: relayconstant.RelayModeImagesGenerations, channelType: constant.ChannelTypeOpenAI, stream: true},
		{name: "native OpenAI image edit stream", relayMode: relayconstant.RelayModeImagesEdits, channelType: constant.ChannelTypeOpenAI, stream: true},
		{name: "native OpenAI audio stream", relayMode: relayconstant.RelayModeAudioSpeech, channelType: constant.ChannelTypeOpenAI, stream: true},
		{name: "native Volc websocket audio stream", relayMode: relayconstant.RelayModeAudioSpeech, channelType: constant.ChannelTypeVolcEngine, stream: true},
		{name: "native Volc websocket trailing slash", relayMode: relayconstant.RelayModeAudioSpeech, channelType: constant.ChannelTypeVolcEngine, baseURL: constant.ChannelBaseURLs[constant.ChannelTypeVolcEngine] + "/", stream: true},
		{name: "custom Volc HTTP audio rejects stream", relayMode: relayconstant.RelayModeAudioSpeech, channelType: constant.ChannelTypeVolcEngine, baseURL: "https://volc-proxy.example", stream: true, wantError: true},
		{name: "non-Volc blank-base audio rejects stream", relayMode: relayconstant.RelayModeAudioSpeech, channelType: constant.ChannelTypeMiniMax, stream: true, wantError: true},
		{name: "Gemini image rejects stream", relayMode: relayconstant.RelayModeImagesGenerations, channelType: constant.ChannelTypeGemini, stream: true, wantError: true},
		{name: "MiniMax image rejects stream", relayMode: relayconstant.RelayModeImagesGenerations, channelType: constant.ChannelTypeMiniMax, stream: true, wantError: true},
		{name: "Ali image rejects stream", relayMode: relayconstant.RelayModeImagesGenerations, channelType: constant.ChannelTypeAli, stream: true, wantError: true},
		{name: "Replicate image rejects stream", relayMode: relayconstant.RelayModeImagesGenerations, channelType: constant.ChannelTypeReplicate, stream: true, wantError: true},
		{name: "Zhipu image rejects stream", relayMode: relayconstant.RelayModeImagesGenerations, channelType: constant.ChannelTypeZhipu_v4, stream: true, wantError: true},
		{name: "transcription rejects stream", relayMode: relayconstant.RelayModeAudioTranscription, channelType: constant.ChannelTypeOpenAI, stream: true, wantError: true},
		{name: "translation rejects stream", relayMode: relayconstant.RelayModeAudioTranslation, channelType: constant.ChannelTypeOpenAI, stream: true, wantError: true},
		{name: "non-media stream is unchanged", relayMode: relayconstant.RelayModeChatCompletions, channelType: constant.ChannelTypeGemini, stream: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				IsStream:  test.stream,
				RelayMode: test.relayMode,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType:    test.channelType,
					ChannelBaseUrl: test.baseURL,
				},
			}

			apiErr := ValidateMediaStreamingCapability(info)

			if !test.wantError {
				assert.Nil(t, apiErr)
				return
			}
			require.NotNil(t, apiErr)
			assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
			assert.Equal(t, types.ErrorCodeInvalidRequest, apiErr.GetErrorCode())
			assert.True(t, types.IsSkipRetryError(apiErr))
		})
	}
}

func TestValidateMediaStreamingCapabilityNilInfo(t *testing.T) {
	assert.Nil(t, ValidateMediaStreamingCapability(nil))
	apiErr := ValidateMediaStreamingCapability(&relaycommon.RelayInfo{
		IsStream:  true,
		RelayMode: relayconstant.RelayModeAudioSpeech,
	})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
}
