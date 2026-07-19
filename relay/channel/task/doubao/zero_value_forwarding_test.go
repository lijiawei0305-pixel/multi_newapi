package doubao

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestDoubaoTaskMetadataPreservesExplicitZeroAndFalse(t *testing.T) {
	converted, err := (&TaskAdaptor{}).convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Prompt: "hello",
		Metadata: map[string]any{
			"duration": 0, "frames": 0, "seed": 0,
			"return_last_frame": false, "generate_audio": false, "draft": false, "camera_fixed": false, "watermark": false,
		},
	})
	require.NoError(t, err)
	encoded, err := common.Marshal(converted)
	require.NoError(t, err)
	for _, path := range []string{
		"duration", "frames", "seed", "return_last_frame", "generate_audio", "draft", "camera_fixed", "watermark",
	} {
		assert.Truef(t, gjson.GetBytes(encoded, path).Exists(), "%s must be forwarded", path)
	}
}
