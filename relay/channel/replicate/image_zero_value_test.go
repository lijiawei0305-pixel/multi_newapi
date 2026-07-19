package replicate

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestReplicateImageConversionPreservesExplicitZeroN(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	converted, err := (&Adaptor{}).ConvertImageRequest(c, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}, dto.ImageRequest{
		Prompt: "hello",
		N:      common.GetPointer(uint(0)),
	})
	require.NoError(t, err)
	encoded, err := common.Marshal(converted)
	require.NoError(t, err)
	assert.True(t, gjson.GetBytes(encoded, "input.num_outputs").Exists())
	assert.Zero(t, gjson.GetBytes(encoded, "input.num_outputs").Int())
}
