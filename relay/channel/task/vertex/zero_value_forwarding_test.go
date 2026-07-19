package vertex

import (
	"io"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestVertexVeoTaskPreservesExplicitZeroDuration(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/videos", nil)
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Prompt: "hello", Metadata: map[string]any{"durationSeconds": 0},
	})
	reader, err := (&TaskAdaptor{}).BuildRequestBody(c, &relaycommon.RelayInfo{})
	require.NoError(t, err)
	encoded, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.True(t, gjson.GetBytes(encoded, "parameters.durationSeconds").Exists())
	assert.Zero(t, gjson.GetBytes(encoded, "parameters.durationSeconds").Int())
}
