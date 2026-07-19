package ali

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAliImagePollingCancellationReturnsPromptlyAndStaysAccepted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil).WithContext(ctx)
	started := time.Now()

	_, _, err := asyncTaskWait(c, &relaycommon.RelayInfo{}, "accepted-task-id")

	require.ErrorIs(t, err, context.Canceled)
	assert.Less(t, time.Since(started), 200*time.Millisecond)
	assert.True(t, service.IsUpstreamAccepted(c), "a parsed async task id is an irreversible provider ACK")
}

func TestAliImageStructured200ErrorRemainsUnaccepted(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	response := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"code":"InvalidParameter","message":"prompt rejected"}`)),
	}

	apiErr, usage := aliImageHandler(&Adaptor{IsSyncImageModel: true}, c, response, &relaycommon.RelayInfo{})

	assert.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.False(t, service.IsUpstreamAccepted(c))
}

type trackingAliImageBody struct {
	io.Reader
	closed bool
}

func (b *trackingAliImageBody) Close() error {
	b.closed = true
	return nil
}

func TestAliImageClosesBodyWhenBoundedReadFails(t *testing.T) {
	t.Setenv("RELAY_UPSTREAM_JSON_MAX_BYTES", "4")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	body := &trackingAliImageBody{Reader: strings.NewReader(`{"output":{}}`)}
	response := &http.Response{StatusCode: http.StatusOK, Body: body}

	apiErr, usage := aliImageHandler(&Adaptor{IsSyncImageModel: true}, c, response, &relaycommon.RelayInfo{})

	assert.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.True(t, service.IsUpstreamAccepted(c))
	assert.True(t, body.closed)
}

func TestAliInlineImagesShareOneCumulativeEncodedBudget(t *testing.T) {
	t.Setenv("RELAY_UPSTREAM_JSON_MAX_BYTES", "1000")
	t.Setenv("RELAY_RESPONSE_BUFFER_MAX_BYTES", "300")
	output := AliOutput{Results: []TaskResult{
		{B64Image: strings.Repeat("A", 100)},
		{B64Image: strings.Repeat("B", 100)},
	}}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	images := output.ResultToOpenAIImageDate(c, "b64_json", service.NewImageResponseEncodedBudget())

	require.Len(t, images, 1)
	assert.Equal(t, strings.Repeat("A", 100), images[0].B64Json)
}
