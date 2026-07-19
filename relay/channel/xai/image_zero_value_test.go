package xai

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestXAIImageConversionPreservesExplicitZeroN(t *testing.T) {
	converted, err := (&Adaptor{}).ConvertImageRequest(nil, nil, dto.ImageRequest{N: common.GetPointer(uint(0))})
	require.NoError(t, err)
	encoded, err := common.Marshal(converted)
	require.NoError(t, err)
	assert.True(t, gjson.GetBytes(encoded, "n").Exists())
	assert.Zero(t, gjson.GetBytes(encoded, "n").Int())
}
