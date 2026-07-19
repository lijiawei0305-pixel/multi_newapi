package gemini

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newGeminiStreamLifecycleContext(t *testing.T, body string) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set(common.RequestIdKey, "gemini-stream-lifecycle-test")
	info := &relaycommon.RelayInfo{
		IsStream:        true,
		DisablePing:     true,
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "gemini-test",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-test",
		},
	}
	return c, recorder, &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}, info
}

func TestGeminiStreamHandlerRejectsIncompleteSuccessfulHTTPStreams(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	truncated, err := common.Marshal(dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{
			Content: dto.GeminiChatContent{Role: "model", Parts: []dto.GeminiPart{{Text: "partial"}}},
		}},
	})
	require.NoError(t, err)
	tests := []struct {
		name string
		body string
	}{
		{name: "empty body", body: ""},
		{name: "malformed event", body: "data: {not-json}\n\n"},
		{name: "truncated after output", body: "data: " + string(truncated) + "\n\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _, resp, info := newGeminiStreamLifecycleContext(t, tt.body)

			usage, apiErr := geminiStreamHandler(c, info, resp, func(string, *dto.GeminiChatResponse) bool { return true })

			require.NotNil(t, usage)
			require.NotNil(t, apiErr)
			require.True(t, service.IsUpstreamAccepted(c))
			require.True(t, types.IsSkipRetryError(apiErr))
		})
	}
}

func TestGeminiStreamHandlerAcceptsFinishReasonWithoutDoneSentinel(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	stop := "STOP"
	payload, err := common.Marshal(dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{
			FinishReason: &stop,
			Content:      dto.GeminiChatContent{Role: "model", Parts: []dto.GeminiPart{{Text: "complete"}}},
		}},
		UsageMetadata: dto.GeminiUsageMetadata{PromptTokenCount: 2, CandidatesTokenCount: 3, TotalTokenCount: 5},
	})
	require.NoError(t, err)
	c, _, resp, info := newGeminiStreamLifecycleContext(t, "data: "+string(payload)+"\n\n")

	usage, apiErr := geminiStreamHandler(c, info, resp, func(string, *dto.GeminiChatResponse) bool { return true })

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Equal(t, 5, usage.TotalTokens)
	require.True(t, service.IsUpstreamAccepted(c))
}

func TestGeminiStreamHandlerKeepsFirstExplicitErrorRetryable(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := `data: {"error":{"code":429,"message":"rate limited","status":"RESOURCE_EXHAUSTED"}}` + "\n\n"
	c, recorder, resp, info := newGeminiStreamLifecycleContext(t, body)

	usage, apiErr := geminiStreamHandler(c, info, resp, func(string, *dto.GeminiChatResponse) bool { return true })

	require.NotNil(t, usage)
	require.NotNil(t, apiErr)
	require.True(t, service.IsExplicitUpstreamRejection(apiErr))
	require.False(t, service.IsUpstreamAccepted(c))
	require.False(t, types.IsSkipRetryError(apiErr))
	require.Empty(t, recorder.Body.String())
}

func TestGeminiResponsesStreamHandlerDoesNotFabricateCompletion(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	payload, err := common.Marshal(dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{
			Content: dto.GeminiChatContent{Role: "model", Parts: []dto.GeminiPart{{Text: "partial"}}},
		}},
	})
	require.NoError(t, err)
	c, recorder, resp, _ := newGeminiStreamLifecycleContext(t, "data: "+string(payload)+"\n\n")
	info := newGeminiResponsesRelayInfo(true)

	usage, apiErr := GeminiResponsesStreamHandler(c, info, resp)

	require.NotNil(t, usage)
	require.NotNil(t, apiErr)
	require.True(t, service.IsUpstreamAccepted(c))
	require.True(t, types.IsSkipRetryError(apiErr))
	require.Contains(t, recorder.Body.String(), "partial")
	require.NotContains(t, recorder.Body.String(), "response.completed")
}
