package vidu

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestViduTaskConversionPreservesExplicitZeroAndFalse(t *testing.T) {
	converted, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Prompt:   "hello",
		Metadata: map[string]any{"duration": 0, "seed": 0, "bgm": false},
	}, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}})
	require.NoError(t, err)
	encoded, err := common.Marshal(converted)
	require.NoError(t, err)
	for _, path := range []string{"duration", "seed", "bgm"} {
		assert.Truef(t, gjson.GetBytes(encoded, path).Exists(), "%s must be forwarded", path)
	}

	absent, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{Prompt: "hello"}, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}})
	require.NoError(t, err)
	absentJSON, err := common.Marshal(absent)
	require.NoError(t, err)
	assert.Equal(t, int64(5), gjson.GetBytes(absentJSON, "duration").Int())
	assert.False(t, gjson.GetBytes(absentJSON, "seed").Exists())
	assert.False(t, gjson.GetBytes(absentJSON, "bgm").Exists())
}
