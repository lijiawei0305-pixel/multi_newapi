package service

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExplicitUpstreamRejectionMarkerSurvivesNewAPIError(t *testing.T) {
	apiErr := types.NewError(errors.New("provider rejected request"), types.ErrorCodeBadResponse)

	marked := MarkExplicitUpstreamRejection(apiErr)

	require.Same(t, apiErr, marked)
	assert.True(t, IsExplicitUpstreamRejection(marked))
	assert.EqualError(t, marked, "provider rejected request")
	assert.Same(t, marked, MarkExplicitUpstreamRejection(marked))
}

func TestExplicitUpstreamRejectionDoesNotMatchLocalError(t *testing.T) {
	assert.False(t, IsExplicitUpstreamRejection(errors.New("decode failed")))
	assert.False(t, IsExplicitUpstreamRejection(nil))
}

func TestExplicitUpstreamRejectionHandlesErrorWithoutCause(t *testing.T) {
	apiErr := &types.NewAPIError{}

	marked := MarkExplicitUpstreamRejection(apiErr)

	require.Same(t, apiErr, marked)
	assert.True(t, IsExplicitUpstreamRejection(marked))
	assert.EqualError(t, marked, "upstream rejected request")
}
