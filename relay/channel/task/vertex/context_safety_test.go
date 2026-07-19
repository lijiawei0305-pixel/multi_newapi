package vertex

import (
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVertexTaskRejectsWrongRequestContextType(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	c.Set("task_request", map[string]any{"prompt": "wrong concrete type"})
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "veo-3.0-generate-001"},
	}

	assert.Nil(t, adaptor.EstimateBilling(c, info))
	reader, err := adaptor.BuildRequestBody(c, info)
	require.Error(t, err)
	assert.Nil(t, reader)
	assert.Contains(t, err.Error(), "invalid task request")
}
