package zhipu

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestZhipuConversionPreservesExplicitZeroTopP(t *testing.T) {
	absent, err := common.Marshal(requestOpenAI2Zhipu(dto.GeneralOpenAIRequest{}))
	require.NoError(t, err)
	assert.False(t, gjson.GetBytes(absent, "top_p").Exists())

	zero, err := common.Marshal(requestOpenAI2Zhipu(dto.GeneralOpenAIRequest{TopP: common.GetPointer(0.0)}))
	require.NoError(t, err)
	assert.True(t, gjson.GetBytes(zero, "top_p").Exists())
	assert.Zero(t, gjson.GetBytes(zero, "top_p").Float())
}
