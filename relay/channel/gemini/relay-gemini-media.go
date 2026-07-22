package gemini

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func GeminiEmbeddingHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, readErr := common.ReadAllWithLimit(resp.Body)
	if readErr != nil {
		return nil, types.NewOpenAIError(readErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if apiErr := explicitGeminiResponseError(responseBody, resp.StatusCode); apiErr != nil {
		return nil, apiErr
	}

	var geminiResponse dto.GeminiBatchEmbeddingResponse
	if jsonErr := common.Unmarshal(responseBody, &geminiResponse); jsonErr != nil {
		return nil, types.NewOpenAIError(jsonErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	service.MarkUpstreamAccepted(c)

	// convert to openai format response
	openAIResponse := dto.OpenAIEmbeddingResponse{
		Object: "list",
		Data:   make([]dto.OpenAIEmbeddingResponseItem, 0, len(geminiResponse.Embeddings)),
		Model:  info.UpstreamModelName,
	}

	for i, embedding := range geminiResponse.Embeddings {
		openAIResponse.Data = append(openAIResponse.Data, dto.OpenAIEmbeddingResponseItem{
			Object:    "embedding",
			Embedding: embedding.Values,
			Index:     i,
		})
	}

	// calculate usage
	// https://ai.google.dev/gemini-api/docs/pricing?hl=zh-cn#text-embedding-004
	// Google has not yet clarified how embedding models will be billed
	// refer to openai billing method to use input tokens billing
	// https://platform.openai.com/docs/guides/embeddings#what-are-embeddings
	usage := service.ResponseText2Usage(c, "", info.UpstreamModelName, info.GetEstimatePromptTokens())
	openAIResponse.Usage = *usage

	jsonResponse, jsonErr := common.Marshal(openAIResponse)
	if jsonErr != nil {
		return nil, types.NewOpenAIError(jsonErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	service.IOCopyBytesGracefully(c, resp, jsonResponse)
	return usage, nil
}

func GeminiImageHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	var geminiResponse dto.GeminiImageResponse
	defer service.CloseResponseBodyGracefully(resp)
	if jsonErr := common.DecodeJsonWithLimit(resp.Body, &geminiResponse, common.UpstreamJSONBodyLimit()); jsonErr != nil {
		service.MarkUpstreamAccepted(c)
		common.SysError(fmt.Sprintf("accepted Gemini image response decode failed: error_type=%T", jsonErr))
		return nil, relaychannel.AcceptedResponseDeliveryError()
	}

	if len(geminiResponse.Predictions) == 0 {
		service.MarkUpstreamAccepted(c)
		return nil, relaychannel.AcceptedResponseDeliveryError()
	}

	// convert to openai format response
	openAIResponse := dto.ImageResponse{
		Created: common.GetTimestamp(),
		Data:    make([]dto.ImageData, 0, len(geminiResponse.Predictions)),
	}
	budget := service.NewImageResponseEncodedBudget()
	filtered := false

	for _, prediction := range geminiResponse.Predictions {
		if prediction.RaiFilteredReason != "" {
			filtered = true
			continue // skip filtered image
		}
		if prediction.BytesBase64Encoded == "" {
			continue
		}
		service.MarkUpstreamAccepted(c)
		if err := budget.ConsumeBase64(prediction.BytesBase64Encoded); err != nil {
			common.SysError(fmt.Sprintf("accepted Gemini image response exceeded cumulative encoded budget: error_type=%T", err))
			return nil, relaychannel.AcceptedResponseDeliveryError()
		}
		openAIResponse.Data = append(openAIResponse.Data, dto.ImageData{
			B64Json: prediction.BytesBase64Encoded,
		})
	}
	if len(openAIResponse.Data) == 0 {
		if filtered {
			return nil, types.NewErrorWithStatusCode(
				errors.New("image request was rejected by the provider safety filter"),
				types.ErrorCodePromptBlocked,
				http.StatusBadRequest,
				types.ErrOptionWithSkipRetry(),
			)
		}
		service.MarkUpstreamAccepted(c)
		return nil, relaychannel.AcceptedResponseDeliveryError()
	}

	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(resp.StatusCode)
	if writeErr := relaychannel.WriteImageResponse(c.Writer, &openAIResponse); writeErr != nil {
		if buffered, ok := c.Writer.(*common.BufferedResponseWriter); ok {
			_ = buffered.Fail(writeErr)
		}
		return nil, relaychannel.AcceptedResponseDeliveryError()
	}

	// https://github.com/google-gemini/cookbook/blob/719a27d752aac33f39de18a8d3cb42a70874917e/quickstarts/Counting_Tokens.ipynb
	// each image has fixed 258 tokens
	const imageTokens = 258
	generatedImages := len(openAIResponse.Data)

	usage := &dto.Usage{
		PromptTokens:     imageTokens * generatedImages, // each generated image has fixed 258 tokens
		CompletionTokens: 0,                             // image generation does not calculate completion tokens
		TotalTokens:      imageTokens * generatedImages,
	}

	return usage, nil
}
