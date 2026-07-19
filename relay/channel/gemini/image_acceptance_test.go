package gemini

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeminiImageAllSafetyFilteredIsExplicitUnacceptedRejection(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"predictions":[{"raiFilteredReason":"safety policy"}]}`)),
	}

	usage, apiErr := GeminiImageHandler(c, &relaycommon.RelayInfo{}, resp)

	assert.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodePromptBlocked, apiErr.GetErrorCode())
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.False(t, service.IsUpstreamAccepted(c))
	assert.Empty(t, recorder.Body.Bytes())
}

func TestGeminiImageEmptySuccessIsAcceptedDeliveryFailure(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"predictions":[]}`))}

	usage, apiErr := GeminiImageHandler(c, &relaycommon.RelayInfo{}, resp)

	assert.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.True(t, service.IsUpstreamAccepted(c))
	assert.Empty(t, recorder.Body.Bytes())
}

func TestGeminiImageUsableBase64MarksAccepted(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"predictions":[{"mimeType":"image/png","bytesBase64Encoded":"aW1hZ2U="}]}`)),
	}

	usage, apiErr := GeminiImageHandler(c, &relaycommon.RelayInfo{}, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.True(t, service.IsUpstreamAccepted(c))
	assert.Contains(t, recorder.Body.String(), `"b64_json":"aW1hZ2U="`)
}

func TestGeminiImageCumulativeEncodedBudgetIsTerminal(t *testing.T) {
	t.Setenv("RELAY_UPSTREAM_JSON_MAX_BYTES", "256")
	t.Setenv("RELAY_RESPONSE_BUFFER_MAX_BYTES", "256")
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	encoded := strings.Repeat("A", 150)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"predictions":[{"bytesBase64Encoded":"` + encoded + `"}]}`)),
	}

	usage, apiErr := GeminiImageHandler(c, &relaycommon.RelayInfo{}, resp)

	assert.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.True(t, service.IsUpstreamAccepted(c))
	assert.Empty(t, recorder.Body.Bytes())
}
