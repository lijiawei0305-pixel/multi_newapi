package vertex

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVertexChatToImagenRejectsExplicitZeroNInsteadOfDefaulting(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	request := &dto.GeneralOpenAIRequest{
		Model: "imagen-3.0-generate-001",
		N:     common.GetPointer(0),
		Messages: []dto.Message{{
			Role: "user", Content: "draw a mountain",
		}},
	}
	converted, err := (&Adaptor{RequestMode: RequestModeGemini}).ConvertOpenAIRequest(c, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "imagen-3.0-generate-001"},
	}, request)
	assert.Nil(t, converted)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "n must be greater than zero")
}
