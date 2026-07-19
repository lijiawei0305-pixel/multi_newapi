package xai

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func streamResponseXAI2OpenAI(xAIResp *dto.ChatCompletionsStreamResponse, usage *dto.Usage) *dto.ChatCompletionsStreamResponse {
	if xAIResp == nil {
		return nil
	}
	if xAIResp.Usage != nil {
		xAIResp.Usage.CompletionTokens = usage.CompletionTokens
	}
	openAIResp := &dto.ChatCompletionsStreamResponse{
		Id:      xAIResp.Id,
		Object:  xAIResp.Object,
		Created: xAIResp.Created,
		Model:   xAIResp.Model,
		Choices: xAIResp.Choices,
		Usage:   xAIResp.Usage,
	}

	return openAIResp
}

func xAIStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	usage := &dto.Usage{}
	var responseTextBuilder strings.Builder
	var toolCount int
	var containStreamUsage bool
	var streamErr *types.NewAPIError
	seenValidResponse := false
	completed := false

	helper.SetEventStreamHeaders(c)

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		var errorResponse dto.OpenAITextResponse
		if err := common.UnmarshalJsonStr(data, &errorResponse); err == nil {
			if openAIError := errorResponse.GetOpenAIError(); openAIError != nil &&
				(strings.TrimSpace(openAIError.Message) != "" || strings.TrimSpace(openAIError.Type) != "" || strings.TrimSpace(openAIError.Param) != "" || openAIError.Code != nil) {
				apiErr := types.WithOpenAIError(*openAIError, http.StatusBadGateway)
				if seenValidResponse {
					service.MarkUpstreamAccepted(c)
					streamErr = relaychannel.AcceptedResponseDeliveryError()
				} else {
					streamErr = service.MarkExplicitUpstreamRejection(apiErr)
				}
				sr.Stop(streamErr)
				return
			}
		}
		var xAIResp *dto.ChatCompletionsStreamResponse
		if err := common.UnmarshalJsonStr(data, &xAIResp); err != nil {
			common.SysLog("error unmarshalling stream response: " + err.Error())
			service.MarkUpstreamAccepted(c)
			streamErr = relaychannel.AcceptedResponseDeliveryError()
			sr.Stop(streamErr)
			return
		}
		if xAIResp == nil {
			service.MarkUpstreamAccepted(c)
			streamErr = relaychannel.AcceptedResponseDeliveryError()
			sr.Stop(streamErr)
			return
		}
		service.MarkUpstreamAccepted(c)
		seenValidResponse = true
		completed = completed || xAIResp.IsFinished()
		// 把 xAI 的usage转换为 OpenAI 的usage
		if xAIResp.Usage != nil {
			containStreamUsage = true
			usage.PromptTokens = xAIResp.Usage.PromptTokens
			usage.TotalTokens = xAIResp.Usage.TotalTokens
			usage.CompletionTokens = usage.TotalTokens - usage.PromptTokens
		}

		openaiResponse := streamResponseXAI2OpenAI(xAIResp, usage)
		if openaiResponse == nil {
			streamErr = relaychannel.AcceptedResponseDeliveryError()
			sr.Stop(streamErr)
			return
		}
		if err := openai.ProcessStreamResponse(*openaiResponse, &responseTextBuilder, &toolCount); err != nil {
			streamErr = relaychannel.AcceptedResponseDeliveryError()
			sr.Stop(streamErr)
			return
		}
		if err := helper.ObjectData(c, openaiResponse); err != nil {
			common.SysLog(err.Error())
			streamErr = relaychannel.AcceptedResponseDeliveryError()
			sr.Stop(streamErr)
		}
	})

	if !containStreamUsage {
		usage = service.ResponseText2Usage(c, responseTextBuilder.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
		usage.CompletionTokens += toolCount * 7
	}
	if streamErr != nil {
		return usage, streamErr
	}
	finishedNormally := completed || info.StreamStatus != nil && info.StreamStatus.EndReason == relaycommon.StreamEndReasonDone
	if info.StreamStatus == nil || !info.StreamStatus.IsNormalEnd() || info.StreamStatus.HasErrors() || !seenValidResponse || !finishedNormally {
		service.MarkUpstreamAccepted(c)
		return usage, relaychannel.AcceptedResponseDeliveryError()
	}
	if err := helper.StringData(c, "[DONE]"); err != nil {
		return usage, relaychannel.AcceptedResponseDeliveryError()
	}
	return usage, nil
}

func xAIHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, err := common.ReadAllWithLimit(resp.Body)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	var xaiResponse ChatCompletionResponse
	err = common.Unmarshal(responseBody, &xaiResponse)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	if xaiResponse.Usage != nil {
		xaiResponse.Usage.CompletionTokens = xaiResponse.Usage.TotalTokens - xaiResponse.Usage.PromptTokens
		xaiResponse.Usage.CompletionTokenDetails.TextTokens = xaiResponse.Usage.CompletionTokens - xaiResponse.Usage.CompletionTokenDetails.ReasoningTokens
	}

	// new body
	encodeJson, err := common.Marshal(xaiResponse)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}

	service.IOCopyBytesGracefully(c, resp, encodeJson)

	return xaiResponse.Usage, nil
}
