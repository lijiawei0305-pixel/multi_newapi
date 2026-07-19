package ali

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestAliImageParametersPreserveExplicitZeroAndFalse(t *testing.T) {
	var request dto.ImageRequest
	require.NoError(t, common.Unmarshal([]byte(`{
		"model":"wanx-v1","prompt":"hello",
		"parameters":{"seed":0,"watermark":false,"prompt_extend":false}
	}`), &request))

	converted, err := oaiImage2AliImageRequest(&relaycommon.RelayInfo{}, request, false)
	require.NoError(t, err)
	encoded, err := common.Marshal(converted)
	require.NoError(t, err)
	for _, path := range []string{"parameters.seed", "parameters.watermark", "parameters.prompt_extend"} {
		assert.Truef(t, gjson.GetBytes(encoded, path).Exists(), "%s must be forwarded", path)
	}
}

func TestAliImageParametersRejectExplicitZeroNInsteadOfUsingProviderDefault(t *testing.T) {
	var request dto.ImageRequest
	require.NoError(t, common.Unmarshal([]byte(`{
		"model":"wanx-v1","prompt":"hello","parameters":{"n":0}
	}`), &request))

	converted, err := oaiImage2AliImageRequest(&relaycommon.RelayInfo{}, request, false)
	assert.Nil(t, converted)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parameters.n must be greater than zero")
}
