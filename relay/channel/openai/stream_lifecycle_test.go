package openai

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestOaiStreamHandlerRejectsIncompleteSuccessfulHTTPStreams(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	validButTruncated := `data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-test","choices":[{"index":0,"delta":{"content":"partial"},"finish_reason":null}]}` + "\n\n"
	tests := []struct {
		name string
		body string
	}{
		{name: "empty body", body: ""},
		{name: "malformed event", body: "data: {not-json}\n\n"},
		{name: "truncated after output", body: validButTruncated},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder, resp, info := newResponsesChatTestContext(t, tt.body, true)
			info.RelayMode = relayconstant.RelayModeChatCompletions

			usage, apiErr := OaiStreamHandler(c, info, resp)

			require.Nil(t, usage)
			require.NotNil(t, apiErr)
			require.True(t, service.IsUpstreamAccepted(c))
			require.True(t, types.IsSkipRetryError(apiErr))
			require.NotContains(t, recorder.Body.String(), "data: [DONE]")
		})
	}
}

func TestOaiStreamHandlerAcceptsExplicitTerminalChunkWithoutDoneSentinel(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","created":1710000000,"model":"gpt-test","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","created":1710000000,"model":"gpt-test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		``,
	}, "\n")
	c, recorder, resp, info := newResponsesChatTestContext(t, body, true)
	info.RelayMode = relayconstant.RelayModeChatCompletions
	info.ShouldIncludeUsage = false

	usage, apiErr := OaiStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.True(t, service.IsUpstreamAccepted(c))
	require.Contains(t, recorder.Body.String(), `"content":"hello"`)
	require.Contains(t, recorder.Body.String(), `"finish_reason":"stop"`)
	require.Contains(t, recorder.Body.String(), "data: [DONE]")
}

func TestOaiStreamHandlerKeepsFirstExplicitRejectionRetryable(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := `data: {"error":{"message":"provider rejected","type":"invalid_request_error","code":"invalid_request"}}` + "\n\n"
	c, recorder, resp, info := newResponsesChatTestContext(t, body, true)
	info.RelayMode = relayconstant.RelayModeChatCompletions

	usage, apiErr := OaiStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.True(t, service.IsExplicitUpstreamRejection(apiErr))
	require.False(t, service.IsUpstreamAccepted(c))
	require.False(t, types.IsSkipRetryError(apiErr))
	require.Empty(t, recorder.Body.String())
}

func TestOaiResponsesStreamHandlerRequiresTerminalEvent(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	tests := []struct {
		name string
		body string
	}{
		{name: "empty body", body: ""},
		{name: "malformed event", body: "data: {not-json}\n\n"},
		{name: "truncated after delta", body: `data: {"type":"response.output_text.delta","delta":"partial"}` + "\n\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder, resp, info := newResponsesChatTestContext(t, tt.body, true)

			usage, apiErr := OaiResponsesStreamHandler(c, info, resp)

			require.Nil(t, usage)
			require.NotNil(t, apiErr)
			require.True(t, service.IsUpstreamAccepted(c))
			require.True(t, types.IsSkipRetryError(apiErr))
			require.NotContains(t, recorder.Body.String(), "response.completed")
		})
	}
}

func TestOaiResponsesStreamHandlerStopsAtCompletedEvent(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := `data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}}` + "\n\n"
	c, recorder, resp, info := newResponsesChatTestContext(t, body, true)

	usage, apiErr := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Equal(t, 2, usage.PromptTokens)
	require.Equal(t, 3, usage.CompletionTokens)
	require.Equal(t, 5, usage.TotalTokens)
	require.Contains(t, recorder.Body.String(), "response.completed")
}

func TestResponsesChatConversionHandlersRejectMissingTerminalEvent(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := `data: {"type":"response.output_text.delta","delta":"partial"}` + "\n\n"
	t.Run("streaming conversion", func(t *testing.T) {
		c, recorder, resp, info := newResponsesChatTestContext(t, body, true)

		usage, apiErr := OaiResponsesToChatStreamHandler(c, info, resp)

		require.Nil(t, usage)
		require.NotNil(t, apiErr)
		require.True(t, service.IsUpstreamAccepted(c))
		require.True(t, types.IsSkipRetryError(apiErr))
		require.Contains(t, recorder.Body.String(), "partial")
		require.NotContains(t, recorder.Body.String(), "data: [DONE]")
	})

	t.Run("buffered conversion", func(t *testing.T) {
		c, recorder, resp, info := newResponsesChatTestContext(t, body, false)

		usage, apiErr := OaiResponsesToChatBufferedStreamHandler(c, info, resp)

		require.Nil(t, usage)
		require.NotNil(t, apiErr)
		require.True(t, service.IsUpstreamAccepted(c))
		require.True(t, types.IsSkipRetryError(apiErr))
		require.Empty(t, recorder.Body.String())
	})
}

func TestChatResponsesConversionRejectsMissingTerminalChunk(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := `data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-test","choices":[{"index":0,"delta":{"content":"partial"},"finish_reason":null}]}` + "\n\n"
	c, recorder, resp, info := newResponsesChatTestContext(t, body, true)

	usage, apiErr := OaiChatToResponsesStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.True(t, service.IsUpstreamAccepted(c))
	require.True(t, types.IsSkipRetryError(apiErr))
	require.Contains(t, recorder.Body.String(), "partial")
	require.NotContains(t, recorder.Body.String(), "response.completed")
}
