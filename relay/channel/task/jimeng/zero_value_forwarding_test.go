package jimeng

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestJimengTaskFramesDefaultOnlyWhenAbsent(t *testing.T) {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	absent, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{Prompt: "hello"}, info)
	require.NoError(t, err)
	absentJSON, err := common.Marshal(absent)
	require.NoError(t, err)
	assert.Equal(t, int64(121), gjson.GetBytes(absentJSON, "frames").Int())
	assert.False(t, gjson.GetBytes(absentJSON, "seed").Exists())

	zero, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Prompt: "hello", Duration: common.GetPointer(0), Metadata: map[string]any{"frames": 0, "seed": 0},
	}, info)
	require.NoError(t, err)
	zeroJSON, err := common.Marshal(zero)
	require.NoError(t, err)
	assert.True(t, gjson.GetBytes(zeroJSON, "frames").Exists())
	assert.Zero(t, gjson.GetBytes(zeroJSON, "frames").Int())
	assert.True(t, gjson.GetBytes(zeroJSON, "seed").Exists())
	assert.Zero(t, gjson.GetBytes(zeroJSON, "seed").Int())
}
