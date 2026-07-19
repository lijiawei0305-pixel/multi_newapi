package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCoerceTestUsageRejectsTypedNilAndWrongStreamUsage(t *testing.T) {
	var typedNil *dto.Usage
	for _, value := range []any{nil, typedNil, "wrong"} {
		assert.NotPanics(t, func() {
			usage, err := coerceTestUsage(value, true, 12)
			assert.Nil(t, usage)
			require.Error(t, err)
		})
	}
}

func TestCoerceTestUsageAcceptsPointerAndValue(t *testing.T) {
	pointer := &dto.Usage{TotalTokens: 3}
	usage, err := coerceTestUsage(pointer, false, 0)
	require.NoError(t, err)
	assert.Same(t, pointer, usage)

	usage, err = coerceTestUsage(dto.Usage{TotalTokens: 4}, false, 0)
	require.NoError(t, err)
	assert.Equal(t, 4, usage.TotalTokens)
}
