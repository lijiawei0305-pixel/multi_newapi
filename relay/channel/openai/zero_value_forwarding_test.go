package openai

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReasoningModelConversionMovesExplicitZeroMaxTokens(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{Model: "gpt-5", MaxTokens: common.GetPointer(uint(0))}
	converted, err := (&Adaptor{}).ConvertOpenAIRequest(nil, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5"},
	}, request)
	require.NoError(t, err)
	result := converted.(*dto.GeneralOpenAIRequest)
	assert.Nil(t, result.MaxTokens)
	require.NotNil(t, result.MaxCompletionTokens)
	assert.Zero(t, *result.MaxCompletionTokens)
}

func TestOpenRouterThinkingPreservesExplicitZeroBudget(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{
		Model:    "anthropic/claude",
		THINKING: []byte(`{"type":"enabled","budget_tokens":0}`),
	}
	converted, err := (&Adaptor{}).ConvertOpenAIRequest(nil, &relaycommon.RelayInfo{
		OriginModelName: "anthropic/claude",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenRouter,
			UpstreamModelName: "anthropic/claude",
		},
	}, request)
	require.NoError(t, err)
	result := converted.(*dto.GeneralOpenAIRequest)
	encoded := result.Reasoning
	var reasoning map[string]any
	require.NoError(t, common.Unmarshal(encoded, &reasoning))
	assert.Contains(t, reasoning, "max_tokens")
	assert.Equal(t, float64(0), reasoning["max_tokens"])
}
