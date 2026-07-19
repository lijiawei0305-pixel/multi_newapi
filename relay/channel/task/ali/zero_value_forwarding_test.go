package ali

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestAliTaskMetadataPreservesExplicitZeroAndFalse(t *testing.T) {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	converted, err := (&TaskAdaptor{}).convertToAliRequest(info, relaycommon.TaskSubmitReq{
		Model: "wan2.6-i2v", Prompt: "hello",
		Metadata: map[string]any{"parameters": map[string]any{
			"duration": 0, "prompt_extend": false, "watermark": false, "seed": 0,
		}},
	})
	require.NoError(t, err)
	encoded, err := common.Marshal(converted)
	require.NoError(t, err)
	for _, path := range []string{"parameters.duration", "parameters.prompt_extend", "parameters.watermark", "parameters.seed"} {
		assert.Truef(t, gjson.GetBytes(encoded, path).Exists(), "%s must be forwarded", path)
	}
}

func TestAliTaskDurationDefaultsOnlyWhenAbsent(t *testing.T) {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	absent, err := (&TaskAdaptor{}).convertToAliRequest(info, relaycommon.TaskSubmitReq{Model: "wan2.6-i2v", Prompt: "hello"})
	require.NoError(t, err)
	require.NotNil(t, absent.Parameters.Duration)
	assert.Equal(t, 5, *absent.Parameters.Duration)

	zero, err := (&TaskAdaptor{}).convertToAliRequest(info, relaycommon.TaskSubmitReq{
		Model: "wan2.6-i2v", Prompt: "hello", Duration: common.GetPointer(0),
	})
	require.NoError(t, err)
	require.NotNil(t, zero.Parameters.Duration)
	assert.Zero(t, *zero.Parameters.Duration)
}
