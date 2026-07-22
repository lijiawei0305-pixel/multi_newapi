package zhipu

import (
	"bufio"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// https://open.bigmodel.cn/doc/api#chatglm_std
// chatglm_std, chatglm_lite
// https://open.bigmodel.cn/api/paas/v3/model-api/chatglm_std/invoke
// https://open.bigmodel.cn/api/paas/v3/model-api/chatglm_std/sse-invoke

var zhipuTokens sync.Map
var expSeconds int64 = 24 * 3600

func getZhipuToken(apikey string) string {
	data, ok := zhipuTokens.Load(apikey)
	if ok {
		tokenData := data.(zhipuTokenData)
		if time.Now().Before(tokenData.ExpiryTime) {
			return tokenData.Token
		}
	}

	split := strings.Split(apikey, ".")
	if len(split) != 2 {
		common.SysLog("invalid zhipu key format")
		return ""
	}

	id := split[0]
	secret := split[1]

	expMillis := time.Now().Add(time.Duration(expSeconds)*time.Second).UnixNano() / 1e6
	expiryTime := time.Now().Add(time.Duration(expSeconds) * time.Second)

	timestamp := time.Now().UnixNano() / 1e6

	payload := jwt.MapClaims{
		"api_key":   id,
		"exp":       expMillis,
		"timestamp": timestamp,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, payload)

	token.Header["alg"] = "HS256"
	token.Header["sign_type"] = "SIGN"

	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		return ""
	}

	zhipuTokens.Store(apikey, zhipuTokenData{
		Token:      tokenString,
		ExpiryTime: expiryTime,
	})

	return tokenString
}

func requestOpenAI2Zhipu(request dto.GeneralOpenAIRequest) *ZhipuRequest {
	messages := make([]ZhipuMessage, 0, len(request.Messages))
	for _, message := range request.Messages {
		if message.Role == "system" {
			messages = append(messages, ZhipuMessage{
				Role:    "system",
				Content: message.StringContent(),
			})
			messages = append(messages, ZhipuMessage{
				Role:    "user",
				Content: "Okay",
			})
		} else {
			messages = append(messages, ZhipuMessage{
				Role:    message.Role,
				Content: message.StringContent(),
			})
		}
	}
	return &ZhipuRequest{
		Prompt:      messages,
		Temperature: request.Temperature,
		TopP:        request.TopP,
	}
}

func responseZhipu2OpenAI(response *ZhipuResponse) *dto.OpenAITextResponse {
	fullTextResponse := dto.OpenAITextResponse{
		Id:      response.Data.TaskId,
		Object:  "chat.completion",
		Created: common.GetTimestamp(),
		Choices: make([]dto.OpenAITextResponseChoice, 0, len(response.Data.Choices)),
		Usage:   response.Data.Usage,
	}
	for i, choice := range response.Data.Choices {
		openaiChoice := dto.OpenAITextResponseChoice{
			Index: i,
			Message: dto.Message{
				Role:    choice.Role,
				Content: strings.Trim(choice.Content, "\""),
			},
			FinishReason: "",
		}
		if i == len(response.Data.Choices)-1 {
			openaiChoice.FinishReason = "stop"
		}
		fullTextResponse.Choices = append(fullTextResponse.Choices, openaiChoice)
	}
	return &fullTextResponse
}

func streamResponseZhipu2OpenAI(zhipuResponse string) *dto.ChatCompletionsStreamResponse {
	var choice dto.ChatCompletionsStreamResponseChoice
	choice.Delta.SetContentString(zhipuResponse)
	response := dto.ChatCompletionsStreamResponse{
		Object:  "chat.completion.chunk",
		Created: common.GetTimestamp(),
		Model:   "chatglm",
		Choices: []dto.ChatCompletionsStreamResponseChoice{choice},
	}
	return &response
}

func streamMetaResponseZhipu2OpenAI(zhipuResponse *ZhipuStreamMetaResponse) (*dto.ChatCompletionsStreamResponse, *dto.Usage) {
	var choice dto.ChatCompletionsStreamResponseChoice
	choice.Delta.SetContentString("")
	choice.FinishReason = &constant.FinishReasonStop
	response := dto.ChatCompletionsStreamResponse{
		Id:      zhipuResponse.RequestId,
		Object:  "chat.completion.chunk",
		Created: common.GetTimestamp(),
		Model:   "chatglm",
		Choices: []dto.ChatCompletionsStreamResponseChoice{choice},
	}
	return &response, &zhipuResponse.Usage
}

func zhipuStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)
	var usage *dto.Usage
	var responseText strings.Builder
	scanner := helper.NewStreamScanner(resp.Body)
	scanner.Split(bufio.ScanLines)
	helper.SetEventStreamHeaders(c)
	seenValidResponse := false
	completed := false
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < 5 {
			continue
		}
		switch line[:5] {
		case "data:":
			data := line[5:]
			service.MarkUpstreamAccepted(c)
			seenValidResponse = true
			responseText.WriteString(data)
			response := streamResponseZhipu2OpenAI(data)
			if err := helper.ObjectData(c, response); err != nil {
				return usage, relaychannel.AcceptedResponseDeliveryError()
			}
		case "meta:":
			data := line[5:]
			var zhipuResponse ZhipuStreamMetaResponse
			if err := common.UnmarshalJsonStr(data, &zhipuResponse); err != nil {
				common.SysLog("error unmarshalling stream response: " + err.Error())
				service.MarkUpstreamAccepted(c)
				return usage, relaychannel.AcceptedResponseDeliveryError()
			}
			status := strings.ToLower(strings.TrimSpace(zhipuResponse.TaskStatus))
			if status == "failed" || status == "error" {
				if seenValidResponse {
					service.MarkUpstreamAccepted(c)
					return usage, relaychannel.AcceptedResponseDeliveryError()
				}
				return usage, service.MarkExplicitUpstreamRejection(types.NewOpenAIError(
					errors.New("Zhipu rejected the streaming request"),
					types.ErrorCodeBadResponse,
					http.StatusBadGateway,
				))
			}
			service.MarkUpstreamAccepted(c)
			seenValidResponse = true
			completed = true
			response, zhipuUsage := streamMetaResponseZhipu2OpenAI(&zhipuResponse)
			usage = zhipuUsage
			if err := helper.ObjectData(c, response); err != nil {
				return usage, relaychannel.AcceptedResponseDeliveryError()
			}
		}
	}
	if err := scanner.Err(); err != nil {
		common.SysLog("error reading stream: " + err.Error())
		service.MarkUpstreamAccepted(c)
		return usage, relaychannel.AcceptedResponseDeliveryError()
	}
	if usage == nil {
		usage = service.ResponseText2Usage(c, responseText.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
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

func zhipuHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	var zhipuResponse ZhipuResponse
	defer service.CloseResponseBodyGracefully(resp)
	responseBody, err := common.ReadAllWithLimit(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	err = common.Unmarshal(responseBody, &zhipuResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if zhipuResponse.Success == nil {
		return nil, types.NewOpenAIError(errors.New("Zhipu response omitted success state"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	if !*zhipuResponse.Success {
		return nil, service.MarkExplicitUpstreamRejection(types.WithOpenAIError(types.OpenAIError{
			Message: zhipuResponse.Msg,
			Code:    zhipuResponse.Code,
		}, http.StatusBadGateway))
	}
	fullTextResponse := responseZhipu2OpenAI(&zhipuResponse)
	jsonResponse, err := common.Marshal(fullTextResponse)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(resp.StatusCode)
	if _, err = c.Writer.Write(jsonResponse); err != nil {
		return &fullTextResponse.Usage, relaychannel.AcceptedResponseDeliveryError()
	}
	return &fullTextResponse.Usage, nil
}
