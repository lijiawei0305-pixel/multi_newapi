package xunfei

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestXunfeiConversionPreservesExplicitZeroScalars(t *testing.T) {
	absent, err := common.Marshal(requestOpenAI2Xunfei(dto.GeneralOpenAIRequest{}, "app", "domain"))
	require.NoError(t, err)
	assert.False(t, gjson.GetBytes(absent, "parameter.chat.top_k").Exists())
	assert.False(t, gjson.GetBytes(absent, "parameter.chat.max_tokens").Exists())

	zero, err := common.Marshal(requestOpenAI2Xunfei(dto.GeneralOpenAIRequest{
		N:                   common.GetPointer(0),
		MaxCompletionTokens: common.GetPointer(uint(0)),
	}, "app", "domain"))
	require.NoError(t, err)
	assert.True(t, gjson.GetBytes(zero, "parameter.chat.top_k").Exists())
	assert.True(t, gjson.GetBytes(zero, "parameter.chat.max_tokens").Exists())
}
