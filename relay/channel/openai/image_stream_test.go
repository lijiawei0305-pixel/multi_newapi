package openai

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type failingImageResponseWriter struct {
	gin.ResponseWriter
	err error
}

func (w *failingImageResponseWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func (w *failingImageResponseWriter) WriteString(string) (int, error) {
	return 0, w.err
}

func newImageTestContext(t *testing.T, body, contentType string, isStream bool) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	t.Helper()
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{contentType}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{},
		IsStream:    isStream,
	}
	return c, recorder, resp, info
}

// TestOpenaiImageStreamHandlerForwardsSSEAndUsage covers the core SSE path:
// chunks are forwarded with rebuilt event lines, usage is extracted and
// normalized (input_tokens -> prompt_tokens with details), and [DONE] is
// re-emitted to the client.
func TestOpenaiImageStreamHandlerForwardsSSEAndUsage(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`event: image_generation.partial_image`,
		`data: {"type":"image_generation.partial_image","b64_json":"partial"}`,
		``,
		`data: {"usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7,"input_tokens_details":{"image_tokens":2,"text_tokens":1}}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	c, recorder, resp, info := newImageTestContext(t, body, "text/event-stream", true)

	usage, err := OpenaiImageStreamHandler(c, info, resp)
	require.Nil(t, err, "stream errors: %+v", info.StreamStatus.Errors)
	require.True(t, service.IsUpstreamAccepted(c))
	require.Equal(t, 3, usage.PromptTokens)
	require.Equal(t, 4, usage.CompletionTokens)
	require.Equal(t, 7, usage.TotalTokens)
	require.Equal(t, 2, usage.PromptTokensDetails.ImageTokens)
	require.Equal(t, 1, usage.PromptTokensDetails.TextTokens)
	require.Contains(t, recorder.Body.String(), `event: image_generation.partial_image`)
	require.Contains(t, recorder.Body.String(), `data: {"type":"image_generation.partial_image","b64_json":"partial"}`)
	require.Contains(t, recorder.Body.String(), `data: {"usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7,"input_tokens_details":{"image_tokens":2,"text_tokens":1}}}`)
	require.Contains(t, recorder.Body.String(), `data: [DONE]`)
	require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
}

// TestOpenaiImageStreamHandlerWrapsJSONResponse covers the non-SSE fallback:
// a JSON upstream response is wrapped into pseudo-SSE completed events.
func TestOpenaiImageStreamHandlerWrapsJSONResponse(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	body := `{"created":1710000000,"data":[{"b64_json":"final","revised_prompt":"draw a cat"}],"usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7,"input_tokens_details":{"image_tokens":2,"text_tokens":1}}}`

	c, recorder, resp, info := newImageTestContext(t, body, "application/json", true)

	usage, err := OpenaiImageStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.True(t, service.IsUpstreamAccepted(c))
	require.Equal(t, 3, usage.PromptTokens)
	require.Equal(t, 4, usage.CompletionTokens)
	require.Equal(t, 7, usage.TotalTokens)
	require.Equal(t, 2, usage.PromptTokensDetails.ImageTokens)
	require.Equal(t, 1, usage.PromptTokensDetails.TextTokens)
	require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	require.Empty(t, recorder.Header().Get("Content-Length"))
	require.Contains(t, recorder.Body.String(), `event: image_generation.completed`)
	require.Contains(t, recorder.Body.String(), `"type":"image_generation.completed"`)
	require.Contains(t, recorder.Body.String(), `"b64_json":"final"`)
	require.Contains(t, recorder.Body.String(), `"revised_prompt":"draw a cat"`)
	require.Contains(t, recorder.Body.String(), `data: [DONE]`)
}

// TestOpenaiImageHandlersReturnJSONError covers JSON error responses for both
// entry points: the non-streaming handler and the stream handler's non-SSE
// fallback. Neither must leak the error body to the client.
func TestOpenaiImageHandlersReturnJSONError(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	body := `{"error":{"message":"content moderation failed","type":"upstream_error","code":"content_moderation_failed","status":502}}`

	t.Run("non-streaming handler", func(t *testing.T) {
		c, recorder, resp, info := newImageTestContext(t, body, "application/json", false)

		usage, err := OpenaiImageHandler(c, info, resp)
		require.Nil(t, usage)
		require.NotNil(t, err)
		require.False(t, service.IsUpstreamAccepted(c))
		require.Equal(t, http.StatusBadGateway, err.StatusCode)
		oaiError := err.ToOpenAIError()
		require.Equal(t, "content moderation failed", oaiError.Message)
		require.Equal(t, "upstream_error", oaiError.Type)
		require.Equal(t, "content_moderation_failed", oaiError.Code)
		require.Empty(t, recorder.Body.String())
	})

	t.Run("stream handler JSON fallback", func(t *testing.T) {
		c, recorder, resp, info := newImageTestContext(t, body, "application/json", true)

		usage, err := OpenaiImageStreamHandler(c, info, resp)
		require.Nil(t, usage)
		require.NotNil(t, err)
		require.False(t, service.IsUpstreamAccepted(c))
		require.Equal(t, http.StatusBadGateway, err.StatusCode)
		require.Equal(t, "content moderation failed", err.ToOpenAIError().Message)
		require.Empty(t, recorder.Body.String())
	})
}

func TestOpenAIImageErrorWithoutTypeRemainsUnaccepted(t *testing.T) {
	c, recorder, resp, info := newImageTestContext(t, `{"error":{"message":"policy rejected","code":"policy_rejected"}}`, "application/json", false)

	usage, apiErr := OpenaiImageHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	require.False(t, service.IsUpstreamAccepted(c))
	require.Empty(t, recorder.Body.String())
}

// TestOpenaiImageStreamHandlerRecordsUpstreamErrorEvent verifies that an error
// event after partial output is terminal and cannot trigger a retry/refund.
func TestOpenaiImageStreamHandlerRecordsUpstreamErrorEvent(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`event: image_generation.partial_image`,
		`data: {"type":"image_generation.partial_image","b64_json":"partial"}`,
		``,
		`event: error`,
		`data: {"type":"upstream_error","error":{"message":"stream error: stream ID 77; INTERNAL_ERROR; received from peer"}}`,
		``,
	}, "\n")

	c, recorder, resp, info := newImageTestContext(t, body, "text/event-stream", true)

	usage, apiErr := OpenaiImageStreamHandler(c, info, resp)
	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.True(t, types.IsSkipRetryError(apiErr))
	require.True(t, service.IsUpstreamAccepted(c))
	require.NotNil(t, info.StreamStatus)
	require.True(t, info.StreamStatus.HasErrors())
	require.Equal(t, 1, info.StreamStatus.TotalErrorCount())
	require.Contains(t, info.StreamStatus.Errors[0].Message, "INTERNAL_ERROR")
	require.Contains(t, recorder.Body.String(), `event: image_generation.partial_image`)
	require.NotContains(t, recorder.Body.String(), `event: upstream_error`)
	require.NotContains(t, recorder.Body.String(), `stream ID 77`)
	require.NotContains(t, recorder.Body.String(), `data: [DONE]`)
}

func TestOpenaiImageStreamHandlerFirstErrorIsExplicitRejection(t *testing.T) {
	body := "data: {\"type\":\"upstream_error\",\"error\":{\"message\":\"policy rejected\",\"code\":\"policy_rejected\"}}\n\n"
	c, recorder, resp, info := newImageTestContext(t, body, "text/event-stream", true)

	usage, apiErr := OpenaiImageStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.True(t, service.IsExplicitUpstreamRejection(apiErr))
	require.False(t, service.IsUpstreamAccepted(c))
	require.Empty(t, recorder.Body.String())
}

func TestOpenaiImageStreamHandlerRejectsTruncatedAndMalformedStreams(t *testing.T) {
	tests := []struct {
		name             string
		body             string
		wantPartialEvent bool
	}{
		{
			name:             "partial event without completion",
			body:             "data: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"partial\"}\n\n",
			wantPartialEvent: true,
		},
		{
			name: "malformed event",
			body: "data: not-json\n\n",
		},
		{
			name: "usage without image",
			body: "data: {\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}\n\ndata: [DONE]\n\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, recorder, resp, info := newImageTestContext(t, test.body, "text/event-stream", true)

			usage, apiErr := OpenaiImageStreamHandler(c, info, resp)

			require.Nil(t, usage)
			require.NotNil(t, apiErr)
			require.True(t, types.IsSkipRetryError(apiErr))
			require.True(t, service.IsUpstreamAccepted(c))
			require.NotContains(t, recorder.Body.String(), `data: [DONE]`)
			if test.wantPartialEvent {
				require.Contains(t, recorder.Body.String(), `event: image_generation.partial_image`)
			}
		})
	}
}

func TestOpenaiImageStreamHandlerAcceptsCompletedEventAtEOF(t *testing.T) {
	body := "data: {\"type\":\"image_generation.completed\",\"b64_json\":\"final\"}\n\n"
	c, recorder, resp, info := newImageTestContext(t, body, "text/event-stream", true)

	usage, apiErr := OpenaiImageStreamHandler(c, info, resp)

	require.NotNil(t, usage)
	require.Nil(t, apiErr)
	require.True(t, service.IsUpstreamAccepted(c))
	require.Contains(t, recorder.Body.String(), `event: image_generation.completed`)
}

func TestOpenaiImageStreamHandlerRejectsUnsafeEventName(t *testing.T) {
	body := "data: {\"type\":\"image_generation.partial_image\\nevent: injected\",\"b64_json\":\"partial\"}\n\ndata: [DONE]\n\n"
	c, recorder, resp, info := newImageTestContext(t, body, "text/event-stream", true)

	usage, apiErr := OpenaiImageStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.True(t, service.IsUpstreamAccepted(c))
	require.NotContains(t, recorder.Body.String(), "event: injected")
}

func TestOpenaiImageJSONAsStreamHandlerWriteFailureIsTerminal(t *testing.T) {
	body := `{"created":1710000000,"data":[{"b64_json":"final"}],"usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7}}`
	c, _, resp, info := newImageTestContext(t, body, "application/json", true)
	writeErr := errors.New("downstream write failed")
	c.Writer = &failingImageResponseWriter{ResponseWriter: c.Writer, err: writeErr}

	usage, apiErr := OpenaiImageJSONAsStreamHandler(c, info, resp)

	require.NotNil(t, usage)
	require.NotNil(t, apiErr)
	require.True(t, types.IsSkipRetryError(apiErr))
	require.True(t, service.IsUpstreamAccepted(c))
	require.NotNil(t, info.StreamStatus)
	require.Equal(t, relaycommon.StreamEndReasonClientGone, info.StreamStatus.EndReason)
	require.ErrorIs(t, info.StreamStatus.EndError, writeErr)
}
