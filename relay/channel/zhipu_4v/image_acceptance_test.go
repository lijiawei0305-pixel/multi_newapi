package zhipu_4v

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestZhipuImageBase64OnlyOutputIsDelivered(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"data":[{"b64_json":"aW1hZ2U="}]}`)),
	}
	info := &relaycommon.RelayInfo{StartTime: time.Unix(1700000000, 0)}

	usage, apiErr := zhipu4vImageHandler(c, resp, info)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.True(t, service.IsUpstreamAccepted(c))
	assert.Contains(t, recorder.Body.String(), `"b64_json":"aW1hZ2U="`)
}

func TestZhipuImagesShareOneCumulativeEncodedBudget(t *testing.T) {
	t.Setenv("RELAY_UPSTREAM_JSON_MAX_BYTES", "1000")
	t.Setenv("RELAY_RESPONSE_BUFFER_MAX_BYTES", "300")
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	first := strings.Repeat("A", 100)
	second := strings.Repeat("B", 100)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(
			`{"data":[{"b64_json":"` + first + `"},{"b64_json":"` + second + `"}]}`,
		)),
	}

	usage, apiErr := zhipu4vImageHandler(c, resp, &relaycommon.RelayInfo{StartTime: time.Now()})

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.True(t, service.IsUpstreamAccepted(c))
	assert.Contains(t, recorder.Body.String(), first)
	assert.NotContains(t, recorder.Body.String(), second)
}
