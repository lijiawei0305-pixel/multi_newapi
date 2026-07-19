package minimax

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestMiniMaxImageConversionPreservesExplicitZeroN(t *testing.T) {
	converted := oaiImage2MiniMaxImageRequest(dto.ImageRequest{N: common.GetPointer(uint(0))})
	encoded, err := common.Marshal(converted)
	require.NoError(t, err)
	assert.True(t, gjson.GetBytes(encoded, "n").Exists())
	assert.Zero(t, gjson.GetBytes(encoded, "n").Int())
}
