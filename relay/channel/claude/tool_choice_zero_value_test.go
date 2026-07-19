package claude

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestClaudeToolChoiceForwardsExplicitFalseDisableParallel(t *testing.T) {
	choice := mapToolChoice("auto", common.GetPointer(true))
	require.NotNil(t, choice)
	require.NotNil(t, choice.DisableParallelToolUse)
	assert.False(t, *choice.DisableParallelToolUse)

	encoded, err := common.Marshal(choice)
	require.NoError(t, err)
	assert.True(t, gjson.GetBytes(encoded, "disable_parallel_tool_use").Exists())
	assert.False(t, gjson.GetBytes(encoded, "disable_parallel_tool_use").Bool())
}
