package xai

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestXAIMiniConversionMovesExplicitZeroMaxTokens(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{Model: "grok-3-mini", MaxTokens: common.GetPointer(uint(0))}
	converted, err := (&Adaptor{}).ConvertOpenAIRequest(nil, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "grok-3-mini"},
	}, request)
	require.NoError(t, err)
	result := converted.(*dto.GeneralOpenAIRequest)
	assert.Nil(t, result.MaxTokens)
	require.NotNil(t, result.MaxCompletionTokens)
	assert.Zero(t, *result.MaxCompletionTokens)
}
