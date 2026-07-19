package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyClaudeDefaultMaxTokensDistinguishesAbsentFromExplicitZero(t *testing.T) {
	model := "claude-test"
	absent := &dto.ClaudeRequest{Model: model}
	applyClaudeDefaultMaxTokens(absent)
	require.NotNil(t, absent.MaxTokens)
	assert.Equal(t, uint(model_setting.GetClaudeSettings().GetDefaultMaxTokens(model)), *absent.MaxTokens)

	explicitZero := &dto.ClaudeRequest{Model: model, MaxTokens: common.GetPointer(uint(0))}
	applyClaudeDefaultMaxTokens(explicitZero)
	require.NotNil(t, explicitZero.MaxTokens)
	assert.Zero(t, *explicitZero.MaxTokens)
}
