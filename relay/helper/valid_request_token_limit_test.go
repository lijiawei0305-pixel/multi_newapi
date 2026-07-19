package helper

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newTokenLimitTestContext(t *testing.T, path, body string) *gin.Context {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	return c
}

func TestRequestValidatorsRejectTokenLimitsThatCouldOverflowAccounting(t *testing.T) {
	t.Run("OpenAI max completion tokens", func(t *testing.T) {
		c := newTokenLimitTestContext(t, "/v1/chat/completions", `{"model":"gpt-test","messages":[{"role":"user","content":"hi"}],"max_completion_tokens":1073741824}`)
		request, err := GetAndValidateTextRequest(c, relayconstant.RelayModeChatCompletions)
		require.Nil(t, request)
		require.EqualError(t, err, "max_tokens is invalid")
	})

	t.Run("Responses max output tokens", func(t *testing.T) {
		c := newTokenLimitTestContext(t, "/v1/responses", `{"model":"gpt-test","input":"hi","max_output_tokens":1073741824}`)
		request, err := GetAndValidateResponsesRequest(c)
		require.Nil(t, request)
		require.EqualError(t, err, "max_output_tokens is invalid")
	})

	t.Run("Claude max tokens", func(t *testing.T) {
		c := newTokenLimitTestContext(t, "/v1/messages", `{"model":"claude-test","messages":[{"role":"user","content":"hi"}],"max_tokens":1073741824}`)
		request, err := GetAndValidateClaudeRequest(c)
		require.Nil(t, request)
		require.EqualError(t, err, "max_tokens is invalid")
	})

	t.Run("Gemini max output tokens", func(t *testing.T) {
		c := newTokenLimitTestContext(t, "/v1beta/models/gemini-test:generateContent", `{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"maxOutputTokens":1073741824}}`)
		request, err := GetAndValidateGeminiRequest(c)
		require.Nil(t, request)
		require.EqualError(t, err, "maxOutputTokens is invalid")
	})
}

func TestOpenAIRequestValidatorAcceptsLargestSupportedTokenLimit(t *testing.T) {
	c := newTokenLimitTestContext(t, "/v1/chat/completions", `{"model":"gpt-test","messages":[{"role":"user","content":"hi"}],"max_completion_tokens":1073741823}`)
	request, err := GetAndValidateTextRequest(c, relayconstant.RelayModeChatCompletions)
	require.NoError(t, err)
	require.NotNil(t, request)
	require.Equal(t, uint(1073741823), *request.MaxCompletionTokens)
}
