package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIImageJSONLimitFailsAcceptedResponseWithoutPublishing(t *testing.T) {
	t.Setenv("RELAY_UPSTREAM_JSON_MAX_BYTES", "8")
	t.Setenv("RELAY_RESPONSE_BUFFER_TEMP_DIR", t.TempDir())
	t.Setenv("RELAY_RESPONSE_BUFFER_MEMORY_BYTES", "4")
	t.Setenv("RELAY_RESPONSE_BUFFER_MAX_BYTES", "64")
	t.Setenv("RELAY_RESPONSE_BUFFER_TOTAL_BYTES", "64")
	t.Setenv("RELAY_RESPONSE_BUFFER_MAX_CONCURRENT", "1")
	t.Setenv("RELAY_RESPONSE_BUFFER_DISK_SAFETY_BYTES", "0")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	buffered, err := common.NewBufferedResponseWriter(c.Writer)
	require.NoError(t, err)
	c.Writer = buffered
	t.Cleanup(buffered.Discard)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", 9))),
		Header:     make(http.Header),
	}

	usage, apiErr := OpenaiImageHandler(c, &relaycommon.RelayInfo{}, resp)

	assert.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	assert.True(t, types.IsSkipRetryError(apiErr))
	require.ErrorIs(t, buffered.Err(), common.ErrReadLimitExceeded)
	assert.Empty(t, recorder.Body.String())
}

func TestOpenAITTSSpoolLimitReturnsUnknownUsageForReservedFallback(t *testing.T) {
	t.Setenv("RELAY_RESPONSE_BUFFER_TEMP_DIR", t.TempDir())
	t.Setenv("RELAY_RESPONSE_BUFFER_MEMORY_BYTES", "4")
	t.Setenv("RELAY_RESPONSE_BUFFER_MAX_BYTES", "8")
	t.Setenv("RELAY_RESPONSE_BUFFER_TOTAL_BYTES", "8")
	t.Setenv("RELAY_RESPONSE_BUFFER_MAX_CONCURRENT", "1")
	t.Setenv("RELAY_RESPONSE_BUFFER_DISK_SAFETY_BYTES", "0")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
	buffered, err := common.NewBufferedResponseWriter(c.Writer)
	require.NoError(t, err)
	c.Writer = buffered
	t.Cleanup(buffered.Discard)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("123456789")),
		Header:     make(http.Header),
	}

	usage, apiErr := OpenaiTTSHandler(c, resp, &relaycommon.RelayInfo{})

	assert.Nil(t, usage, "partial copied bytes must never become fabricated actual usage")
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	assert.True(t, types.IsSkipRetryError(apiErr))
	require.ErrorIs(t, buffered.Err(), common.ErrBufferedResponseTooLarge)
	assert.Empty(t, recorder.Body.String())
}

func TestOpenAITTSStructured200ErrorRemainsUnacceptedRejection(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"voice rejected","type":"invalid_request_error","code":"voice_rejected"}}`)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}

	usage, apiErr := OpenaiTTSHandler(c, resp, &relaycommon.RelayInfo{})

	assert.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.False(t, service.IsUpstreamAccepted(c), "provider-declared rejection remains refundable/retryable")
	assert.False(t, types.IsSkipRetryError(apiErr))
	assert.Contains(t, apiErr.Error(), "voice rejected")
	assert.Empty(t, recorder.Body.String())
}

func TestOpenAITTSStructured200ErrorWithoutTypeRemainsUnaccepted(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"voice rejected","code":"voice_rejected"}}`)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}

	usage, apiErr := OpenaiTTSHandler(c, resp, &relaycommon.RelayInfo{})

	assert.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.False(t, service.IsUpstreamAccepted(c))
	assert.False(t, types.IsSkipRetryError(apiErr))
}
