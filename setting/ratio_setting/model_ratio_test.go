package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultPerplexityLargeModelRatiosPreserveFractionalPrice(t *testing.T) {
	t.Parallel()

	for _, model := range []string{
		"llama-3-sonar-large-32k-chat",
		"llama-3-sonar-large-32k-online",
	} {
		t.Run(model, func(t *testing.T) {
			ratio := GetDefaultModelRatioMap()[model]
			assert.InDelta(t, 0.5, ratio, 1e-12)
		})
	}
}
