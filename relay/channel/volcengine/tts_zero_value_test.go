package volcengine

import (
	"io"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestVolcengineTTSConversionPreservesExplicitZeroAndFalseMetadata(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	request := dto.AudioRequest{
		Input: "hello", Voice: "voice", Speed: common.GetPointer(0.0),
		Metadata: []byte(`{
			"audio":{"bitrate":0,"loudness_ratio":0,"enable_emotion":false,"emotion_scale":0},
			"request":{"silence_duration":0,"extra_param":{
				"disable_markdown_filter":false,"enable_latex_tn":false,"disable_emoji_filter":false,
				"unsupported_char_ratio_thresh":0,"aigc_watermark":false,
				"cache_config":{"text_type":0,"use_cache":false}
			}}
		}`),
	}
	reader, err := (&Adaptor{}).ConvertAudioRequest(c, &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeAudioSpeech,
		ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "app|token"},
	}, request)
	require.NoError(t, err)
	encoded, err := io.ReadAll(reader)
	require.NoError(t, err)
	for _, path := range []string{
		"audio.speed_ratio", "audio.bitrate", "audio.loudness_ratio", "audio.enable_emotion", "audio.emotion_scale",
		"request.silence_duration", "request.extra_param.disable_markdown_filter", "request.extra_param.enable_latex_tn",
		"request.extra_param.disable_emoji_filter", "request.extra_param.unsupported_char_ratio_thresh",
		"request.extra_param.aigc_watermark", "request.extra_param.cache_config.text_type", "request.extra_param.cache_config.use_cache",
	} {
		assert.Truef(t, gjson.GetBytes(encoded, path).Exists(), "%s must be forwarded", path)
	}
}

func TestVolcengineTTSConversionOmitsAbsentSpeed(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	reader, err := (&Adaptor{}).ConvertAudioRequest(c, &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeAudioSpeech,
		ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "app|token"},
	}, dto.AudioRequest{Input: "hello", Voice: "voice"})
	require.NoError(t, err)
	encoded, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.False(t, gjson.GetBytes(encoded, "audio.speed_ratio").Exists())
}
