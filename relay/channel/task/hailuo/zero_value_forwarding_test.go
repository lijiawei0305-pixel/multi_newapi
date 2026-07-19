package hailuo

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestHailuoTaskConversionPreservesExplicitZeroAndFalse(t *testing.T) {
	converted, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Prompt: "hello", Duration: common.GetPointer(0),
		Metadata: map[string]any{"prompt_optimizer": false, "fast_pretreatment": false, "aigc_watermark": false},
	}, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "T2V-01"}})
	require.NoError(t, err)
	encoded, err := common.Marshal(converted)
	require.NoError(t, err)
	for _, path := range []string{"duration", "prompt_optimizer", "fast_pretreatment", "aigc_watermark"} {
		assert.Truef(t, gjson.GetBytes(encoded, path).Exists(), "%s must be forwarded", path)
	}
}
