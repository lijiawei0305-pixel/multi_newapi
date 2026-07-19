package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel/replicate"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplicateCreatedACKProtectsMalformedOrPendingResponse(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "malformed", body: `{"status":`},
		{name: "still processing", body: `{"id":"prediction-1","status":"processing"}`},
		{name: "terminal failed after ACK", body: `{"id":"prediction-1","status":"failed"}`},
		{name: "provider error", body: `{"id":"prediction-1","error":{"message":"prediction failed"}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiType: constant.APITypeReplicate}}
			response := &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(test.body))}

			require.True(t, acceptReplicateCreatedResponse(c, info, response))
			assert.Equal(t, http.StatusOK, response.StatusCode)
			assert.True(t, service.IsUpstreamAccepted(c), "a 201 prediction ACK must never be retried or refunded")
			_, apiErr := (&replicate.Adaptor{}).DoResponse(c, response, info)
			require.NotNil(t, apiErr)
		})
	}
}

func TestReplicateHTTP200TerminalFailureRemainsUnaccepted(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiType: constant.APITypeReplicate}}
	response := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"id":"prediction-1","status":"failed","output":null}`)),
	}

	usage, apiErr := (&replicate.Adaptor{}).DoResponse(c, response, info)

	assert.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.False(t, service.IsUpstreamAccepted(c))
}

type trackingReplicateBody struct {
	io.Reader
	closed bool
}

func (b *trackingReplicateBody) Close() error {
	b.closed = true
	return nil
}

func TestReplicateClosesBodyWhenBoundedReadFails(t *testing.T) {
	t.Setenv("RELAY_UPSTREAM_JSON_MAX_BYTES", "4")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiType: constant.APITypeReplicate}}
	body := &trackingReplicateBody{Reader: strings.NewReader(`{"status":"succeeded"}`)}
	response := &http.Response{StatusCode: http.StatusOK, Body: body}

	usage, apiErr := (&replicate.Adaptor{}).DoResponse(c, response, info)

	assert.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.True(t, service.IsUpstreamAccepted(c))
	assert.True(t, body.closed)
}
