package xunfei

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// https://console.xfyun.cn/services/cbm
// https://www.xfyun.cn/doc/spark/Web.html

func requestOpenAI2Xunfei(request dto.GeneralOpenAIRequest, xunfeiAppId string, domain string) *XunfeiChatRequest {
	messages := make([]XunfeiMessage, 0, len(request.Messages))
	shouldCovertSystemMessage := !strings.HasSuffix(request.Model, "3.5")
	for _, message := range request.Messages {
		if message.Role == "system" && shouldCovertSystemMessage {
			messages = append(messages, XunfeiMessage{
				Role:    "user",
				Content: message.StringContent(),
			})
			messages = append(messages, XunfeiMessage{
				Role:    "assistant",
				Content: "Okay",
			})
		} else {
			messages = append(messages, XunfeiMessage{
				Role:    message.Role,
				Content: message.StringContent(),
			})
		}
	}
	xunfeiRequest := XunfeiChatRequest{}
	xunfeiRequest.Header.AppId = xunfeiAppId
	xunfeiRequest.Parameter.Chat.Domain = domain
	xunfeiRequest.Parameter.Chat.Temperature = request.Temperature
	if request.N != nil {
		topK := int(*request.N)
		xunfeiRequest.Parameter.Chat.TopK = &topK
	}
	if request.MaxCompletionTokens != nil || request.MaxTokens != nil {
		maxTokens := request.GetMaxTokens()
		xunfeiRequest.Parameter.Chat.MaxTokens = &maxTokens
	}
	xunfeiRequest.Payload.Message.Text = messages
	return &xunfeiRequest
}

func responseXunfei2OpenAI(response *XunfeiChatResponse) *dto.OpenAITextResponse {
	if len(response.Payload.Choices.Text) == 0 {
		response.Payload.Choices.Text = []XunfeiChatResponseTextItem{
			{
				Content: "",
			},
		}
	}
	choice := dto.OpenAITextResponseChoice{
		Index: 0,
		Message: dto.Message{
			Role:    "assistant",
			Content: response.Payload.Choices.Text[0].Content,
		},
		FinishReason: constant.FinishReasonStop,
	}
	fullTextResponse := dto.OpenAITextResponse{
		Object:  "chat.completion",
		Created: common.GetTimestamp(),
		Choices: []dto.OpenAITextResponseChoice{choice},
		Usage:   response.Payload.Usage.Text,
	}
	return &fullTextResponse
}

func streamResponseXunfei2OpenAI(xunfeiResponse *XunfeiChatResponse) *dto.ChatCompletionsStreamResponse {
	if len(xunfeiResponse.Payload.Choices.Text) == 0 {
		xunfeiResponse.Payload.Choices.Text = []XunfeiChatResponseTextItem{
			{
				Content: "",
			},
		}
	}
	var choice dto.ChatCompletionsStreamResponseChoice
	choice.Delta.SetContentString(xunfeiResponse.Payload.Choices.Text[0].Content)
	if xunfeiResponse.Payload.Choices.Status == 2 {
		choice.FinishReason = &constant.FinishReasonStop
	}
	response := dto.ChatCompletionsStreamResponse{
		Object:  "chat.completion.chunk",
		Created: common.GetTimestamp(),
		Model:   "SparkDesk",
		Choices: []dto.ChatCompletionsStreamResponseChoice{choice},
	}
	return &response
}

func buildXunfeiAuthUrl(hostUrl string, apiKey, apiSecret string) (string, error) {
	HmacWithShaToBase64 := func(algorithm, data, key string) string {
		mac := hmac.New(sha256.New, []byte(key))
		mac.Write([]byte(data))
		encodeData := mac.Sum(nil)
		return base64.StdEncoding.EncodeToString(encodeData)
	}
	ul, err := url.Parse(hostUrl)
	if err != nil || ul.Scheme != "wss" || ul.Host == "" {
		return "", errors.New("invalid Xunfei websocket endpoint")
	}
	date := time.Now().UTC().Format(time.RFC1123)
	signString := []string{"host: " + ul.Host, "date: " + date, "GET " + ul.Path + " HTTP/1.1"}
	sign := strings.Join(signString, "\n")
	sha := HmacWithShaToBase64("hmac-sha256", sign, apiSecret)
	authUrl := fmt.Sprintf("hmac username=\"%s\", algorithm=\"%s\", headers=\"%s\", signature=\"%s\"", apiKey,
		"hmac-sha256", "host date request-line", sha)
	authorization := base64.StdEncoding.EncodeToString([]byte(authUrl))
	v := url.Values{}
	v.Add("host", ul.Host)
	v.Add("date", date)
	v.Add("authorization", authorization)
	callUrl := hostUrl + "?" + v.Encode()
	return callUrl, nil
}

type xunfeiEvent struct {
	response          *XunfeiChatResponse
	err               error
	explicitRejection bool
}

func xunfeiRequestContext(c *gin.Context) (context.Context, context.CancelFunc, error) {
	if c == nil || c.Request == nil {
		return nil, nil, errors.New("Xunfei downstream context is unavailable")
	}
	seconds := common.GetEnvOrDefault("XUNFEI_UPSTREAM_TIMEOUT_SECONDS", 600)
	if seconds < 5 {
		seconds = 5
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), time.Duration(seconds)*time.Second)
	return ctx, cancel, nil
}

func resolveXunfeiEvent(c *gin.Context, event xunfeiEvent, receivedValid bool) (*XunfeiChatResponse, *types.NewAPIError) {
	if event.err != nil {
		if event.explicitRejection && !receivedValid {
			apiErr := types.NewErrorWithStatusCode(
				errors.New("Xunfei upstream rejected the request"),
				types.ErrorCodeBadResponse,
				http.StatusBadGateway,
			)
			return nil, service.MarkExplicitUpstreamRejection(apiErr)
		}
		service.MarkUpstreamAccepted(c)
		return nil, relaychannel.AcceptedResponseDeliveryError()
	}
	if event.response == nil {
		service.MarkUpstreamAccepted(c)
		return nil, relaychannel.AcceptedResponseDeliveryError()
	}
	service.MarkUpstreamAccepted(c)
	return event.response, nil
}

func xunfeiStartError(c *gin.Context, sent bool, err error) *types.NewAPIError {
	if sent {
		service.MarkUpstreamAccepted(c)
		return relaychannel.AcceptedResponseDeliveryError()
	}
	return types.NewError(err, types.ErrorCodeDoRequestFailed)
}

func startXunfeiRequest(c *gin.Context, textRequest dto.GeneralOpenAIRequest, appID, apiSecret, apiKey string) (context.Context, context.CancelFunc, <-chan xunfeiEvent, *types.NewAPIError) {
	requestCtx, cancel, err := xunfeiRequestContext(c)
	if err != nil {
		return nil, nil, nil, types.NewError(err, types.ErrorCodeDoRequestFailed)
	}
	domain, authURL, err := getXunfeiAuthUrl(c, apiKey, apiSecret, textRequest.Model)
	if err != nil {
		cancel()
		return nil, nil, nil, types.NewError(err, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	events, sent, err := xunfeiMakeRequest(requestCtx, textRequest, domain, authURL, appID)
	if err == nil {
		return requestCtx, cancel, events, nil
	}
	cancel()
	return nil, nil, nil, xunfeiStartError(c, sent, err)
}

func xunfeiStreamHandler(c *gin.Context, textRequest dto.GeneralOpenAIRequest, appID, apiSecret, apiKey string) (*dto.Usage, *types.NewAPIError) {
	requestCtx, cancel, events, apiErr := startXunfeiRequest(c, textRequest, appID, apiSecret, apiKey)
	if apiErr != nil {
		return nil, apiErr
	}
	defer cancel()
	var usage dto.Usage
	receivedValid := false
	for {
		select {
		case event, ok := <-events:
			if !ok {
				if receivedValid {
					service.MarkUpstreamAccepted(c)
				}
				return nil, relaychannel.AcceptedResponseDeliveryError()
			}
			xunfeiResponse, eventErr := resolveXunfeiEvent(c, event, receivedValid)
			if eventErr != nil {
				return nil, eventErr
			}
			receivedValid = true
			usage.PromptTokens += xunfeiResponse.Payload.Usage.Text.PromptTokens
			usage.CompletionTokens += xunfeiResponse.Payload.Usage.Text.CompletionTokens
			usage.TotalTokens += xunfeiResponse.Payload.Usage.Text.TotalTokens
			helper.SetEventStreamHeaders(c)
			if err := helper.ObjectData(c, streamResponseXunfei2OpenAI(xunfeiResponse)); err != nil {
				return nil, relaychannel.AcceptedResponseDeliveryError()
			}
			if xunfeiResponse.Payload.Choices.Status == 2 {
				if err := helper.StringData(c, "[DONE]"); err != nil {
					return nil, relaychannel.AcceptedResponseDeliveryError()
				}
				return &usage, nil
			}
		case <-requestCtx.Done():
			service.MarkUpstreamAccepted(c)
			return nil, relaychannel.AcceptedResponseDeliveryError()
		}
	}
}

func xunfeiHandler(c *gin.Context, textRequest dto.GeneralOpenAIRequest, appID, apiSecret, apiKey string) (*dto.Usage, *types.NewAPIError) {
	requestCtx, cancel, events, apiErr := startXunfeiRequest(c, textRequest, appID, apiSecret, apiKey)
	if apiErr != nil {
		return nil, apiErr
	}
	defer cancel()
	var usage dto.Usage
	var content string
	var lastResponse *XunfeiChatResponse
	receivedValid := false
	for {
		select {
		case event, ok := <-events:
			if !ok {
				service.MarkUpstreamAccepted(c)
				return nil, relaychannel.AcceptedResponseDeliveryError()
			}
			xunfeiResponse, eventErr := resolveXunfeiEvent(c, event, receivedValid)
			if eventErr != nil {
				return nil, eventErr
			}
			receivedValid = true
			lastResponse = xunfeiResponse
			if len(xunfeiResponse.Payload.Choices.Text) > 0 {
				content += xunfeiResponse.Payload.Choices.Text[0].Content
			}
			usage.PromptTokens += xunfeiResponse.Payload.Usage.Text.PromptTokens
			usage.CompletionTokens += xunfeiResponse.Payload.Usage.Text.CompletionTokens
			usage.TotalTokens += xunfeiResponse.Payload.Usage.Text.TotalTokens
			if xunfeiResponse.Payload.Choices.Status != 2 {
				continue
			}
			if len(lastResponse.Payload.Choices.Text) == 0 {
				lastResponse.Payload.Choices.Text = []XunfeiChatResponseTextItem{{}}
			}
			lastResponse.Payload.Choices.Text[0].Content = content
			jsonResponse, err := common.Marshal(responseXunfei2OpenAI(lastResponse))
			if err != nil {
				return nil, relaychannel.AcceptedResponseDeliveryError()
			}
			c.Writer.Header().Set("Content-Type", "application/json")
			if _, err := c.Writer.Write(jsonResponse); err != nil {
				if buffered, ok := c.Writer.(*common.BufferedResponseWriter); ok {
					_ = buffered.Fail(err)
				}
				return nil, relaychannel.AcceptedResponseDeliveryError()
			}
			return &usage, nil
		case <-requestCtx.Done():
			service.MarkUpstreamAccepted(c)
			return nil, relaychannel.AcceptedResponseDeliveryError()
		}
	}
}

func xunfeiMakeRequest(ctx context.Context, textRequest dto.GeneralOpenAIRequest, domain, authURL, appID string) (<-chan xunfeiEvent, bool, error) {
	if ctx == nil || strings.TrimSpace(domain) == "" || strings.TrimSpace(authURL) == "" || strings.TrimSpace(appID) == "" || len(textRequest.Messages) == 0 {
		return nil, false, errors.New("invalid Xunfei request")
	}
	parsedURL, err := url.Parse(authURL)
	if err != nil || (parsedURL.Scheme != "wss" && parsedURL.Scheme != "ws") || parsedURL.Host == "" {
		return nil, false, errors.New("invalid Xunfei websocket endpoint")
	}
	requestData, err := common.Marshal(requestOpenAI2Xunfei(textRequest, appID, domain))
	if err != nil {
		return nil, false, errors.New("Xunfei request encoding failed")
	}
	d := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
	}
	conn, resp, err := d.DialContext(ctx, authURL, nil)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		common.SysLog(fmt.Sprintf("Xunfei websocket dial failed url_%s error_type=%T", common.PayloadMetadata([]byte(authURL)), err))
		return nil, false, errors.New("Xunfei websocket dial failed")
	}
	if resp == nil || resp.StatusCode != http.StatusSwitchingProtocols {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		_ = conn.Close()
		return nil, false, errors.New("Xunfei websocket handshake failed")
	}
	err = conn.WriteMessage(websocket.TextMessage, requestData)
	if err != nil {
		_ = conn.Close()
		return nil, true, errors.New("Xunfei websocket write failed")
	}

	events := make(chan xunfeiEvent, 1)
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	go func() {
		defer close(events)
		defer close(done)
		defer conn.Close()
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				if ctx.Err() == nil {
					common.SysLog(fmt.Sprintf("Xunfei websocket read failed: error_type=%T", err))
				}
				select {
				case events <- xunfeiEvent{err: errors.New("Xunfei websocket response read failed")}:
				case <-ctx.Done():
				}
				return
			}
			var response XunfeiChatResponse
			err = common.Unmarshal(msg, &response)
			if err != nil {
				common.SysLog(fmt.Sprintf("Xunfei websocket response decode failed: error_type=%T", err))
				select {
				case events <- xunfeiEvent{err: errors.New("Xunfei websocket response is invalid")}:
				case <-ctx.Done():
				}
				return
			}
			if response.Header.Code != 0 {
				common.SysLog(fmt.Sprintf("Xunfei upstream rejection code=%d message_%s", response.Header.Code, common.PayloadMetadata([]byte(response.Header.Message))))
				select {
				case events <- xunfeiEvent{err: errors.New("Xunfei upstream rejected the request"), explicitRejection: true}:
				case <-ctx.Done():
				}
				return
			}
			select {
			case events <- xunfeiEvent{response: &response}:
			case <-ctx.Done():
				return
			}
			if response.Payload.Choices.Status == 2 {
				return
			}
		}
	}()

	return events, true, nil
}

func apiVersion2domain(apiVersion string) string {
	switch apiVersion {
	case "v1.1":
		return "lite"
	case "v2.1":
		return "generalv2"
	case "v3.1":
		return "generalv3"
	case "v3.5":
		return "generalv3.5"
	case "v4.0":
		return "4.0Ultra"
	}
	return "general" + apiVersion
}

func getXunfeiAuthUrl(c *gin.Context, apiKey string, apiSecret string, modelName string) (string, string, error) {
	if c == nil || c.Request == nil || strings.TrimSpace(apiKey) == "" || strings.TrimSpace(apiSecret) == "" {
		return "", "", errors.New("invalid Xunfei authentication configuration")
	}
	apiVersion := getAPIVersion(c, modelName)
	switch apiVersion {
	case "v1.1", "v2.1", "v3.1", "v3.5", "v4.0":
	default:
		return "", "", errors.New("invalid Xunfei API version")
	}
	domain := apiVersion2domain(apiVersion)
	authURL, err := buildXunfeiAuthUrl(fmt.Sprintf("wss://spark-api.xf-yun.com/%s/chat", apiVersion), apiKey, apiSecret)
	if err != nil {
		return "", "", err
	}
	return domain, authURL, nil
}

func getAPIVersion(c *gin.Context, modelName string) string {
	query := c.Request.URL.Query()
	apiVersion := query.Get("api-version")
	if apiVersion != "" {
		return apiVersion
	}
	parts := strings.Split(modelName, "-")
	if len(parts) == 2 {
		apiVersion = parts[1]
		return apiVersion

	}
	apiVersion = c.GetString("api_version")
	if apiVersion != "" {
		return apiVersion
	}
	apiVersion = "v1.1"
	common.SysLog("api_version not found, using default: " + apiVersion)
	return apiVersion
}
