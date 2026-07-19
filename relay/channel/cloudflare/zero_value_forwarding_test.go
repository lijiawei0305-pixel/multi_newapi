package cloudflare

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCloudflareConversionPreservesZeroAndFalsePresence(t *testing.T) {
	absent, err := common.Marshal(convertCf2CompletionsRequest(dto.GeneralOpenAIRequest{}))
	require.NoError(t, err)
	assert.False(t, gjson.GetBytes(absent, "max_tokens").Exists())
	assert.False(t, gjson.GetBytes(absent, "stream").Exists())

	zero, err := common.Marshal(convertCf2CompletionsRequest(dto.GeneralOpenAIRequest{
		MaxTokens:           common.GetPointer(uint(100)),
		MaxCompletionTokens: common.GetPointer(uint(0)),
		Stream:              common.GetPointer(false),
	}))
	require.NoError(t, err)
	assert.True(t, gjson.GetBytes(zero, "max_tokens").Exists())
	assert.Zero(t, gjson.GetBytes(zero, "max_tokens").Int())
	assert.True(t, gjson.GetBytes(zero, "stream").Exists())
	assert.False(t, gjson.GetBytes(zero, "stream").Bool())
}
