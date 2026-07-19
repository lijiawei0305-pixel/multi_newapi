package baidu

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestBaiduConversionPreservesExplicitZeroScalars(t *testing.T) {
	absent, err := common.Marshal(requestOpenAI2Baidu(dto.GeneralOpenAIRequest{}))
	require.NoError(t, err)
	for _, path := range []string{"top_p", "penalty_score", "stream", "max_output_tokens"} {
		assert.Falsef(t, gjson.GetBytes(absent, path).Exists(), "%s must be absent", path)
	}

	zero, err := common.Marshal(requestOpenAI2Baidu(dto.GeneralOpenAIRequest{
		TopP:                common.GetPointer(0.0),
		FrequencyPenalty:    common.GetPointer(0.0),
		Stream:              common.GetPointer(false),
		MaxCompletionTokens: common.GetPointer(uint(0)),
	}))
	require.NoError(t, err)
	for _, path := range []string{"top_p", "penalty_score", "stream", "max_output_tokens"} {
		assert.Truef(t, gjson.GetBytes(zero, path).Exists(), "%s must be forwarded", path)
	}
}
