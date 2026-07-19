package gemini

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGeminiEmbeddingForwardsExplicitZeroDimensions(t *testing.T) {
	for _, test := range []struct {
		name       string
		dimensions *int
		wantField  bool
	}{
		{name: "absent"},
		{name: "explicit zero", dimensions: common.GetPointer(0), wantField: true},
		{name: "positive", dimensions: common.GetPointer(256), wantField: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gemini-embedding-001"}}
			converted, err := (&Adaptor{}).ConvertEmbeddingRequest(nil, info, dto.EmbeddingRequest{
				Input:      "hello",
				Dimensions: test.dimensions,
			})
			require.NoError(t, err)

			encoded, err := common.Marshal(converted)
			require.NoError(t, err)
			field := gjson.GetBytes(encoded, "requests.0.outputDimensionality")
			assert.Equal(t, test.wantField, field.Exists())
			if test.dimensions != nil {
				assert.Equal(t, int64(*test.dimensions), field.Int())
			}
		})
	}
}

func TestGeminiExtraBodyForwardsExplicitFalseIncludeThoughts(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatOpenAI,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeGemini,
			UpstreamModelName: "gemini-2.0-flash",
		},
	}
	var request dto.GeneralOpenAIRequest
	require.NoError(t, common.Unmarshal([]byte(`{
		"model":"gemini-2.0-flash",
		"messages":[{"role":"user","content":"hello"}],
		"extra_body":{"google":{"thinking_config":{"include_thoughts":false}}}
	}`), &request))

	converted, err := CovertOpenAI2Gemini(c, request, info)
	require.NoError(t, err)
	require.NotNil(t, converted.GenerationConfig.ThinkingConfig)
	require.NotNil(t, converted.GenerationConfig.ThinkingConfig.IncludeThoughts)
	assert.False(t, *converted.GenerationConfig.ThinkingConfig.IncludeThoughts)

	encoded, err := common.Marshal(converted)
	require.NoError(t, err)
	field := gjson.GetBytes(encoded, "generationConfig.thinkingConfig.includeThoughts")
	require.True(t, field.Exists())
	assert.False(t, field.Bool())
}

func TestGeminiChatConversionForwardsExplicitZeroScalars(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatOpenAI,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeGemini,
			UpstreamModelName: "gemini-2.0-flash",
		},
	}
	var request dto.GeneralOpenAIRequest
	require.NoError(t, common.Unmarshal([]byte(`{
		"model":"gemini-2.0-flash",
		"messages":[{"role":"user","content":"hello"}],
		"top_p":0,
		"seed":0,
		"max_tokens":7,
		"max_completion_tokens":0
	}`), &request))

	converted, err := CovertOpenAI2Gemini(c, request, info)
	require.NoError(t, err)
	encoded, err := common.Marshal(converted)
	require.NoError(t, err)

	for _, path := range []string{
		"generationConfig.topP",
		"generationConfig.seed",
		"generationConfig.maxOutputTokens",
	} {
		field := gjson.GetBytes(encoded, path)
		require.Truef(t, field.Exists(), "%s must be forwarded", path)
		assert.Zero(t, field.Int())
	}
}

func TestGeminiUnsupportedMIMEErrorDoesNotExposeSource(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatOpenAI,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeGemini,
			UpstreamModelName: "gemini-2.0-flash",
		},
	}
	const sensitiveSource = "data:application/x-private;base64,c2VjcmV0LXNpZ25lZC11cmw="
	var request dto.GeneralOpenAIRequest
	require.NoError(t, common.Unmarshal([]byte(`{
		"model":"gemini-2.0-flash",
		"messages":[{"role":"user","content":[{
			"type":"image_url",
			"image_url":{"url":"`+sensitiveSource+`"}
		}]}]
	}`), &request))

	converted, err := CovertOpenAI2Gemini(c, request, info)

	assert.Nil(t, converted)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "application/x-private")
	assert.Contains(t, err.Error(), "supported types")
	assert.NotContains(t, err.Error(), sensitiveSource)
	assert.NotContains(t, err.Error(), "c2VjcmV0LXNpZ25lZC11cmw")
}

func TestGeminiInvalidToolArgumentsErrorDoesNotExposePayload(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatOpenAI,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeGemini,
			UpstreamModelName: "gemini-2.0-flash",
		},
	}
	const sensitiveArguments = "secret=customer-token-not-json"
	var request dto.GeneralOpenAIRequest
	require.NoError(t, common.Unmarshal([]byte(`{
		"model":"gemini-2.0-flash",
		"messages":[{"role":"assistant","tool_calls":[{
			"id":"call_1",
			"type":"function",
			"function":{"name":"lookup","arguments":"`+sensitiveArguments+`"}
		}]}]
	}`), &request))

	converted, err := CovertOpenAI2Gemini(c, request, info)

	assert.Nil(t, converted)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "function lookup")
	assert.NotContains(t, err.Error(), sensitiveArguments)
	assert.NotContains(t, err.Error(), "customer-token")
}
