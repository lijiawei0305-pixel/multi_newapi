package gemini

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertImageRequestPreservesExplicitZeroSampleCount(t *testing.T) {
	zero := uint(0)
	converted, err := (&Adaptor{}).ConvertImageRequest(nil, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "imagen-3.0-generate-002"},
	}, dto.ImageRequest{Prompt: "test", N: &zero})

	require.NoError(t, err)
	request, ok := converted.(dto.GeminiImageRequest)
	require.True(t, ok)
	require.NotNil(t, request.Parameters.SampleCount)
	assert.Zero(t, *request.Parameters.SampleCount)
	body, err := common.Marshal(request)
	require.NoError(t, err)
	assert.Contains(t, string(body), `"sampleCount":0`)
}

func TestConvertImageRequestDefaultsAbsentSampleCountToOne(t *testing.T) {
	converted, err := (&Adaptor{}).ConvertImageRequest(nil, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "imagen-3.0-generate-002"},
	}, dto.ImageRequest{Prompt: "test"})

	require.NoError(t, err)
	request, ok := converted.(dto.GeminiImageRequest)
	require.True(t, ok)
	require.NotNil(t, request.Parameters.SampleCount)
	assert.Equal(t, 1, *request.Parameters.SampleCount)
}
