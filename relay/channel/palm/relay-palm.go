package palm

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// https://developers.generativeai.google/api/rest/generativelanguage/models/generateMessage#request-body
// https://developers.generativeai.google/api/rest/generativelanguage/models/generateMessage#response-body

func responsePaLM2OpenAI(response *PaLMChatResponse) *dto.OpenAITextResponse {
	fullTextResponse := dto.OpenAITextResponse{
		Choices: make([]dto.OpenAITextResponseChoice, 0, len(response.Candidates)),
	}
	for i, candidate := range response.Candidates {
		choice := dto.OpenAITextResponseChoice{
			Index: i,
			Message: dto.Message{
				Role:    "assistant",
				Content: candidate.Content,
			},
			FinishReason: "stop",
		}
		fullTextResponse.Choices = append(fullTextResponse.Choices, choice)
	}
	return &fullTextResponse
}

func streamResponsePaLM2OpenAI(palmResponse *PaLMChatResponse) *dto.ChatCompletionsStreamResponse {
	var choice dto.ChatCompletionsStreamResponseChoice
	if len(palmResponse.Candidates) > 0 {
		choice.Delta.SetContentString(palmResponse.Candidates[0].Content)
	}
	choice.FinishReason = &constant.FinishReasonStop
	var response dto.ChatCompletionsStreamResponse
	response.Object = "chat.completion.chunk"
	response.Model = "palm2"
	response.Choices = []dto.ChatCompletionsStreamResponseChoice{choice}
	return &response
}

func palmStreamHandler(c *gin.Context, resp *http.Response) (*types.NewAPIError, string) {
	defer service.CloseResponseBodyGracefully(resp)
	responseBody, err := common.ReadAllWithLimit(resp.Body)
	if err != nil {
		service.MarkUpstreamAccepted(c)
		return relaychannel.AcceptedResponseDeliveryError(), ""
	}
	var palmResponse PaLMChatResponse
	if err := common.Unmarshal(responseBody, &palmResponse); err != nil {
		service.MarkUpstreamAccepted(c)
		return relaychannel.AcceptedResponseDeliveryError(), ""
	}
	if palmResponse.Error.Code != 0 {
		return service.MarkExplicitUpstreamRejection(types.WithOpenAIError(types.OpenAIError{
			Message: palmResponse.Error.Message,
			Type:    palmResponse.Error.Status,
			Code:    palmResponse.Error.Code,
		}, http.StatusBadGateway)), ""
	}
	if len(palmResponse.Candidates) == 0 {
		if len(palmResponse.Filters) > 0 {
			filter := palmResponse.Filters[0]
			return service.MarkExplicitUpstreamRejection(types.NewOpenAIError(
				fmt.Errorf("PaLM rejected the request: %s %s", filter.Reason, filter.Message),
				types.ErrorCodeBadResponse,
				http.StatusBadGateway,
			)), ""
		}
		service.MarkUpstreamAccepted(c)
		return relaychannel.AcceptedResponseDeliveryError(), ""
	}

	responseText := palmResponse.Candidates[0].Content
	responseId := helper.GetResponseID(c)
	createdTime := common.GetTimestamp()
	fullTextResponse := streamResponsePaLM2OpenAI(&palmResponse)
	fullTextResponse.Id = responseId
	fullTextResponse.Created = createdTime
	helper.SetEventStreamHeaders(c)
	service.MarkUpstreamAccepted(c)
	if err := helper.ObjectData(c, fullTextResponse); err != nil {
		return relaychannel.AcceptedResponseDeliveryError(), responseText
	}
	if err := helper.StringData(c, "[DONE]"); err != nil {
		return relaychannel.AcceptedResponseDeliveryError(), responseText
	}
	return nil, responseText
}

func palmHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)
	responseBody, err := common.ReadAllWithLimit(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	var palmResponse PaLMChatResponse
	err = common.Unmarshal(responseBody, &palmResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if palmResponse.Error.Code != 0 {
		return nil, service.MarkExplicitUpstreamRejection(types.WithOpenAIError(types.OpenAIError{
			Message: palmResponse.Error.Message,
			Type:    palmResponse.Error.Status,
			Param:   "",
			Code:    palmResponse.Error.Code,
		}, http.StatusBadGateway))
	}
	if len(palmResponse.Candidates) == 0 {
		return nil, types.NewOpenAIError(errors.New("empty response from PaLM API"), types.ErrorCodeEmptyResponse, http.StatusBadGateway)
	}
	fullTextResponse := responsePaLM2OpenAI(&palmResponse)
	usage := service.ResponseText2Usage(c, palmResponse.Candidates[0].Content, info.UpstreamModelName, info.GetEstimatePromptTokens())
	fullTextResponse.Usage = *usage
	jsonResponse, err := common.Marshal(fullTextResponse)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(resp.StatusCode)
	service.IOCopyBytesGracefully(c, resp, jsonResponse)
	return usage, nil
}
