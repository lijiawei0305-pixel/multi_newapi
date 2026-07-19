package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageResponseEncodedBudgetUsesSmallestResponseLimit(t *testing.T) {
	t.Setenv("RELAY_UPSTREAM_JSON_MAX_BYTES", "512")
	t.Setenv("RELAY_RESPONSE_BUFFER_MAX_BYTES", "400")
	budget := NewImageResponseEncodedBudget()

	assert.Equal(t, int64(400), budget.Limit())
	assert.Equal(t, int64(204), budget.RemainingRawBytes())
	require.NoError(t, budget.ConsumeBase64(strings.Repeat("A", 272)))
	assert.Zero(t, budget.RemainingRawBytes())
	err := budget.ConsumeBase64("AAAA")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrImageResponseBudgetExceeded))
}

func TestImageResponseEncodedBudgetIsCumulative(t *testing.T) {
	t.Setenv("RELAY_UPSTREAM_JSON_MAX_BYTES", "500")
	t.Setenv("RELAY_RESPONSE_BUFFER_MAX_BYTES", "500")
	budget := NewImageResponseEncodedBudget()

	require.NoError(t, budget.ConsumeBase64(strings.Repeat("A", 100)))
	assert.Equal(t, int64(108), budget.RemainingRawBytes())
	err := budget.ConsumeBase64(strings.Repeat("B", 145))
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrImageResponseBudgetExceeded))
}

func TestImageResponseEncodedBudgetAccountsForExistingMetadata(t *testing.T) {
	t.Setenv("RELAY_UPSTREAM_JSON_MAX_BYTES", "500")
	t.Setenv("RELAY_RESPONSE_BUFFER_MAX_BYTES", "500")
	budget := NewImageResponseEncodedBudget()

	require.NoError(t, budget.ReserveEncodedBytes(300))
	assert.Equal(t, int64(54), budget.RemainingRawBytes())
	err := budget.ConsumeBase64(strings.Repeat("A", 100))
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrImageResponseBudgetExceeded))
}
