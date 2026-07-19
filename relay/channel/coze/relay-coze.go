package coze

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func convertCozeChatRequest(c *gin.Context, request dto.GeneralOpenAIRequest) (*CozeChatRequest, error) {
	var messages []CozeEnterMessage
	// 将 request的messages的role为user的content转换为CozeMessage
	for _, message := range request.Messages {
		if message.Role == "user" {
			messages = append(messages, CozeEnterMessage{
				Role:    "user",
				Content: message.Content,
				// TODO: support more content type
				ContentType: "text",
			})
		}
	}
	user := request.User
	if len(user) == 0 {
		var err error
		user, err = common.Marshal(helper.GetResponseID(c))
		if err != nil {
			return nil, fmt.Errorf("marshal default Coze user id: %w", err)
		}
	}
	cozeRequest := &CozeChatRequest{
		BotId:              c.GetString("bot_id"),
		UserId:             user,
		AdditionalMessages: messages,
		Stream:             request.Stream,
	}
	return cozeRequest, nil
}

func cozeChatHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)
	responseBody, err := common.ReadAllWithLimit(resp.Body)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	// convert coze response to openai response
	var response dto.TextResponse
	var cozeResponse CozeChatDetailResponse
	response.Model = info.UpstreamModelName
	err = common.Unmarshal(responseBody, &cozeResponse)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	if cozeResponse.Code != 0 {
		return nil, service.MarkExplicitUpstreamRejection(types.NewError(errors.New(cozeResponse.Msg), types.ErrorCodeBadResponseBody))
	}
	// 从上下文获取 usage
	var usage dto.Usage
	usage.PromptTokens = c.GetInt("coze_input_count")
	usage.CompletionTokens = c.GetInt("coze_output_count")
	usage.TotalTokens = c.GetInt("coze_token_count")
	response.Usage = usage
	response.Id = helper.GetResponseID(c)

	var responseContent json.RawMessage
	for _, data := range cozeResponse.Data {
		if data.Type == "answer" {
			responseContent = data.Content
			response.Created = data.CreatedAt
		}
	}
	// 添加 response.Choices
	response.Choices = []dto.OpenAITextResponseChoice{
		{
			Index:        0,
			Message:      dto.Message{Role: "assistant", Content: responseContent},
			FinishReason: "stop",
		},
	}
	jsonResponse, err := common.Marshal(response)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(resp.StatusCode)
	_, _ = c.Writer.Write(jsonResponse)

	return &usage, nil
}

func cozeChatStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)
	scanner := helper.NewStreamScanner(resp.Body)
	scanner.Split(bufio.ScanLines)
	helper.SetEventStreamHeaders(c)
	id := helper.GetResponseID(c)
	var responseText string

	var currentEvent string
	var currentData string
	var usage = &dto.Usage{}
	var streamErr *types.NewAPIError
	seenValidResponse := false
	completed := false
	processEvent := func() bool {
		accepted, done, apiErr := handleCozeEvent(c, currentEvent, currentData, &responseText, usage, id, info)
		if apiErr != nil {
			if service.IsExplicitUpstreamRejection(apiErr) && !seenValidResponse {
				streamErr = apiErr
			} else {
				service.MarkUpstreamAccepted(c)
				streamErr = relaychannel.AcceptedResponseDeliveryError()
			}
			return false
		}
		if accepted {
			service.MarkUpstreamAccepted(c)
			seenValidResponse = true
		}
		completed = completed || done
		return true
	}

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			if currentEvent != "" && currentData != "" {
				if !processEvent() {
					break
				}
				currentEvent = ""
				currentData = ""
			}
			continue
		}

		if strings.HasPrefix(line, "event:") {
			currentEvent = strings.TrimSpace(line[6:])
			continue
		}

		if strings.HasPrefix(line, "data:") {
			currentData = strings.TrimSpace(line[5:])
			continue
		}
	}

	// Last event
	if streamErr == nil && currentEvent != "" && currentData != "" {
		processEvent()
	}

	if err := scanner.Err(); err != nil {
		service.MarkUpstreamAccepted(c)
		return nil, relaychannel.AcceptedResponseDeliveryError()
	}
	if usage.TotalTokens == 0 {
		usage = service.ResponseText2Usage(c, responseText, info.UpstreamModelName, c.GetInt("coze_input_count"))
	}
	if streamErr != nil {
		return usage, streamErr
	}
	if !seenValidResponse || !completed {
		service.MarkUpstreamAccepted(c)
		return usage, relaychannel.AcceptedResponseDeliveryError()
	}
	if err := helper.StringData(c, "[DONE]"); err != nil {
		service.MarkUpstreamAccepted(c)
		return usage, relaychannel.AcceptedResponseDeliveryError()
	}
	return usage, nil
}

func handleCozeEvent(c *gin.Context, event string, data string, responseText *string, usage *dto.Usage, id string, info *relaycommon.RelayInfo) (accepted bool, completed bool, apiErr *types.NewAPIError) {
	switch event {
	case "conversation.chat.completed":
		// 将 data 解析为 CozeChatResponseData
		var chatData CozeChatResponseData
		err := common.Unmarshal([]byte(data), &chatData)
		if err != nil {
			common.SysLog("error_unmarshalling_stream_response: " + err.Error())
			return false, false, types.NewError(err, types.ErrorCodeBadResponseBody)
		}

		usage.PromptTokens = chatData.Usage.InputCount
		usage.CompletionTokens = chatData.Usage.OutputCount
		usage.TotalTokens = chatData.Usage.TokenCount

		finishReason := "stop"
		stopResponse := helper.GenerateStopResponse(id, common.GetTimestamp(), info.UpstreamModelName, finishReason)
		if err := helper.ObjectData(c, stopResponse); err != nil {
			return false, false, types.NewError(err, types.ErrorCodeBadResponse)
		}
		return true, true, nil

	case "conversation.message.delta":
		// 将 data 解析为 CozeChatV3MessageDetail
		var messageData CozeChatV3MessageDetail
		err := common.Unmarshal([]byte(data), &messageData)
		if err != nil {
			common.SysLog("error_unmarshalling_stream_response: " + err.Error())
			return false, false, types.NewError(err, types.ErrorCodeBadResponseBody)
		}

		var content string
		err = common.Unmarshal(messageData.Content, &content)
		if err != nil {
			common.SysLog("error_unmarshalling_stream_response: " + err.Error())
			return false, false, types.NewError(err, types.ErrorCodeBadResponseBody)
		}

		*responseText += content

		openaiResponse := dto.ChatCompletionsStreamResponse{
			Id:      id,
			Object:  "chat.completion.chunk",
			Created: common.GetTimestamp(),
			Model:   info.UpstreamModelName,
		}

		choice := dto.ChatCompletionsStreamResponseChoice{
			Index: 0,
		}
		choice.Delta.SetContentString(content)
		openaiResponse.Choices = append(openaiResponse.Choices, choice)

		if err := helper.ObjectData(c, openaiResponse); err != nil {
			return false, false, types.NewError(err, types.ErrorCodeBadResponse)
		}
		return true, false, nil

	case "error":
		var errorData CozeError
		err := common.Unmarshal([]byte(data), &errorData)
		if err != nil {
			common.SysLog("error_unmarshalling_stream_response: " + err.Error())
			return false, false, types.NewError(err, types.ErrorCodeBadResponseBody)
		}

		common.SysLog(fmt.Sprintf("stream event error: code=%v message_%s", errorData.Code, logger.PayloadMetadata([]byte(errorData.Message))))
		return false, false, service.MarkExplicitUpstreamRejection(types.WithOpenAIError(types.OpenAIError{
			Message: errorData.Message,
			Code:    errorData.Code,
		}, http.StatusBadGateway))
	case "done":
		return true, true, nil
	default:
		return true, false, nil
	}
}

func checkIfChatComplete(a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo, ctx context.Context) (bool, error) {
	requestURL := fmt.Sprintf("%s/v3/chat/retrieve", info.ChannelBaseUrl)

	requestURL = requestURL + "?conversation_id=" + c.GetString("coze_conversation_id") + "&chat_id=" + c.GetString("coze_chat_id")
	// 将 conversationId和chatId作为参数发送get请求
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return false, err
	}
	err = a.SetupRequestHeader(c, &req.Header, info)
	if err != nil {
		return false, err
	}

	resp, err := doRequest(ctx, req, info) // 调用 doRequest
	if err != nil {
		return false, err
	}
	if resp == nil { // 确保在 doRequest 失败时 resp 不为 nil 导致 panic
		return false, errors.New("Coze polling response is nil")
	}
	defer service.CloseResponseBodyGracefully(resp)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return false, fmt.Errorf("Coze polling returned HTTP %d", resp.StatusCode)
	}

	// 解析 resp 到 CozeChatResponse
	var cozeResponse CozeChatResponse
	responseBody, err := common.ReadAllWithLimit(resp.Body)
	if err != nil {
		return false, fmt.Errorf("read response body failed: %w", err)
	}
	err = common.Unmarshal(responseBody, &cozeResponse)
	if err != nil {
		return false, fmt.Errorf("unmarshal response body failed: %w", err)
	}
	if cozeResponse.Code != 0 {
		return false, fmt.Errorf("Coze polling failed: code=%d message=%s", cozeResponse.Code, cozeResponse.Msg)
	}
	if cozeResponse.Data.Status == "completed" {
		// 在上下文设置 usage
		c.Set("coze_token_count", cozeResponse.Data.Usage.TokenCount)
		c.Set("coze_output_count", cozeResponse.Data.Usage.OutputCount)
		c.Set("coze_input_count", cozeResponse.Data.Usage.InputCount)
		return true, nil
	} else if cozeResponse.Data.Status == "failed" || cozeResponse.Data.Status == "canceled" || cozeResponse.Data.Status == "requires_action" {
		return false, fmt.Errorf("chat status: %s", cozeResponse.Data.Status)
	} else {
		return false, nil
	}
}

func getChatDetail(a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo, ctx context.Context) (*http.Response, error) {
	requestURL := fmt.Sprintf("%s/v3/chat/message/list", info.ChannelBaseUrl)

	requestURL = requestURL + "?conversation_id=" + c.GetString("coze_conversation_id") + "&chat_id=" + c.GetString("coze_chat_id")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("new request failed: %w", err)
	}
	err = a.SetupRequestHeader(c, &req.Header, info)
	if err != nil {
		return nil, fmt.Errorf("setup request header failed: %w", err)
	}
	resp, err := doRequest(ctx, req, info)
	if err != nil {
		return nil, fmt.Errorf("do request failed: %w", err)
	}
	return resp, nil
}

func doRequest(ctx context.Context, req *http.Request, info *relaycommon.RelayInfo) (*http.Response, error) {
	if ctx == nil {
		return nil, errors.New("missing Coze polling context")
	}
	if req == nil {
		return nil, errors.New("Coze polling request is nil")
	}
	req = req.WithContext(ctx)
	var client *http.Client
	var err error // 声明 err 变量
	if info.ChannelSetting.Proxy != "" {
		client, err = service.NewProxyHttpClient(info.ChannelSetting.Proxy)
		if err != nil {
			return nil, fmt.Errorf("new proxy http client failed: %w", err)
		}
	} else {
		client = service.GetHttpClient()
	}
	if client == nil {
		return nil, errors.New("Coze HTTP client is unavailable")
	}
	clientWithTimeout := *client
	if clientWithTimeout.Timeout <= 0 || clientWithTimeout.Timeout > 30*time.Second {
		clientWithTimeout.Timeout = 30 * time.Second
	}
	resp, err := clientWithTimeout.Do(req)
	if err != nil { // 增加对 client.Do(req) 返回错误的检查
		return nil, fmt.Errorf("client.Do failed: %w", err)
	}
	// _ = resp.Body.Close()
	return resp, nil
}
