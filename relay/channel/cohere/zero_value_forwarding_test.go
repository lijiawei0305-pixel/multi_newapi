package cohere

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCohereMaxTokensDefaultsOnlyWhenAbsent(t *testing.T) {
	absent := requestOpenAI2Cohere(dto.GeneralOpenAIRequest{})
	assert.Equal(t, uint(4000), absent.MaxTokens)

	explicitZero := requestOpenAI2Cohere(dto.GeneralOpenAIRequest{
		MaxTokens:           common.GetPointer(uint(100)),
		MaxCompletionTokens: common.GetPointer(uint(0)),
	})
	assert.Zero(t, explicitZero.MaxTokens)
}

func TestCohereRerankConversionPreservesExplicitZeroAndFalse(t *testing.T) {
	converted := requestConvertRerank2Cohere(dto.RerankRequest{
		TopN:            common.GetPointer(0),
		ReturnDocuments: common.GetPointer(false),
	})
	require.NotNil(t, converted)
	assert.Zero(t, converted.TopN)
	assert.False(t, converted.ReturnDocuments)

	absent := requestConvertRerank2Cohere(dto.RerankRequest{})
	assert.Equal(t, 1, absent.TopN)
	assert.True(t, absent.ReturnDocuments)
}
