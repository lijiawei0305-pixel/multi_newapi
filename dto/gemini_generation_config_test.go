package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeminiChatGenerationConfigPreservesExplicitZeroValuesCamelCase(t *testing.T) {
	raw := []byte(`{
		"contents":[{"role":"user","parts":[{"text":"hello"}]}],
		"generationConfig":{
			"topP":0,
			"topK":0,
			"maxOutputTokens":0,
			"candidateCount":0,
			"seed":0,
			"responseLogprobs":false
		}
	}`)

	var req GeminiChatRequest
	require.NoError(t, common.Unmarshal(raw, &req))

	encoded, err := common.Marshal(req)
	require.NoError(t, err)

	var out map[string]any
	require.NoError(t, common.Unmarshal(encoded, &out))

	generationConfig, ok := out["generationConfig"].(map[string]any)
	require.True(t, ok)

	assert.Contains(t, generationConfig, "topP")
	assert.Contains(t, generationConfig, "topK")
	assert.Contains(t, generationConfig, "maxOutputTokens")
	assert.Contains(t, generationConfig, "candidateCount")
	assert.Contains(t, generationConfig, "seed")
	assert.Contains(t, generationConfig, "responseLogprobs")

	assert.Equal(t, float64(0), generationConfig["topP"])
	assert.Equal(t, float64(0), generationConfig["topK"])
	assert.Equal(t, float64(0), generationConfig["maxOutputTokens"])
	assert.Equal(t, float64(0), generationConfig["candidateCount"])
	assert.Equal(t, float64(0), generationConfig["seed"])
	assert.Equal(t, false, generationConfig["responseLogprobs"])
}

func TestGeminiChatGenerationConfigPreservesExplicitZeroValuesSnakeCase(t *testing.T) {
	raw := []byte(`{
		"contents":[{"role":"user","parts":[{"text":"hello"}]}],
		"generationConfig":{
			"top_p":0,
			"top_k":0,
			"max_output_tokens":0,
			"candidate_count":0,
			"seed":0,
			"response_logprobs":false
		}
	}`)

	var req GeminiChatRequest
	require.NoError(t, common.Unmarshal(raw, &req))

	encoded, err := common.Marshal(req)
	require.NoError(t, err)

	var out map[string]any
	require.NoError(t, common.Unmarshal(encoded, &out))

	generationConfig, ok := out["generationConfig"].(map[string]any)
	require.True(t, ok)

	assert.Contains(t, generationConfig, "topP")
	assert.Contains(t, generationConfig, "topK")
	assert.Contains(t, generationConfig, "maxOutputTokens")
	assert.Contains(t, generationConfig, "candidateCount")
	assert.Contains(t, generationConfig, "seed")
	assert.Contains(t, generationConfig, "responseLogprobs")

	assert.Equal(t, float64(0), generationConfig["topP"])
	assert.Equal(t, float64(0), generationConfig["topK"])
	assert.Equal(t, float64(0), generationConfig["maxOutputTokens"])
	assert.Equal(t, float64(0), generationConfig["candidateCount"])
	assert.Equal(t, float64(0), generationConfig["seed"])
	assert.Equal(t, false, generationConfig["responseLogprobs"])
}

func TestGeminiThinkingConfigPreservesExplicitFalse(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "camel case", body: `{"includeThoughts":false}`},
		{name: "snake case", body: `{"include_thoughts":false}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var config GeminiThinkingConfig
			require.NoError(t, common.Unmarshal([]byte(test.body), &config))
			require.NotNil(t, config.IncludeThoughts)
			assert.False(t, *config.IncludeThoughts)

			encoded, err := common.Marshal(config)
			require.NoError(t, err)
			var out map[string]any
			require.NoError(t, common.Unmarshal(encoded, &out))
			assert.Contains(t, out, "includeThoughts")
			assert.Equal(t, false, out["includeThoughts"])
		})
	}
}

func TestGeminiEmbeddingRequestPreservesExplicitZeroDimensions(t *testing.T) {
	var request GeminiEmbeddingRequest
	require.NoError(t, common.Unmarshal([]byte(`{
		"model":"models/gemini-embedding-001",
		"content":{"parts":[{"text":"hello"}]},
		"outputDimensionality":0
	}`), &request))
	require.NotNil(t, request.OutputDimensionality)
	assert.Zero(t, *request.OutputDimensionality)

	encoded, err := common.Marshal(request)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, common.Unmarshal(encoded, &out))
	assert.Contains(t, out, "outputDimensionality")
	assert.Equal(t, float64(0), out["outputDimensionality"])
}

func TestGeminiPartPreservesExplicitFalseThought(t *testing.T) {
	var part GeminiPart
	require.NoError(t, common.Unmarshal([]byte(`{"text":"answer","thought":false}`), &part))
	require.NotNil(t, part.Thought)
	assert.False(t, *part.Thought)

	encoded, err := common.Marshal(part)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, common.Unmarshal(encoded, &out))
	assert.Contains(t, out, "thought")
	assert.Equal(t, false, out["thought"])
}
