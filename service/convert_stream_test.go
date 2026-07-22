package service

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamResponseOpenAI2ClaudeClosesFinishedChunkWithStoredUsage(t *testing.T) {
	finishReason := "stop"
	storedUsage := &dto.Usage{
		PromptTokens:     12,
		CompletionTokens: 4,
		TotalTokens:      16,
	}
	info := &relaycommon.RelayInfo{
		ClaudeConvertInfo: &relaycommon.ClaudeConvertInfo{
			LastMessagesType: relaycommon.LastMessageTypeNone,
			Usage:            storedUsage,
		},
	}
	response := &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{FinishReason: &finishReason},
		},
	}

	converted := StreamResponseOpenAI2Claude(response, info)

	require.Len(t, converted, 2)
	assert.Equal(t, "message_delta", converted[0].Type)
	require.NotNil(t, converted[0].Usage)
	assert.Equal(t, storedUsage.PromptTokens, converted[0].Usage.InputTokens)
	assert.Equal(t, storedUsage.CompletionTokens, converted[0].Usage.OutputTokens)
	assert.Equal(t, "message_stop", converted[1].Type)
	assert.True(t, info.ClaudeConvertInfo.Done)
}
