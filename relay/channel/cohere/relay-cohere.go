package cohere

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

func requestOpenAI2Cohere(textRequest dto.GeneralOpenAIRequest) *CohereRequest {
	cohereReq := CohereRequest{
		Model:       textRequest.Model,
		ChatHistory: []ChatHistory{},
		Message:     "",
		Stream:      lo.FromPtrOr(textRequest.Stream, false),
	}
	if common.CohereSafetySetting != "NONE" {
		cohereReq.SafetyMode = common.CohereSafetySetting
	}
	if textRequest.MaxCompletionTokens != nil || textRequest.MaxTokens != nil {
		cohereReq.MaxTokens = textRequest.GetMaxTokens()
	} else {
		cohereReq.MaxTokens = 4000
	}
	for _, msg := range textRequest.Messages {
		if msg.Role == "user" {
			cohereReq.Message = msg.StringContent()
		} else {
			var role string
			if msg.Role == "assistant" {
				role = "CHATBOT"
			} else if msg.Role == "system" {
				role = "SYSTEM"
			} else {
				role = "USER"
			}
			cohereReq.ChatHistory = append(cohereReq.ChatHistory, ChatHistory{
				Role:    role,
				Message: msg.StringContent(),
			})
		}
	}

	return &cohereReq
}

func requestConvertRerank2Cohere(rerankRequest dto.RerankRequest) *CohereRerankRequest {
	topN := 1
	if rerankRequest.TopN != nil {
		topN = *rerankRequest.TopN
	}
	cohereReq := CohereRerankRequest{
		Query:           rerankRequest.Query,
		Documents:       rerankRequest.Documents,
		Model:           rerankRequest.Model,
		TopN:            topN,
		ReturnDocuments: lo.FromPtrOr(rerankRequest.ReturnDocuments, true),
	}
	return &cohereReq
}

func stopReasonCohere2OpenAI(reason string) string {
	switch reason {
	case "COMPLETE":
		return "stop"
	case "MAX_TOKENS":
		return "max_tokens"
	default:
		return reason
	}
}

func cohereStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)
	responseId := helper.GetResponseID(c)
	createdTime := common.GetTimestamp()
	usage := &dto.Usage{}
	responseText := ""
	scanner := helper.NewStreamScanner(resp.Body)
	scanner.Split(func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		if atEOF && len(data) == 0 {
			return 0, nil, nil
		}
		if i := strings.Index(string(data), "\n"); i >= 0 {
			return i + 1, data[0:i], nil
		}
		if atEOF {
			return len(data), data, nil
		}
		return 0, nil, nil
	})
	helper.SetEventStreamHeaders(c)
	seenValidResponse := false
	completed := false
	for scanner.Scan() {
		data := strings.TrimSuffix(scanner.Text(), "\r")
		if strings.TrimSpace(data) == "" {
			continue
		}
		var cohereResp CohereResponse
		if err := common.Unmarshal([]byte(data), &cohereResp); err != nil {
			common.SysLog("error unmarshalling stream response: " + err.Error())
			service.MarkUpstreamAccepted(c)
			return usage, relaychannel.AcceptedResponseDeliveryError()
		}
		eventType := strings.ToLower(strings.TrimSpace(cohereResp.EventType))
		finishReasonValue := strings.ToUpper(strings.TrimSpace(cohereResp.FinishReason))
		if strings.Contains(eventType, "error") || strings.HasPrefix(finishReasonValue, "ERROR") {
			if seenValidResponse {
				service.MarkUpstreamAccepted(c)
				return usage, relaychannel.AcceptedResponseDeliveryError()
			}
			return usage, service.MarkExplicitUpstreamRejection(types.NewOpenAIError(
				fmt.Errorf("Cohere rejected the streaming request: event=%s finish_reason=%s", cohereResp.EventType, cohereResp.FinishReason),
				types.ErrorCodeBadResponse,
				http.StatusBadGateway,
			))
		}
		service.MarkUpstreamAccepted(c)
		seenValidResponse = true
		info.SetFirstResponseTime()
		var openaiResp dto.ChatCompletionsStreamResponse
		openaiResp.Id = responseId
		openaiResp.Created = createdTime
		openaiResp.Object = "chat.completion.chunk"
		openaiResp.Model = info.UpstreamModelName
		if cohereResp.IsFinished || eventType == "stream-end" {
			completed = true
			finishReason := stopReasonCohere2OpenAI(cohereResp.FinishReason)
			openaiResp.Choices = []dto.ChatCompletionsStreamResponseChoice{{
				Delta:        dto.ChatCompletionsStreamResponseChoiceDelta{},
				Index:        0,
				FinishReason: &finishReason,
			}}
			if cohereResp.Response != nil {
				usage.PromptTokens = cohereResp.Response.Meta.BilledUnits.InputTokens
				usage.CompletionTokens = cohereResp.Response.Meta.BilledUnits.OutputTokens
				usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
			}
		} else {
			openaiResp.Choices = []dto.ChatCompletionsStreamResponseChoice{{
				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
					Role:    "assistant",
					Content: &cohereResp.Text,
				},
				Index: 0,
			}}
			responseText += cohereResp.Text
		}
		if err := helper.ObjectData(c, openaiResp); err != nil {
			return usage, relaychannel.AcceptedResponseDeliveryError()
		}
	}
	if err := scanner.Err(); err != nil {
		common.SysLog("error reading stream: " + err.Error())
		service.MarkUpstreamAccepted(c)
		return usage, relaychannel.AcceptedResponseDeliveryError()
	}
	if usage.PromptTokens == 0 {
		usage = service.ResponseText2Usage(c, responseText, info.UpstreamModelName, info.GetEstimatePromptTokens())
	}
	if !seenValidResponse || !completed {
		service.MarkUpstreamAccepted(c)
		return usage, relaychannel.AcceptedResponseDeliveryError()
	}
	if err := helper.StringData(c, "[DONE]"); err != nil {
		return usage, relaychannel.AcceptedResponseDeliveryError()
	}
	return usage, nil
}

func cohereHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	createdTime := common.GetTimestamp()
	defer service.CloseResponseBodyGracefully(resp)
	responseBody, err := common.ReadAllWithLimit(resp.Body)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	var cohereResp CohereResponseResult
	err = common.Unmarshal(responseBody, &cohereResp)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	usage := dto.Usage{}
	usage.PromptTokens = cohereResp.Meta.BilledUnits.InputTokens
	usage.CompletionTokens = cohereResp.Meta.BilledUnits.OutputTokens
	usage.TotalTokens = cohereResp.Meta.BilledUnits.InputTokens + cohereResp.Meta.BilledUnits.OutputTokens

	var openaiResp dto.TextResponse
	openaiResp.Id = cohereResp.ResponseId
	openaiResp.Created = createdTime
	openaiResp.Object = "chat.completion"
	openaiResp.Model = info.UpstreamModelName
	openaiResp.Usage = usage

	openaiResp.Choices = []dto.OpenAITextResponseChoice{
		{
			Index:        0,
			Message:      dto.Message{Content: cohereResp.Text, Role: "assistant"},
			FinishReason: stopReasonCohere2OpenAI(cohereResp.FinishReason),
		},
	}

	jsonResponse, err := common.Marshal(openaiResp)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(resp.StatusCode)
	_, _ = c.Writer.Write(jsonResponse)
	return &usage, nil
}

func cohereRerankHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)
	responseBody, err := common.ReadAllWithLimit(resp.Body)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	var cohereResp CohereRerankResponseResult
	err = common.Unmarshal(responseBody, &cohereResp)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	usage := dto.Usage{}
	if cohereResp.Meta.BilledUnits.InputTokens == 0 {
		usage.PromptTokens = info.GetEstimatePromptTokens()
		usage.CompletionTokens = 0
		usage.TotalTokens = info.GetEstimatePromptTokens()
	} else {
		usage.PromptTokens = cohereResp.Meta.BilledUnits.InputTokens
		usage.CompletionTokens = cohereResp.Meta.BilledUnits.OutputTokens
		usage.TotalTokens = cohereResp.Meta.BilledUnits.InputTokens + cohereResp.Meta.BilledUnits.OutputTokens
	}

	var rerankResp dto.RerankResponse
	rerankResp.Results = cohereResp.Results
	rerankResp.Usage = usage

	jsonResponse, err := common.Marshal(rerankResp)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(resp.StatusCode)
	if _, err = c.Writer.Write(jsonResponse); err != nil {
		return &usage, relaychannel.AcceptedResponseDeliveryError()
	}
	return &usage, nil
}
