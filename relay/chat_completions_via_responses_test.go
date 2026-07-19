package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type responsesFailureAdaptor struct {
	channel.Adaptor
	response *http.Response
}

func (a *responsesFailureAdaptor) ConvertOpenAIResponsesRequest(_ *gin.Context, _ *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	return &request, nil
}

func (a *responsesFailureAdaptor) DoRequest(_ *gin.Context, _ *relaycommon.RelayInfo, _ io.Reader) (any, error) {
	return a.response, nil
}

func TestIsResponsesEventStreamContentType(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		want        bool
	}{
		{name: "plain", contentType: "text/event-stream", want: true},
		{name: "mixed case with charset", contentType: "Text/Event-Stream; charset=utf-8", want: true},
		{name: "json", contentType: "application/json", want: false},
		{name: "empty", contentType: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isResponsesEventStreamContentType(tt.contentType))
		})
	}
}

func TestChatCompletionsViaResponsesConservativelyAcceptsMalformedOrOversizedDecode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name      string
		body      string
		limit     string
		errorCode types.ErrorCode
	}{
		{name: "malformed", body: `{`, limit: "1024", errorCode: types.ErrorCodeBadResponseBody},
		{name: "oversized", body: "private-upstream-secret", limit: "4", errorCode: types.ErrorCodeReadResponseBodyFailed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("RELAY_UPSTREAM_JSON_MAX_BYTES", test.limit)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			c.Set(common.RequestIdKey, "chat-response-failure")
			info := &relaycommon.RelayInfo{
				OriginModelName: "gpt-test", RequestURLPath: "/v1/chat/completions",
				ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"},
			}
			adaptor := &responsesFailureAdaptor{response: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(test.body)),
			}}

			usage, apiErr := chatCompletionsViaResponses(c, info, adaptor, &dto.GeneralOpenAIRequest{Model: "gpt-test"})
			require.Nil(t, usage)
			require.NotNil(t, apiErr)
			assert.Equal(t, test.errorCode, apiErr.GetErrorCode())
			assert.True(t, service.IsUpstreamAccepted(c), "an unclassified provider 2xx remains accepted when local decoding fails")
			assert.NotContains(t, recorder.Body.String(), test.body)
			assert.NotContains(t, apiErr.Error(), "private-upstream-secret")
		})
	}
}

func TestChatCompletionsViaResponsesLeavesStructuredProviderRejectionUnaccepted(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-test", RequestURLPath: "/v1/chat/completions",
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"},
	}
	adaptor := &responsesFailureAdaptor{response: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"error":{"message":"provider rejected request","code":"invalid_request"}}`,
		)),
	}}

	usage, apiErr := chatCompletionsViaResponses(c, info, adaptor, &dto.GeneralOpenAIRequest{Model: "gpt-test"})

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.True(t, service.IsExplicitUpstreamRejection(apiErr))
	assert.False(t, service.IsUpstreamAccepted(c))
	assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	assert.Empty(t, recorder.Body.String(), "an error-first response must not publish provider bytes")
}

func TestChatCompletionsViaResponsesRejectsNilResponseWithoutPanic(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-test",
		RequestURLPath:  "/v1/chat/completions",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"},
	}

	require.NotPanics(t, func() {
		usage, apiErr := chatCompletionsViaResponses(
			c,
			info,
			&responsesFailureAdaptor{},
			&dto.GeneralOpenAIRequest{Model: "gpt-test"},
		)
		require.Nil(t, usage)
		require.NotNil(t, apiErr)
		assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
		assert.Equal(t, types.ErrorCodeBadResponse, apiErr.GetErrorCode())
		assert.EqualError(t, apiErr, "upstream returned an empty response")
		assert.False(t, service.IsUpstreamAccepted(c))
	})
}
