package ali

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestAliTextConversionDoesNotDefaultExplicitZeroTopP(t *testing.T) {
	absent := requestOpenAI2Ali(dto.GeneralOpenAIRequest{})
	assert.Nil(t, absent.TopP)

	zero := requestOpenAI2Ali(dto.GeneralOpenAIRequest{TopP: common.GetPointer(0.0)})
	require.NotNil(t, zero.TopP)
	assert.Zero(t, *zero.TopP)
	encoded, err := common.Marshal(zero)
	require.NoError(t, err)
	assert.True(t, gjson.GetBytes(encoded, "top_p").Exists())
}
