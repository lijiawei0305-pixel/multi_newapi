package ollama

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOllamaConversionsPreserveExplicitZeroAndFalse(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	request := &dto.GeneralOpenAIRequest{
		Stream:              common.GetPointer(false),
		MaxCompletionTokens: common.GetPointer(uint(0)),
	}

	chat, err := openAIChatToOllamaChat(c, request)
	require.NoError(t, err)
	chatJSON, err := common.Marshal(chat)
	require.NoError(t, err)
	assert.True(t, gjson.GetBytes(chatJSON, "stream").Exists())
	assert.True(t, gjson.GetBytes(chatJSON, "options.num_predict").Exists())

	generate, err := openAIToGenerate(c, request)
	require.NoError(t, err)
	generateJSON, err := common.Marshal(generate)
	require.NoError(t, err)
	assert.True(t, gjson.GetBytes(generateJSON, "stream").Exists())
	assert.True(t, gjson.GetBytes(generateJSON, "options.num_predict").Exists())

	embeddingJSON, err := common.Marshal(requestOpenAI2Embeddings(dto.EmbeddingRequest{
		Input:      "hello",
		Dimensions: common.GetPointer(0),
	}))
	require.NoError(t, err)
	assert.True(t, gjson.GetBytes(embeddingJSON, "dimensions").Exists())
	assert.True(t, gjson.GetBytes(embeddingJSON, "options.dimensions").Exists())
}
