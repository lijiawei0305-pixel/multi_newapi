package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRWMapInvalidJSONPreservesPublishedSnapshot(t *testing.T) {
	values := NewRWMap[string, float64]()
	values.Set("existing", 1.5)
	callbackCalls := 0

	require.Error(t, LoadFromJsonString(values, `{`))
	assert.Equal(t, map[string]float64{"existing": 1.5}, values.ReadAll())

	require.Error(t, LoadFromJsonStringWithCallback(values, `[]`, func() {
		callbackCalls++
	}))
	assert.Equal(t, map[string]float64{"existing": 1.5}, values.ReadAll())
	assert.Zero(t, callbackCalls)

	require.Error(t, values.UnmarshalJSON([]byte(`{"broken":`)))
	assert.Equal(t, map[string]float64{"existing": 1.5}, values.ReadAll())
}

func TestRWMapValidJSONPublishesCompleteReplacement(t *testing.T) {
	values := NewRWMap[string, int]()
	values.Set("old", 1)
	callbackCalls := 0

	require.NoError(t, LoadFromJsonStringWithCallback(values, `{"new":2}`, func() {
		callbackCalls++
	}))
	assert.Equal(t, map[string]int{"new": 2}, values.ReadAll())
	assert.Equal(t, 1, callbackCalls)
}
