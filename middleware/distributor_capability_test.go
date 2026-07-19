package middleware

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
)

func TestChannelAffinityRejectsUnsupportedClaudeCandidate(t *testing.T) {
	assert.False(t, channelSupportsRequestPath(&model.Channel{Type: constant.ChannelTypeBaidu}, "/v1/messages"))
	assert.True(t, channelSupportsRequestPath(&model.Channel{Type: constant.ChannelTypeAnthropic}, "/v1/messages"))
	assert.True(t, channelSupportsRequestPath(&model.Channel{Type: constant.ChannelTypeBaidu}, "/v1/chat/completions"))
}
