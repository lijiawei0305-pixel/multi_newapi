package minimax

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

func TestMiniMaxTTSConversionPreservesExplicitZeroAndFalseMetadata(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	request := dto.AudioRequest{
		Input: "hello", Voice: "voice", Speed: common.GetPointer(0.0),
		Metadata: []byte(`{
			"stream":false,"subtitle_enable":false,"aigc_watermark":false,
			"stream_options":{"exclude_aggregated_audio":false},
			"voice_setting":{"voice_id":"voice","speed":0,"vol":0,"pitch":0,"text_normalization":false,"latex_read":false},
			"audio_setting":{"sample_rate":0,"bitrate":0,"channel":0,"force_cbr":false},
			"voice_modify":{"pitch":0,"intensity":0,"timbre":0}
		}`),
	}
	reader, err := (&Adaptor{}).ConvertAudioRequest(c, &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeAudioSpeech}, request)
	require.NoError(t, err)
	encoded, err := io.ReadAll(reader)
	require.NoError(t, err)
	for _, path := range []string{
		"stream", "subtitle_enable", "aigc_watermark", "stream_options.exclude_aggregated_audio",
		"voice_setting.speed", "voice_setting.vol", "voice_setting.pitch", "voice_setting.text_normalization", "voice_setting.latex_read",
		"audio_setting.sample_rate", "audio_setting.bitrate", "audio_setting.channel", "audio_setting.force_cbr",
		"voice_modify.pitch", "voice_modify.intensity", "voice_modify.timbre",
	} {
		assert.Truef(t, gjson.GetBytes(encoded, path).Exists(), "%s must be forwarded", path)
	}
}

func TestMiniMaxTTSConversionOmitsAbsentSpeed(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	reader, err := (&Adaptor{}).ConvertAudioRequest(c, &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeAudioSpeech}, dto.AudioRequest{
		Input: "hello", Voice: "voice",
	})
	require.NoError(t, err)
	encoded, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.False(t, gjson.GetBytes(encoded, "voice_setting.speed").Exists())
}
