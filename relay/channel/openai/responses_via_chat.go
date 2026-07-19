package openai

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/relayconvert"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func OaiChatToResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	defer service.CloseResponseBodyGracefully(resp)

	body, err := common.ReadAllWithLimit(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	var chatResp dto.OpenAITextResponse
	if err := common.Unmarshal(body, &chatResp); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if apiErr := explicitOpenAIRejection(chatResp.GetOpenAIError(), resp.StatusCode); apiErr != nil {
		return nil, apiErr
	}
	service.MarkUpstreamAccepted(c)

	responseID := helper.GetResponseID(c)
	responsesResp, usage, err := service.ChatCompletionsResponseToResponsesResponse(&chatResp, responseID)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if usage == nil || usage.TotalTokens == 0 {
		text := service.ExtractOutputTextFromResponses(responsesResp)
		usage = service.ResponseText2Usage(c, text, info.UpstreamModelName, info.GetEstimatePromptTokens())
		responsesResp.Usage = relayconvert.UsageFromChatUsage(usage)
	}

	responseBody, err := common.Marshal(responsesResp)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
	}

	service.IOCopyBytesGracefully(c, resp, responseBody)
	return usage, nil
}

func OaiChatToResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	defer service.CloseResponseBodyGracefully(resp)

	responseID := helper.GetResponseID(c)
	state := relayconvert.NewChatToResponsesStreamState(responseID, info.UpstreamModelName)
	streamErr := (*types.NewAPIError)(nil)
	seenValidResponse := false
	completed := false

	sendEvent := func(event relayconvert.ChatToResponsesStreamEvent) bool {
		data, err := common.Marshal(event.Payload)
		if err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
			return false
		}
		if err := helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: event.Type}, string(data)); err != nil {
			streamErr = relaychannel.AcceptedResponseDeliveryError()
			return false
		}
		return true
	}

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		if streamErr != nil {
			sr.Stop(streamErr)
			return
		}

		var errorResp dto.OpenAITextResponse
		if err := common.UnmarshalJsonStr(data, &errorResp); err == nil {
			if apiErr := explicitOpenAIRejection(errorResp.GetOpenAIError(), resp.StatusCode); apiErr != nil {
				if seenValidResponse {
					service.MarkUpstreamAccepted(c)
					streamErr = relaychannel.AcceptedResponseDeliveryError()
				} else {
					streamErr = apiErr
				}
				sr.Stop(streamErr)
				return
			}
		}

		var chunk dto.ChatCompletionsStreamResponse
		if err := common.UnmarshalJsonStr(data, &chunk); err != nil {
			logger.LogError(c, "failed to unmarshal chat stream response: "+err.Error())
			service.MarkUpstreamAccepted(c)
			streamErr = relaychannel.AcceptedResponseDeliveryError()
			sr.Stop(streamErr)
			return
		}
		if chunk.Id == "" && chunk.Object == "" && chunk.Model == "" && chunk.Choices == nil && chunk.Usage == nil {
			service.MarkUpstreamAccepted(c)
			streamErr = relaychannel.AcceptedResponseDeliveryError()
			sr.Stop(streamErr)
			return
		}
		service.MarkUpstreamAccepted(c)
		seenValidResponse = true
		completed = completed || chunk.IsFinished()

		events, err := relayconvert.ChatCompletionsStreamChunkToResponsesEvents(&chunk, state)
		if err != nil {
			streamErr = relaychannel.AcceptedResponseDeliveryError()
			sr.Stop(streamErr)
			return
		}
		for _, event := range events {
			if !sendEvent(event) {
				sr.Stop(streamErr)
				return
			}
		}
	})

	if streamErr != nil {
		return nil, streamErr
	}
	finishedNormally := completed || info.StreamStatus != nil && info.StreamStatus.EndReason == relaycommon.StreamEndReasonDone
	if info.StreamStatus == nil || !info.StreamStatus.IsNormalEnd() || info.StreamStatus.HasErrors() || !seenValidResponse || !finishedNormally {
		service.MarkUpstreamAccepted(c)
		return nil, relaychannel.AcceptedResponseDeliveryError()
	}

	usage := state.Usage
	if usage == nil || usage.TotalTokens == 0 {
		usage = service.ResponseText2Usage(c, state.UsageText(), info.UpstreamModelName, info.GetEstimatePromptTokens())
		state.Usage = relayconvert.UsageFromChatUsage(usage)
	}

	for _, event := range relayconvert.FinalizeChatCompletionsStreamToResponses(state) {
		if !sendEvent(event) {
			return nil, streamErr
		}
	}

	return usage, nil
}
