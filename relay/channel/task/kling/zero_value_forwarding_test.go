package kling

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestKlingTaskMetadataPreservesExplicitZeroScalars(t *testing.T) {
	converted, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Prompt: "hello",
		Metadata: map[string]any{
			"cfg_scale": 0,
			"camera_control": map[string]any{"config": map[string]any{
				"horizontal": 0, "vertical": 0, "pan": 0, "tilt": 0, "roll": 0, "zoom": 0,
			}},
		},
	}, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}})
	require.NoError(t, err)
	encoded, err := common.Marshal(converted)
	require.NoError(t, err)
	for _, path := range []string{
		"cfg_scale", "camera_control.config.horizontal", "camera_control.config.vertical",
		"camera_control.config.pan", "camera_control.config.tilt", "camera_control.config.roll", "camera_control.config.zoom",
	} {
		assert.Truef(t, gjson.GetBytes(encoded, path).Exists(), "%s must be forwarded", path)
	}
}
