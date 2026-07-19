package siliconflow

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestSiliconFlowImageExtrasPreserveExplicitZeroScalars(t *testing.T) {
	var request dto.ImageRequest
	require.NoError(t, common.Unmarshal([]byte(`{
		"model":"black-forest-labs/FLUX.1-schnell",
		"prompt":"hello",
		"batch_size":0,
		"seed":0,
		"num_inference_steps":0,
		"guidance_scale":0,
		"cfg":0
	}`), &request))

	converted, err := (&Adaptor{}).ConvertImageRequest(nil, nil, request)
	require.NoError(t, err)
	encoded, err := common.Marshal(converted)
	require.NoError(t, err)
	for _, path := range []string{"batch_size", "seed", "num_inference_steps", "guidance_scale", "cfg"} {
		assert.Truef(t, gjson.GetBytes(encoded, path).Exists(), "%s must be forwarded", path)
		assert.Zero(t, gjson.GetBytes(encoded, path).Float())
	}
}
