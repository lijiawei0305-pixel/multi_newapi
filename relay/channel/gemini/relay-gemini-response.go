package gemini

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func getResponseToolCall(item *dto.GeminiPart) *dto.ToolCallResponse {
	var argsBytes []byte
	var err error
	// 移除 unescapeMapOrSlice 调用，直接序列化
	// JSON 序列化/反序列化已经正确处理了转义字符
	argsBytes, err = common.Marshal(item.FunctionCall.Arguments)

	if err != nil {
		return nil
	}
	return &dto.ToolCallResponse{
		ID:   fmt.Sprintf("call_%s", common.GetUUID()),
		Type: "function",
		Function: dto.FunctionResponse{
			Arguments: string(argsBytes),
			Name:      item.FunctionCall.FunctionName,
		},
	}
}

func buildUsageFromGeminiMetadata(metadata dto.GeminiUsageMetadata, fallbackPromptTokens int) dto.Usage {
	promptTokens := metadata.PromptTokenCount + metadata.ToolUsePromptTokenCount
	if promptTokens <= 0 && fallbackPromptTokens > 0 {
		promptTokens = fallbackPromptTokens
	}

	usage := dto.Usage{
		PromptTokens:     promptTokens,
		CompletionTokens: metadata.CandidatesTokenCount + metadata.ThoughtsTokenCount,
		TotalTokens:      metadata.TotalTokenCount,
	}
	usage.CompletionTokenDetails.ReasoningTokens = metadata.ThoughtsTokenCount
	usage.PromptTokensDetails.CachedTokens = metadata.CachedContentTokenCount

	for _, detail := range metadata.PromptTokensDetails {
		if detail.Modality == "AUDIO" {
			usage.PromptTokensDetails.AudioTokens += detail.TokenCount
		} else if detail.Modality == "TEXT" {
			usage.PromptTokensDetails.TextTokens += detail.TokenCount
		}
	}
	for _, detail := range metadata.ToolUsePromptTokensDetails {
		if detail.Modality == "AUDIO" {
			usage.PromptTokensDetails.AudioTokens += detail.TokenCount
		} else if detail.Modality == "TEXT" {
			usage.PromptTokensDetails.TextTokens += detail.TokenCount
		}
	}
	for _, detail := range metadata.CandidatesTokensDetails {
		switch detail.Modality {
		case "IMAGE":
			usage.CompletionTokenDetails.ImageTokens += detail.TokenCount
		case "AUDIO":
			usage.CompletionTokenDetails.AudioTokens += detail.TokenCount
		case "TEXT":
			usage.CompletionTokenDetails.TextTokens += detail.TokenCount
		}
	}

	if usage.TotalTokens > 0 && usage.CompletionTokens <= 0 {
		usage.CompletionTokens = usage.TotalTokens - usage.PromptTokens
	}

	if usage.PromptTokens > 0 && usage.PromptTokensDetails.TextTokens == 0 && usage.PromptTokensDetails.AudioTokens == 0 {
		usage.PromptTokensDetails.TextTokens = usage.PromptTokens
	}

	return usage
}

func responseGeminiChat2OpenAI(c *gin.Context, response *dto.GeminiChatResponse) *dto.OpenAITextResponse {
	fullTextResponse := dto.OpenAITextResponse{
		Id:      helper.GetResponseID(c),
		Object:  "chat.completion",
		Created: common.GetTimestamp(),
		Choices: make([]dto.OpenAITextResponseChoice, 0, len(response.Candidates)),
	}
	isToolCall := false
	for _, candidate := range response.Candidates {
		choice := dto.OpenAITextResponseChoice{
			Index: int(candidate.Index),
			Message: dto.Message{
				Role:    "assistant",
				Content: "",
			},
			FinishReason: constant.FinishReasonStop,
		}
		if len(candidate.Content.Parts) > 0 {
			// 使用 strings.Builder 直接累积最终 content，避免:
			//   1) 每张 inline image 生成一次中间 "![image](...)" 字符串
			//   2) 末尾 strings.Join 再分配一份等大缓冲
			// Gemini 图片返回时 InlineData.Data 可能是数 MB 的 base64，
			// 上述两份临时分配在高并发下会显著放大堆驻留。
			var content strings.Builder
			var inlineGrow int
			for _, part := range candidate.Content.Parts {
				if part.InlineData != nil {
					inlineGrow += len(part.InlineData.MimeType) + len(part.InlineData.Data) + 32
				}
			}
			if inlineGrow > 0 {
				content.Grow(inlineGrow)
			}
			appended := 0
			writeSep := func() {
				if appended > 0 {
					content.WriteByte('\n')
				}
				appended++
			}
			var toolCalls []dto.ToolCallResponse
			for _, part := range candidate.Content.Parts {
				if part.InlineData != nil {
					// 媒体内容
					if strings.HasPrefix(part.InlineData.MimeType, "image") {
						writeSep()
						content.WriteString("![image](data:")
						content.WriteString(part.InlineData.MimeType)
						content.WriteString(";base64,")
						content.WriteString(part.InlineData.Data)
						content.WriteByte(')')
					} else {
						// 其他媒体类型，直接显示链接
						writeSep()
						content.WriteString("[media](data:")
						content.WriteString(part.InlineData.MimeType)
						content.WriteString(";base64,")
						content.WriteString(part.InlineData.Data)
						content.WriteByte(')')
					}
				} else if part.FunctionCall != nil {
					choice.FinishReason = constant.FinishReasonToolCalls
					if call := getResponseToolCall(&part); call != nil {
						toolCalls = append(toolCalls, *call)
					}
				} else if part.Thought != nil && *part.Thought {
					choice.Message.ReasoningContent = &part.Text
				} else {
					if part.ExecutableCode != nil {
						writeSep()
						content.WriteString("```")
						content.WriteString(part.ExecutableCode.Language)
						content.WriteByte('\n')
						content.WriteString(part.ExecutableCode.Code)
						content.WriteString("\n```")
					} else if part.CodeExecutionResult != nil {
						writeSep()
						content.WriteString("```output\n")
						content.WriteString(part.CodeExecutionResult.Output)
						content.WriteString("\n```")
					} else {
						// 过滤掉空行
						if part.Text != "\n" {
							writeSep()
							content.WriteString(part.Text)
						}
					}
				}
			}
			if len(toolCalls) > 0 {
				choice.Message.SetToolCalls(toolCalls)
				isToolCall = true
			}
			choice.Message.SetStringContent(content.String())

		}
		if candidate.FinishReason != nil {
			switch *candidate.FinishReason {
			case "STOP":
				choice.FinishReason = constant.FinishReasonStop
			case "MAX_TOKENS":
				choice.FinishReason = constant.FinishReasonLength
			case "SAFETY":
				// Safety filter triggered
				choice.FinishReason = constant.FinishReasonContentFilter
			case "RECITATION":
				// Recitation (citation) detected
				choice.FinishReason = constant.FinishReasonContentFilter
			case "BLOCKLIST":
				// Blocklist triggered
				choice.FinishReason = constant.FinishReasonContentFilter
			case "PROHIBITED_CONTENT":
				// Prohibited content detected
				choice.FinishReason = constant.FinishReasonContentFilter
			case "SPII":
				// Sensitive personally identifiable information
				choice.FinishReason = constant.FinishReasonContentFilter
			case "OTHER":
				// Other reasons
				choice.FinishReason = constant.FinishReasonContentFilter
			default:
				choice.FinishReason = constant.FinishReasonContentFilter
			}
		}
		if isToolCall {
			choice.FinishReason = constant.FinishReasonToolCalls
		}

		fullTextResponse.Choices = append(fullTextResponse.Choices, choice)
	}
	return &fullTextResponse
}

func GeminiChatHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)
	responseBody, err := common.ReadAllWithLimit(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	logger.LogPayload(c, "Gemini response body", responseBody)
	if apiErr := explicitGeminiResponseError(responseBody, resp.StatusCode); apiErr != nil {
		return nil, apiErr
	}
	var geminiResponse dto.GeminiChatResponse
	err = common.Unmarshal(responseBody, &geminiResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if len(geminiResponse.Candidates) == 0 {
		usage := buildUsageFromGeminiMetadata(geminiResponse.UsageMetadata, info.GetEstimatePromptTokens())
		if geminiResponse.PromptFeedback != nil && geminiResponse.PromptFeedback.BlockReason != nil {
			common.SetContextKey(c, constant.ContextKeyAdminRejectReason, fmt.Sprintf("gemini_block_reason=%s", *geminiResponse.PromptFeedback.BlockReason))
			return &usage, explicitGeminiPromptBlock(*geminiResponse.PromptFeedback.BlockReason)
		}
		common.SetContextKey(c, constant.ContextKeyAdminRejectReason, "gemini_empty_candidates")
		return &usage, unknownGeminiEmptyResponse()
	}
	service.MarkUpstreamAccepted(c)
	fullTextResponse := responseGeminiChat2OpenAI(c, &geminiResponse)
	fullTextResponse.Model = info.UpstreamModelName
	usage := buildUsageFromGeminiMetadata(geminiResponse.UsageMetadata, info.GetEstimatePromptTokens())

	fullTextResponse.Usage = usage

	switch info.RelayFormat {
	case types.RelayFormatOpenAI:
		responseBody, err = common.Marshal(fullTextResponse)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
		}
	case types.RelayFormatClaude:
		claudeResp := service.ResponseOpenAI2Claude(fullTextResponse, info)
		claudeRespStr, err := common.Marshal(claudeResp)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
		}
		responseBody = claudeRespStr
	case types.RelayFormatGemini:
		break
	}

	service.IOCopyBytesGracefully(c, resp, responseBody)

	return &usage, nil
}
