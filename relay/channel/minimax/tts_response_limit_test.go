package minimax

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMiniMaxBufferedContext(t *testing.T) (*gin.Context, *common.BufferedResponseWriter, *httptest.ResponseRecorder) {
	t.Helper()
	t.Setenv("RELAY_RESPONSE_BUFFER_TEMP_DIR", t.TempDir())
	t.Setenv("RELAY_RESPONSE_BUFFER_MEMORY_BYTES", "4")
	t.Setenv("RELAY_RESPONSE_BUFFER_MAX_BYTES", "128")
	t.Setenv("RELAY_RESPONSE_BUFFER_TOTAL_BYTES", "128")
	t.Setenv("RELAY_RESPONSE_BUFFER_MAX_CONCURRENT", "1")
	t.Setenv("RELAY_RESPONSE_BUFFER_DISK_SAFETY_BYTES", "0")
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	buffered, err := common.NewBufferedResponseWriter(c.Writer)
	require.NoError(t, err)
	c.Writer = buffered
	t.Cleanup(buffered.Discard)
	return c, buffered, recorder
}

func TestMiniMaxTTSJSONLimitUsesConservativeAcceptedFailure(t *testing.T) {
	t.Setenv("RELAY_UPSTREAM_JSON_MAX_BYTES", "8")
	c, buffered, recorder := newMiniMaxBufferedContext(t)
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", 9)))}

	usage, apiErr := handleTTSResponse(c, resp, &relaycommon.RelayInfo{})

	assert.Nil(t, usage, "unknown usage must not be replaced with a fabricated zero estimate")
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	assert.True(t, types.IsSkipRetryError(apiErr))
	require.ErrorIs(t, buffered.Err(), common.ErrReadLimitExceeded)
	assert.Empty(t, recorder.Body.String())
}

func TestMiniMaxTTSDecodeFailurePreservesReliableUsageForActualSettlement(t *testing.T) {
	t.Setenv("RELAY_UPSTREAM_JSON_MAX_BYTES", "1024")
	c, buffered, recorder := newMiniMaxBufferedContext(t)
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{
		"data":{"audio":"0","status":2},
		"extra_info":{"usage_characters":123},
		"base_resp":{"status_code":0,"status_msg":""}
	}`))}

	usageValue, apiErr := handleTTSResponse(c, resp, &relaycommon.RelayInfo{})

	assert.Nil(t, apiErr)
	usage, ok := usageValue.(*dto.Usage)
	require.True(t, ok)
	assert.Equal(t, 123, usage.TotalTokens)
	require.Error(t, buffered.Err(), "decoded delivery failure must be surfaced after actual usage settlement")
	assert.Empty(t, recorder.Body.String())
}

func TestMiniMaxTTSStructured200ErrorRemainsUnacceptedRejection(t *testing.T) {
	t.Setenv("RELAY_UPSTREAM_JSON_MAX_BYTES", "1024")
	c, _, recorder := newMiniMaxBufferedContext(t)
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{
		"data":{"audio":"","status":0},
		"base_resp":{"status_code":1008,"status_msg":"voice rejected"}
	}`))}

	usage, apiErr := handleTTSResponse(c, resp, &relaycommon.RelayInfo{})

	assert.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.False(t, service.IsUpstreamAccepted(c))
	assert.False(t, types.IsSkipRetryError(apiErr))
	assert.Empty(t, recorder.Body.String())
}
